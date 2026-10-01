package catalog

import (
	"context"
	"io"
)

// Provider is an external catalog: Hasheous for ROMs, IGDB for everything
// else. Providers make Claims; they never own a Game (FDR-001, decision 5).
// ErrNotFound and ErrUnavailable mean the provider has nothing to add, and
// the service moves on to the next one.
type Provider interface {
	// Name is stamped onto stored media and selection tokens, so it must not
	// change between releases.
	Name() string
	// Resolve claims the game some evidence identifies. A claim must repeat
	// at least one identifier or fingerprint it was asked about.
	Resolve(ctx context.Context, evidence Evidence) (*Claim, error)
	// Search lists candidates for a title; Match redeems one candidate's
	// selection token as a Claim.
	Search(ctx context.Context, input SearchGames) ([]GameCandidate, error)
	Match(ctx context.Context, selectionToken string) (*Claim, error)
	OpenMedia(ctx context.Context, reference MediaReference) (format string, payload io.ReadCloser, err error)
}

// Claim is what a provider says a game is: more evidence for its identity,
// and the metadata and media that describe it. Source and Title are
// required.
type Claim struct {
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
	Media           []MediaReference
}

// MediaReference locates provider media before it is stored locally.
type MediaReference struct {
	Provider    string
	Kind        string
	Position    int
	ProviderID  string
	Attribution string
}

// SearchGames asks for candidates by title, optionally narrowed by platform.
type SearchGames struct {
	Title    string
	Platform string
	Limit    int
}

// GameCandidate is a search result a person can choose. Its selection token
// redeems the provider's claim through Match.
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

// MatchGame applies the candidate a selection token names.
type MatchGame struct {
	SelectionToken string `json:"selection_token"`
}
