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
var ErrConflict = errors.New("storage: conflict")
var ErrArtifactMismatch = errors.New("storage: artifact mismatch")

// HeadConflict carries the actual head observed during an atomic commit.
type HeadConflict struct {
	ActualHeadID *string
}

func (e *HeadConflict) Error() string { return ErrConflict.Error() }
func (e *HeadConflict) Unwrap() error { return ErrConflict }

// OmnisaveRepository persists save records without applying application rules.
type OmnisaveRepository interface {
	InsertOmnisave(ctx context.Context, save omnisave.Omnisave) error
	ListOmnisaves(ctx context.Context) ([]omnisave.Omnisave, error)
	GetOmnisave(ctx context.Context, id string) (*omnisave.Omnisave, error)
	UpdateOmnisaveDisplayName(ctx context.Context, id, displayName string) error
	DeleteOmnisave(ctx context.Context, id string) error
	ForkOmnisave(ctx context.Context, save omnisave.Omnisave, initial omnisave.Revision) error

	CommitRevision(ctx context.Context, expectedHeadID *string, revision omnisave.Revision) error
	GetRevision(ctx context.Context, omnisaveID, revisionID string) (*omnisave.Revision, error)
	ListRevisions(ctx context.Context, omnisaveID string) ([]omnisave.Revision, error)

	StoreArtifact(ctx context.Context, artifact Artifact, payload io.Reader) error
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
	StatArtifact(ctx context.Context, sha256 string) (int64, error)
}

// CatalogRepository persists locally cached game catalog records.
type CatalogRepository interface {
	FindGameByIdentifier(ctx context.Context, identifier catalog.GameIdentifier) (*catalog.Game, error)
	FindGameByFingerprint(ctx context.Context, fingerprint catalog.GameFingerprint) (*catalog.Game, error)
	GetGame(ctx context.Context, id string) (*catalog.Game, error)
	ListGames(ctx context.Context) ([]catalog.Game, error)
	SaveGame(ctx context.Context, game catalog.Game, rom *catalog.GameROM) error
	DeleteGame(ctx context.Context, id string) error
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
	StatArtifact(ctx context.Context, sha256 string) (int64, error)
}

// Repository provides all persistence used by the server.
type Repository interface {
	OmnisaveRepository
	CatalogRepository
	ArtifactStore
}
