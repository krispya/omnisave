// Package storagetest provides storage implementations for tests.
package storagetest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

// MemoryRepository stores test data in memory without applying service rules.
type MemoryRepository struct {
	// mu guards every map below. Services reach this repository from more
	// than one goroutine — the access service touches credentials in the
	// background — so every method takes the lock, and internal helpers
	// assume it is held.
	mu sync.Mutex

	saves        map[string]omnisave.Omnisave
	revisions    map[string][]omnisave.Revision
	achievements map[string][]omnisave.Achievement
	blobs        map[string][]byte
	games        map[string]catalog.Game
	roms         map[string]catalog.GameROM
	media        map[string]catalog.GameMedia
	devices      map[string]catalog.Device
	tracking     map[string]map[string]catalog.GameTracking

	credentials map[string]storage.CredentialRecord
	pairing     map[string]storage.PairingRecord
	settings    map[string]string
	ownerPIN    *storage.OwnerPIN
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		saves:        make(map[string]omnisave.Omnisave),
		revisions:    make(map[string][]omnisave.Revision),
		achievements: make(map[string][]omnisave.Achievement),
		blobs:        make(map[string][]byte),
		games:        make(map[string]catalog.Game),
		roms:         make(map[string]catalog.GameROM),
		media:        make(map[string]catalog.GameMedia),
		devices:      make(map[string]catalog.Device),
		credentials:  make(map[string]storage.CredentialRecord),
		pairing:      make(map[string]storage.PairingRecord),
		settings:     make(map[string]string),
		tracking:     make(map[string]map[string]catalog.GameTracking),
	}
}

func (r *MemoryRepository) InsertOmnisave(_ context.Context, save omnisave.Omnisave) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.saves[save.ID]; exists {
		return storage.ErrConflict
	}
	r.saves[save.ID] = save
	return nil
}

func (r *MemoryRepository) ListOmnisaves(context.Context) ([]omnisave.Omnisave, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]omnisave.Omnisave, 0, len(r.saves))
	for _, save := range r.saves {
		result = append(result, r.deriveCurrentRevisionCreatedAt(save))
	}
	return result, nil
}

