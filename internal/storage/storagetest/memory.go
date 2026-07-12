// Package storagetest provides storage implementations for tests.
package storagetest

import (
	"bytes"
	"context"
	"io"
	"slices"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

// MemoryRepository stores test data in memory without applying service rules.
type MemoryRepository struct {
	saves     map[string]omnisave.OmniSave
	revisions map[string][]omnisave.Revision
	blobs     map[string][]byte
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		saves:     make(map[string]omnisave.OmniSave),
		revisions: make(map[string][]omnisave.Revision),
		blobs:     make(map[string][]byte),
	}
}

func (r *MemoryRepository) InsertOmniSave(_ context.Context, save omnisave.OmniSave) error {
	r.saves[save.ID] = save
	return nil
}

func (r *MemoryRepository) ListOmniSaves(context.Context) ([]omnisave.OmniSave, error) {
	result := make([]omnisave.OmniSave, 0, len(r.saves))
	for _, save := range r.saves {
		result = append(result, save)
	}
	return result, nil
}

func (r *MemoryRepository) GetOmniSave(_ context.Context, id string) (*omnisave.OmniSave, error) {
	save, ok := r.saves[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &save, nil
}

func (r *MemoryRepository) InsertRevision(_ context.Context, revision omnisave.Revision, payload io.Reader) error {
	data, err := io.ReadAll(payload)
	if err != nil {
		return err
	}
	r.revisions[revision.OmniSaveID] = append(r.revisions[revision.OmniSaveID], revision)
	r.blobs[revision.Artifact.SHA256] = data
	return nil
}

func (r *MemoryRepository) GetRevision(_ context.Context, saveID, revisionID string) (*omnisave.Revision, error) {
	for _, revision := range r.revisions[saveID] {
		if revision.ID == revisionID {
			copy := revision
			return &copy, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (r *MemoryRepository) ListRevisions(_ context.Context, saveID string) ([]omnisave.Revision, error) {
	if _, ok := r.saves[saveID]; !ok {
		return nil, storage.ErrNotFound
	}
	return slices.Clone(r.revisions[saveID]), nil
}

func (r *MemoryRepository) DeleteRevision(_ context.Context, saveID, revisionID string) error {
	for i, revision := range r.revisions[saveID] {
		if revision.ID == revisionID {
			r.revisions[saveID] = slices.Delete(r.revisions[saveID], i, i+1)
			return nil
		}
	}
	return storage.ErrNotFound
}

func (r *MemoryRepository) OpenArtifact(_ context.Context, hash string) (io.ReadCloser, error) {
	data, ok := r.blobs[hash]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

var _ storage.Repository = (*MemoryRepository)(nil)
