package binding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// ArtifactSource opens immutable server content for a fast-forward.
type ArtifactSource interface {
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}

type localLayout struct {
	roots       map[string]string
	currentPath map[string]string
}

type stagedFile struct {
	target string
	stage  string
}

type plannedFile struct {
	revision omnisave.RevisionFile
	target   string
}

type backupFile struct {
	original string
	backup   string
}

// FastForward replaces a local save that still equals matched with the head
// snapshot. Every artifact is downloaded and verified before local files move;
// failures during the move restore the original snapshot.
func FastForward(ctx context.Context, source ArtifactSource, save target.Save, matched, head omnisave.Revision) error {
	if matched.ID == "" || head.ID == "" {
		return fmt.Errorf("fast-forward revisions must not be empty")
	}
	stillMatches, err := FindContentMatches(save, []Lineage{{
		Omnisave:  omnisave.Omnisave{ID: matched.OmnisaveID},
		Revisions: []omnisave.Revision{matched},
	}})
	if err != nil {
		return err
	}
	if len(stillMatches) != 1 {
		return fmt.Errorf("local save changed before fast-forward")
	}

	layout, err := describeLocalLayout(save)
	if err != nil {
		return err
	}
	temporaryRoots := make(map[string]string)
	defer func() {
		for _, temporary := range temporaryRoots {
			_ = os.RemoveAll(temporary)
		}
	}()
	ensureTemporary := func(root string) (string, error) {
		if temporary := temporaryRoots[root]; temporary != "" {
			return temporary, nil
		}
		temporary, err := os.MkdirTemp(root, ".omnisave-fast-forward-")
		if err != nil {
			return "", fmt.Errorf("prepare local save update: %w", err)
		}
		temporaryRoots[root] = temporary
		return temporary, nil
	}

	for _, root := range layout.roots {
		if _, err := ensureTemporary(root); err != nil {
			return err
		}
	}
	staged := make([]stagedFile, 0, len(head.Files))
	headTargets := make(map[string]bool, len(head.Files))
	for index, file := range head.Files {
		targetPath, root, err := layout.pathFor(file.Path)
		if err != nil {
			return err
		}
		if headTargets[targetPath] {
			return fmt.Errorf("head revision has duplicate local paths")
		}
		headTargets[targetPath] = true
		if _, current := layout.currentPath[targetPath]; !current {
			if _, err := os.Lstat(targetPath); err == nil {
				return fmt.Errorf("fast-forward would overwrite an untracked local file")
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("inspect local save path: %w", err)
			}
		}
		temporary, err := ensureTemporary(root)
		if err != nil {
			return err
		}
		stagePath := filepath.Join(temporary, fmt.Sprintf("download-%d", index))
		if err := downloadArtifact(ctx, source, file.Artifact, stagePath); err != nil {
			return err
		}
		staged = append(staged, stagedFile{target: targetPath, stage: stagePath})
	}

	backups := make([]backupFile, 0, len(layout.currentPath))
	for original, root := range layout.currentPath {
		temporary := temporaryRoots[root]
		backup := filepath.Join(temporary, fmt.Sprintf("backup-%d", len(backups)))
		if err := os.Rename(original, backup); err != nil {
			return joinRollback(fmt.Errorf("back up local save: %w", err), restoreBackups(backups))
		}
		backups = append(backups, backupFile{original: original, backup: backup})
	}

	var applied []string
	for _, file := range staged {
		if err := os.MkdirAll(filepath.Dir(file.target), 0o700); err != nil {
			return joinRollback(fmt.Errorf("prepare local save directory: %w", err), rollbackApplied(applied, backups))
		}
		if err := os.Rename(file.stage, file.target); err != nil {
			return joinRollback(fmt.Errorf("replace local save file: %w", err), rollbackApplied(applied, backups))
		}
		applied = append(applied, file.target)
	}
	return nil
}

