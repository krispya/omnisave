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
