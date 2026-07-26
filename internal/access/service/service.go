// Package service issues credentials and runs the pairing that mints them.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

const (
	// A request lives long enough to walk to another room and no longer.
	pairingLifetime = 5 * time.Minute
	// Resolved and expired requests are kept past their life so the rate limit
	// still counts them; nothing reads them after that.
	pairingRetention = time.Hour
	rateLimitWindow  = 10 * time.Minute
	rateLimitCount   = 10
	// Last-used is a fact the owner reads, not an audit log, so one write a
	// minute per credential is enough and keeps authentication cheap.
	touchInterval = time.Minute

	codeLength = 6
	// No I, L, O, U, 0 or 1: a person retypes this from across the room.
	codeAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"
)

type service struct {
	repository storage.AccessRepository
	ownerToken string
	now        func() time.Time

	mu          sync.Mutex
	lastTouched map[string]time.Time
	// Failed sign-ins, per source address and across the server. A
	// four-digit PIN is guessable; being unhurried is what makes it safe.
	signInAttempts    map[string]*attemptCounter
	allSignInAttempts attemptCounter
}

// New creates the access service. The owner token stays deployment
// configuration (ADR-003); here it is the credential that bootstraps the
// first issued one and the way back in when none of them work.
func New(repository storage.AccessRepository, ownerToken string) access.Service {
	return NewWithClock(repository, ownerToken, time.Now)
}

// NewWithClock creates the service with a caller-supplied clock, so a test can
// walk a pairing request past its expiry without waiting out the minutes.
func NewWithClock(repository storage.AccessRepository, ownerToken string, now func() time.Time) access.Service {
	return &service{
		repository:     repository,
		ownerToken:     ownerToken,
		now:            now,
		lastTouched:    make(map[string]time.Time),
		signInAttempts: make(map[string]*attemptCounter),
	}
}

func (s *service) Authenticate(ctx context.Context, token string) (*access.Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, access.ErrUnauthorized
	}
	if s.ownerToken != "" &&
		subtle.ConstantTimeCompare([]byte(token), []byte(s.ownerToken)) == 1 {
		return &access.Principal{Owner: true, Name: "Owner token"}, nil
	}

	credential, err := s.repository.FindCredentialByTokenHash(ctx, hashSecret(token))
	if errors.Is(err, storage.ErrNotFound) {
		return nil, access.ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if credential.Revoked() {
		return nil, access.ErrUnauthorized
	}
	s.touch(ctx, credential.ID)
	return &access.Principal{
		Kind:         credential.Kind,
		CredentialID: credential.ID,
		DeviceID:     credential.DeviceID,
		Name:         credential.DeviceName,
	}, nil
}

// touch records that a credential was used, at most once a minute, and off the
// request's own path. Last-used is a fact the owner reads later; nothing in the
// answer depends on it, and the write would otherwise take the database's one
// connection mid-request. A failure is not worth failing a request the
// credential was entitled to make.
func (s *service) touch(ctx context.Context, credentialID string) {
	now := s.now()
	s.mu.Lock()
	last, seen := s.lastTouched[credentialID]
	if seen && now.Sub(last) < touchInterval {
		s.mu.Unlock()
		return
	}
	s.lastTouched[credentialID] = now
	s.mu.Unlock()

	go func() {
		_ = s.repository.TouchCredential(context.WithoutCancel(ctx), credentialID, now)
	}()
}

