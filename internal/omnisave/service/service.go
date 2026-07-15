// Package service implements the OmniSave application service.
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

type service struct {
	repository storage.OmniSaveRepository
}

const maxDisplayNameLength = 100

// New creates an OmniSave service backed by repository.
func New(repository storage.OmniSaveRepository) omnisave.Service {
	return &service{repository: repository}
}

func (s *service) Create(ctx context.Context, input omnisave.CreateOmniSave) (*omnisave.OmniSave, error) {
	if input.GameID == "" {
		return nil, omnisave.ErrInvalid
	}
	displayName, valid := normalizeDisplayName(input.DisplayName)
	if !valid {
		return nil, omnisave.ErrInvalid
	}
	save := omnisave.OmniSave{
		ID:          uuid.NewString(),
		GameID:      input.GameID,
		DisplayName: displayName,
		CreatedAt:   time.Now().UTC(),
		Metadata:    cloneMap(input.Metadata),
	}
	if err := s.repository.InsertOmniSave(ctx, save); err != nil {
		return nil, translateError(err)
	}
	return &save, nil
}

func (s *service) List(ctx context.Context) ([]omnisave.OmniSave, error) {
	saves, err := s.repository.ListOmniSaves(ctx)
	return saves, translateError(err)
}

func (s *service) Get(ctx context.Context, id string) (*omnisave.OmniSave, error) {
	save, err := s.repository.GetOmniSave(ctx, id)
	return save, translateError(err)
}

func (s *service) Update(ctx context.Context, id string, input omnisave.UpdateOmniSave) (*omnisave.OmniSave, error) {
	if input.DisplayName == nil {
		return nil, omnisave.ErrInvalid
	}
	displayName, valid := normalizeDisplayName(*input.DisplayName)
	if !valid {
		return nil, omnisave.ErrInvalid
	}
	if err := s.repository.UpdateOmniSaveDisplayName(ctx, id, displayName); err != nil {
		return nil, translateError(err)
	}
	return s.Get(ctx, id)
}

func (s *service) Delete(ctx context.Context, id string) error {
	return translateError(s.repository.DeleteOmniSave(ctx, id))
}

func (s *service) AddRevision(ctx context.Context, saveID string, input omnisave.CreateRevision, payload io.Reader) (*omnisave.Revision, error) {
	if input.Format == "" || payload == nil {
		return nil, omnisave.ErrInvalid
	}
	if _, err := s.repository.GetOmniSave(ctx, saveID); err != nil {
		return nil, translateError(err)
	}
	seenParents := make(map[string]struct{}, len(input.ParentIDs))
	for _, parentID := range input.ParentIDs {
		if parentID == "" {
			return nil, omnisave.ErrInvalid
		}
		if _, duplicate := seenParents[parentID]; duplicate {
			return nil, omnisave.ErrInvalid
		}
		seenParents[parentID] = struct{}{}
		if _, err := s.repository.GetRevision(ctx, saveID, parentID); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return nil, omnisave.ErrInvalid
			}
			return nil, err
		}
	}

	data, err := io.ReadAll(payload)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	revision := omnisave.Revision{
		ID:         uuid.NewString(),
		OmniSaveID: saveID,
		ParentIDs:  append([]string{}, input.ParentIDs...),
		CreatedAt:  time.Now().UTC(),
		Artifact: omnisave.Artifact{
			Format: input.Format,
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(data)),
		},
		Metadata: cloneMap(input.Metadata),
	}
	if err := s.repository.InsertRevision(ctx, revision, bytes.NewReader(data)); err != nil {
		return nil, translateError(err)
	}
	return &revision, nil
}

func (s *service) GetRevision(ctx context.Context, saveID, revisionID string) (*omnisave.Revision, error) {
	revision, err := s.repository.GetRevision(ctx, saveID, revisionID)
	return revision, translateError(err)
}

func (s *service) ListRevisions(ctx context.Context, saveID string) ([]omnisave.Revision, error) {
	revisions, err := s.repository.ListRevisions(ctx, saveID)
	return revisions, translateError(err)
}

func (s *service) DeleteRevision(ctx context.Context, saveID, revisionID string) error {
	if _, err := s.repository.GetRevision(ctx, saveID, revisionID); err != nil {
		return translateError(err)
	}
	revisions, err := s.repository.ListRevisions(ctx, saveID)
	if err != nil {
		return translateError(err)
	}
	for _, revision := range revisions {
		if slices.Contains(revision.ParentIDs, revisionID) {
			return omnisave.ErrInUse
		}
	}
	return translateError(s.repository.DeleteRevision(ctx, saveID, revisionID))
}

func (s *service) OpenArtifact(ctx context.Context, hash string) (io.ReadCloser, error) {
	payload, err := s.repository.OpenArtifact(ctx, hash)
	return payload, translateError(err)
}

func translateError(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return omnisave.ErrNotFound
	}
	return err
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func normalizeDisplayName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, utf8.RuneCountInString(name) <= maxDisplayNameLength
}

var _ omnisave.Service = (*service)(nil)
