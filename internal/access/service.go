package access

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("access: not found")
	ErrInvalid      = errors.New("access: invalid input")
	ErrUnauthorized = errors.New("access: unauthorized")
	ErrRateLimited  = errors.New("access: too many pairing requests")
	ErrNotPending   = errors.New("access: request is no longer pending")
	ErrClaimed      = errors.New("access: this server has already been claimed")
	ErrPIN          = errors.New("access: that is not the PIN")
	ErrNoPIN        = errors.New("access: this server has no PIN")
)

// LockedOut reports sign-in refused because too many PINs have been tried.
// The wait is what protects a four-digit PIN, so it is part of the answer
// rather than an implementation detail (ADR-010).
type LockedOut struct {
	RetryAfter time.Duration
}

func (e *LockedOut) Error() string { return ErrPIN.Error() }
func (e *LockedOut) Unwrap() error { return ErrPIN }

// Service authenticates, issues, and revokes server credentials.
type Service interface {
	// Authenticate resolves a bearer token to the principal behind it.
	Authenticate(ctx context.Context, token string) (*Principal, error)

	// RequestPairing records a client with no credential asking for one. It is
	// reachable without authentication, so it expires, is single use, and is
	// rate limited by source address.
	RequestPairing(ctx context.Context, input RequestPairing) (*PairingTicket, error)

	// Collect answers one poll from a waiting client. An approved request
	// gives up its token here exactly once.
	Collect(ctx context.Context, handle string) (*Collection, error)

	// ListPendingRequests returns the requests still worth showing: pending,
	// unexpired, and in the order they arrived.
	ListPendingRequests(ctx context.Context) ([]PairingRequest, error)

	// Approve mints a credential for the Device that asked and holds it for
	// that request's poller. Nothing else mints one.
	Approve(ctx context.Context, id string) error

	// Deny ends a request without minting anything.
	Deny(ctx context.Context, id string) error

	// ExchangeOwnerToken trades the owner token for a credential of the
	// caller's own, so a browser never stores the owner's secret.
	ExchangeOwnerToken(ctx context.Context, name string) (*IssuedCredential, error)

	// Claimable reports a server that has issued nothing and can therefore
	// still be claimed by the first browser to reach it (ADR-010).
	Claimable(ctx context.Context) (bool, error)

	// Claim mints the first credential and hands it to that browser, and sets
	// the owner PIN every later browser signs in with. It answers ErrClaimed
	// once anything has been issued, which is permanent.
	Claim(ctx context.Context, input ClaimServer) (*IssuedCredential, error)

	// HasPIN reports whether an owner PIN has been set, so a browser knows
	// whether to offer signing in.
	HasPIN(ctx context.Context) (bool, error)

	// SetPIN replaces the owner PIN. Holding a credential is what entitles a
	// caller to it, which is how a forgotten PIN is recovered.
	SetPIN(ctx context.Context, pin string) error

	// SignIn exchanges the owner PIN for a credential of the caller's own.
	SignIn(ctx context.Context, input SignIn) (*IssuedCredential, error)

	// ListCredentials returns every credential this server has issued,
	// including revoked ones, newest first.
	ListCredentials(ctx context.Context) ([]Credential, error)

	// Revoke withdraws one credential. Every other credential keeps working.
	Revoke(ctx context.Context, id string) error
}
