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
	"sort"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client/activity"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// ArtifactSource opens immutable server content for applying a revision locally.
type ArtifactSource interface {
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}

type localLayout struct {
	roots       map[string]string
	currentPath map[string]string
	// aliases are the identities this save's location is also known under —
	// the other OSes' spellings of the same logical place. A canonical path
	// naming one of them lands in the save's single root; the mapping is
	// refused when the save has several roots, since nothing then says which
	// one the alias means.
	aliases map[string]bool
	// singleFlatFile is, for a location holding exactly one file directly at
	// its root, that file's relative path. Such a location is shaped like a
	// single save file, and an aliased spelling may only address that file by
	// its native name — placing a foreign name beside it would swap the save
	// for a file the game never reads.
	singleFlatFile map[string]string
}

type stagedFile struct {
	target string
	stage  string
	// fresh marks a target path the local save did not carry when the
	// apply checked it, so placing files must refuse anything that
	// appeared there since.
	fresh bool
}

type plannedFile struct {
	revision omnisave.RevisionFile
	target   string
}

type backupFile struct {
	original string
	backup   string
}

// ApplyCurrent replaces a local save that still equals matched with the current
// snapshot. Every artifact is staged and verified before local files move —
// from the local save where it already holds that exact content, and from the
// server otherwise; failures during the move restore the original snapshot.
func ApplyCurrent(ctx context.Context, source ArtifactSource, save target.Save, matched, current omnisave.Revision) error {
	if matched.ID == "" || current.ID == "" {
		return fmt.Errorf("apply needs a matched and a current revision")
	}
	stillMatches, err := FindContentMatches(save, []Lineage{{
		Omnisave:  omnisave.Omnisave{ID: matched.OmnisaveID},
		Revisions: []omnisave.Revision{matched},
	}})
	if err != nil {
		return err
	}
	if len(stillMatches) != 1 {
		return fmt.Errorf("local save changed before applying the current revision")
	}

	layout, err := describeLocalLayout(save)
	if err != nil {
		return err
	}
	temporaryRoots := make(map[string]string)
	// A failed rollback means the backups in the staging directories are the
	// only copy of the user's original save; preserving them turns a cleanup
	// into data loss, so cleanup skips them and the error names where they are.
	preserveTemporary := false
	defer func() {
		if preserveTemporary {
			return
		}
		for _, temporary := range temporaryRoots {
			_ = os.RemoveAll(temporary)
		}
	}()
	ensureTemporary := func(root string) (string, error) {
		if temporary := temporaryRoots[root]; temporary != "" {
			return temporary, nil
		}
		temporary, err := os.MkdirTemp(root, ".omnisave-apply-")
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
	staged := make([]stagedFile, 0, len(current.Files))
	currentTargets := make(map[string]bool, len(current.Files))
	currentLocation := singleLocation(current.Files)
	// Counted before the loop, where the revision is still what current means.
	incoming := len(current.Files)
	held := heldArtifacts(layout, matched)
	for index, file := range current.Files {
		targetPath, root, err := layout.pathFor(file.Path, currentLocation, incoming)
		if err != nil {
			return err
		}
		if currentTargets[targetPath] {
			return fmt.Errorf("current revision has duplicate local paths")
		}
		currentTargets[targetPath] = true
		_, current := layout.currentPath[targetPath]
		if !current {
			if _, err := os.Lstat(targetPath); err == nil {
				return fmt.Errorf("applying the current revision would overwrite an untracked local file")
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("inspect local save path: %w", err)
			}
		}
		temporary, err := ensureTemporary(root)
		if err != nil {
			return err
		}
		stagePath := filepath.Join(temporary, fmt.Sprintf("download-%d", index))
		// The transfer describes itself; this only says which file it is on,
		// the way an upload's progress reads.
		fetched, total := index+1, incoming
		fetchCtx := activity.WithReporter(ctx, func(message string) {
			activity.Report(ctx, fmt.Sprintf("%s (%d/%d)", message, fetched, total))
		})
		reused := false
		if origin := held[file.Artifact.SHA256]; origin != "" {
			activity.Report(ctx, fmt.Sprintf("copying (%d/%d)", fetched, total))
			// A copy that fails for any reason — the file moved, or it is no
			// longer the content the revision said it was — falls through to
			// the download, which never depends on local state.
			if err := copyArtifact(origin, stagePath, file.Artifact); err == nil {
				reused = true
			} else if err := os.Remove(stagePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("stage current artifact: %w", err)
			}
		}
		if !reused {
			if err := downloadArtifact(fetchCtx, source, file.Artifact, stagePath); err != nil {
				return err
			}
		}
		// Content staged once serves every path the revision puts it at, so a
		// revision carrying one file under several names transfers it once.
		if _, known := held[file.Artifact.SHA256]; !known {
			held[file.Artifact.SHA256] = stagePath
		}
		staged = append(staged, stagedFile{target: targetPath, stage: stagePath, fresh: !current})
	}

	backups := make([]backupFile, 0, len(layout.currentPath))
	var applied []string
	failRollback := func(operation error) error {
		rollback := rollbackApplied(applied, backups)
		if rollback != nil {
			preserveTemporary = true
			preserved := make([]string, 0, len(temporaryRoots))
			for _, temporary := range temporaryRoots {
				preserved = append(preserved, temporary)
			}
			sort.Strings(preserved)
			rollback = fmt.Errorf("%w (original save files kept under %s)", rollback, strings.Join(preserved, ", "))
		}
		return joinRollback(operation, rollback)
	}
	for original, root := range layout.currentPath {
		temporary := temporaryRoots[root]
		backup := filepath.Join(temporary, fmt.Sprintf("backup-%d", len(backups)))
		if err := os.Rename(original, backup); err != nil {
			return failRollback(fmt.Errorf("back up local save: %w", err))
		}
		backups = append(backups, backupFile{original: original, backup: backup})
	}

	activity.Report(ctx, "placing")
	for _, file := range staged {
		if err := os.MkdirAll(filepath.Dir(file.target), 0o700); err != nil {
			return failRollback(fmt.Errorf("prepare local save directory: %w", err))
		}
		if file.fresh {
			// Link without replacement so a concurrently created target wins.
			if err := os.Link(file.stage, file.target); err != nil {
				operation := fmt.Errorf("place local save file: %w", err)
				if os.IsExist(err) {
					operation = fmt.Errorf("applying the current revision would overwrite an untracked local file")
				}
				return failRollback(operation)
			}
		} else if err := os.Rename(file.stage, file.target); err != nil {
			return failRollback(fmt.Errorf("replace local save file: %w", err))
		}
		applied = append(applied, file.target)
	}
	return nil
}

// Materialize places a verified Current Revision without replacing concurrent content.
func Materialize(ctx context.Context, source ArtifactSource, destination target.SaveDestination, current omnisave.Revision) (target.Save, error) {
	planned, err := materializationPlan(destination, current)
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

// AppliedFiles reports the local files a successful ApplyCurrent of current
// into save left on disk: every revision file at the native path the save's
// layout maps it to. The apply replaces the save's files wholesale, so this
// — not the discovery the apply started from — is what the disk now holds;
// a restored revision can carry files the local save had lost, and anything
// acting on the placement (registering it with a store, say) must see them.
func AppliedFiles(save target.Save, current omnisave.Revision) ([]target.File, error) {
	layout, err := describeLocalLayout(save)
	if err != nil {
		return nil, err
	}
	currentLocation := singleLocation(current.Files)
	files := make([]target.File, 0, len(current.Files))
	for _, file := range current.Files {
		targetPath, _, err := layout.pathFor(file.Path, currentLocation, len(current.Files))
		if err != nil {
			return nil, err
		}
		locationID, relative, err := splitCanonicalPath(file.Path)
		if err != nil {
			return nil, err
		}
		files = append(files, target.File{
			Path:         targetPath,
			LocationID:   locationID,
			RelativePath: filepath.FromSlash(relative),
			Size:         file.Artifact.Size,
		})
	}
	return files, nil
}

// CanApply reports whether current maps into the local save's layout: every
// canonical path must resolve under a location root this save carries. It
// validates layout only and does not inspect or change the filesystem, so a
// flow that ends in ApplyCurrent can refuse an impossible adoption before
// spending a preservation or a prompt on it.
func CanApply(save target.Save, current omnisave.Revision) error {
	if current.ID == "" || len(current.Files) == 0 {
		return fmt.Errorf("apply needs a non-empty current revision")
	}
	layout, err := describeLocalLayout(save)
	if err != nil {
		return err
	}
	targets := make(map[string]bool, len(current.Files))
	currentLocation := singleLocation(current.Files)
	for _, file := range current.Files {
		targetPath, _, err := layout.pathFor(file.Path, currentLocation, len(current.Files))
		if err != nil {
			return err
		}
		if targets[targetPath] {
			return fmt.Errorf("current revision has duplicate local paths")
		}
		targets[targetPath] = true
	}
	return nil
}

// CanMaterialize reports whether one current maps into one native destination.
// It validates layout only and does not inspect or change the filesystem.
func CanMaterialize(destination target.SaveDestination, current omnisave.Revision) error {
	_, err := materializationPlan(destination, current)
	return err
}

func materializationPlan(destination target.SaveDestination, current omnisave.Revision) ([]plannedFile, error) {
	if destination.ID == "" || destination.TargetID == "" || destination.GameID == "" || current.ID == "" || len(current.Files) == 0 {
		return nil, fmt.Errorf("materialize needs a save destination and non-empty current")
	}
	return planMaterialization(destination, current)
}

func planMaterialization(destination target.SaveDestination, current omnisave.Revision) ([]plannedFile, error) {
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
	aliases := make(map[string]bool, len(destination.LocationAliases))
	for _, alias := range destination.LocationAliases {
		aliases[alias] = true
	}
	currentLocation := singleLocation(current.Files)
	// resolveLocation honors another OS's spelling of the destination's one
	// location (FDR-003, decision 11); several locations leave nothing to
	// say which one an alias means.
	resolveLocation := func(locationID string) (target.SaveLocation, bool, bool) {
		if location, exists := locations[locationID]; exists {
			return location, false, true
		}
		if locationID == currentLocation && aliases[locationID] && len(destination.Locations) == 1 {
			return destination.Locations[0], true, true
		}
		return target.SaveLocation{}, false, false
	}
	counts := make(map[string]int)
	for _, file := range current.Files {
		locationID, _, err := splitCanonicalPath(file.Path)
		if err != nil {
			return nil, err
		}
		if location, _, exists := resolveLocation(locationID); exists {
			counts[location.ID]++
		}
	}

	planned := make([]plannedFile, 0, len(current.Files))
	targets := make(map[string]bool, len(current.Files))
	for _, file := range current.Files {
		locationID, relative, err := splitCanonicalPath(file.Path)
		if err != nil {
			return nil, err
		}
		location, viaAlias, exists := resolveLocation(locationID)
		if !exists {
			return nil, fmt.Errorf("current revision uses an unknown save location")
		}
		targetPath, err := materializedPath(location, relative, counts[location.ID], viaAlias)
		if err != nil {
			return nil, err
		}
		if targets[targetPath] {
			return nil, fmt.Errorf("current revision has duplicate local paths")
		}
		targets[targetPath] = true
		planned = append(planned, plannedFile{revision: file, target: targetPath})
	}
	return planned, nil
}

func splitCanonicalPath(canonical string) (string, string, error) {
	locationID, relative, found := strings.Cut(canonical, "/")
	if !found || locationID == "" || relative == "" || path.Clean(relative) != relative || strings.HasPrefix(relative, "/") {
		return "", "", fmt.Errorf("current revision has an invalid canonical path")
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", "", fmt.Errorf("current revision has an invalid canonical path")
		}
	}
	return locationID, relative, nil
}

func materializedPath(location target.SaveLocation, relative string, count int, viaAlias bool) (string, error) {
	base := filepath.Clean(location.Path)
	nativeRelative := filepath.FromSlash(relative)
	kind := location.Kind
	if kind == target.SaveLocationUnknown {
		if count == 1 && nativeRelative == filepath.Base(base) {
			kind = target.SaveLocationFile
		} else if viaAlias && count == 1 {
			// The revision's one file arrived under another OS's spelling
			// with a name that is not this location's own. A location that
			// does not exist yet cannot say whether it is that file or a
			// directory holding it, and guessing directory would bury a
			// save file inside a folder wearing its name — an unusable
			// placement reported as success. Refusing keeps the strict
			// fallback honest (FDR-003, decision 11).
			return "", fmt.Errorf("current revision does not fit its save location")
		} else {
			kind = target.SaveLocationDirectory
		}
	}
	if kind == target.SaveLocationFile {
		if count != 1 || nativeRelative != filepath.Base(base) {
			return "", fmt.Errorf("current revision does not fit its file save location")
		}
		return base, nil
	}
	if kind != target.SaveLocationDirectory {
		return "", fmt.Errorf("save destination has an invalid location kind")
	}
	targetPath := filepath.Clean(filepath.Join(base, nativeRelative))
	contained, err := filepath.Rel(base, targetPath)
	if err != nil || relativeEscapesRoot(contained) {
		return "", fmt.Errorf("current revision path escapes its save location")
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
		roots:          make(map[string]string),
		currentPath:    make(map[string]string),
		aliases:        make(map[string]bool, len(save.LocationAliases)),
		singleFlatFile: make(map[string]string),
	}
	for _, alias := range save.LocationAliases {
		layout.aliases[alias] = true
	}
	fileCounts := make(map[string]int, len(save.Files))
	for _, file := range save.Files {
		fileCounts[file.LocationID]++
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
		if fileCounts[file.LocationID] == 1 && !strings.ContainsRune(relative, filepath.Separator) {
			layout.singleFlatFile[file.LocationID] = filepath.ToSlash(relative)
		}
		fullPath := filepath.Clean(file.Path)
		if _, duplicate := layout.currentPath[fullPath]; duplicate {
			return localLayout{}, fmt.Errorf("local save has duplicate native paths")
		}
		layout.currentPath[fullPath] = root
	}
	return layout, nil
}

// pathFor maps one revision path into this save. aliasLocation is the
// revision's sole location; empty keeps a several-location revision strict.
func (l localLayout) pathFor(canonical, aliasLocation string, aliasFileCount int) (string, string, error) {
	locationID, relative, err := splitCanonicalPath(canonical)
	if err != nil {
		return "", "", err
	}
	root := l.roots[locationID]
	if root == "" && locationID == aliasLocation && l.aliases[locationID] && len(l.roots) == 1 {
		// Another OS's spelling of this save's one location (FDR-003,
		// decision 11).
		for own, only := range l.roots {
			if flat, isFile := l.singleFlatFile[own]; isFile && aliasFileCount == 1 && flat != relative {
				// The location is shaped like a single save file, and the
				// foreign spelling names something else. Placing that name
				// would swap the file the game reads for one it never will,
				// so the translation is refused rather than guessed.
				return "", "", fmt.Errorf("current revision does not fit its save location")
			}
			root = only
		}
	}
	if root == "" {
		return "", "", fmt.Errorf("current revision uses an unknown save location")
	}
	targetPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	contained, err := filepath.Rel(root, targetPath)
	if err != nil || relativeEscapesRoot(contained) {
		return "", "", fmt.Errorf("current revision path escapes its save location")
	}
	return targetPath, root, nil
}

func relativeEscapesRoot(relative string) bool {
	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// heldArtifacts maps content the local save already holds to where it holds
// it. A rewind or a fast-forward inside one lineage usually moves a few files
// out of many and leaves the rest identical byte for byte, and the revision
// says which those are: content is addressed by hash, so a file whose hash
// appears in both revisions is already on this disk. Staging it from there
// reads a local file where downloading it would fetch the whole save again.
//
// The local save is known to equal matched — the apply proved it just now —
// which is what makes matched a truthful index of what is on disk.
func heldArtifacts(layout localLayout, matched omnisave.Revision) map[string]string {
	held := make(map[string]string, len(matched.Files))
	matchedLocation := singleLocation(matched.Files)
	for _, file := range matched.Files {
		targetPath, _, err := layout.pathFor(file.Path, matchedLocation, len(matched.Files))
		if err != nil {
			// A matched file this layout cannot place is simply not offered
			// as a source; its artifact downloads exactly as before.
			continue
		}
		if _, known := held[file.Artifact.SHA256]; !known {
			held[file.Artifact.SHA256] = targetPath
		}
	}
	return held
}

func downloadArtifact(ctx context.Context, source ArtifactSource, artifact omnisave.Artifact, destination string) error {
	payload, err := source.OpenArtifact(ctx, artifact.SHA256)
	if err != nil {
		return fmt.Errorf("download current artifact: %w", err)
	}
	return stageArtifact(payload, destination, artifact, "downloaded current")
}

// copyArtifact stages content the local save already holds. It is checked
// exactly as a download is: a local file is only a faster way to obtain the
// bytes, never a reason to trust them, and one the game may have rewritten
// since the apply looked.
func copyArtifact(origin, destination string, artifact omnisave.Artifact) error {
	payload, err := os.Open(origin)
	if err != nil {
		return fmt.Errorf("open local save file: %w", err)
	}
	return stageArtifact(payload, destination, artifact, "local")
}

// stageArtifact writes payload into destination and proves it is the artifact
// before returning. What names the payload in errors, since staged content
// comes from the server or from the local save.
func stageArtifact(payload io.ReadCloser, destination string, artifact omnisave.Artifact, what string) error {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		payload.Close()
		return fmt.Errorf("stage current artifact: %w", err)
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), payload)
	// Staged bytes become the live save by rename or link, which makes the
	// name durable before the data unless the data is flushed first; without
	// this a power loss just after an apply leaves a truncated save file.
	syncErr := file.Sync()
	closeFileErr := file.Close()
	closePayloadErr := payload.Close()
	if copyErr != nil {
		return fmt.Errorf("read %s artifact: %w", what, copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("stage current artifact: %w", syncErr)
	}
	if closeFileErr != nil {
		return fmt.Errorf("stage current artifact: %w", closeFileErr)
	}
	if closePayloadErr != nil {
		return fmt.Errorf("close %s artifact: %w", what, closePayloadErr)
	}
	actualHash := hex.EncodeToString(digest.Sum(nil))
	if size != artifact.Size || !strings.EqualFold(actualHash, artifact.SHA256) {
		return fmt.Errorf("%s artifact failed verification", what)
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
