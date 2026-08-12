package service

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

const (
	pinLength     = 4
	pinIterations = 200_000

	// The PIN is four digits, so ten thousand guesses exhaust it. What makes
	// that safe is refusing to be hurried: a handful of tries, then a wait
	// that grows. At these numbers a full sweep takes years, and an owner who
	// fat-fingers their own PIN waits a minute (ADR-010).
	pinAttemptsBeforeLock = 5
	pinFirstLock          = time.Minute
	pinMaximumLock        = 15 * time.Minute
)

// attemptCounter tracks failed sign-ins for one bucket — a source address, or
// the server as a whole.
type attemptCounter struct {
	failures    int
	lockedUntil time.Time
}

func (c *attemptCounter) locked(now time.Time) bool { return now.Before(c.lockedUntil) }

// fail records a failure and locks once they accumulate, doubling the wait for
// every further run of them.
func (c *attemptCounter) fail(now time.Time) {
	c.failures++
	if c.failures%pinAttemptsBeforeLock != 0 {
		return
	}
	lock := pinFirstLock << (c.failures/pinAttemptsBeforeLock - 1)
	if lock > pinMaximumLock || lock <= 0 {
		lock = pinMaximumLock
	}
	c.lockedUntil = now.Add(lock)
}

func (c *attemptCounter) succeed() {
	c.failures = 0
	c.lockedUntil = time.Time{}
}

func (s *service) HasPIN(ctx context.Context) (bool, error) {
	_, err := s.repository.GetOwnerPIN(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// SetPIN stores the owner's PIN, replacing any earlier one. It is reachable
// only by something already holding a credential, which is what makes a
// forgotten PIN recoverable without involving the owner token.
func (s *service) SetPIN(ctx context.Context, pin string) error {
	pin, err := normalizePIN(pin)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hash, err := hashPIN(pin, salt, pinIterations)
	if err != nil {
		return err
	}
	// Clearing the throttle here keeps a deliberate PIN change from being
	// blocked by failures against the PIN it replaces.
	s.mu.Lock()
	s.signInAttempts = make(map[string]*attemptCounter)
	s.allSignInAttempts = attemptCounter{}
	s.mu.Unlock()

	return s.repository.SetOwnerPIN(ctx, storage.OwnerPIN{
		Salt:       base64.RawStdEncoding.EncodeToString(salt),
		Hash:       hash,
		Iterations: pinIterations,
		UpdatedAt:  s.now(),
	})
}

// SignIn exchanges the owner PIN for a credential and rate-limits failures per source and server.
func (s *service) SignIn(ctx context.Context, input access.SignIn) (*access.IssuedCredential, error) {
	stored, err := s.repository.GetOwnerPIN(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, access.ErrNoPIN
	}
	if err != nil {
		return nil, err
	}

	now := s.now()
	if wait := s.lockedFor(input.SourceAddress, now); wait > 0 {
		return nil, &access.LockedOut{RetryAfter: wait}
	}

	pin, err := normalizePIN(input.PIN)
	if err == nil {
		var candidate string
		salt, decodeErr := base64.RawStdEncoding.DecodeString(stored.Salt)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if candidate, err = hashPIN(pin, salt, stored.Iterations); err != nil {
			return nil, err
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(stored.Hash)) == 1 {
			s.recordSignIn(input.SourceAddress, true, now)
			return s.mintCredential(ctx, input.Name, s.repository.InsertCredential)
		}
	}

	s.recordSignIn(input.SourceAddress, false, now)
	if wait := s.lockedFor(input.SourceAddress, s.now()); wait > 0 {
		return nil, &access.LockedOut{RetryAfter: wait}
	}
	return nil, access.ErrPIN
}

// lockedFor reports how long this caller must wait, taking the longer of its
// own lock and the whole server's.
func (s *service) lockedFor(sourceAddress string, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	wait := time.Duration(0)
	if s.allSignInAttempts.locked(now) {
		wait = s.allSignInAttempts.lockedUntil.Sub(now)
	}
	if counter, ok := s.signInAttempts[sourceAddress]; ok && counter.locked(now) {
		if mine := counter.lockedUntil.Sub(now); mine > wait {
			wait = mine
		}
	}
	return wait
}

func (s *service) recordSignIn(sourceAddress string, succeeded bool, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	counter, ok := s.signInAttempts[sourceAddress]
	if !ok {
		counter = &attemptCounter{}
		s.signInAttempts[sourceAddress] = counter
	}
	if succeeded {
		counter.succeed()
		s.allSignInAttempts.succeed()
		return
	}
	counter.fail(now)
	s.allSignInAttempts.fail(now)
}

// normalizePIN accepts exactly four digits and nothing else — no spaces to
// trim into validity, no letters, no length the owner did not intend.
func normalizePIN(pin string) (string, error) {
	pin = strings.TrimSpace(pin)
	if len(pin) != pinLength {
		return "", fmt.Errorf("%w: the PIN is %d digits", access.ErrInvalid, pinLength)
	}
	for _, digit := range pin {
		if digit < '0' || digit > '9' {
			return "", fmt.Errorf("%w: the PIN is digits only", access.ErrInvalid)
		}
	}
	return pin, nil
}

func hashPIN(pin string, salt []byte, iterations int) (string, error) {
	key, err := pbkdf2.Key(sha256.New, pin, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}
