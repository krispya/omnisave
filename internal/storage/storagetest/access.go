package storagetest

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

func (r *MemoryRepository) InsertCredential(_ context.Context, record storage.CredentialRecord) error {
	r.credentials[record.ID] = record
	return nil
}

func (r *MemoryRepository) InsertFirstCredential(_ context.Context, record storage.CredentialRecord) error {
	if len(r.credentials) > 0 {
		return storage.ErrConflict
	}
	r.credentials[record.ID] = record
	return nil
}

func (r *MemoryRepository) FindCredentialByTokenHash(_ context.Context, tokenHash string) (*access.Credential, error) {
	for _, record := range r.credentials {
		if record.TokenHash == tokenHash {
			credential := record.Credential
			return &credential, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (r *MemoryRepository) ListCredentials(_ context.Context) ([]access.Credential, error) {
	credentials := make([]access.Credential, 0, len(r.credentials))
	for _, record := range r.credentials {
		credentials = append(credentials, record.Credential)
	}
	slices.SortFunc(credentials, func(left, right access.Credential) int {
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return right.CreatedAt.Compare(left.CreatedAt)
		}
		return cmp.Compare(right.ID, left.ID)
	})
	return credentials, nil
}

func (r *MemoryRepository) TouchCredential(_ context.Context, id string, at time.Time) error {
	record, ok := r.credentials[id]
	if !ok {
		return storage.ErrNotFound
	}
	record.LastUsedAt = &at
	r.credentials[id] = record
	return nil
}

func (r *MemoryRepository) RevokeCredential(_ context.Context, id string, at time.Time) error {
	record, ok := r.credentials[id]
	if !ok {
		return storage.ErrNotFound
	}
	if record.RevokedAt == nil {
		record.RevokedAt = &at
		r.credentials[id] = record
	}
	return nil
}

func (r *MemoryRepository) InsertPairingRequest(_ context.Context, record storage.PairingRecord) error {
	r.pairing[record.ID] = record
	return nil
}

func (r *MemoryRepository) GetPairingRequest(_ context.Context, id string) (*access.PairingRequest, error) {
	record, ok := r.pairing[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	request := record.PairingRequest
	return &request, nil
}

func (r *MemoryRepository) ListPendingPairingRequests(_ context.Context, now time.Time) ([]access.PairingRequest, error) {
	requests := []access.PairingRequest{}
	for _, record := range r.pairing {
		if record.Status == access.PairingPending && !record.Expired(now) {
			requests = append(requests, record.PairingRequest)
		}
	}
	slices.SortFunc(requests, func(left, right access.PairingRequest) int {
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Compare(right.CreatedAt)
		}
		return cmp.Compare(left.ID, right.ID)
	})
	return requests, nil
}

func (r *MemoryRepository) ResolvePairingRequest(
	_ context.Context, id string, status access.PairingStatus, credentialID, mintedToken string,
) error {
	record, ok := r.pairing[id]
	if !ok || record.Status != access.PairingPending {
		return storage.ErrNotFound
	}
	record.Status = status
	record.MintedToken = mintedToken
	r.pairing[id] = record
	return nil
}

func (r *MemoryRepository) TakePairingToken(_ context.Context, handleHash string, _ time.Time) (*storage.PairingRecord, error) {
	for id, record := range r.pairing {
		if record.HandleHash != handleHash {
			continue
		}
		taken := record
		if record.Status == access.PairingApproved && record.MintedToken != "" {
			record.MintedToken = ""
			r.pairing[id] = record
		}
		return &taken, nil
	}
	return nil, storage.ErrNotFound
}

func (r *MemoryRepository) CountRecentPairingRequests(_ context.Context, sourceAddress string, since time.Time) (int, error) {
	count := 0
	for _, record := range r.pairing {
		if record.SourceAddress == sourceAddress && !record.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (r *MemoryRepository) DeleteExpiredPairingRequests(_ context.Context, before time.Time) error {
	for id, record := range r.pairing {
		if record.CreatedAt.Before(before) {
			delete(r.pairing, id)
		}
	}
	return nil
}

func (r *MemoryRepository) GetOwnerPIN(_ context.Context) (*storage.OwnerPIN, error) {
	if r.ownerPIN == nil {
		return nil, storage.ErrNotFound
	}
	pin := *r.ownerPIN
	return &pin, nil
}

func (r *MemoryRepository) SetOwnerPIN(_ context.Context, pin storage.OwnerPIN) error {
	r.ownerPIN = &pin
	return nil
}

func (r *MemoryRepository) GetOwnerSetting(_ context.Context, key string) (string, error) {
	value, ok := r.settings[key]
	if !ok {
		return "", storage.ErrNotFound
	}
	return value, nil
}

func (r *MemoryRepository) SetOwnerSetting(_ context.Context, key, value string, _ time.Time) error {
	r.settings[key] = value
	return nil
}
