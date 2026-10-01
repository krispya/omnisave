package omnisave

import (
	"context"

	"github.com/krisbaumgartner/omnisave/internal/artifact"
)

// Repository persists save history. The Service validates and shapes input;
// the Repository enforces every invariant that must hold atomically with the
// write that could break it — the Current Revision check, the graph's refusal
// to lose a node something still needs, and whole-lineage path migrations —
// and reports each refusal with this package's error types. Missing records
// answer ErrNotFound.
type Repository interface {
	artifact.Store

	// InsertOmnisave stores a new save. Its GameID must name a Library game
	// (ErrInvalid otherwise), checked in the same transaction so a save can
	// never outlive the game it belongs to.
	InsertOmnisave(ctx context.Context, save Omnisave) error
	ListOmnisaves(ctx context.Context) ([]Omnisave, error)
	GetOmnisave(ctx context.Context, id string) (*Omnisave, error)
	UpdateOmnisaveDisplayName(ctx context.Context, id, displayName string) error
	DeleteOmnisave(ctx context.Context, id string) error
	ForkOmnisave(ctx context.Context, save Omnisave) error
	// RestoreOmnisave moves the save's Current Revision to revisionID after
	// verifying it still equals expectedCurrentRevisionID
	// (*CurrentRevisionConflict otherwise).
	RestoreOmnisave(ctx context.Context, id, revisionID string, expectedCurrentRevisionID *string) error

	// RecordAchievements stores unlocks against a save, keeping the placement
	// an achievement was first given: an ID already recorded is left alone.
	// It returns only the achievements this call added.
	RecordAchievements(ctx context.Context, omnisaveID string, achievements []Achievement) ([]Achievement, error)
	// ListAchievements returns a save's achievements in unlock order.
	ListAchievements(ctx context.Context, omnisaveID string) ([]Achievement, error)

	// CommitRevision inserts a revision after verifying the save's Current
	// Revision matches the caller's expectation (*CurrentRevisionConflict
	// otherwise) and that every referenced artifact is available
	// (*artifact.Unavailable otherwise). The new node becomes current unless
	// keepCurrent leaves the pointer where the check proved it — a branch
	// committed only to preserve content (FDR-005, decision 4).
	CommitRevision(ctx context.Context, expectedCurrentRevisionID *string, revision Revision, keepCurrent bool) error
	// MigrateRevisionPaths applies the migration that advances a lineage at
	// fromVersion (the PathFormatMigrations chain owns the mapping and target
	// version) — every `from/rest` becomes `to/rest` — and records the step
	// as a durable migration fact recovery can replay. Refused
	// (*MigrationRefused) when fromVersion is not the lineage's current
	// version, the save shares revisions with a fork, any file speaks another
	// location, no file speaks the retired one, or a renamed path would
	// exceed MaxRevisionPathLength.
	MigrateRevisionPaths(ctx context.Context, omnisaveID string, fromVersion int, to string) (MigrationResult, error)
	GetRevision(ctx context.Context, omnisaveID, revisionID string) (*Revision, error)
	ListRevisions(ctx context.Context, omnisaveID string) ([]Revision, error)
	UpdateRevisionDisplayName(ctx context.Context, omnisaveID, revisionID, displayName string) error
	// UpdateRevisionLabel records the result of an explicitly requested
	// relabel, replacing the revision's current name and its source.
	UpdateRevisionLabel(ctx context.Context, omnisaveID, revisionID, displayName string) error
	// DeleteRevision removes one revision reachable through the named save.
	// Only a node the graph no longer needs may go — no children, not any
	// save's current revision, not any fork's origin — and *RevisionInUse
	// reports which of those still holds.
	DeleteRevision(ctx context.Context, omnisaveID, revisionID string) error
}
