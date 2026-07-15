// Package storage defines persistence required by application services.
package storage

import (
	"context"
	"errors"
	"io"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

var ErrNotFound = errors.New("storage: not found")

// OmniSaveRepository persists save records without applying application rules.
type OmniSaveRepository interface {
	InsertOmniSave(ctx context.Context, save omnisave.OmniSave) error
	ListOmniSaves(ctx context.Context) ([]omnisave.OmniSave, error)
	GetOmniSave(ctx context.Context, id string) (*omnisave.OmniSave, error)
	UpdateOmniSaveDisplayName(ctx context.Context, id, displayName string) error
	DeleteOmniSave(ctx context.Context, id string) error

	InsertRevision(ctx context.Context, revision omnisave.Revision, payload io.Reader) error
	GetRevision(ctx context.Context, omnisaveID, revisionID string) (*omnisave.Revision, error)
	ListRevisions(ctx context.Context, omnisaveID string) ([]omnisave.Revision, error)
	DeleteRevision(ctx context.Context, omnisaveID, revisionID string) error

	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}

// CatalogRepository persists locally cached game catalog records.
type CatalogRepository interface {
	FindGameByFingerprint(ctx context.Context, fingerprint catalog.Fingerprint) (*catalog.Game, error)
	FindGameByProvider(ctx context.Context, provider, providerID string) (*catalog.Game, error)
	GetGame(ctx context.Context, id string) (*catalog.Game, error)
	ListGames(ctx context.Context) ([]catalog.Game, error)
	SaveGameMetadata(ctx context.Context, game catalog.Game) error
	SaveGame(ctx context.Context, game catalog.Game, rom catalog.GameROM) error
	SaveGameMedia(ctx context.Context, media catalog.GameMedia) error
	ClearGameMedia(ctx context.Context, gameID string) error
	GetGameMedia(ctx context.Context, gameID, mediaID string) (*catalog.GameMedia, error)
}

// Artifact describes content-addressed bytes stored outside metadata tables.
type Artifact struct {
	Format string
	SHA256 string
	Size   int64
}

// ArtifactStore persists and opens content-addressed bytes.
type ArtifactStore interface {
	StoreArtifact(ctx context.Context, artifact Artifact, payload io.Reader) error
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}

// Repository provides all persistence used by the server.
type Repository interface {
	OmniSaveRepository
	CatalogRepository
	ArtifactStore
}