func (r *MemoryRepository) GetOmnisave(_ context.Context, id string) (*omnisave.Omnisave, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	save, ok := r.saves[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	save = r.deriveCurrentRevisionCreatedAt(save)
	return &save, nil
}

// deriveCurrentRevisionCreatedAt mirrors the SQL repository: the selected
// revision's original creation and saved times, or the save's own creation
// time and no saved time when it has none.
func (r *MemoryRepository) deriveCurrentRevisionCreatedAt(save omnisave.Omnisave) omnisave.Omnisave {
	save.CurrentRevisionCreatedAt = save.CreatedAt
	save.CurrentRevisionSavedAt = nil
	if save.CurrentRevisionID == nil {
		return save
	}
	for _, revision := range r.allRevisions() {
		if revision.ID == *save.CurrentRevisionID {
			save.CurrentRevisionCreatedAt = revision.CreatedAt
			save.CurrentRevisionSavedAt = revision.SavedAt
		}
	}
	return save
}

func (r *MemoryRepository) UpdateOmnisaveDisplayName(_ context.Context, id, displayName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	save, ok := r.saves[id]
	if !ok {
		return storage.ErrNotFound
	}
	save.DisplayName = displayName
	r.saves[id] = save
	return nil
}

func (r *MemoryRepository) DeleteOmnisave(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deleteOmnisave(id)
}

func (r *MemoryRepository) deleteOmnisave(id string) error {
	if _, ok := r.saves[id]; !ok {
		return storage.ErrNotFound
	}
	delete(r.saves, id)
	delete(r.achievements, id)
	all := r.allRevisions()
	byID := make(map[string]omnisave.Revision, len(all))
	retained := make(map[string]bool)
	for _, revision := range all {
		byID[revision.ID] = revision
		if _, creatorSurvives := r.saves[revision.OmnisaveID]; creatorSurvives {
			retained[revision.ID] = true
		}
	}
	for _, save := range r.saves {
		if save.CurrentRevisionID != nil {
			retained[*save.CurrentRevisionID] = true
		}
		if save.ForkedFrom != nil {
			retained[save.ForkedFrom.RevisionID] = true
		}
	}
	for revisionID := range retained {
		for revision := byID[revisionID]; revision.ParentID != nil; revision = byID[*revision.ParentID] {
			if retained[*revision.ParentID] {
				break
			}
			retained[*revision.ParentID] = true
		}
	}
	for creator, revisions := range r.revisions {
		kept := revisions[:0]
		for _, revision := range revisions {
			if retained[revision.ID] {
				kept = append(kept, revision)
			}
		}
		if len(kept) == 0 {
			delete(r.revisions, creator)
		} else {
			r.revisions[creator] = kept
		}
	}
	r.pruneUnusedBlobs()
	return nil
}

// pruneUnusedBlobs drops content no surviving revision or media references,
// assuming the lock is held.
func (r *MemoryRepository) pruneUnusedBlobs() {
	usedArtifacts := make(map[string]bool)
	for _, revision := range r.allRevisions() {
		for _, file := range revision.Files {
			usedArtifacts[file.Artifact.SHA256] = true
		}
	}
	for _, media := range r.media {
		usedArtifacts[media.SHA256] = true
	}
	for hash := range r.blobs {
		if !usedArtifacts[hash] {
			delete(r.blobs, hash)
		}
	}
}

func (r *MemoryRepository) ForkOmnisave(_ context.Context, save omnisave.Omnisave) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.saves[save.ID]; exists {
		return storage.ErrConflict
	}
	if save.ForkedFrom == nil || save.CurrentRevisionID == nil ||
		*save.CurrentRevisionID != save.ForkedFrom.RevisionID {
		return storage.ErrNotFound
	}
	source, exists := r.saves[save.ForkedFrom.OmnisaveID]
	if !exists || source.GameID != save.GameID {
		return storage.ErrNotFound
	}
	if _, err := r.findRevision(source.ID, save.ForkedFrom.RevisionID); err != nil {
		return err
	}
	r.saves[save.ID] = save
	return nil
}

func (r *MemoryRepository) RestoreOmnisave(_ context.Context, id, revisionID string, expectedCurrentRevisionID *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	save, ok := r.saves[id]
	if !ok {
		return storage.ErrNotFound
	}
	if !equalStringPointers(save.CurrentRevisionID, expectedCurrentRevisionID) {
		return &storage.CurrentRevisionConflict{ActualCurrentRevisionID: copyStringPointer(save.CurrentRevisionID)}
	}
	if _, err := r.findRevision(id, revisionID); err != nil {
		return err
	}
	save.CurrentRevisionID = &revisionID
	r.saves[id] = save
	return nil
}

func (r *MemoryRepository) CommitRevision(_ context.Context, expectedCurrentRevisionID *string, revision omnisave.Revision, keepCurrent bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	save, ok := r.saves[revision.OmnisaveID]
	if !ok {
		return storage.ErrNotFound
	}
	if !equalStringPointers(save.CurrentRevisionID, expectedCurrentRevisionID) {
		return &storage.CurrentRevisionConflict{ActualCurrentRevisionID: copyStringPointer(save.CurrentRevisionID)}
	}
	r.revisions[revision.OmnisaveID] = append(r.revisions[revision.OmnisaveID], revision)
	if !keepCurrent {
		save.CurrentRevisionID = &revision.ID
	}
	r.saves[revision.OmnisaveID] = save
	// Marks waiting on this save now have the snapshot they were waiting for,
	// matching the SQL repository.
	for index, achievement := range r.achievements[revision.OmnisaveID] {
		if achievement.RevisionID == nil && achievement.UnlockedAt.Unix() <= revision.CreatedAt.Unix() {
			id := revision.ID
			r.achievements[revision.OmnisaveID][index].RevisionID = &id
		}
	}
	return nil
}

