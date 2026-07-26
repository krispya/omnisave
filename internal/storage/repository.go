// Package storage defines persistence required by application services.
package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
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
	UpsertDevice(ctx context.Context, device catalog.Device) error
	TrackGame(ctx context.Context, gameID string, record catalog.GameTracking) error
	UntrackGame(ctx context.Context, gameID, deviceID string, at time.Time) error
	ListGameProvenance(ctx context.Context, gameID string) ([]catalog.GameTracking, error)
	SaveGameMedia(ctx context.Context, media catalog.GameMedia) error
	ClearGameMedia(ctx context.Context, gameID string) error
	GetGameMedia(ctx context.Context, gameID, mediaID string) (*catalog.GameMedia, error)
}

// CredentialRecord is a credential as it is stored, carrying the hash that
// authentication looks up.
//
// The secret-carrying shapes live here rather than beside the API types they
// embed. A hash and a minted token have no business in a package that answers
// HTTP: keeping them one import away from the handlers is what stops them from
// being serialized by a writeJSON that meant to send the credential itself.
type CredentialRecord struct {
	access.Credential
	TokenHash string
}

// PairingRecord is a pairing request as it is stored. It carries the two
// secrets the API never returns together: the hashed poll handle, which is
// what collects the credential, and the minted token, which is held only
// between approval and the one collection that takes it.
type PairingRecord struct {
	access.PairingRequest
	HandleHash  string
	MintedToken string
}

// AccessRepository persists issued credentials and pairing requests.
type AccessRepository interface {
	InsertCredential(ctx context.Context, record CredentialRecord) error
	// InsertFirstCredential stores a credential only while the server has
	// none, and answers ErrConflict once it has one. Claiming a server is a
	// race between whoever reaches it first, so "only the first" has to be
	// decided by the write rather than by a read before it (ADR-010).
	InsertFirstCredential(ctx context.Context, record CredentialRecord) error
	FindCredentialByTokenHash(ctx context.Context, tokenHash string) (*access.Credential, error)
	ListCredentials(ctx context.Context) ([]access.Credential, error)
	TouchCredential(ctx context.Context, id string, at time.Time) error
	RevokeCredential(ctx context.Context, id string, at time.Time) error

	InsertPairingRequest(ctx context.Context, record PairingRecord) error
	GetPairingRequest(ctx context.Context, id string) (*access.PairingRequest, error)
	ListPendingPairingRequests(ctx context.Context, now time.Time) ([]access.PairingRequest, error)
	ResolvePairingRequest(ctx context.Context, id string, status access.PairingStatus, credentialID, mintedToken string) error
	// TakePairingToken reads a request by its handle hash and, when one is
	// waiting there, takes the minted token in the same step. Collecting a
	// credential has to be single use, so the read and the clear are one
	// operation rather than two a second poller could interleave with.
	TakePairingToken(ctx context.Context, handleHash string, now time.Time) (*PairingRecord, error)
	CountRecentPairingRequests(ctx context.Context, sourceAddress string, since time.Time) (int, error)
	DeleteExpiredPairingRequests(ctx context.Context, before time.Time) error

	GetOwnerPIN(ctx context.Context) (*OwnerPIN, error)
	SetOwnerPIN(ctx context.Context, pin OwnerPIN) error
}

// OwnerPIN is the stored form of the owner's PIN: a salted, slow hash and the
// cost it was computed at, so the cost can be raised later without stranding
// PINs set before the change.
type OwnerPIN struct {
	Salt       string
	Hash       string
	Iterations int
	UpdatedAt  time.Time
}

// SettingsRepository persists owner settings, the small tier of configuration
// that belongs to the owner rather than the deployment (ADR-008).
type SettingsRepository interface {
	GetOwnerSetting(ctx context.Context, key string) (string, error)
	SetOwnerSetting(ctx context.Context, key, value string, at time.Time) error
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
	AccessRepository
	SettingsRepository
}
