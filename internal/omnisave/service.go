package omnisave

import (
	"context"
	"errors"
	"io"
)

var (
	ErrNotFound = errors.New("omnisave: not found")
	ErrInvalid  = errors.New("omnisave: invalid input")
	ErrInUse    = errors.New("omnisave: in use")
)

// Service is the application boundary for working with OmniSave records.
type Service interface {
	Create(ctx context.Context, input CreateOmniSave) (*OmniSave, error)
	List(ctx context.Context) ([]OmniSave, error)
	Get(ctx context.Context, id string) (*OmniSave, error)
	Delete(ctx context.Context, id string) error

	AddRevision(ctx context.Context, omnisaveID string, input CreateRevision, payload io.Reader) (*Revision, error)
	GetRevision(ctx context.Context, omnisaveID, revisionID string) (*Revision, error)
	ListRevisions(ctx context.Context, omnisaveID string) ([]Revision, error)
	DeleteRevision(ctx context.Context, omnisaveID, revisionID string) error

	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}