func (r *MemoryRepository) RecordAchievements(_ context.Context, saveID string, achievements []omnisave.Achievement) ([]omnisave.Achievement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.saves[saveID]; !ok {
		return nil, storage.ErrNotFound
	}
	recorded := make([]omnisave.Achievement, 0, len(achievements))
	for _, achievement := range achievements {
		if slices.ContainsFunc(r.achievements[saveID], func(known omnisave.Achievement) bool {
			return known.ID == achievement.ID
		}) {
			continue
		}
		r.achievements[saveID] = append(r.achievements[saveID], achievement)
		recorded = append(recorded, achievement)
	}
	return recorded, nil
}

func (r *MemoryRepository) ListAchievements(_ context.Context, saveID string) ([]omnisave.Achievement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.saves[saveID]; !ok {
		return nil, storage.ErrNotFound
	}
	achievements := slices.Clone(r.achievements[saveID])
	slices.SortFunc(achievements, func(left, right omnisave.Achievement) int {
		if !left.UnlockedAt.Equal(right.UnlockedAt) {
			return left.UnlockedAt.Compare(right.UnlockedAt)
		}
		return strings.Compare(left.ID, right.ID)
	})
	return achievements, nil
}

func (r *MemoryRepository) GetRevision(_ context.Context, saveID, revisionID string) (*omnisave.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// The save has to exist before its membership is consulted: shared-ancestry
	// revisions still name a deleted creator, and a deleted save's URLs must be
	// dead.
	if _, ok := r.saves[saveID]; !ok {
		return nil, storage.ErrNotFound
	}
	return r.findRevision(saveID, revisionID)
}

// findRevision resolves a revision through a save's membership, without
// checking the save itself; callers that need the save to exist check first.
func (r *MemoryRepository) findRevision(saveID, revisionID string) (*omnisave.Revision, error) {
	for _, revision := range r.memberRevisions(saveID) {
		if revision.ID == revisionID {
			copy := revision
			return &copy, nil
		}
	}
	return nil, storage.ErrNotFound
}

func (r *MemoryRepository) ListRevisions(_ context.Context, saveID string) ([]omnisave.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.saves[saveID]; !ok {
		return nil, storage.ErrNotFound
	}
	return slices.Clone(r.memberRevisions(saveID)), nil
}

func (r *MemoryRepository) UpdateRevisionDisplayName(_ context.Context, saveID, revisionID, displayName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.saves[saveID]; !exists {
		return storage.ErrNotFound
	}
	if _, err := r.findRevision(saveID, revisionID); err != nil {
		return err
	}
	for creator, revisions := range r.revisions {
		for index := range revisions {
			if revisions[index].ID == revisionID {
				revisions[index].DisplayName = displayName
				// Renaming is the human path, so it stamps the name manual,
				// matching the SQL repository.
				revisions[index].NameSource = omnisave.NameSourceManual
				r.revisions[creator] = revisions
				return nil
			}
		}
	}
	return storage.ErrNotFound
}

// DeleteRevision mirrors the SQL repository: only a node the graph no longer
// needs may go, and the refusal names the first reason that still holds.
func (r *MemoryRepository) DeleteRevision(_ context.Context, saveID, revisionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.saves[saveID]; !ok {
		return storage.ErrNotFound
	}
	if _, err := r.findRevision(saveID, revisionID); err != nil {
		return err
	}
	for _, save := range r.saves {
		if save.CurrentRevisionID != nil && *save.CurrentRevisionID == revisionID {
			return &storage.RevisionInUse{Reason: storage.RevisionInUseCurrent}
		}
	}
	for _, revision := range r.allRevisions() {
		if revision.ParentID != nil && *revision.ParentID == revisionID {
			return &storage.RevisionInUse{Reason: storage.RevisionInUseChildren}
		}
	}
	for _, save := range r.saves {
		if save.ForkedFrom != nil && save.ForkedFrom.RevisionID == revisionID {
			return &storage.RevisionInUse{Reason: storage.RevisionInUseForkOrigin}
		}
	}
	for creator, revisions := range r.revisions {
		kept := slices.DeleteFunc(revisions, func(revision omnisave.Revision) bool {
			return revision.ID == revisionID
		})
		if len(kept) == 0 {
			delete(r.revisions, creator)
		} else {
			r.revisions[creator] = kept
		}
	}
	// A mark placed on the deleted node goes back to waiting rather than
	// vanishing with it, matching the SQL repository's ON DELETE SET NULL.
	for saveID, achievements := range r.achievements {
		for index, achievement := range achievements {
			if achievement.RevisionID != nil && *achievement.RevisionID == revisionID {
				r.achievements[saveID][index].RevisionID = nil
			}
		}
	}
	r.pruneUnusedBlobs()
	return nil
}

