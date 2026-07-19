// Package binding connects discovered local saves to server Omnisaves.
package binding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// Server is the remote surface seeding needs: content upload, save creation,
// and revision commits, with deletion for cleanup when a seed half-fails.
type Server interface {
	UploadArtifact(ctx context.Context, artifact omnisave.Artifact, content io.Reader) error
	CreateOmnisave(ctx context.Context, input omnisave.CreateOmnisave) (*omnisave.Omnisave, error)
	CommitRevision(ctx context.Context, omnisaveID string, input omnisave.CreateRevision) (*omnisave.Revision, error)
	DeleteOmnisave(ctx context.Context, id string) error
}

// Lineage pairs one Omnisave with the revision history whose content can
// identify whether a local save already belongs to it.
type Lineage struct {
	Omnisave  omnisave.Omnisave
	Revisions []omnisave.Revision
}

// ContentMatch contains every revision in one lineage whose file content is
// identical to the local save. A lineage is returned at most once.
type ContentMatch struct {
	Omnisave  omnisave.Omnisave
	Revisions []omnisave.Revision
}

// MatchesHead reports whether this lineage's current head has the local
// save's exact content.
func (m ContentMatch) MatchesHead() bool {
	if m.Omnisave.HeadRevisionID == nil {
		return false
	}
	for _, revision := range m.Revisions {
		if revision.ID == *m.Omnisave.HeadRevisionID {
			return true
		}
	}
	return false
}

// FindContentMatches compares a local save with every revision in each
// lineage. Equality means the canonical file paths and their content match;
// revision ordering and media types do not affect the result.
func FindContentMatches(save target.Save, lineages []Lineage) ([]ContentMatch, error) {
	manifest, err := Manifest(save)
	if err != nil {
		return nil, err
	}

	var matches []ContentMatch
	for _, lineage := range lineages {
		match := ContentMatch{Omnisave: lineage.Omnisave}
		for _, revision := range lineage.Revisions {
			if sameManifest(manifest, revision.Files) {
				match.Revisions = append(match.Revisions, revision)
			}
		}
		if len(match.Revisions) > 0 {
			matches = append(matches, match)
		}
	}
	return matches, nil
}

// Manifest reads a local save into the canonical, content-addressed file list
// used by server revisions.
func Manifest(save target.Save) ([]omnisave.RevisionFile, error) {
	if len(save.Files) == 0 {
		return nil, fmt.Errorf("local save has no files to match")
	}

	manifest := make([]omnisave.RevisionFile, 0, len(save.Files))
	paths := make(map[string]bool, len(save.Files))
	for _, file := range save.Files {
		canonical := CanonicalPath(file)
		if canonical == "" || paths[canonical] {
			return nil, fmt.Errorf("local save has duplicate or empty canonical paths")
		}
		paths[canonical] = true

		payload, err := os.Open(file.Path)
		if err != nil {
			return nil, fmt.Errorf("open local save file: %w", err)
		}
		digest := sha256.New()
		size, copyErr := io.Copy(digest, payload)
		closeErr := payload.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("read local save file: %w", copyErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close local save file: %w", closeErr)
		}
		manifest = append(manifest, omnisave.RevisionFile{
			Path: canonical,
			Artifact: omnisave.Artifact{
				Format: "application/octet-stream",
				SHA256: hex.EncodeToString(digest.Sum(nil)),
				Size:   size,
			},
		})
	}
	sort.Slice(manifest, func(i, j int) bool { return manifest[i].Path < manifest[j].Path })
	return manifest, nil
}

func sameManifest(local, remote []omnisave.RevisionFile) bool {
	if len(local) != len(remote) {
		return false
	}
	remoteByPath := make(map[string]omnisave.Artifact, len(remote))
	for _, file := range remote {
		if _, duplicate := remoteByPath[file.Path]; duplicate {
			return false
		}
		remoteByPath[file.Path] = file.Artifact
	}
	for _, file := range local {
		artifact, exists := remoteByPath[file.Path]
		if !exists || artifact.SHA256 != file.Artifact.SHA256 || artifact.Size != file.Artifact.Size {
			return false
		}
	}
	return true
}

// Seed creates a new Omnisave for a Library game and commits the local save's
// current content as its initial revision (FDR-003). The returned revision is
// the binding's sync baseline.
func Seed(ctx context.Context, server Server, serverGameID string, save target.Save) (*omnisave.Omnisave, *omnisave.Revision, error) {
	if serverGameID == "" {
		return nil, nil, fmt.Errorf("seed needs a resolved Library game")
	}
	if len(save.Files) == 0 {
		return nil, nil, fmt.Errorf("local save has no files to seed")
	}

	upserts, err := uploadFiles(ctx, server, save)
	if err != nil {
		return nil, nil, err
	}
	created, err := server.CreateOmnisave(ctx, omnisave.CreateOmnisave{GameID: serverGameID})
	if err != nil {
		return nil, nil, fmt.Errorf("create Omnisave: %w", err)
	}
	revision, err := server.CommitRevision(ctx, created.ID, omnisave.CreateRevision{Upserts: upserts})
	if err != nil {
		// An Omnisave with no revisions is indistinguishable from lost data;
		// remove the empty shell so a retry starts clean.
		server.DeleteOmnisave(context.WithoutCancel(ctx), created.ID)
		return nil, nil, fmt.Errorf("commit seed revision: %w", err)
	}
	return created, revision, nil
}

func uploadFiles(ctx context.Context, server Server, save target.Save) ([]omnisave.RevisionFile, error) {
	upserts := make([]omnisave.RevisionFile, 0, len(save.Files))
	for _, file := range save.Files {
		content, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, fmt.Errorf("read local save file: %w", err)
		}
		digest := sha256.Sum256(content)
		artifact := omnisave.Artifact{
			Format: "application/octet-stream",
			SHA256: hex.EncodeToString(digest[:]),
			Size:   int64(len(content)),
		}
		if err := server.UploadArtifact(ctx, artifact, bytes.NewReader(content)); err != nil {
			return nil, fmt.Errorf("upload %s: %w", filepath.Base(file.Path), err)
		}
		upserts = append(upserts, omnisave.RevisionFile{
			Path:     CanonicalPath(file),
			Artifact: artifact,
		})
	}
	return upserts, nil
}

// CanonicalPath names a native file inside a revision: location-scoped,
// forward-slashed, and free of machine-specific roots.
func CanonicalPath(file target.File) string {
	relative := filepath.ToSlash(file.RelativePath)
	if relative == "" {
		relative = filepath.Base(file.Path)
	}
	return path.Join(file.LocationID, relative)
}
