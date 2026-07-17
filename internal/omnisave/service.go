package omnisave

import (
	"context"
	"errors"
	"io"
)

var (
	ErrNotFound        = errors.New("omnisave: not found")
	ErrInvalid         = errors.New("omnisave: invalid input")
	ErrConflict        = errors.New("omnisave: head conflict")
	ErrArtifactMissing = errors.New("omnisave: artifact missing")
)

// HeadConflict reports the current head that prevented a linear commit.
type HeadConflict struct {
	ExpectedHeadID *string
	ActualHeadID   *string
}

func (e *HeadConflict) Error() string { return ErrConflict.Error() }
func (e *HeadConflict) Unwrap() error { return ErrConflict }

// MissingArtifacts reports blobs required by a revision manifest.
type MissingArtifacts struct {
	SHA256 []string
}

func (e *MissingArtifacts) Error() string { return ErrArtifactMissing.Error() }
func (e *MissingArtifacts) Unwrap() error { return ErrArtifactMissing }

// Service is the application boundary for working with OmniSave records.
type Service interface {
	Create(ctx context.Context, input CreateOmniSave) (*OmniSave, error)
	List(ctx context.Context) ([]OmniSave, error)
	Get(ctx context.Context, id string) (*OmniSave, error)
	Update(ctx context.Context, id string, input UpdateOmniSave) (*OmniSave, error)
	Delete(ctx context.Context, id string) error
	Fork(ctx context.Context, omnisaveID string, input ForkOmniSave) (*ForkResult, error)

	CommitRevision(ctx context.Context, omnisaveID string, input CreateRevision) (*Revision, error)
	GetRevision(ctx context.Context, omnisaveID, revisionID string) (*Revision, error)
	ListRevisions(ctx context.Context, omnisaveID string) ([]Revision, error)

	StoreArtifact(ctx context.Context, artifact Artifact, payload io.Reader) error
	StatArtifact(ctx context.Context, sha256 string) (int64, error)
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}
