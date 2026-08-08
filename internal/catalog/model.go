// Package catalog defines locally cached game identity and metadata.
package catalog

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrNotFound    = errors.New("catalog: not found")
	ErrInvalid     = errors.New("catalog: invalid input")
	ErrUnavailable = errors.New("catalog: provider unavailable")
	ErrConflict    = errors.New("catalog: identity conflict")
)

// IdentityConflict reports evidence already assigned to different Games.
type IdentityConflict struct {
	GameIDs []string
}

func (e *IdentityConflict) Error() string { return ErrConflict.Error() }
func (e *IdentityConflict) Unwrap() error { return ErrConflict }

// GameIdentifier is an ID scoped to an external namespace.
type GameIdentifier struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

// GameFingerprint identifies exact game content without transferring it.
type GameFingerprint struct {
	Platform  string `json:"platform"`
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// Game is one server-owned canonical catalog record.
type Game struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	SortTitle       string            `json:"sort_title,omitempty"`
	Platform        string            `json:"platform,omitempty"`
	PlatformCompany string            `json:"platform_company,omitempty"`
	Publisher       string            `json:"publisher,omitempty"`
	Description     string            `json:"description,omitempty"`
	MetadataSource  string            `json:"metadata_source"`
	Identifiers     []GameIdentifier  `json:"identifiers"`
	Fingerprints    []GameFingerprint `json:"fingerprints"`
	Metadata        map[string]any    `json:"metadata,omitempty"`
	Media           []GameMedia       `json:"media"`
	Provenance      []GameTracking    `json:"provenance"`
	RefreshedAt     time.Time         `json:"refreshed_at"`
}

// Device is one self-identified client installation known to the server.
type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Platform   string    `json:"platform,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// RegisterDevice is a Device's self-reported identity.
type RegisterDevice struct {
	Name     string `json:"name"`
	Platform string `json:"platform,omitempty"`
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
	// Playing is presence, not provenance: the Device recently reported a
	// live session of this Game. It is stitched in when a Game is served
	// and never persists.
	Playing           bool       `json:"playing,omitempty"`
	PlayingReportedAt *time.Time `json:"playing_reported_at,omitempty"`
}

// TrackGame reports that a Device tracks a Game.
type TrackGame struct {
	Adapter   string `json:"adapter,omitempty"`
	Installed bool   `json:"installed"`
}

// GameROM records the exact provider signature associated with a game.
type GameROM struct {
	ID         string            `json:"id"`
	GameID     string            `json:"game_id"`
	System     string            `json:"system,omitempty"`
	Name       string            `json:"name,omitempty"`
	Region     string            `json:"region,omitempty"`
	Languages  []string          `json:"languages,omitempty"`
	Size       int64             `json:"size,omitempty"`
	CRC32      string            `json:"crc32,omitempty"`
	MD5        string            `json:"md5,omitempty"`
	SHA1       string            `json:"sha1,omitempty"`
	SHA256     string            `json:"sha256,omitempty"`
	Source     string            `json:"source"`
	SourceID   string            `json:"source_id"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// GameMedia describes provider media stored as a local artifact.
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

// ResolveGame supplies strong identities and weak descriptive hints.
type ResolveGame struct {
	Identifiers  []GameIdentifier  `json:"identifiers,omitempty"`
	Fingerprints []GameFingerprint `json:"fingerprints,omitempty"`
	TitleHint    string            `json:"title_hint,omitempty"`
	PlatformHint string            `json:"platform_hint,omitempty"`
}

// ResolutionStatus describes whether resolution reused or created a Game.
type ResolutionStatus string

const (
	ResolutionExisting ResolutionStatus = "existing"
	ResolutionCreated  ResolutionStatus = "created"
)

// GameResolution contains the canonical Game selected for supplied evidence.
type GameResolution struct {
	Game   Game             `json:"game"`
	Status ResolutionStatus `json:"status"`
}

// SearchGames describes a title search against the configured catalog provider.
type SearchGames struct {
	Title    string
	Platform string
	Limit    int
}

// GameCandidate is a provider match a user can select without a local fingerprint.
type GameCandidate struct {
	Provider       string `json:"provider"`
	ProviderID     string `json:"provider_id"`
	Title          string `json:"title"`
	Edition        string `json:"edition,omitempty"`
	Platform       string `json:"platform,omitempty"`
	Publisher      string `json:"publisher,omitempty"`
	Year           string `json:"year,omitempty"`
	Region         string `json:"region,omitempty"`
	Language       string `json:"language,omitempty"`
	SelectionToken string `json:"selection_token"`
}

// MatchGame applies a provider-owned search selection to a local game.
type MatchGame struct {
	SelectionToken string `json:"selection_token"`
}

// ProviderMatch is a provider's identity and metadata claim.
type ProviderMatch struct {
	Source          string
	Identifiers     []GameIdentifier
	Fingerprints    []GameFingerprint
	Title           string
	SortTitle       string
	Platform        string
	PlatformCompany string
	Publisher       string
	Description     string
	Metadata        map[string]any
	ROM             ROMMatch
	Media           []MediaReference
}

// ROMMatch is an exact ROM signature returned by a catalog provider.
type ROMMatch struct {
	ProviderID string
	System     string
	Name       string
	Region     string
	Languages  []string
	Size       int64
	CRC32      string
	MD5        string
	SHA1       string
	SHA256     string
	Source     string
	Attributes map[string]string
}

// MediaReference locates provider media before it is stored locally.
type MediaReference struct {
	Provider    string
	Kind        string
	Position    int
	ProviderID  string
	Attribution string
}

// Provider resolves evidence, supports manual search, and opens catalog media.
type Provider interface {
	Name() string
	Resolve(ctx context.Context, evidence ResolveGame) (*ProviderMatch, error)
	Search(ctx context.Context, input SearchGames) ([]GameCandidate, error)
	Match(ctx context.Context, selectionToken string) (*ProviderMatch, error)
	OpenMedia(ctx context.Context, reference MediaReference) (format string, payload io.ReadCloser, err error)
}

// Service is the application boundary for the local game catalog.
type Service interface {
	Resolve(ctx context.Context, input ResolveGame) (*GameResolution, error)
	Search(ctx context.Context, input SearchGames) ([]GameCandidate, error)
	Match(ctx context.Context, gameID string, input MatchGame) (*Game, error)
	List(ctx context.Context) ([]Game, error)
	Get(ctx context.Context, id string) (*Game, error)
	Delete(ctx context.Context, id string) error
	OpenMedia(ctx context.Context, gameID, mediaID string) (*GameMedia, io.ReadCloser, error)
	RegisterDevice(ctx context.Context, id string, input RegisterDevice) (*Device, error)
	GetDevice(ctx context.Context, id string) (*Device, error)
	TrackGame(ctx context.Context, gameID, deviceID string, input TrackGame) error
	UntrackGame(ctx context.Context, gameID, deviceID string) error
}