func (s *service) RequestPairing(ctx context.Context, input access.RequestPairing) (*access.PairingTicket, error) {
	deviceID := strings.TrimSpace(input.DeviceID)
	deviceName := strings.TrimSpace(input.DeviceName)
	if deviceID == "" || deviceName == "" {
		return nil, fmt.Errorf("%w: device id and name are required", access.ErrInvalid)
	}
	if len(deviceID) > 200 || len(deviceName) > 200 || len(input.Platform) > 200 {
		return nil, fmt.Errorf("%w: device identity is too long", access.ErrInvalid)
	}

	now := s.now()
	// Old rows are swept here rather than on a timer: pairing is the only
	// thing that creates them, so it is the only thing that has to tidy up.
	_ = s.repository.DeleteExpiredPairingRequests(ctx, now.Add(-pairingRetention))

	recent, err := s.repository.CountRecentPairingRequests(ctx, input.SourceAddress, now.Add(-rateLimitWindow))
	if err != nil {
		return nil, err
	}
	if recent >= rateLimitCount {
		return nil, access.ErrRateLimited
	}

	handle, err := newSecret()
	if err != nil {
		return nil, err
	}
	code, err := s.mintCode(ctx)
	if err != nil {
		return nil, err
	}
	record := storage.PairingRecord{
		PairingRequest: access.PairingRequest{
			ID:            uuid.NewString(),
			Code:          code,
			DeviceID:      deviceID,
			DeviceName:    deviceName,
			Platform:      strings.TrimSpace(input.Platform),
			SourceAddress: input.SourceAddress,
			Status:        access.PairingPending,
			CreatedAt:     now,
			ExpiresAt:     now.Add(pairingLifetime),
		},
		HandleHash: hashSecret(handle),
	}
	if err := s.repository.InsertPairingRequest(ctx, record); err != nil {
		return nil, err
	}
	return &access.PairingTicket{Code: code, Handle: handle, ExpiresAt: record.ExpiresAt}, nil
}

// mintCode finds a code no live request is already showing, so the one the
// owner reads names exactly one request.
func (s *service) mintCode(ctx context.Context) (string, error) {
	pending, err := s.repository.ListPendingPairingRequests(ctx, s.now())
	if err != nil {
		return "", err
	}
	taken := make(map[string]bool, len(pending))
	for _, request := range pending {
		taken[request.Code] = true
	}
	for attempt := 0; attempt < 20; attempt++ {
		code, err := newCode()
		if err != nil {
			return "", err
		}
		if !taken[code] {
			return code, nil
		}
	}
	return "", errors.New("could not mint a unique pairing code")
}

func (s *service) Collect(ctx context.Context, handle string) (*access.Collection, error) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return nil, fmt.Errorf("%w: pairing handle is required", access.ErrInvalid)
	}
	record, err := s.repository.TakePairingToken(ctx, hashSecret(handle), s.now())
	if err != nil {
		return nil, translateError(err)
	}

	switch record.Status {
	case access.PairingApproved:
		if record.MintedToken == "" {
			// Approved, and the credential has already been collected. The
			// handle is spent; nothing here answers twice.
			return nil, access.ErrNotFound
		}
		return &access.Collection{Status: access.CollectionApproved, Token: record.MintedToken}, nil
	case access.PairingDenied:
		return &access.Collection{Status: access.CollectionDenied}, nil
	default:
		if record.Expired(s.now()) {
			return &access.Collection{Status: access.CollectionExpired}, nil
		}
		return &access.Collection{Status: access.CollectionPending}, nil
	}
}

func (s *service) ListPendingRequests(ctx context.Context) ([]access.PairingRequest, error) {
	return s.repository.ListPendingPairingRequests(ctx, s.now())
}

func (s *service) Approve(ctx context.Context, id string) error {
	request, err := s.repository.GetPairingRequest(ctx, id)
	if err != nil {
		return translateError(err)
	}
	now := s.now()
	if request.Status != access.PairingPending || request.Expired(now) {
		return access.ErrNotPending
	}

	token, err := newSecret()
	if err != nil {
		return err
	}
	credential := storage.CredentialRecord{
		Credential: access.Credential{
			ID:         uuid.NewString(),
			Kind:       access.KindDevice,
			DeviceID:   request.DeviceID,
			DeviceName: request.DeviceName,
			CreatedAt:  now,
		},
		TokenHash: hashSecret(token),
	}
	if err := s.repository.InsertCredential(ctx, credential); err != nil {
		return err
	}
	// The token rests on the request only between here and the one poll that
	// takes it, which is what lets approval be the thing that mints.
	return s.repository.ResolvePairingRequest(ctx, id, access.PairingApproved, credential.ID, token)
}

