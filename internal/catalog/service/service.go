// Package service implements the local game catalog.
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

const maxMediaSize = 10 << 20

type service struct {
	repository storage.CatalogRepository
	artifacts  storage.ArtifactStore
	provider   catalog.Provider
}

// New creates a catalog backed by local storage and an external provider.
func New(repository storage.CatalogRepository, artifacts storage.ArtifactStore, provider catalog.Provider) catalog.Service {
	return &service{repository: repository, artifacts: artifacts, provider: provider}
}

func (s *service) Identify(ctx context.Context, input catalog.IdentifyGame) (*catalog.Game, error) {
	fingerprint, valid := normalizeFingerprint(input.Fingerprint)
	if !valid || s.provider == nil {
		return nil, catalog.ErrInvalid
	}
	if game, err := s.repository.FindGameByFingerprint(ctx, fingerprint); err == nil {
		return game, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}

	match, err := s.provider.Identify(ctx, fingerprint)
	if err != nil {
		return nil, err
	}
	if match == nil || match.Provider == "" || match.ProviderID == "" || match.Title == "" {
		return nil, catalog.ErrUnavailable
	}
	if !mergeAndValidateROM(&match.ROM, fingerprint) {
		return nil, catalog.ErrUnavailable
	}

	gameID := strings.TrimSpace(input.GameID)
	if gameID == "" {
		if existing, findErr := s.repository.FindGameByProvider(ctx, match.Provider, match.ProviderID); findErr == nil {
			gameID = existing.ID
		} else if !errors.Is(findErr, storage.ErrNotFound) {
			return nil, findErr
		} else {
			gameID = uuid.NewString()
		}
	}
	game := gameFromMatch(gameID, match, "automatic")
	rom := catalog.GameROM{
		ID:         uuid.NewString(),
		GameID:     gameID,
		System:     match.ROM.System,
		Name:       match.ROM.Name,
		Region:     match.ROM.Region,
		Languages:  slices.Clone(match.ROM.Languages),
		Size:       match.ROM.Size,
		CRC32:      match.ROM.CRC32,
		MD5:        match.ROM.MD5,
		SHA1:       match.ROM.SHA1,
		SHA256:     match.ROM.SHA256,
		Source:     match.ROM.Source,
		SourceID:   match.ROM.ProviderID,
		Attributes: match.ROM.Attributes,
	}
	if err := s.repository.SaveGame(ctx, game, rom); err != nil {
		return nil, err
	}

	for _, reference := range match.Media {
		_ = s.cacheMedia(ctx, gameID, match.Provider, reference)
	}
	stored, err := s.repository.GetGame(ctx, gameID)
	return stored, translateStorageError(err)
}

func (s *service) Search(ctx context.Context, input catalog.SearchGames) ([]catalog.GameCandidate, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Platform = strings.TrimSpace(input.Platform)
	if input.Title == "" || s.provider == nil || input.Limit < 0 || input.Limit > 25 {
		return nil, catalog.ErrInvalid
	}
	if input.Limit == 0 {
		input.Limit = 10
	}
	candidates, err := s.provider.Search(ctx, input)
	if err != nil {
		return nil, err
	}
	if candidates == nil {
		candidates = []catalog.GameCandidate{}
	}
	return candidates, nil
}

func (s *service) Match(ctx context.Context, gameID string, input catalog.MatchGame) (*catalog.Game, error) {
	gameID = strings.TrimSpace(gameID)
	input.SelectionToken = strings.TrimSpace(input.SelectionToken)
	if gameID == "" || input.SelectionToken == "" || s.provider == nil {
		return nil, catalog.ErrInvalid
	}
	match, err := s.provider.Match(ctx, input.SelectionToken)
	if err != nil {
		return nil, err
	}
	if match == nil || match.Provider == "" || match.ProviderID == "" || match.Title == "" {
		return nil, catalog.ErrUnavailable
	}
	game := gameFromMatch(gameID, match, "manual")
	if err := s.repository.SaveGameMetadata(ctx, game); err != nil {
		return nil, err
	}
	if err := s.repository.ClearGameMedia(ctx, gameID); err != nil {
		return nil, err
	}
	for _, reference := range match.Media {
		_ = s.cacheMedia(ctx, gameID, match.Provider, reference)
	}
	stored, err := s.repository.GetGame(ctx, gameID)
	return stored, translateStorageError(err)
}

func (s *service) List(ctx context.Context) ([]catalog.Game, error) {
	games, err := s.repository.ListGames(ctx)
	return games, translateStorageError(err)
}

func (s *service) Get(ctx context.Context, id string) (*catalog.Game, error) {
	game, err := s.repository.GetGame(ctx, id)
	return game, translateStorageError(err)
}

