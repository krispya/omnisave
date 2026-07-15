// Package omnisave defines versioned, persistent game-save records.
package omnisave

import "time"

// OmniSave identifies one independently versioned game save.
type OmniSave struct {
	ID          string            `json:"id"`
	GameID      string            `json:"game_id"`
	DisplayName string            `json:"display_name"`
	CreatedAt   time.Time         `json:"created_at"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Revision is an immutable state in an OmniSave's revision graph.
type Revision struct {
	ID         string            `json:"id"`
	OmniSaveID string            `json:"omnisave_id"`
	ParentIDs  []string          `json:"parent_ids"`
	CreatedAt  time.Time         `json:"created_at"`
	Artifact   Artifact          `json:"artifact"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Artifact locates and describes a revision's canonical bytes.
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

// CreateRevision describes a revision whose payload is supplied separately.
type CreateRevision struct {
	ParentIDs []string          `json:"parent_ids"`
	Format    string            `json:"format"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}
