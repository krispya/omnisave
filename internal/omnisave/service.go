package omnisave

import (
	"context"
	"errors"
	"io"
)

var (
	ErrNotFound        = errors.New("omnisave: not found")
	ErrInvalid         = errors.New("omnisave: invalid input")
	ErrConflict        = errors.New("omnisave: current revision conflict")
	ErrArtifactMissing = errors.New("omnisave: artifact missing")
)

// CurrentRevisionConflict reports the current revision that rejected a stale write.
type CurrentRevisionConflict struct {
	ExpectedCurrentRevisionID *string
	ActualCurrentRevisionID   *string
}

func (e *CurrentRevisionConflict) Error() string { return ErrConflict.Error() }
func (e *CurrentRevisionConflict) Unwrap() error { return ErrConflict }

// MissingArtifacts reports blobs required by a revision manifest.
type MissingArtifacts struct {
	SHA256 []string
}

func (e *MissingArtifacts) Error() string { return ErrArtifactMissing.Error() }
func (e *MissingArtifacts) Unwrap() error { return ErrArtifactMissing }

var ErrRevisionInUse = errors.New("omnisave: revision in use")

// Reasons deleting a revision is refused: the node is a save's current
// revision, has children building on it, or is where a fork began.
const (
	RevisionInUseCurrent    = "current"
	RevisionInUseChildren   = "children"
	RevisionInUseForkOrigin = "fork_origin"
)

// RevisionInUse reports a revision deletion refused because the graph still
// needs the node, and names why.
type RevisionInUse struct {
	Reason string
}

func (e *RevisionInUse) Error() string { return ErrRevisionInUse.Error() + ": " + e.Reason }
func (e *RevisionInUse) Unwrap() error { return ErrRevisionInUse }

// Service is the application boundary for working with Omnisave records.
type Service interface {
	Create(ctx context.Context, input CreateOmnisave) (*Omnisave, error)
	List(ctx context.Context) ([]Omnisave, error)
	Get(ctx context.Context, id string) (*Omnisave, error)
	Update(ctx context.Context, id string, input UpdateOmnisave) (*Omnisave, error)
	Delete(ctx context.Context, id string) error
	Fork(ctx context.Context, omnisaveID string, input ForkOmnisave) (*ForkResult, error)
	Restore(ctx context.Context, omnisaveID string, input RestoreRevision) (*Omnisave, error)

	// RecordAchievements files unlocks a Device observed against the revision
	// each one lands on. Recording is idempotent: an ID already recorded for
	// this save keeps the placement it was first given, so a Device may repeat
	// a report without moving a mark. It returns only what this call added.
	RecordAchievements(ctx context.Context, omnisaveID string, unlocks []AchievementUnlock) ([]Achievement, error)
	ListAchievements(ctx context.Context, omnisaveID string) ([]Achievement, error)

	CommitRevision(ctx context.Context, omnisaveID string, input CreateRevision) (*Revision, error)
	GetRevision(ctx context.Context, omnisaveID, revisionID string) (*Revision, error)
	ListRevisions(ctx context.Context, omnisaveID string) ([]Revision, error)
	UpdateRevision(ctx context.Context, omnisaveID, revisionID string, input UpdateRevision) (*Revision, error)
	// HasLabeler reports whether the server can label revisions for a game.
	HasLabeler(ctx context.Context, gameID string) bool
	// LabelRevision reruns the game's labeler against an existing revision.
	// A manual name remains unchanged.
	LabelRevision(ctx context.Context, omnisaveID, revisionID string) (*Revision, error)
	DeleteRevision(ctx context.Context, omnisaveID, revisionID string) error

	StoreArtifact(ctx context.Context, artifact Artifact, payload io.Reader) error
	StatArtifact(ctx context.Context, sha256 string) (int64, error)
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}
