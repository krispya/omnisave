// Package service implements the Library's Games: it resolves evidence into
// Games, asks providers for claims, and keeps media and Provenance.
package service

import (
	"context"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/krisbaumgartner/omnisave/internal/artifact"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
)

type service struct {
	repository catalog.Repository
	artifacts  artifact.Store
	// resolvers are asked in order to claim unknown evidence; searchers are
	// asked in order for candidates. providers finds the provider behind a
	// selection token or a stored media reference.
	resolvers []catalog.Provider
	searchers []catalog.Provider
	providers map[string]catalog.Provider
}

// New creates the Library's Games, asking the same providers in the same
// order to resolve and to search.
func New(repository catalog.Repository, artifacts artifact.Store, providers ...catalog.Provider) catalog.Service {
	return NewWithProviders(repository, artifacts, providers, providers)
}

// NewWithProviders orders providers separately for resolving and searching:
// a hash catalog is the most exact answer to a ROM, and a title catalog the
// most useful answer to a search.
func NewWithProviders(
	repository catalog.Repository,
	artifacts artifact.Store,
	resolvers []catalog.Provider,
	searchers []catalog.Provider,
) catalog.Service {
	s := &service{repository: repository, artifacts: artifacts, providers: make(map[string]catalog.Provider)}
	s.resolvers = s.register(resolvers)
	s.searchers = s.register(searchers)
	return s
}

// register keeps each named provider once, in order.
func (s *service) register(providers []catalog.Provider) []catalog.Provider {
	var registered []catalog.Provider
	for _, provider := range providers {
		if provider == nil || strings.TrimSpace(provider.Name()) == "" {
			continue
		}
		name := provider.Name()
		if slices.ContainsFunc(registered, func(p catalog.Provider) bool { return p.Name() == name }) {
			continue
		}
		s.providers[name] = provider
		registered = append(registered, provider)
	}
	return registered
}

// Resolve answers known evidence locally, before any provider is asked
// (FDR-001, decision 4). Otherwise the providers' claim can still connect the
// evidence to a known Game; failing that, a new Game is created.
func (s *service) Resolve(ctx context.Context, input catalog.Evidence) (*catalog.Resolution, error) {
	evidence, valid := normalizeEvidence(input)
	if !valid {
		return nil, catalog.ErrInvalid
	}
	known, err := s.find(ctx, evidence)
	if err != nil {
		return nil, err
	}
	if known != nil {
		return s.absorb(ctx, known, evidence)
	}

	claim, err := s.claim(ctx, evidence, "")
	if err != nil {
		return nil, err
	}
	if claim != nil {
		if evidence, err = withClaimed(evidence, claim); err != nil {
			return nil, err
		}
	}
	id, status := uuid.NewString(), catalog.ResolutionCreated
	known, err = s.find(ctx, evidence)
	if err != nil {
		return nil, err
	}
	if known != nil {
		id, status = known.ID, catalog.ResolutionExisting
		evidence = mergeEvidence(evidence, known)
	}
	game, named := describeGame(id, evidence, claim)
	if !named {
		// Neither a provider nor the Device's own hints name the game.
		return nil, catalog.ErrNotFound
	}
	if err := s.repository.SaveGame(ctx, game); err != nil {
		// A concurrent resolution may have claimed this evidence first.
		if errors.Is(err, catalog.ErrConflict) {
			if raced, findErr := s.find(ctx, evidence); findErr == nil && raced != nil {
				return s.absorb(ctx, raced, evidence)
			}
		}
		return nil, err
	}
	s.cacheMedia(ctx, id, claim)
	return s.resolution(ctx, id, status)
}

// absorb joins evidence to the Game that already holds some of it.
func (s *service) absorb(ctx context.Context, game *catalog.Game, evidence catalog.Evidence) (*catalog.Resolution, error) {
	merged := mergeEvidence(evidence, game)
	game.Identifiers, game.Fingerprints = merged.Identifiers, merged.Fingerprints
	if err := s.repository.SaveGame(ctx, *game); err != nil {
		return nil, err
	}
	return s.resolution(ctx, game.ID, catalog.ResolutionExisting)
}

func (s *service) resolution(ctx context.Context, id string, status catalog.ResolutionStatus) (*catalog.Resolution, error) {
	game, err := s.repository.GetGame(ctx, id)
	if err != nil {
		return nil, err
	}
	return &catalog.Resolution{Game: *game, Status: status}, nil
}