// Materialize places a complete verified head into an empty native destination.
// Target files are checked before and after download, and linked into place
// without replacement so content appearing concurrently is never overwritten.
func Materialize(ctx context.Context, source ArtifactSource, destination target.SaveDestination, head omnisave.Revision) (target.Save, error) {
	planned, err := materializationPlan(destination, head)
	if err != nil {
		return target.Save{}, err
	}
	for _, file := range planned {
		if err := requireAbsent(file.target); err != nil {
			return target.Save{}, err
		}
	}

	temporaryRoots := make(map[string]string)
	defer func() {
		for _, temporary := range temporaryRoots {
			_ = os.RemoveAll(temporary)
		}
	}()
	staged := make([]stagedFile, 0, len(planned))
	for index, file := range planned {
		anchor, err := existingDirectory(filepath.Dir(file.target))
		if err != nil {
			return target.Save{}, err
		}
		temporary := temporaryRoots[anchor]
		if temporary == "" {
			temporary, err = os.MkdirTemp(anchor, ".omnisave-materialize-")
			if err != nil {
				return target.Save{}, fmt.Errorf("prepare local save placement: %w", err)
			}
			temporaryRoots[anchor] = temporary
		}
		stagePath := filepath.Join(temporary, fmt.Sprintf("download-%d", index))
		if err := downloadArtifact(ctx, source, file.revision.Artifact, stagePath); err != nil {
			return target.Save{}, err
		}
		staged = append(staged, stagedFile{target: file.target, stage: stagePath})
	}
	if err := ctx.Err(); err != nil {
		return target.Save{}, err
	}
	for _, file := range planned {
		if err := requireAbsent(file.target); err != nil {
			return target.Save{}, err
		}
	}

	var createdDirectories []string
	var applied []string
	for _, file := range staged {
		created, err := createParentDirectories(filepath.Dir(file.target))
		createdDirectories = append(createdDirectories, created...)
		if err != nil {
			return target.Save{}, joinRollback(
				fmt.Errorf("prepare local save directory: %w", err),
				rollbackMaterialization(applied, createdDirectories),
			)
		}
		// Linking is an atomic no-replace operation. Staging lives on the same
		// filesystem as the destination, so an existing target always wins.
		if err := os.Link(file.stage, file.target); err != nil {
			operation := fmt.Errorf("place local save file: %w", err)
			if os.IsExist(err) {
				operation = fmt.Errorf("materialize would overwrite an existing local file")
			}
			return target.Save{}, joinRollback(operation, rollbackMaterialization(applied, createdDirectories))
		}
		applied = append(applied, file.target)
	}

	files := make([]target.File, 0, len(planned))
	for _, file := range planned {
		info, err := os.Stat(file.target)
		if err != nil {
			return target.Save{}, joinRollback(
				fmt.Errorf("inspect materialized save: %w", err),
				rollbackMaterialization(applied, createdDirectories),
			)
		}
		locationID, relative, _ := strings.Cut(file.revision.Path, "/")
		files = append(files, target.File{
			Path:         file.target,
			LocationID:   locationID,
			RelativePath: filepath.FromSlash(relative),
			Size:         info.Size(),
			Modified:     info.ModTime(),
		})
	}
	return target.Save{
		ID: destination.ID, TargetID: destination.TargetID, GameID: destination.GameID,
		Kind: destination.Kind, Files: files, Metadata: destination.Metadata,
	}, nil
}

// CanMaterialize reports whether one head maps into one native destination.
// It validates layout only and does not inspect or change the filesystem.
func CanMaterialize(destination target.SaveDestination, head omnisave.Revision) error {
	_, err := materializationPlan(destination, head)
	return err
}

func materializationPlan(destination target.SaveDestination, head omnisave.Revision) ([]plannedFile, error) {
	if destination.ID == "" || destination.TargetID == "" || destination.GameID == "" || head.ID == "" || len(head.Files) == 0 {
		return nil, fmt.Errorf("materialize needs a save destination and non-empty head")
	}
	return planMaterialization(destination, head)
}

