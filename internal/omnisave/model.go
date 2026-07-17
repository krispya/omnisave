// Package omnisave defines versioned, persistent game-save records.
package omnisave

import "time"

// OmniSave identifies one independently versioned game save.
type OmniSave struct {
	ID             string            `json:"id"`
	GameID         string            `json:"game_id"`
	DisplayName    string            `json:"display_name"`
	HeadRevisionID *string           `json:"head_revision_id"`
	ForkedFrom     *ForkOrigin       `json:"forked_from,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// ForkOrigin identifies the snapshot from which another OmniSave began.
type ForkOrigin struct {
	OmniSaveID string `json:"omnisave_id"`
	RevisionID string `json:"revision_id"`
}

// Revision is an immutable state in an OmniSave's linear history.
type Revision struct {
	ID         string            `json:"id"`
	OmniSaveID string            `json:"omnisave_id"`
	ParentID   *string           `json:"parent_id"`
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

// CreateOmniSave describes a new logical game save.
type CreateOmniSave struct {
	GameID      string            `json:"game_id"`
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// UpdateOmniSave describes mutable OmniSave fields.
type UpdateOmniSave struct {
	DisplayName *string `json:"display_name"`
}

// CreateRevision describes partial changes committed against an expected head.
type CreateRevision struct {
	ExpectedHeadID *string           `json:"expected_head_id"`
	Upserts        []RevisionFile    `json:"upserts,omitempty"`
	Deletes        []string          `json:"deletes,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// ForkOmniSave creates a new selectable lineage from an existing snapshot.
type ForkOmniSave struct {
	RevisionID  string            `json:"revision_id"`
	DisplayName string            `json:"display_name"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ForkResult contains the new save and its copied initial snapshot.
type ForkResult struct {
	OmniSave OmniSave `json:"omnisave"`
	Revision Revision `json:"revision"`
}
