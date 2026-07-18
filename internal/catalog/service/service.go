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

func (s *service) Resolve(ctx context.Context, input catalog.ResolveGame) (*catalog.GameResolution, error) {
	evidence, valid := normalizeEvidence(input)
	if !valid {
		return nil, catalog.ErrInvalid
	}
	if existing, err := s.findExisting(ctx, evidence); err != nil {
		return nil, err
	} else if existing != nil {
		mergeGameEvidence(existing, evidence)
		if err := s.repository.SaveGame(ctx, *existing, nil); err != nil {
			return nil, translateStorageError(err)
		}
		stored, err := s.repository.GetGame(ctx, existing.ID)
		if err != nil {
			return nil, translateStorageError(err)
		}
		return &catalog.GameResolution{Game: *stored, Status: catalog.ResolutionExisting}, nil
	}

	match, err := s.resolveProvider(ctx, evidence)
	if err != nil {
		return nil, err
	}
	combined := evidence
	if match != nil {
		providerEvidence, valid := evidenceFromMatch(match)
		if !valid || !providerSupportsEvidence(providerEvidence, evidence) {
			return nil, catalog.ErrUnavailable
		}
		combined.Identifiers = append(combined.Identifiers, providerEvidence.Identifiers...)
		combined.Fingerprints = append(combined.Fingerprints, providerEvidence.Fingerprints...)
		combined, valid = normalizeEvidence(combined)
		if !valid {
			return nil, catalog.ErrUnavailable
		}
	}

	existing, err := s.findExisting(ctx, combined)
	if err != nil {
		return nil, err
	}
	status := catalog.ResolutionExisting
	gameID := ""
	if existing != nil {
		gameID = existing.ID
	} else {
		gameID = uuid.NewString()
		status = catalog.ResolutionCreated
	}
	if existing != nil {
		combined.Identifiers = append(combined.Identifiers, existing.Identifiers...)
		combined.Fingerprints = append(combined.Fingerprints, existing.Fingerprints...)
		combined, _ = normalizeEvidence(combined)
	}
	game, rom, valid := gameFromResolution(gameID, combined, match)
	if !valid {
		return nil, catalog.ErrNotFound
	}
	if err := s.repository.SaveGame(ctx, game, rom); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			if raced, findErr := s.findExisting(ctx, combined); findErr == nil && raced != nil {
				mergeGameEvidence(raced, combined)
				if saveErr := s.repository.SaveGame(ctx, *raced, nil); saveErr != nil {
					return nil, translateStorageError(saveErr)
				}
				stored, getErr := s.repository.GetGame(ctx, raced.ID)
				if getErr != nil {
					return nil, translateStorageError(getErr)
				}
				return &catalog.GameResolution{Game: *stored, Status: catalog.ResolutionExisting}, nil
			}
		}
		return nil, translateStorageError(err)
	}
	if match != nil {
		for _, reference := range match.Media {
			_ = s.cacheMedia(ctx, gameID, match.Source, reference)
		}
	}
	stored, err := s.repository.GetGame(ctx, gameID)
	if err != nil {
		return nil, translateStorageError(err)
	}
	return &catalog.GameResolution{Game: *stored, Status: status}, nil
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
	providerEvidence, valid := evidenceFromMatch(match)
	if !valid {
		return nil, catalog.ErrUnavailable
	}
	if existing, findErr := s.findExisting(ctx, providerEvidence); findErr != nil {
		return nil, findErr
	} else if existing != nil && existing.ID != gameID {
		return nil, &catalog.IdentityConflict{GameIDs: []string{existing.ID, gameID}}
	}
	if target, getErr := s.repository.GetGame(ctx, gameID); getErr == nil {
		providerEvidence.Identifiers = append(providerEvidence.Identifiers, target.Identifiers...)
		providerEvidence.Fingerprints = append(providerEvidence.Fingerprints, target.Fingerprints...)
		providerEvidence, _ = normalizeEvidence(providerEvidence)
	} else if !errors.Is(getErr, storage.ErrNotFound) {
		return nil, getErr
	}
	game, rom, valid := gameFromResolution(gameID, providerEvidence, match)
	if !valid {
		return nil, catalog.ErrUnavailable
	}
	game.Metadata["match_method"] = "manual"
	if err := s.repository.SaveGame(ctx, game, rom); err != nil {
		return nil, err
	}
	if err := s.repository.ClearGameMedia(ctx, gameID); err != nil {
		return nil, err
	}
	for _, reference := range match.Media {
		_ = s.cacheMedia(ctx, gameID, match.Source, reference)
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

func (s *service) resolveProvider(ctx context.Context, evidence catalog.ResolveGame) (*catalog.ProviderMatch, error) {
	if s.provider == nil {
		return nil, nil
	}
	match, err := s.provider.Resolve(ctx, evidence)
	if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, catalog.ErrUnavailable) {
		return nil, nil
	}
	return match, err
}

