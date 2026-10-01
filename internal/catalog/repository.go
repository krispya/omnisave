package catalog

import (
	"context"
	"time"
)

// Repository persists the Library's Games. Lookups of unknown records answer
// ErrNotFound.
type Repository interface {
	FindGameByIdentifier(ctx context.Context, identifier GameIdentifier) (*Game, error)
	FindGameByFingerprint(ctx context.Context, fingerprint GameFingerprint) (*Game, error)
	GetGame(ctx context.Context, id string) (*Game, error)
	ListGames(ctx context.Context) ([]Game, error)
	// SaveGame stores the Game and claims all of its evidence for it, in one
	// transaction: ErrConflict when another Game already holds any of it.
	SaveGame(ctx context.Context, game Game) error
	// DeleteGame removes the Game with its media, Provenance, and every
	// omnisave and revision of the Game, in one committed deletion.
	DeleteGame(ctx context.Context, id string) error

	// TrackGame upserts one Provenance record; ErrNotFound unless both the
	// Game and the Device exist.
	TrackGame(ctx context.Context, gameID string, record GameTracking) error
	UntrackGame(ctx context.Context, gameID, deviceID string, at time.Time) error
	ListGameProvenance(ctx context.Context, gameID string) ([]GameTracking, error)

	SaveGameMedia(ctx context.Context, media GameMedia) error
	ClearGameMedia(ctx context.Context, gameID string) error
	GetGameMedia(ctx context.Context, gameID, mediaID string) (*GameMedia, error)
}
