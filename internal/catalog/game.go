package catalog

import "time"

// Game is one server-owned record in the Library: its identity evidence, what
// the claim that described it said, its media, and its Provenance.
type Game struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	SortTitle       string `json:"sort_title,omitempty"`
	Platform        string `json:"platform,omitempty"`
	PlatformCompany string `json:"platform_company,omitempty"`
	Publisher       string `json:"publisher,omitempty"`
	Description     string `json:"description,omitempty"`
	// MetadataSource names the provider whose claim described the Game, or
	// "client" for a provisional Game named by a Device's own hints.
	MetadataSource string            `json:"metadata_source"`
	Identifiers    []GameIdentifier  `json:"identifiers"`
	Fingerprints   []GameFingerprint `json:"fingerprints"`
	// Metadata is provider-specific detail such as release_year and genres,
	// plus match_method: provisional, automatic, or manual.
	Metadata    map[string]any `json:"metadata,omitempty"`
	Media       []GameMedia    `json:"media"`
	Provenance  []GameTracking `json:"provenance"`
	RefreshedAt time.Time      `json:"refreshed_at"`
}

// GameMedia is provider media stored as a local artifact, so the Library
// renders without reaching the provider again.
type GameMedia struct {
	ID          string `json:"id"`
	GameID      string `json:"game_id"`
	Kind        string `json:"kind"`
	Position    int    `json:"position"`
	Format      string `json:"format"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	Provider    string `json:"provider"`
	ProviderID  string `json:"provider_id"`
	Attribution string `json:"attribution,omitempty"`
}

// GameTracking is one Provenance record: a Device's history with a Game.
// Untracking annotates the record; only deleting the Game removes it.
type GameTracking struct {
	DeviceID       string     `json:"device_id"`
	DeviceName     string     `json:"device_name"`
	Adapter        string     `json:"adapter,omitempty"`
	Installed      bool       `json:"installed"`
	FirstTrackedAt time.Time  `json:"first_tracked_at"`
	LastSeenAt     time.Time  `json:"last_seen_at"`
	UntrackedAt    *time.Time `json:"untracked_at,omitempty"`
}

// TrackGame reports that a Device tracks a Game.
type TrackGame struct {
	Adapter   string `json:"adapter,omitempty"`
	Installed bool   `json:"installed"`
}
