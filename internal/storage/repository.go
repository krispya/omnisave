// Package storage defines persistence required by application services.
package storage

import (
	"context"
	"errors"
	"io"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

var ErrNotFound = errors.New("storage: not found")

// Repository persists OmniSave records without applying application rules.
type Repository interface {
	InsertOmniSave(ctx context.Context, save omnisave.OmniSave) error
	ListOmniSaves(ctx context.Context) ([]omnisave.OmniSave, error)
	GetOmniSave(ctx context.Context, id string) (*omnisave.OmniSave, error)

	InsertRevision(ctx context.Context, revision omnisave.Revision, payload io.Reader) error
	GetRevision(ctx context.Context, omnisaveID, revisionID string) (*omnisave.Revision, error)
	ListRevisions(ctx context.Context, omnisaveID string) ([]omnisave.Revision, error)
	DeleteRevision(ctx context.Context, omnisaveID, revisionID string) error

	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}