func (r *MemoryRepository) allRevisions() []omnisave.Revision {
	var all []omnisave.Revision
	for _, revisions := range r.revisions {
		all = append(all, revisions...)
	}
	return all
}

func (r *MemoryRepository) memberRevisions(saveID string) []omnisave.Revision {
	all := r.allRevisions()
	byID := make(map[string]omnisave.Revision, len(all))
	members := make(map[string]bool)
	for _, revision := range all {
		byID[revision.ID] = revision
		if revision.OmnisaveID == saveID {
			members[revision.ID] = true
		}
	}
	if save, exists := r.saves[saveID]; exists {
		if save.CurrentRevisionID != nil {
			members[*save.CurrentRevisionID] = true
		}
		if save.ForkedFrom != nil {
			members[save.ForkedFrom.RevisionID] = true
		}
	}
	for id := range members {
		for current := byID[id]; current.ParentID != nil; current = byID[*current.ParentID] {
			if members[*current.ParentID] {
				break
			}
			members[*current.ParentID] = true
		}
	}
	result := make([]omnisave.Revision, 0, len(members))
	for _, revision := range all {
		if members[revision.ID] {
			result = append(result, revision)
		}
	}
	slices.SortFunc(result, func(left, right omnisave.Revision) int {
		if order := left.CreatedAt.Compare(right.CreatedAt); order != 0 {
			return order
		}
		return strings.Compare(left.ID, right.ID)
	})
	return result
}

func (r *MemoryRepository) OpenArtifact(_ context.Context, hash string) (io.ReadCloser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	sum := sha256.Sum256(data)
	if int64(len(data)) != artifact.Size || hex.EncodeToString(sum[:]) != artifact.SHA256 {
		return fmt.Errorf("%w: payload does not match descriptor", storage.ErrArtifactMismatch)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blobs[artifact.SHA256] = data
	return nil
}

func (r *MemoryRepository) StatArtifact(_ context.Context, hash string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, ok := r.blobs[hash]
	if !ok {
		return 0, storage.ErrNotFound
	}
	return int64(len(data)), nil
}

func equalStringPointers(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func copyStringPointer(source *string) *string {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}

func (r *MemoryRepository) FindGameByIdentifier(_ context.Context, identifier catalog.GameIdentifier) (*catalog.Game, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, game := range r.games {
		for _, candidate := range game.Identifiers {
			if candidate == identifier {
				return r.game(game.ID)
			}
		}
	}
	return nil, storage.ErrNotFound
}

func (r *MemoryRepository) FindGameByFingerprint(_ context.Context, fingerprint catalog.GameFingerprint) (*catalog.Game, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, game := range r.games {
		for _, candidate := range game.Fingerprints {
			if candidate == fingerprint {
				return r.game(game.ID)
			}
		}
	}
	return nil, storage.ErrNotFound
}

func (r *MemoryRepository) GetGame(_ context.Context, id string) (*catalog.Game, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.game(id)
}

func (r *MemoryRepository) ListGames(context.Context) ([]catalog.Game, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	games := make([]catalog.Game, 0, len(r.games))
	for id := range r.games {
		game, _ := r.game(id)
		games = append(games, *game)
	}
	return games, nil
}

func (r *MemoryRepository) SaveGame(_ context.Context, game catalog.Game, rom *catalog.GameROM) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.games {
		if existing.ID == game.ID {
			continue
		}
		for _, identifier := range game.Identifiers {
			if slices.Contains(existing.Identifiers, identifier) {
				return storage.ErrConflict
			}
		}
		for _, fingerprint := range game.Fingerprints {
			if slices.Contains(existing.Fingerprints, fingerprint) {
				return storage.ErrConflict
			}
		}
	}
	game.Media = nil
	r.games[game.ID] = game
	if rom != nil {
		r.roms[rom.ID] = *rom
	}
	return nil
}

func (r *MemoryRepository) DeleteGame(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.games[id]; !ok {
		return storage.ErrNotFound
	}
	for saveID, save := range r.saves {
		if save.GameID == id {
			r.deleteOmnisave(saveID)
		}
	}
	delete(r.games, id)
	delete(r.tracking, id)
	for mediaID, media := range r.media {
		if media.GameID == id {
			delete(r.media, mediaID)
		}
	}
	for romID, rom := range r.roms {
		if rom.GameID == id {
			delete(r.roms, romID)
		}
	}
	return nil
}

func (r *MemoryRepository) UpsertDevice(_ context.Context, device catalog.Device) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.devices[device.ID]; ok {
		device.CreatedAt = existing.CreatedAt
	}
	r.devices[device.ID] = device
	return nil
}