func (s *service) Search(ctx context.Context, input catalog.SearchGames) ([]catalog.GameCandidate, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Platform = strings.TrimSpace(input.Platform)
	if input.Title == "" || len(s.searchers) == 0 || input.Limit < 0 || input.Limit > 25 {
		return nil, catalog.ErrInvalid
	}
	if input.Limit == 0 {
		input.Limit = 10
	}
	for _, provider := range s.searchers {
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
		// The token names the provider too, so Match knows whom to ask.
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

// Match enriches the chosen claim with the other providers, as a resolution
// would, and refuses it when its evidence belongs to another Game.
func (s *service) Match(ctx context.Context, gameID string, input catalog.MatchGame) (*catalog.Game, error) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return nil, catalog.ErrInvalid
	}
	provider, token, err := s.selection(strings.TrimSpace(input.SelectionToken))
	if err != nil {
		return nil, err
	}
	claim, err := provider.Match(ctx, token)
	if err != nil {
		return nil, err
	}
	stampMedia(claim, provider.Name())
	evidence, valid := claimedEvidence(claim)
	if !valid {
		return nil, catalog.ErrUnavailable
	}
	enrichment, err := s.claim(ctx, evidence, provider.Name())
	if err != nil {
		return nil, err
	}
	claim = mergeClaims(claim, enrichment)
	if evidence, valid = claimedEvidence(claim); !valid {
		return nil, catalog.ErrUnavailable
	}

	if other, err := s.find(ctx, evidence); err != nil {
		return nil, err
	} else if other != nil && other.ID != gameID {
		return nil, &catalog.IdentityConflict{GameIDs: []string{other.ID, gameID}}
	}
	if target, err := s.repository.GetGame(ctx, gameID); err == nil {
		evidence = mergeEvidence(evidence, target)
	} else if !errors.Is(err, catalog.ErrNotFound) {
		return nil, err
	}
	game, named := describeGame(gameID, evidence, claim)
	if !named {
		return nil, catalog.ErrUnavailable
	}
	game.Metadata["match_method"] = "manual"
	if err := s.repository.SaveGame(ctx, game); err != nil {
		return nil, err
	}
	if err := s.repository.ClearGameMedia(ctx, gameID); err != nil {
		return nil, err
	}
	s.cacheMedia(ctx, gameID, claim)
	return s.repository.GetGame(ctx, gameID)
}

func (s *service) List(ctx context.Context) ([]catalog.Game, error) {
	return s.repository.ListGames(ctx)
}

func (s *service) Get(ctx context.Context, id string) (*catalog.Game, error) {
	return s.repository.GetGame(ctx, id)
}

func (s *service) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return catalog.ErrInvalid
	}
	return s.repository.DeleteGame(ctx, id)
}

func (s *service) TrackGame(ctx context.Context, gameID, deviceID string, input catalog.TrackGame) error {
	gameID = strings.TrimSpace(gameID)
	deviceID = strings.TrimSpace(deviceID)
	adapter := strings.TrimSpace(input.Adapter)
	if gameID == "" || deviceID == "" || len(adapter) > 64 {
		return catalog.ErrInvalid
	}
	now := time.Now().UTC()
	return s.repository.TrackGame(ctx, gameID, catalog.GameTracking{
		DeviceID:       deviceID,
		Adapter:        adapter,
		Installed:      input.Installed,
		FirstTrackedAt: now,
		LastSeenAt:     now,
	})
}

func (s *service) UntrackGame(ctx context.Context, gameID, deviceID string) error {
	gameID = strings.TrimSpace(gameID)
	deviceID = strings.TrimSpace(deviceID)
	if gameID == "" || deviceID == "" {
		return catalog.ErrInvalid
	}
	return s.repository.UntrackGame(ctx, gameID, deviceID, time.Now().UTC())
}

// OpenMedia streams a Game's stored media.
func (s *service) OpenMedia(ctx context.Context, gameID, mediaID string) (*catalog.GameMedia, io.ReadCloser, error) {
	media, err := s.repository.GetGameMedia(ctx, gameID, mediaID)
	if err != nil {
		return nil, nil, err
	}
	payload, err := s.artifacts.OpenArtifact(ctx, media.SHA256)
	if errors.Is(err, artifact.ErrNotFound) {
		return nil, nil, catalog.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return media, payload, nil
}

// describeGame is the Game its evidence and an optional claim describe.
// Without a claim the Game is provisional, named by the Device's own hints.
// It reports false when nothing names the game.
func describeGame(id string, evidence catalog.Evidence, claim *catalog.Claim) (catalog.Game, bool) {
	game := catalog.Game{
		ID:             id,
		Title:          evidence.TitleHint,
		Platform:       evidence.PlatformHint,
		MetadataSource: "client",
		Identifiers:    slices.Clone(evidence.Identifiers),
		Fingerprints:   slices.Clone(evidence.Fingerprints),
		Metadata:       map[string]any{"match_method": "provisional"},
		RefreshedAt:    time.Now().UTC(),
	}
	if claim == nil {
		return game, game.Title != ""
	}
	game.Title = strings.TrimSpace(claim.Title)
	game.SortTitle = strings.TrimSpace(claim.SortTitle)
	game.Platform = strings.TrimSpace(claim.Platform)
	game.PlatformCompany = strings.TrimSpace(claim.PlatformCompany)
	game.Publisher = strings.TrimSpace(claim.Publisher)
	game.Description = strings.TrimSpace(claim.Description)
	game.MetadataSource = strings.TrimSpace(claim.Source)
	game.Metadata = maps.Clone(claim.Metadata)
	if game.Metadata == nil {
		game.Metadata = make(map[string]any)
	}
	game.Metadata["match_method"] = "automatic"
	return game, game.Title != "" && game.MetadataSource != ""
}

var _ catalog.Service = (*service)(nil)
