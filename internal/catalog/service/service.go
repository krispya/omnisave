// Package service implements the local game catalog.
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	repository          storage.CatalogRepository
	artifacts           storage.ArtifactStore
	resolutionProviders []catalog.Provider
	searchProviders     []catalog.Provider
	providers           map[string]catalog.Provider
}

// New creates a catalog using the same ordered providers for resolution and search.
func New(repository storage.CatalogRepository, artifacts storage.ArtifactStore, providers ...catalog.Provider) catalog.Service {
	return NewWithProviders(repository, artifacts, providers, providers)
}

// NewWithProviders creates a catalog with independent resolution and search order.
func NewWithProviders(
	repository storage.CatalogRepository,
	artifacts storage.ArtifactStore,
	resolutionProviders []catalog.Provider,
	searchProviders []catalog.Provider,
) catalog.Service {
	service := &service{
		repository: repository,
		artifacts:  artifacts,
		providers:  make(map[string]catalog.Provider),
	}
	for _, provider := range resolutionProviders {
		service.addProvider(provider, true, false)
	}
	for _, provider := range searchProviders {
		service.addProvider(provider, false, true)
	}
	return service
}

func (s *service) addProvider(provider catalog.Provider, resolve, search bool) {
	if provider == nil || strings.TrimSpace(provider.Name()) == "" {
		return
	}
	s.providers[provider.Name()] = provider
	if resolve && !containsProvider(s.resolutionProviders, provider.Name()) {
		s.resolutionProviders = append(s.resolutionProviders, provider)
	}
	if search && !containsProvider(s.searchProviders, provider.Name()) {
		s.searchProviders = append(s.searchProviders, provider)
	}
}

func containsProvider(providers []catalog.Provider, name string) bool {
	for _, provider := range providers {
		if provider.Name() == name {
			return true
		}
	}
	return false
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

	match, err := s.resolveProviders(ctx, evidence, "")
	if err != nil {
		return nil, err
	}
	combined := evidence
	if match != nil {
		providerEvidence, valid := evidenceFromMatch(match)
		if !valid {
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
			_ = s.cacheMedia(ctx, gameID, reference)
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
	if input.Title == "" || len(s.searchProviders) == 0 || input.Limit < 0 || input.Limit > 25 {
		return nil, catalog.ErrInvalid
	}
	if input.Limit == 0 {
		input.Limit = 10
	}
	for _, provider := range s.searchProviders {
		candidates, err := provider.Search(ctx, input)
		if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, catalog.ErrUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			continue
		}
		for index := range candidates {
			candidates[index].Provider = provider.Name()
			candidates[index].SelectionToken, err = encodeSelection(provider.Name(), candidates[index].SelectionToken)
			if err != nil {
				return nil, catalog.ErrUnavailable
			}
		}
		return candidates, nil
	}
	return []catalog.GameCandidate{}, nil
}

func (s *service) Match(ctx context.Context, gameID string, input catalog.MatchGame) (*catalog.Game, error) {
	gameID = strings.TrimSpace(gameID)
	input.SelectionToken = strings.TrimSpace(input.SelectionToken)
	if gameID == "" || input.SelectionToken == "" {
		return nil, catalog.ErrInvalid
	}
	providerName, providerToken, err := decodeSelection(input.SelectionToken)
	if err != nil {
		return nil, catalog.ErrInvalid
	}
	provider := s.providers[providerName]
	if provider == nil {
		return nil, catalog.ErrInvalid
	}
	match, err := provider.Match(ctx, providerToken)
	if err != nil {
		return nil, err
	}
	stampMediaProvider(match, provider.Name())
	match, err = s.enrichMatch(ctx, match, provider.Name())
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
		_ = s.cacheMedia(ctx, gameID, reference)
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

func (s *service) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return catalog.ErrInvalid
	}
	return translateStorageError(s.repository.DeleteGame(ctx, id))
}

func (s *service) RegisterDevice(ctx context.Context, id string, input catalog.RegisterDevice) (*catalog.Device, error) {
	id = strings.TrimSpace(id)
	name := strings.TrimSpace(input.Name)
	platform := strings.TrimSpace(input.Platform)
	if id == "" || len(id) > 128 || name == "" || len(name) > 128 || len(platform) > 64 {
		return nil, catalog.ErrInvalid
	}
	now := time.Now().UTC()
	device := catalog.Device{ID: id, Name: name, Platform: platform, CreatedAt: now, LastSeenAt: now}
	if err := s.repository.UpsertDevice(ctx, device); err != nil {
		return nil, translateStorageError(err)
	}
	return &device, nil
}

func (s *service) GetDevice(ctx context.Context, id string) (*catalog.Device, error) {
	device, err := s.repository.GetDevice(ctx, id)
	return device, translateStorageError(err)
}

func (s *service) TrackGame(ctx context.Context, gameID, deviceID string, input catalog.TrackGame) error {
	gameID = strings.TrimSpace(gameID)
	deviceID = strings.TrimSpace(deviceID)
	adapter := strings.TrimSpace(input.Adapter)
	if gameID == "" || deviceID == "" || len(adapter) > 64 {
		return catalog.ErrInvalid
	}
	now := time.Now().UTC()
	return translateStorageError(s.repository.TrackGame(ctx, gameID, catalog.GameTracking{
		DeviceID:       deviceID,
		Adapter:        adapter,
		Installed:      input.Installed,
		FirstTrackedAt: now,
		LastSeenAt:     now,
	}))
}