func (r *MemoryRepository) GetDevice(_ context.Context, id string) (*catalog.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	device, ok := r.devices[id]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &device, nil
}

func (r *MemoryRepository) TrackGame(_ context.Context, gameID string, record catalog.GameTracking) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.games[gameID]; !ok {
		return storage.ErrNotFound
	}
	if _, ok := r.devices[record.DeviceID]; !ok {
		return storage.ErrNotFound
	}
	records := r.tracking[gameID]
	if records == nil {
		records = make(map[string]catalog.GameTracking)
		r.tracking[gameID] = records
	}
	if existing, ok := records[record.DeviceID]; ok {
		record.FirstTrackedAt = existing.FirstTrackedAt
	}
	record.UntrackedAt = nil
	records[record.DeviceID] = record
	device := r.devices[record.DeviceID]
	device.LastSeenAt = record.LastSeenAt
	r.devices[record.DeviceID] = device
	return nil
}

func (r *MemoryRepository) UntrackGame(_ context.Context, gameID, deviceID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.tracking[gameID][deviceID]
	if !ok {
		return storage.ErrNotFound
	}
	record.UntrackedAt = &at
	r.tracking[gameID][deviceID] = record
	return nil
}

func (r *MemoryRepository) ListGameProvenance(_ context.Context, gameID string) ([]catalog.GameTracking, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listGameProvenance(gameID), nil
}

func (r *MemoryRepository) listGameProvenance(gameID string) []catalog.GameTracking {
	records := make([]catalog.GameTracking, 0, len(r.tracking[gameID]))
	for _, record := range r.tracking[gameID] {
		record.DeviceName = r.devices[record.DeviceID].Name
		records = append(records, record)
	}
	slices.SortFunc(records, func(left, right catalog.GameTracking) int {
		if !left.FirstTrackedAt.Equal(right.FirstTrackedAt) {
			if left.FirstTrackedAt.Before(right.FirstTrackedAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(left.DeviceID, right.DeviceID)
	})
	return records
}

func (r *MemoryRepository) SaveGameMedia(_ context.Context, media catalog.GameMedia) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, media := range r.media {
		if media.GameID == gameID {
			delete(r.media, id)
		}
	}
	return nil
}

func (r *MemoryRepository) GetGameMedia(_ context.Context, gameID, mediaID string) (*catalog.GameMedia, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	game.Provenance = r.listGameProvenance(id)
	return &game, nil
}

var _ storage.Repository = (*MemoryRepository)(nil)
