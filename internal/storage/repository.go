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
var ErrArtifactUnavailable = errors.New("storage: artifact unavailable")

// ArtifactsUnavailable reports content that could not be proven available in
// the same transaction that attempted to reference it.
type ArtifactsUnavailable struct {
	SHA256 []string
}

func (e *ArtifactsUnavailable) Error() string { return ErrArtifactUnavailable.Error() }
func (e *ArtifactsUnavailable) Unwrap() error { return ErrArtifactUnavailable }

// CurrentRevisionConflict carries the actual current revision observed atomically.
type CurrentRevisionConflict struct {
	ActualCurrentRevisionID *string
}

func (e *CurrentRevisionConflict) Error() string { return ErrConflict.Error() }
func (e *CurrentRevisionConflict) Unwrap() error { return ErrConflict }

// Reasons a revision deletion is refused. Only a revision the graph no longer
// needs may go: nothing may build on it, point at it as current, or fork from it.
const (
	RevisionInUseCurrent    = "current"
	RevisionInUseChildren   = "children"
	RevisionInUseForkOrigin = "fork_origin"
)

// RevisionInUse reports a revision deletion refused because the node is still
// needed, and names why.
type RevisionInUse struct {
	Reason string
}

func (e *RevisionInUse) Error() string { return ErrConflict.Error() + ": revision in use: " + e.Reason }
func (e *RevisionInUse) Unwrap() error { return ErrConflict }

// OmnisaveRepository persists save records without applying application rules.
type OmnisaveRepository interface {
	InsertOmnisave(ctx context.Context, save omnisave.Omnisave) error
	ListOmnisaves(ctx context.Context) ([]omnisave.Omnisave, error)
	GetOmnisave(ctx context.Context, id string) (*omnisave.Omnisave, error)
	UpdateOmnisaveDisplayName(ctx context.Context, id, displayName string) error
	DeleteOmnisave(ctx context.Context, id string) error
	ForkOmnisave(ctx context.Context, save omnisave.Omnisave) error
	RestoreOmnisave(ctx context.Context, id, revisionID string, expectedCurrentRevisionID *string) error

	// RecordAchievements stores unlocks against a save, keeping the placement
	// an achievement was first given: an ID already recorded is left alone.
	// It returns only the achievements this call added.
	RecordAchievements(ctx context.Context, omnisaveID string, achievements []omnisave.Achievement) ([]omnisave.Achievement, error)
	// ListAchievements returns a save's achievements in unlock order.
	ListAchievements(ctx context.Context, omnisaveID string) ([]omnisave.Achievement, error)

	// CommitRevision inserts a revision after verifying the save's current
	// revision matches the caller's expectation. The new node becomes current
	// unless keepCurrent leaves the pointer where the check proved it — a
	// branch committed only to preserve content (FDR-005, decision 4).
	CommitRevision(ctx context.Context, expectedCurrentRevisionID *string, revision omnisave.Revision, keepCurrent bool) error
	// MigrateRevisionPaths renames every file path of the save's own
	// revisions from the `from` location to the `to` spelling — `from/rest`
	// becomes `to/rest` — refusing saves that share revisions with a fork
	// and lineages any of whose files speak another location
	// (omnisave.MigrationRefused). It reports how many revisions and files
	// were renamed.
	MigrateRevisionPaths(ctx context.Context, omnisaveID, from, to string) (revisions, files int, err error)
	GetRevision(ctx context.Context, omnisaveID, revisionID string) (*omnisave.Revision, error)
	ListRevisions(ctx context.Context, omnisaveID string) ([]omnisave.Revision, error)
	UpdateRevisionDisplayName(ctx context.Context, omnisaveID, revisionID, displayName string) error
	// UpdateRevisionLabel records the result of an explicitly requested
	// relabel, replacing the revision's current name and its source.
	UpdateRevisionLabel(ctx context.Context, omnisaveID, revisionID, displayName string) error
	// DeleteRevision removes one revision reachable through the named save.
	// Only a node the graph no longer needs may go — no children, not any
	// save's current revision, not any fork's origin — and RevisionInUse
	// reports which of those still holds.
	DeleteRevision(ctx context.Context, omnisaveID, revisionID string) error

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
	GetDevice(ctx context.Context, id string) (*catalog.Device, error)
	TrackGame(ctx context.Context, gameID string, record catalog.GameTracking) error
	UntrackGame(ctx context.Context, gameID, deviceID string, at time.Time) error
	ListGameProvenance(ctx context.Context, gameID string) ([]catalog.GameTracking, error)
	SaveGameMedia(ctx context.Context, media catalog.GameMedia) error
	ClearGameMedia(ctx context.Context, gameID string) error
	GetGameMedia(ctx context.Context, gameID, mediaID string) (*catalog.GameMedia, error)
}

// CredentialRecord is a credential with the hash used for authentication.
type CredentialRecord struct {
	access.Credential
	TokenHash string
}

// PairingRecord stores the hashed poll handle and pending single-use token.
type PairingRecord struct {
	access.PairingRequest
	HandleHash  string
	MintedToken string
}

// AccessRepository persists issued credentials and pairing requests.
type AccessRepository interface {
	InsertCredential(ctx context.Context, record CredentialRecord) error
	// InsertFirstCredential atomically stores only the first server credential.
	InsertFirstCredential(ctx context.Context, record CredentialRecord) error
	FindCredentialByTokenHash(ctx context.Context, tokenHash string) (*access.Credential, error)
	ListCredentials(ctx context.Context) ([]access.Credential, error)
	TouchCredential(ctx context.Context, id string, at time.Time) error
	RevokeCredential(ctx context.Context, id string, at time.Time) error

	InsertPairingRequest(ctx context.Context, record PairingRecord) error
	GetPairingRequest(ctx context.Context, id string) (*access.PairingRequest, error)
	ListPendingPairingRequests(ctx context.Context, now time.Time) ([]access.PairingRequest, error)
	ResolvePairingRequest(ctx context.Context, id string, status access.PairingStatus, credentialID, mintedToken string) error
	// TakePairingToken atomically reads and clears a single-use pairing token.
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
