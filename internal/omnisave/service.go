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

var ErrMigrationRefused = errors.New("omnisave: migration refused")

// ErrPathFormatMigrationRequired prevents ordinary lineage movement while its
// paths still use a retired format. Only the explicit format migration may
// advance such a lineage.
var ErrPathFormatMigrationRequired = errors.New("omnisave: path format migration required")

// Reasons a location migration is refused. The two version refusals are
// distinct answers and must stay so: no step leaves the named version at
// all, which no retry can change, against the lineage having moved on from
// the version the caller expected, which a fresh read resolves.
const (
	// MigrationRefusedForkFamily: the lineage shares revisions with a fork,
	// and a rename must cover the whole family or none of it.
	MigrationRefusedForkFamily = "fork_family"
	// MigrationRefusedMixed: some revision speaks a location other than the
	// one being renamed.
	MigrationRefusedMixed = "mixed_vocabulary"
	// MigrationRefusedEmpty: nothing speaks the retired location at all.
	MigrationRefusedEmpty = "empty"
	// MigrationRefusedUnknownVersion: no migration in the chain advances the
	// named version, so no lineage at it can be migrated by this build.
	MigrationRefusedUnknownVersion = "unknown_path_format_version"
	// MigrationRefusedVersion: the lineage is not at the path format the
	// caller expected.
	MigrationRefusedVersion = "path_format_version"
	// MigrationRefusedPathLength: the rename would mint a path longer than
	// any commit may reference.
	MigrationRefusedPathLength = "path_length"
)

// MigrationRefused reports a location migration the server declined, and why.
type MigrationRefused struct {
	Reason string
}

func (e *MigrationRefused) Error() string { return ErrMigrationRefused.Error() + ": " + e.Reason }
func (e *MigrationRefused) Unwrap() error { return ErrMigrationRefused }

// Service is the application boundary for working with Omnisave records.
type Service interface {
	Create(ctx context.Context, input CreateOmnisave) (*Omnisave, error)
	List(ctx context.Context) ([]Omnisave, error)
	Get(ctx context.Context, id string) (*Omnisave, error)
	Update(ctx context.Context, id string, input UpdateOmnisave) (*Omnisave, error)
	Delete(ctx context.Context, id string) error
	Fork(ctx context.Context, omnisaveID string, input ForkOmnisave) (*ForkResult, error)
	Restore(ctx context.Context, omnisaveID string, input RestoreRevision) (*Omnisave, error)
	// MigrateLocations renames a lineage's location vocabulary in place; see
	// the input type for the contract.
	MigrateLocations(ctx context.Context, omnisaveID string, input MigrateLocations) (*MigrationResult, error)

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
	// LabelRevision reruns the game's labeler against an existing revision,
	// replacing its current name when the labeler produces one.
	LabelRevision(ctx context.Context, omnisaveID, revisionID string) (*Revision, error)
	DeleteRevision(ctx context.Context, omnisaveID, revisionID string) error

	StoreArtifact(ctx context.Context, artifact Artifact, payload io.Reader) error
	StatArtifact(ctx context.Context, sha256 string) (int64, error)
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}