func (s *service) findExisting(ctx context.Context, evidence catalog.ResolveGame) (*catalog.Game, error) {
	found := make(map[string]*catalog.Game)
	for _, identifier := range evidence.Identifiers {
		game, err := s.repository.FindGameByIdentifier(ctx, identifier)
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		found[game.ID] = game
	}
	for _, fingerprint := range evidence.Fingerprints {
		game, err := s.repository.FindGameByFingerprint(ctx, fingerprint)
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		found[game.ID] = game
	}
	if len(found) > 1 {
		ids := make([]string, 0, len(found))
		for id := range found {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		return nil, &catalog.IdentityConflict{GameIDs: ids}
	}
	for _, game := range found {
		return game, nil
	}
	return nil, nil
}

func gameFromResolution(gameID string, evidence catalog.ResolveGame, match *catalog.ProviderMatch) (catalog.Game, *catalog.GameROM, bool) {
	game := catalog.Game{
		ID:             gameID,
		Title:          evidence.TitleHint,
		Platform:       evidence.PlatformHint,
		MetadataSource: "client",
		Identifiers:    slices.Clone(evidence.Identifiers),
		Fingerprints:   slices.Clone(evidence.Fingerprints),
		Metadata:       map[string]any{"match_method": "provisional"},
		RefreshedAt:    time.Now().UTC(),
	}
	if match == nil {
		return game, nil, game.Title != ""
	}
	game.Title = strings.TrimSpace(match.Title)
	game.SortTitle = strings.TrimSpace(match.SortTitle)
	game.Platform = strings.TrimSpace(match.Platform)
	game.Publisher = strings.TrimSpace(match.Publisher)
	game.Description = strings.TrimSpace(match.Description)
	game.MetadataSource = strings.TrimSpace(match.Source)
	game.Metadata = maps.Clone(match.Metadata)
	if game.Metadata == nil {
		game.Metadata = make(map[string]any)
	}
	game.Metadata["match_method"] = "automatic"
	var rom *catalog.GameROM
	if match.ROM.Source != "" && match.ROM.ProviderID != "" {
		rom = &catalog.GameROM{
			ID:         uuid.NewString(),
			GameID:     gameID,
			System:     match.ROM.System,
			Name:       match.ROM.Name,
			Region:     match.ROM.Region,
			Languages:  slices.Clone(match.ROM.Languages),
			Size:       match.ROM.Size,
			CRC32:      strings.ToLower(match.ROM.CRC32),
			MD5:        strings.ToLower(match.ROM.MD5),
			SHA1:       strings.ToLower(match.ROM.SHA1),
			SHA256:     strings.ToLower(match.ROM.SHA256),
			Source:     match.ROM.Source,
			SourceID:   match.ROM.ProviderID,
			Attributes: match.ROM.Attributes,
		}
	}
	return game, rom, game.Title != "" && game.MetadataSource != ""
}

func evidenceFromMatch(match *catalog.ProviderMatch) (catalog.ResolveGame, bool) {
	if match == nil || strings.TrimSpace(match.Source) == "" || strings.TrimSpace(match.Title) == "" {
		return catalog.ResolveGame{}, false
	}
	evidence := catalog.ResolveGame{
		Identifiers:  slices.Clone(match.Identifiers),
		Fingerprints: slices.Clone(match.Fingerprints),
		TitleHint:    match.Title,
		PlatformHint: match.Platform,
	}
	for algorithm, value := range map[string]string{
		"crc32":  match.ROM.CRC32,
		"md5":    match.ROM.MD5,
		"sha1":   match.ROM.SHA1,
		"sha256": match.ROM.SHA256,
	} {
		if value != "" {
			evidence.Fingerprints = append(evidence.Fingerprints, catalog.GameFingerprint{
				Platform: match.Platform, Algorithm: algorithm, Value: value,
			})
		}
	}
	return normalizeEvidence(evidence)
}

func providerSupportsEvidence(provided, requested catalog.ResolveGame) bool {
	for _, identifier := range requested.Identifiers {
		for _, candidate := range provided.Identifiers {
			if identifier == candidate {
				return true
			}
		}
	}
	for _, fingerprint := range requested.Fingerprints {
		for _, candidate := range provided.Fingerprints {
			if fingerprint.Algorithm == candidate.Algorithm && fingerprint.Value == candidate.Value {
				return true
			}
		}
	}
	return false
}

func normalizeEvidence(input catalog.ResolveGame) (catalog.ResolveGame, bool) {
	input.TitleHint = strings.TrimSpace(input.TitleHint)
	input.PlatformHint = strings.TrimSpace(input.PlatformHint)
	if len(input.TitleHint) > 200 || len(input.PlatformHint) > 100 {
		return catalog.ResolveGame{}, false
	}
	identifiers := make([]catalog.GameIdentifier, 0, len(input.Identifiers))
	seenIdentifiers := make(map[catalog.GameIdentifier]bool)
	for _, identifier := range input.Identifiers {
		normalized, valid := normalizeIdentifier(identifier)
		if !valid {
			return catalog.ResolveGame{}, false
		}
		if !seenIdentifiers[normalized] {
			seenIdentifiers[normalized] = true
			identifiers = append(identifiers, normalized)
		}
	}
	fingerprints := make([]catalog.GameFingerprint, 0, len(input.Fingerprints))
	seenFingerprints := make(map[catalog.GameFingerprint]bool)
	for _, fingerprint := range input.Fingerprints {
		normalized, valid := normalizeFingerprint(fingerprint)
		if !valid {
			return catalog.ResolveGame{}, false
		}
		if !seenFingerprints[normalized] {
			seenFingerprints[normalized] = true
			fingerprints = append(fingerprints, normalized)
		}
	}
	if len(identifiers) == 0 && len(fingerprints) == 0 {
		return catalog.ResolveGame{}, false
	}
	slices.SortFunc(identifiers, func(left, right catalog.GameIdentifier) int {
		if left.Namespace != right.Namespace {
			return strings.Compare(left.Namespace, right.Namespace)
		}
		return strings.Compare(left.Value, right.Value)
	})
	slices.SortFunc(fingerprints, func(left, right catalog.GameFingerprint) int {
		if left.Platform != right.Platform {
			return strings.Compare(left.Platform, right.Platform)
		}
		if left.Algorithm != right.Algorithm {
			return strings.Compare(left.Algorithm, right.Algorithm)
		}
		return strings.Compare(left.Value, right.Value)
	})
	input.Identifiers = identifiers
	input.Fingerprints = fingerprints
	return input, true
}

func normalizeIdentifier(input catalog.GameIdentifier) (catalog.GameIdentifier, bool) {
	input.Namespace = strings.ToLower(strings.TrimSpace(input.Namespace))
	input.Value = strings.TrimSpace(input.Value)
	if input.Namespace == "" || len(input.Namespace) > 64 || input.Value == "" || len(input.Value) > 512 {
		return catalog.GameIdentifier{}, false
	}
	for index, character := range input.Namespace {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		if index > 0 && (character == '.' || character == '_' || character == '-') {
			valid = true
		}
		if !valid {
			return catalog.GameIdentifier{}, false
		}
	}
	for _, character := range input.Value {
		if character < 0x20 || character == 0x7f {
			return catalog.GameIdentifier{}, false
		}
	}
	return input, true
}

func normalizeFingerprint(input catalog.GameFingerprint) (catalog.GameFingerprint, bool) {
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.Algorithm = strings.ToLower(strings.TrimSpace(input.Algorithm))
	input.Value = strings.ToLower(strings.TrimSpace(input.Value))
	sizes := map[string]int{"crc32": 8, "md5": 32, "sha1": 40, "sha256": 64}
	size, known := sizes[input.Algorithm]
	if input.Platform == "" || len(input.Platform) > 100 || !known || len(input.Value) != size {
		return catalog.GameFingerprint{}, false
	}
	decoded, err := hex.DecodeString(input.Value)
	return input, err == nil && len(decoded)*2 == size
}

func mergeGameEvidence(game *catalog.Game, evidence catalog.ResolveGame) {
	evidence.Identifiers = append(evidence.Identifiers, game.Identifiers...)
	evidence.Fingerprints = append(evidence.Fingerprints, game.Fingerprints...)
	normalized, _ := normalizeEvidence(evidence)
	game.Identifiers = normalized.Identifiers
	game.Fingerprints = normalized.Fingerprints
}

func translateStorageError(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return catalog.ErrNotFound
	}
	if errors.Is(err, storage.ErrConflict) {
		return catalog.ErrConflict
	}
	return err
}

var _ catalog.Service = (*service)(nil)
