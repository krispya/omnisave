// Package omnisave defines versioned, persistent game-save records.
package omnisave

import "time"

// Omnisave identifies one independently versioned game save.
type Omnisave struct {
	ID                string      `json:"id"`
	GameID            string      `json:"game_id"`
	DisplayName       string      `json:"display_name"`
	CurrentRevisionID *string     `json:"current_revision_id"`
	ForkedFrom        *ForkOrigin `json:"forked_from,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	// CurrentRevisionCreatedAt is the original creation time of the selected
	// snapshot; restoring an older revision deliberately moves it backward.
	CurrentRevisionCreatedAt time.Time `json:"current_revision_created_at"`
	// CurrentRevisionSavedAt is the selected snapshot's SavedAt: when the save
	// itself was written, not when it reached the server. Nil when the snapshot
	// predates devices reporting it.
	CurrentRevisionSavedAt *time.Time        `json:"current_revision_saved_at,omitempty"`
	Metadata               map[string]string `json:"metadata,omitempty"`
}

// ForkOrigin identifies the snapshot from which another Omnisave began.
type ForkOrigin struct {
	OmnisaveID string `json:"omnisave_id"`
	RevisionID string `json:"revision_id"`
}

// NameSource values record who set a revision's DisplayName. The rule they
// exist for: automation may replace a labeler-derived name or fill an empty
// one, but a manually chosen name is never overwritten.
const (
	NameSourceLabeler = "labeler"
	NameSourceManual  = "manual"
)

// Revision is a content-immutable node in a game's single-parent history.
// OmnisaveID records which Omnisave created it; forks may share the node as
// ancestry. DisplayName is shared presentation metadata and may be changed
// without changing the snapshot or its identity.
type Revision struct {
	ID          string  `json:"id"`
	OmnisaveID  string  `json:"omnisave_id"`
	DisplayName string  `json:"display_name"`
	ParentID    *string `json:"parent_id"`
	// NameSource is NameSourceLabeler or NameSourceManual when DisplayName is
	// set, and empty while the revision is unnamed.
	NameSource string    `json:"name_source,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	// SavedAt is when the snapshot's content was written by the game — the
	// newest file-modification time the committing Device saw. CreatedAt is
	// when the snapshot reached the server; a device syncing an old save
	// commits a new revision of old content, and this is what says so. Nil
	// when the committing client did not report it.
	SavedAt  *time.Time        `json:"saved_at,omitempty"`
	Files    []RevisionFile    `json:"files"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// AchievementUnlock is one achievement a Device watched a game unlock, named
// as that game's store names it. UnlockedAt is the store's own time, which is
// what lets two Devices report the same unlock and mean the same instant; it
// is recorded to the second, the precision every store publishes.
type AchievementUnlock struct {
	// ID is the store's stable key for the achievement, unique within its
	// game. Two reports carrying the same ID are the same unlock.
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	UnlockedAt  time.Time `json:"unlocked_at"`
}

// Achievement is a recorded unlock together with where it falls in a save's
// history: RevisionID is the first revision committed at or after the unlock,
// which is the earliest snapshot known to carry it. Restoring anything before
// that revision goes back to before the achievement existed.
//
// RevisionID is nil while no revision has been committed since — an unlock
// reported between commits waits, and the next commit claims it.
type Achievement struct {
	AchievementUnlock
	RevisionID *string `json:"revision_id"`
}

// RevisionFile maps a canonical save path to immutable content.
type RevisionFile struct {
	Path     string   `json:"path"`
	Artifact Artifact `json:"artifact"`
}

// Artifact locates and describes immutable bytes.
type Artifact struct {
	Format string `json:"format"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// CreateOmnisave describes a new logical game save.
type CreateOmnisave struct {
	GameID      string            `json:"game_id"`
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// UpdateOmnisave describes mutable Omnisave fields.
type UpdateOmnisave struct {
	DisplayName *string `json:"display_name"`
}

// UpdateRevision describes mutable revision presentation metadata.
type UpdateRevision struct {
	DisplayName *string `json:"display_name"`
}

// CreateRevision describes partial changes committed against an expected current revision.
type CreateRevision struct {
	ExpectedCurrentRevisionID *string `json:"expected_current_revision_id"`
	// ParentRevisionID selects a branch parent; nil uses ExpectedCurrentRevisionID.
	ParentRevisionID *string `json:"parent_revision_id,omitempty"`
	// SavedAt reports when the committed content was written by the game, as
	// the Device saw it. Optional; a zero time is treated as unreported.
	SavedAt  *time.Time        `json:"saved_at,omitempty"`
	Upserts  []RevisionFile    `json:"upserts,omitempty"`
	Deletes  []string          `json:"deletes,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RestoreRevision moves an Omnisave's current pointer to an existing node.
type RestoreRevision struct {
	ExpectedCurrentRevisionID *string `json:"expected_current_revision_id"`
	RevisionID                string  `json:"revision_id"`
}

// ForkOmnisave creates a new selectable lineage from an existing snapshot.
type ForkOmnisave struct {
	RevisionID  string            `json:"revision_id"`
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ForkResult contains the new save and the shared revision where it began.
type ForkResult struct {
	Omnisave Omnisave `json:"omnisave"`
	Revision Revision `json:"revision"`
}
