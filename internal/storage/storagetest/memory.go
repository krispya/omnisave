// Package storagetest provides storage implementations for tests.
package storagetest

import (
	"bytes"
	"context"
	"io"
	"slices"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

// MemoryRepository stores test data in memory without applying service rules.
type MemoryRepository struct {
	saves     map[string]omnisave.OmniSave
	revisions map[string][]omnisave.Revision
	blobs     map[string][]byte
	games     map[string]catalog.Game
	roms      map[string]catalog.GameROM
	media     map[string]catalog.GameMedia
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		saves:     make(map[string]omnisave.OmniSave),
		revisions: make(map[string][]omnisave.Revision),
		blobs:     make(map[string][]byte),
		games:     make(map[string]catalog.Game),
		roms:      make(map[string]catalog.GameROM),
		media:     make(map[string]catalog.GameMedia),
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

func (r *MemoryRepository) UpdateOmniSaveDisplayName(_ context.Context, id, displayName string) error {
	save, ok := r.saves[id]
	if !ok {
		return storage.ErrNotFound
	}
	save.DisplayName = displayName
	r.saves[id] = save
	return nil
}

func (r *MemoryRepository) DeleteOmniSave(_ context.Context, id string) error {
	if _, ok := r.saves[id]; !ok {
		return storage.ErrNotFound
	}
	deletedRevisions := r.revisions[id]
	delete(r.saves, id)
	delete(r.revisions, id)

	for _, deleted := range deletedRevisions {
		used := false
		for _, revisions := range r.revisions {
			for _, revision := range revisions {
				if revision.Artifact.SHA256 == deleted.Artifact.SHA256 {
					used = true
					break
				}
			}
			if used {
				break
			}
		}
		if !used {
			for _, media := range r.media {
				if media.SHA256 == deleted.Artifact.SHA256 {
					used = true
					break
				}
			}
		}
		if !used {
			delete(r.blobs, deleted.Artifact.SHA256)
		}
	}
	return nil
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

func (r *MemoryRepository) StoreArtifact(_ context.Context, artifact storage.Artifact, payload io.Reader) error {
	data, err := io.ReadAll(payload)
	if err != nil {
		return err
	}
	r.blobs[artifact.SHA256] = data
	return nil
}

func (r *MemoryRepository) FindGameByFingerprint(_ context.Context, fingerprint catalog.Fingerprint) (*catalog.Game, error) {
	for _, rom := range r.roms {
		if (fingerprint.SHA256 != "" && fingerprint.SHA256 == rom.SHA256) ||
			(fingerprint.SHA1 != "" && fingerprint.SHA1 == rom.SHA1) ||
			(fingerprint.MD5 != "" && fingerprint.MD5 == rom.MD5) ||
			(fingerprint.CRC32 != "" && fingerprint.CRC32 == rom.CRC32) {
			return r.game(rom.GameID)
		}
	}
	return nil, storage.ErrNotFound
}

func (r *MemoryRepository) FindGameByProvider(_ context.Context, provider, providerID string) (*catalog.Game, error) {
	for _, game := range r.games {
		if game.Provider == provider && game.ProviderID == providerID {
			return r.game(game.ID)
		}
	}
	return nil, storage.ErrNotFound
}

func (r *MemoryRepository) GetGame(_ context.Context, id string) (*catalog.Game, error) {
	return r.game(id)
}

func (r *MemoryRepository) ListGames(context.Context) ([]catalog.Game, error) {
	games := make([]catalog.Game, 0, len(r.games))
	for id := range r.games {
		game, _ := r.game(id)
		games = append(games, *game)
	}
	return games, nil
}

func (r *MemoryRepository) SaveGame(_ context.Context, game catalog.Game, rom catalog.GameROM) error {
	game.Media = nil
	r.games[game.ID] = game
	r.roms[rom.ID] = rom
	return nil
}

func (r *MemoryRepository) SaveGameMetadata(_ context.Context, game catalog.Game) error {
	game.Media = nil
	r.games[game.ID] = game
	return nil
}

func (r *MemoryRepository) SaveGameMedia(_ context.Context, media catalog.GameMedia) error {
	for id, existing := range r.media {
		if existing.GameID == media.GameID && existing.Kind == media.Kind && existing.Position == media.Position {
			media.ID = existing.ID
			delete(r.media, id)
			break
		}
	}
	r.media[media.ID] = media
	return nil
}

func (r *MemoryRepository) ClearGameMedia(_ context.Context, gameID string) error {
	for id, media := range r.media {
		if media.GameID == gameID {
			delete(r.media, id)
		}
	}
	return nil
}

func (r *MemoryRepository) GetGameMedia(_ context.Context, gameID, mediaID string) (*catalog.GameMedia, error) {
	media, ok := r.media[mediaID]
	if !ok || media.GameID != gameID {
		return nil, storage.ErrNotFound
	}
	return &media, nil
}

func (r *MemoryRepository) game(id string) (*catalog.Game, error) {
	game, ok := r.games[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	game.Media = make([]catalog.GameMedia, 0)
	for _, media := range r.media {
		if media.GameID == id {
			game.Media = append(game.Media, media)
		}
	}
	slices.SortFunc(game.Media, func(left, right catalog.GameMedia) int {
		if left.Kind != right.Kind {
			if left.Kind < right.Kind {
				return -1
			}
			return 1
		}
		return left.Position - right.Position
	})
	return &game, nil
}

var _ storage.Repository = (*MemoryRepository)(nil)
