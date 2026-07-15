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
)

// Fingerprint identifies normalized game media without transferring its contents.
type Fingerprint struct {
	Platform string `json:"platform"`
	CRC32    string `json:"crc32,omitempty"`
	MD5      string `json:"md5,omitempty"`
	SHA1     string `json:"sha1,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

// Game is OmniSave's durable view of a provider catalog entry.
type Game struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	SortTitle   string         `json:"sort_title,omitempty"`
	Platform    string         `json:"platform,omitempty"`
	Publisher   string         `json:"publisher,omitempty"`
	Description string         `json:"description,omitempty"`
	Provider    string         `json:"provider"`
	ProviderID  string         `json:"provider_id"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Media       []GameMedia    `json:"media"`
	RefreshedAt time.Time      `json:"refreshed_at"`
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

// IdentifyGame requests a catalog match for an already normalized fingerprint.
type IdentifyGame struct {
	GameID      string      `json:"game_id,omitempty"`
	Fingerprint Fingerprint `json:"fingerprint"`
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

// ProviderMatch is the provider-neutral result of identifying a fingerprint.
type ProviderMatch struct {
	Provider    string
	ProviderID  string
	Title       string
	SortTitle   string
	Platform    string
	Publisher   string
	Description string
	Metadata    map[string]any
	ROM         ROMMatch
	Media       []MediaReference
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
	Kind        string
	Position    int
	ProviderID  string
	Attribution string
}

// Provider identifies games and opens their catalog media.
type Provider interface {
	Identify(ctx context.Context, fingerprint Fingerprint) (*ProviderMatch, error)
	Search(ctx context.Context, input SearchGames) ([]GameCandidate, error)
	Match(ctx context.Context, selectionToken string) (*ProviderMatch, error)
	OpenMedia(ctx context.Context, reference MediaReference) (format string, payload io.ReadCloser, err error)
}

// Service is the application boundary for the local game catalog.
type Service interface {
	Identify(ctx context.Context, input IdentifyGame) (*Game, error)
	Search(ctx context.Context, input SearchGames) ([]GameCandidate, error)
	Match(ctx context.Context, gameID string, input MatchGame) (*Game, error)
	List(ctx context.Context) ([]Game, error)
	Get(ctx context.Context, id string) (*Game, error)
	OpenMedia(ctx context.Context, gameID, mediaID string) (*GameMedia, io.ReadCloser, error)
}