func planMaterialization(destination target.SaveDestination, head omnisave.Revision) ([]plannedFile, error) {
	locations := make(map[string]target.SaveLocation, len(destination.Locations))
	for _, location := range destination.Locations {
		if location.ID == "" || location.Path == "" || !filepath.IsAbs(location.Path) {
			return nil, fmt.Errorf("save destination has an invalid location")
		}
		if _, duplicate := locations[location.ID]; duplicate {
			return nil, fmt.Errorf("save destination has duplicate locations")
		}
		locations[location.ID] = location
	}
	counts := make(map[string]int)
	for _, file := range head.Files {
		locationID, _, err := splitCanonicalPath(file.Path)
		if err != nil {
			return nil, err
		}
		counts[locationID]++
	}

	planned := make([]plannedFile, 0, len(head.Files))
	targets := make(map[string]bool, len(head.Files))
	for _, file := range head.Files {
		locationID, relative, err := splitCanonicalPath(file.Path)
		if err != nil {
			return nil, err
		}
		location, exists := locations[locationID]
		if !exists {
			return nil, fmt.Errorf("head revision uses an unknown save location")
		}
		targetPath, err := materializedPath(location, relative, counts[locationID])
		if err != nil {
			return nil, err
		}
		if targets[targetPath] {
			return nil, fmt.Errorf("head revision has duplicate local paths")
		}
		targets[targetPath] = true
		planned = append(planned, plannedFile{revision: file, target: targetPath})
	}
	return planned, nil
}

func splitCanonicalPath(canonical string) (string, string, error) {
	locationID, relative, found := strings.Cut(canonical, "/")
	if !found || locationID == "" || relative == "" || path.Clean(relative) != relative || strings.HasPrefix(relative, "/") {
		return "", "", fmt.Errorf("head revision has an invalid canonical path")
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", "", fmt.Errorf("head revision has an invalid canonical path")
		}
	}
	return locationID, relative, nil
}

func materializedPath(location target.SaveLocation, relative string, count int) (string, error) {
	base := filepath.Clean(location.Path)
	nativeRelative := filepath.FromSlash(relative)
	kind := location.Kind
	if kind == target.SaveLocationUnknown {
		if count == 1 && nativeRelative == filepath.Base(base) {
			kind = target.SaveLocationFile
		} else {
			kind = target.SaveLocationDirectory
		}
	}
	if kind == target.SaveLocationFile {
		if count != 1 || nativeRelative != filepath.Base(base) {
			return "", fmt.Errorf("head revision does not fit its file save location")
		}
		return base, nil
	}
	if kind != target.SaveLocationDirectory {
		return "", fmt.Errorf("save destination has an invalid location kind")
	}
	targetPath := filepath.Clean(filepath.Join(base, nativeRelative))
	contained, err := filepath.Rel(base, targetPath)
	if err != nil || relativeEscapesRoot(contained) {
		return "", fmt.Errorf("head revision path escapes its save location")
	}
	return targetPath, nil
}

func requireAbsent(targetPath string) error {
	if _, err := os.Lstat(targetPath); err == nil {
		return fmt.Errorf("materialize would overwrite an existing local file")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect local save path: %w", err)
	}
	return nil
}

func existingDirectory(candidate string) (string, error) {
	for {
		info, err := os.Stat(candidate)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("local save parent is not a directory")
			}
			return candidate, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect local save parent: %w", err)
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", fmt.Errorf("local save has no existing parent directory")
		}
		candidate = parent
	}
}

func createParentDirectories(directory string) ([]string, error) {
	var missing []string
	for current := directory; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("local save parent is not a directory")
			}
			break
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return nil, fmt.Errorf("local save has no existing parent directory")
		}
	}
	created := make([]string, 0, len(missing))
	for index := len(missing) - 1; index >= 0; index-- {
		directory := missing[index]
		if err := os.Mkdir(directory, 0o700); err != nil {
			if os.IsExist(err) {
				info, statErr := os.Stat(directory)
				if statErr == nil && info.IsDir() {
					continue
				}
			}
			return created, err
		}
		created = append(created, directory)
	}
	return created, nil
}

