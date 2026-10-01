// Package catalog is the Library's Games (FDR-001, FDR-002).
//
// A Game is an ID the server owns. It accumulates identity Evidence —
// identifiers scoped to a namespace, such as steam.app 2560710, and
// fingerprints of exact content, such as a ROM's sha1 — and each piece of
// evidence belongs to at most one Game, so the same install always resolves
// to the same Game.
//
// Resolve turns a Device's evidence into its Game. Known evidence is answered
// locally; unknown evidence asks the catalog Providers, whose Claims add
// evidence, metadata, and media; evidence that points at two Games is refused
// rather than merged. Search and Match let a person choose the claim instead.
// Provenance records which Devices track each Game.
package catalog

import (
	"context"
	"errors"
	"io"
)

var (
	ErrNotFound    = errors.New("catalog: not found")
	ErrInvalid     = errors.New("catalog: invalid input")
	ErrUnavailable = errors.New("catalog: provider unavailable")
	ErrConflict    = errors.New("catalog: identity conflict")
)

// IdentityConflict reports evidence already held by different Games. The
// Games are never merged to resolve it (FDR-001, decision 5).
type IdentityConflict struct {
	GameIDs []string
}

func (e *IdentityConflict) Error() string { return ErrConflict.Error() }
func (e *IdentityConflict) Unwrap() error { return ErrConflict }

// Service is the Library's Games.
type Service interface {
	// Resolve returns the Game evidence identifies. Known evidence reuses its
	// Game and joins any new evidence to it. Unknown evidence asks the
	// providers for a claim, and creates a Game from the claim, or from the
	// evidence's title hint when no provider knows it (ResolutionCreated).
	// Evidence held by different Games answers *IdentityConflict.
	Resolve(ctx context.Context, evidence Evidence) (*Resolution, error)
	// Search lists candidates for a title from the first search provider that
	// finds any.
	Search(ctx context.Context, input SearchGames) ([]GameCandidate, error)
	// Match applies the claim behind a chosen candidate to the Game gameID,
	// creating it under that ID when the Library has none. The claim replaces
	// the Game's metadata and media; its evidence joins the Game's own.
	Match(ctx context.Context, gameID string, input MatchGame) (*Game, error)

	List(ctx context.Context) ([]Game, error)
	Get(ctx context.Context, id string) (*Game, error)
	// Delete removes a Game and everything that belongs to it: its media, its
	// Provenance, and every omnisave of the Game with its whole revision
	// history. Deletion is committed durably (ADR-014) and cannot be undone.
	Delete(ctx context.Context, id string) error
	OpenMedia(ctx context.Context, gameID, mediaID string) (*GameMedia, io.ReadCloser, error)

	// TrackGame records a registered Device's Provenance with a Game;
	// ErrNotFound when either is unknown.
	TrackGame(ctx context.Context, gameID, deviceID string, input TrackGame) error
	// UntrackGame marks a Device's Provenance untracked; the record remains.
	UntrackGame(ctx context.Context, gameID, deviceID string) error
}