func (s *service) Deny(ctx context.Context, id string) error {
	request, err := s.repository.GetPairingRequest(ctx, id)
	if err != nil {
		return translateError(err)
	}
	if request.Status != access.PairingPending {
		return access.ErrNotPending
	}
	return s.repository.ResolvePairingRequest(ctx, id, access.PairingDenied, "", "")
}

func (s *service) ExchangeOwnerToken(ctx context.Context, name string) (*access.IssuedCredential, error) {
	return s.mintCredential(ctx, name, s.repository.InsertCredential)
}

// mintCredential issues a browser's own credential. Trading the owner token
// and claiming an unclaimed server produce the same thing and differ only in
// how the write is allowed to fail.
func (s *service) mintCredential(
	ctx context.Context, name string, store func(context.Context, storage.CredentialRecord) error,
) (*access.IssuedCredential, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Dash"
	}
	if len(name) > 200 {
		return nil, fmt.Errorf("%w: name is too long", access.ErrInvalid)
	}
	token, err := newSecret()
	if err != nil {
		return nil, err
	}
	record := storage.CredentialRecord{
		Credential: access.Credential{
			ID:         uuid.NewString(),
			Kind:       access.KindDash,
			DeviceName: name,
			CreatedAt:  s.now(),
		},
		TokenHash: hashSecret(token),
	}
	if err := store(ctx, record); err != nil {
		return nil, err
	}
	return &access.IssuedCredential{Credential: record.Credential, Token: token}, nil
}

// Claimable reports whether this server has issued nothing at all. Revoked
// credentials still count as issued: a server whose only Device was revoked
// has an owner, and is not up for grabs again.
func (s *service) Claimable(ctx context.Context) (bool, error) {
	credentials, err := s.repository.ListCredentials(ctx)
	if err != nil {
		return false, err
	}
	return len(credentials) == 0, nil
}

// Claim mints the first credential for the browser that got here first
// (ADR-010). Whether that browser is entitled to is decided before this — by
// the network it arrived from — because this layer cannot see an address.
//
// Two browsers claiming at once is exactly the case that must not issue two
// credentials, so the write itself decides: the second one loses.
func (s *service) Claim(ctx context.Context, input access.ClaimServer) (*access.IssuedCredential, error) {
	// Validate the PIN before minting anything: a claim that took the server
	// and then failed on a typo would leave it owned and unreachable.
	if _, err := normalizePIN(input.PIN); err != nil {
		return nil, err
	}
	issued, err := s.mintCredential(ctx, input.Name, s.repository.InsertFirstCredential)
	if errors.Is(err, storage.ErrConflict) {
		return nil, access.ErrClaimed
	}
	if err != nil {
		return nil, err
	}
	if err := s.SetPIN(ctx, input.PIN); err != nil {
		return nil, err
	}
	return issued, nil
}

func (s *service) ListCredentials(ctx context.Context) ([]access.Credential, error) {
	return s.repository.ListCredentials(ctx)
}

func (s *service) Revoke(ctx context.Context, id string) error {
	return translateError(s.repository.RevokeCredential(ctx, id, s.now()))
}

// translateError maps storage failures onto this package's errors, the way
// every other service package does.
func translateError(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return access.ErrNotFound
	}
	return err
}

// hashSecret stores what a secret proves without storing the secret. The
// inputs here are full-entropy random strings this server generated, so a
// single SHA-256 is the right shape — there is nothing to guess at.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func newSecret() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func newCode() (string, error) {
	buffer := make([]byte, codeLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	// The alphabet divides 256 evenly enough that the modulo bias is below
	// what matters for a secret whose job is to name a request, not hide it.
	code := make([]byte, codeLength)
	for index, value := range buffer {
		code[index] = codeAlphabet[int(value)%len(codeAlphabet)]
	}
	return string(code), nil
}
