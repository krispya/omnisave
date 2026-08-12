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
	CurrentRevisionCreatedAt time.Time         `json:"current_revision_created_at"`
	Metadata                 map[string]string `json:"metadata,omitempty"`
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
	NameSource string            `json:"name_source,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	Files      []RevisionFile    `json:"files"`
	Metadata   map[string]string `json:"metadata,omitempty"`
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
	// ParentRevisionID attaches the new revision to a node other than the
	// expected current revision, and its file set is the one Upserts and
	// Deletes apply to. It is how a Device commits a branch after a restore
	// moved current off the Device's baseline (FDR-005, decision 15). Nil
	// means the expected current revision is the parent.
	ParentRevisionID *string           `json:"parent_revision_id,omitempty"`
	Upserts          []RevisionFile    `json:"upserts,omitempty"`
	Deletes          []string          `json:"deletes,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
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
