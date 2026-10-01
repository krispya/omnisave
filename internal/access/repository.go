package access

import (
	"context"
	"time"
)

// CredentialRecord is a Credential with the hash used to authenticate it. The
// token itself is never stored.
type CredentialRecord struct {
	Credential
	TokenHash string
}

// PairingRecord stores the hashed poll handle and the pending single-use
// token an approval minted for it.
type PairingRecord struct {
	PairingRequest
	HandleHash  string
	MintedToken string
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

// Repository persists issued credentials, pairing requests, and the owner
// PIN. Unknown records answer ErrNotFound.
type Repository interface {
	InsertCredential(ctx context.Context, record CredentialRecord) error
	// InsertFirstCredential atomically stores the server's first credential,
	// and answers ErrClaimed once any credential has been issued.
	InsertFirstCredential(ctx context.Context, record CredentialRecord) error
	FindCredentialByTokenHash(ctx context.Context, tokenHash string) (*Credential, error)
	ListCredentials(ctx context.Context) ([]Credential, error)
	TouchCredential(ctx context.Context, id string, at time.Time) error
	RevokeCredential(ctx context.Context, id string, at time.Time) error

	InsertPairingRequest(ctx context.Context, record PairingRecord) error
	GetPairingRequest(ctx context.Context, id string) (*PairingRequest, error)
	ListPendingPairingRequests(ctx context.Context, now time.Time) ([]PairingRequest, error)
	ResolvePairingRequest(ctx context.Context, id string, status PairingStatus, credentialID, mintedToken string) error
	// TakePairingToken atomically reads and clears a single-use pairing token.
	TakePairingToken(ctx context.Context, handleHash string, now time.Time) (*PairingRecord, error)
	CountRecentPairingRequests(ctx context.Context, sourceAddress string, since time.Time) (int, error)
	DeleteExpiredPairingRequests(ctx context.Context, before time.Time) error

	// GetOwnerPIN answers ErrNoPIN while no PIN has been set.
	GetOwnerPIN(ctx context.Context) (*OwnerPIN, error)
	SetOwnerPIN(ctx context.Context, pin OwnerPIN) error
}
