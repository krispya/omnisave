package shared

import "time"

// VersionInfo describes a single stored version of a save file.
type VersionInfo struct {
	ID           int64     `json:"id"`
	System       string    `json:"system"`
	Game         string    `json:"game"`
	SHA256       string    `json:"sha256"`
	ClientID     string    `json:"client_id"`
	Timestamp    time.Time `json:"timestamp"`
	IsActive     bool      `json:"is_active"`
	ParentSHA256 string    `json:"parent_sha256"`
}

// PushRequest holds form fields for a push (file is sent as multipart).
type PushRequest struct {
	ClientID     string `json:"client_id"`
	ParentSHA256 string `json:"parent_sha256"`
}

// GameEntry represents a game known to the server.
type GameEntry struct {
	System string `json:"system"`
	Game   string `json:"game"`
}

// SystemEntry represents a system (core name) known to the server.
type SystemEntry struct {
	System string `json:"system"`
}

// PushResponse is returned after a successful push.
type PushResponse struct {
	SHA256   string `json:"sha256"`
	IsActive bool   `json:"is_active"`
	Conflict bool   `json:"conflict"`
}

// ListSystemsResponse wraps a list of systems.
type ListSystemsResponse struct {
	Systems []SystemEntry `json:"systems"`
}

// ListGamesResponse wraps a list of games.
type ListGamesResponse struct {
	Games []GameEntry `json:"games"`
}

// ListVersionsResponse wraps a list of versions.
type ListVersionsResponse struct {
	Versions []VersionInfo `json:"versions"`
}