func (s *service) UntrackGame(ctx context.Context, gameID, deviceID string) error {
	gameID = strings.TrimSpace(gameID)
	deviceID = strings.TrimSpace(deviceID)
	if gameID == "" || deviceID == "" {
		return catalog.ErrInvalid
	}
	return translateStorageError(s.repository.UntrackGame(ctx, gameID, deviceID, time.Now().UTC()))
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

// cacheableMediaKinds limits stored media to the shapes the Dash renders.
var cacheableMediaKinds = map[string]bool{"cover": true, "artwork": true, "screenshot": true}

func (s *service) cacheMedia(ctx context.Context, gameID string, reference catalog.MediaReference) error {
	provider := s.providers[reference.Provider]
	if provider == nil || reference.ProviderID == "" || !cacheableMediaKinds[reference.Kind] {
		return catalog.ErrInvalid
	}
	format, payload, err := provider.OpenMedia(ctx, reference)
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
		Provider:    provider.Name(),
		ProviderID:  reference.ProviderID,
		Attribution: reference.Attribution,
	})
}

func (s *service) resolveProviders(ctx context.Context, evidence catalog.ResolveGame, skip string) (*catalog.ProviderMatch, error) {
	current := evidence
	var combined *catalog.ProviderMatch
	for _, provider := range s.resolutionProviders {
		if provider.Name() == skip {
			continue
		}
		match, err := provider.Resolve(ctx, current)
		if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, catalog.ErrUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		provided, valid := evidenceFromMatch(match)
		if !valid || !providerSupportsEvidence(provided, current) {
			return nil, catalog.ErrUnavailable
		}
		stampMediaProvider(match, provider.Name())
		current.Identifiers = append(current.Identifiers, provided.Identifiers...)
		current.Fingerprints = append(current.Fingerprints, provided.Fingerprints...)
		current.TitleHint = match.Title
		current.PlatformHint = match.Platform
		current, valid = normalizeEvidence(current)
		if !valid {
			return nil, catalog.ErrUnavailable
		}
		combined = mergeProviderMatches(combined, match)
	}
	return combined, nil
}

func (s *service) enrichMatch(ctx context.Context, match *catalog.ProviderMatch, selectedProvider string) (*catalog.ProviderMatch, error) {
	evidence, valid := evidenceFromMatch(match)
	if !valid {
		return nil, catalog.ErrUnavailable
	}
	enrichment, err := s.resolveProviders(ctx, evidence, selectedProvider)
	if err != nil {
		return nil, err
	}
	return mergeProviderMatches(match, enrichment), nil
}

func mergeProviderMatches(base, next *catalog.ProviderMatch) *catalog.ProviderMatch {
	if base == nil {
		return next
	}
	if next == nil {
		return base
	}
	base.Identifiers = append(base.Identifiers, next.Identifiers...)
	base.Fingerprints = append(base.Fingerprints, next.Fingerprints...)
	base.Media = append(base.Media, next.Media...)
	if next.Source != "" {
		base.Source = next.Source
	}
	if next.Title != "" {
		base.Title = next.Title
	}
	if next.SortTitle != "" {
		base.SortTitle = next.SortTitle
	}
	if next.Platform != "" {
		base.Platform = next.Platform
		base.PlatformCompany = next.PlatformCompany
	}
	if next.Publisher != "" {
		base.Publisher = next.Publisher
	}
	if next.Description != "" {
		base.Description = next.Description
	}
	if base.Metadata == nil {
		base.Metadata = make(map[string]any)
	}
	for key, value := range next.Metadata {
		base.Metadata[key] = value
	}
	if base.ROM.Source == "" && next.ROM.Source != "" {
		base.ROM = next.ROM
	}
	return base
}

func stampMediaProvider(match *catalog.ProviderMatch, provider string) {
	if match == nil {
		return
	}
	for index := range match.Media {
		match.Media[index].Provider = provider
	}
}

type providerSelection struct {
	Provider string `json:"provider"`
	Token    string `json:"token"`
}

func encodeSelection(provider, token string) (string, error) {
	payload, err := json.Marshal(providerSelection{Provider: provider, Token: token})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeSelection(encoded string) (string, string, error) {
	if len(encoded) > 8192 {
		return "", "", catalog.ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", err
	}
	var selection providerSelection
	if err := json.Unmarshal(payload, &selection); err != nil {
		return "", "", err
	}
	selection.Provider = strings.TrimSpace(selection.Provider)
	selection.Token = strings.TrimSpace(selection.Token)
	if selection.Provider == "" || len(selection.Provider) > 64 || selection.Token == "" || len(selection.Token) > 4096 {
		return "", "", catalog.ErrInvalid
	}
	return selection.Provider, selection.Token, nil
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
	game.PlatformCompany = strings.TrimSpace(match.PlatformCompany)
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