func (s *service) OpenMedia(ctx context.Context, gameID, mediaID string) (*catalog.GameMedia, io.ReadCloser, error) {
	media, err := s.repository.GetGameMedia(ctx, gameID, mediaID)
	if err != nil {
		return nil, nil, translateStorageError(err)
	}
	payload, err := s.artifacts.OpenArtifact(ctx, media.SHA256)
	if err != nil {
		return nil, nil, translateStorageError(err)
	}
	return media, payload, nil
}

func (s *service) cacheMedia(ctx context.Context, gameID, provider string, reference catalog.MediaReference) error {
	if reference.ProviderID == "" || (reference.Kind != "cover" && reference.Kind != "screenshot") {
		return catalog.ErrInvalid
	}
	format, payload, err := s.provider.OpenMedia(ctx, reference)
	if err != nil {
		return err
	}
	defer payload.Close()
	format = strings.ToLower(strings.TrimSpace(strings.Split(format, ";")[0]))
	if mediaType, _, parseErr := mime.ParseMediaType(format); parseErr != nil || !strings.HasPrefix(mediaType, "image/") {
		return catalog.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(payload, maxMediaSize+1))
	if err != nil || len(data) == 0 || len(data) > maxMediaSize {
		return catalog.ErrUnavailable
	}
	detectedFormat := http.DetectContentType(data)
	if !strings.HasPrefix(detectedFormat, "image/") {
		return catalog.ErrUnavailable
	}
	format = detectedFormat
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	artifact := storage.Artifact{Format: format, SHA256: hash, Size: int64(len(data))}
	if err := s.artifacts.StoreArtifact(ctx, artifact, bytes.NewReader(data)); err != nil {
		return err
	}
	return s.repository.SaveGameMedia(ctx, catalog.GameMedia{
		ID:          uuid.NewString(),
		GameID:      gameID,
		Kind:        reference.Kind,
		Position:    reference.Position,
		Format:      format,
		SHA256:      hash,
		Size:        int64(len(data)),
		Provider:    provider,
		ProviderID:  reference.ProviderID,
		Attribution: reference.Attribution,
	})
}

func gameFromMatch(gameID string, match *catalog.ProviderMatch, method string) catalog.Game {
	metadata := maps.Clone(match.Metadata)
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["match_method"] = method
	return catalog.Game{
		ID:          gameID,
		Title:       match.Title,
		SortTitle:   match.SortTitle,
		Platform:    match.Platform,
		Publisher:   match.Publisher,
		Description: match.Description,
		Provider:    match.Provider,
		ProviderID:  match.ProviderID,
		Metadata:    metadata,
		RefreshedAt: time.Now().UTC(),
	}
}

func normalizeFingerprint(input catalog.Fingerprint) (catalog.Fingerprint, bool) {
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.CRC32 = strings.ToLower(strings.TrimSpace(input.CRC32))
	input.MD5 = strings.ToLower(strings.TrimSpace(input.MD5))
	input.SHA1 = strings.ToLower(strings.TrimSpace(input.SHA1))
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
	if input.Platform == "" {
		return catalog.Fingerprint{}, false
	}
	provided := false
	for value, size := range map[string]int{
		input.CRC32:  8,
		input.MD5:    32,
		input.SHA1:   40,
		input.SHA256: 64,
	} {
		if value == "" {
			continue
		}
		provided = true
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded)*2 != size {
			return catalog.Fingerprint{}, false
		}
	}
	return input, provided
}

func mergeAndValidateROM(rom *catalog.ROMMatch, fingerprint catalog.Fingerprint) bool {
	rom.CRC32 = strings.ToLower(rom.CRC32)
	rom.MD5 = strings.ToLower(rom.MD5)
	rom.SHA1 = strings.ToLower(rom.SHA1)
	rom.SHA256 = strings.ToLower(rom.SHA256)
	pairs := []struct {
		requested string
		actual    *string
	}{
		{fingerprint.CRC32, &rom.CRC32},
		{fingerprint.MD5, &rom.MD5},
		{fingerprint.SHA1, &rom.SHA1},
		{fingerprint.SHA256, &rom.SHA256},
	}
	matched := false
	for _, pair := range pairs {
		if pair.requested == "" {
			continue
		}
		if *pair.actual != "" && *pair.actual != pair.requested {
			return false
		}
		if *pair.actual == pair.requested {
			matched = true
		}
		if *pair.actual == "" {
			*pair.actual = pair.requested
		}
	}
	return matched
}

func translateStorageError(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return catalog.ErrNotFound
	}
	return err
}

var _ catalog.Service = (*service)(nil)
