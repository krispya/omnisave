// Package access models the credentials a server issues and the pairing that
// mints them (see FDR-006 and ADR-007).
package access

import "time"

// CredentialKind separates the things that hold a credential. The Dash is not
// privileged — it holds an issued credential like any Device — but it is worth
// telling apart in a list the owner reads.
type CredentialKind string

const (
	KindDevice CredentialKind = "device"
	KindDash   CredentialKind = "dash"
)

// Credential is one issued key as the owner sees it. The secret itself is
// never part of this record: the server keeps only a hash and cannot recover
// what it handed out.
type Credential struct {
	ID         string         `json:"id"`
	Kind       CredentialKind `json:"kind"`
	DeviceID   string         `json:"device_id,omitempty"`
	DeviceName string         `json:"device_name"`
	CreatedAt  time.Time      `json:"created_at"`
	LastUsedAt *time.Time     `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time     `json:"revoked_at,omitempty"`
}

// Revoked reports a credential that no longer authenticates anything.
func (c Credential) Revoked() bool { return c.RevokedAt != nil }

// PairingStatus is where a request stands. Expiry is not a status: a request
// that ran out of time is still pending and simply stops being offered.
type PairingStatus string

const (
	PairingPending  PairingStatus = "pending"
	PairingApproved PairingStatus = "approved"
	PairingDenied   PairingStatus = "denied"
)

// PairingRequest is a Device asking for access, as the Dash shows it. The code
// is here because the owner reads it: it is the only part of a request that
// ties it to the Device that sent it, since the name and identity are minted
// by the client and the address is worth what its network path is worth.
type PairingRequest struct {
	ID            string        `json:"id"`
	Code          string        `json:"code"`
	DeviceID      string        `json:"device_id"`
	DeviceName    string        `json:"device_name"`
	Platform      string        `json:"platform,omitempty"`
	SourceAddress string        `json:"source_address"`
	Status        PairingStatus `json:"status"`
	CreatedAt     time.Time     `json:"created_at"`
	ExpiresAt     time.Time     `json:"expires_at"`
}

// Expired reports a request that ran out of time before anyone approved it.
func (r PairingRequest) Expired(now time.Time) bool { return !now.Before(r.ExpiresAt) }

// RequestPairing is a client with no credential asking to pair, naming the
// Device identity it already self-identifies with.
type RequestPairing struct {
	DeviceID      string `json:"device_id"`
	DeviceName    string `json:"device_name"`
	Platform      string `json:"platform,omitempty"`
	SourceAddress string `json:"-"`
}

// PairingTicket is what the requesting client keeps: a code to display and a
// handle to poll with. The handle is returned exactly once, here.
type PairingTicket struct {
	Code      string    `json:"code"`
	Handle    string    `json:"handle"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CollectionStatus is the answer to one poll.
type CollectionStatus string

const (
	CollectionPending  CollectionStatus = "pending"
	CollectionApproved CollectionStatus = "approved"
	CollectionDenied   CollectionStatus = "denied"
	CollectionExpired  CollectionStatus = "expired"
)

// Collection is one poll's answer. The token is present only on the single
// approved collection that takes it.
type Collection struct {
	Status CollectionStatus `json:"status"`
	Token  string           `json:"token,omitempty"`
}

// IssuedCredential is a credential and the secret that goes with it, which the
// server can produce only at the moment it mints one.
type IssuedCredential struct {
	Credential Credential `json:"credential"`
	Token      string     `json:"token"`
}

// SignIn is a browser offering the owner's PIN in exchange for a credential
// of its own (ADR-010).
type SignIn struct {
	PIN           string `json:"pin"`
	Name          string `json:"name,omitempty"`
	SourceAddress string `json:"-"`
}

// ClaimServer is the first browser taking ownership, which is also where the
// owner's PIN is set: a claimed server nobody can sign in to would be a server
// with one way in and no second browser.
type ClaimServer struct {
	Name string `json:"name,omitempty"`
	PIN  string `json:"pin"`
}

// Principal is who a request authenticated as. The owner token authenticates
// without a credential — it is the bootstrap and the way back in — so a
// principal with no credential ID is the owner arriving directly.
type Principal struct {
	Owner        bool
	Kind         CredentialKind
	CredentialID string
	DeviceID     string
	Name         string
}

// OwnerPresent reports a request the owner is behind: the owner token, or a
// credential the Dash holds. A Device's credential is not one of these, which
// is what keeps approving another Device something only the owner can do
// (ADR-007).
func (p Principal) OwnerPresent() bool { return p.Owner || p.Kind == KindDash }