func rollbackMaterialization(applied, createdDirectories []string) error {
	var rollbackErrors []error
	for index := len(applied) - 1; index >= 0; index-- {
		if err := os.Remove(applied[index]); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	for index := len(createdDirectories) - 1; index >= 0; index-- {
		if err := os.Remove(createdDirectories[index]); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	return errors.Join(rollbackErrors...)
}

func describeLocalLayout(save target.Save) (localLayout, error) {
	layout := localLayout{
		roots:       make(map[string]string),
		currentPath: make(map[string]string),
	}
	for _, file := range save.Files {
		relative := filepath.Clean(filepath.FromSlash(file.RelativePath))
		if file.RelativePath == "" {
			relative = filepath.Base(file.Path)
		}
		if relative == "." || filepath.IsAbs(relative) || relativeEscapesRoot(relative) {
			return localLayout{}, fmt.Errorf("local save has an invalid relative path")
		}
		root := filepath.Clean(file.Path)
		for _, segment := range strings.Split(relative, string(filepath.Separator)) {
			if segment != "" {
				root = filepath.Dir(root)
			}
		}
		if filepath.Clean(filepath.Join(root, relative)) != filepath.Clean(file.Path) {
			return localLayout{}, fmt.Errorf("local save path is outside its location")
		}
		if known := layout.roots[file.LocationID]; known != "" && known != root {
			return localLayout{}, fmt.Errorf("local save location has inconsistent roots")
		}
		layout.roots[file.LocationID] = root
		fullPath := filepath.Clean(file.Path)
		if _, duplicate := layout.currentPath[fullPath]; duplicate {
			return localLayout{}, fmt.Errorf("local save has duplicate native paths")
		}
		layout.currentPath[fullPath] = root
	}
	return layout, nil
}

func (l localLayout) pathFor(canonical string) (string, string, error) {
	locationID, relative, err := splitCanonicalPath(canonical)
	if err != nil {
		return "", "", err
	}
	root := l.roots[locationID]
	if root == "" {
		return "", "", fmt.Errorf("head revision uses an unknown save location")
	}
	targetPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	contained, err := filepath.Rel(root, targetPath)
	if err != nil || relativeEscapesRoot(contained) {
		return "", "", fmt.Errorf("head revision path escapes its save location")
	}
	return targetPath, root, nil
}

func relativeEscapesRoot(relative string) bool {
	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func downloadArtifact(ctx context.Context, source ArtifactSource, artifact omnisave.Artifact, destination string) error {
	payload, err := source.OpenArtifact(ctx, artifact.SHA256)
	if err != nil {
		return fmt.Errorf("download head artifact: %w", err)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		payload.Close()
		return fmt.Errorf("stage head artifact: %w", err)
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), payload)
	closeFileErr := file.Close()
	closePayloadErr := payload.Close()
	if copyErr != nil {
		return fmt.Errorf("download head artifact: %w", copyErr)
	}
	if closeFileErr != nil {
		return fmt.Errorf("stage head artifact: %w", closeFileErr)
	}
	if closePayloadErr != nil {
		return fmt.Errorf("close head artifact: %w", closePayloadErr)
	}
	actualHash := hex.EncodeToString(digest.Sum(nil))
	if size != artifact.Size || !strings.EqualFold(actualHash, artifact.SHA256) {
		return fmt.Errorf("downloaded head artifact failed verification")
	}
	return nil
}

func rollbackApplied(applied []string, backups []backupFile) error {
	var rollbackErrors []error
	for index := len(applied) - 1; index >= 0; index-- {
		if err := os.Remove(applied[index]); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	if err := restoreBackups(backups); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	return errors.Join(rollbackErrors...)
}

func restoreBackups(backups []backupFile) error {
	var rollbackErrors []error
	for index := len(backups) - 1; index >= 0; index-- {
		backup := backups[index]
		if err := os.MkdirAll(filepath.Dir(backup.original), 0o700); err != nil {
			rollbackErrors = append(rollbackErrors, err)
			continue
		}
		if err := os.Rename(backup.backup, backup.original); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	return errors.Join(rollbackErrors...)
}

func joinRollback(operation, rollback error) error {
	if rollback == nil {
		return operation
	}
	return errors.Join(operation, fmt.Errorf("restore original local save: %w", rollback))
}
