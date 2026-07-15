package service_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

const coverImage = "\x89PNG\r\n\x1a\ncover image"

func TestIdentifyCachesGameAndMedia(t *testing.T) {
	ctx := context.Background()
	repository := storagetest.NewMemoryRepository()
	provider := &fakeProvider{}
	games := catalogservice.New(repository, repository, provider)
	fingerprint := catalog.Fingerprint{
		Platform: "snes",
		SHA1:     "6b47bb75d16514b6a476aa0c73a683a2a4c18765",
	}

	identified, err := games.Identify(ctx, catalog.IdentifyGame{
		GameID:      "super-mario-world",
		Fingerprint: fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	if identified.ID != "super-mario-world" || identified.Title != "Super Mario World" {
		t.Fatalf("unexpected game: %v", identified)
	}
	if len(identified.Media) != 1 || identified.Media[0].Kind != "cover" {
		t.Fatalf("expected locally cached cover art, got %v", identified.Media)
	}

	media, payload, err := games.OpenMedia(ctx, identified.ID, identified.Media[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer payload.Close()
	contents, err := io.ReadAll(payload)
	if err != nil {
		t.Fatal(err)
	}
	if media.Format != "image/png" || string(contents) != coverImage {
		t.Fatalf("unexpected stored media: %v %q", media, contents)
	}

	if _, err := games.Identify(ctx, catalog.IdentifyGame{Fingerprint: fingerprint}); err != nil {
		t.Fatal(err)
	}
	if provider.identifications != 1 || provider.mediaRequests != 1 {
		t.Fatalf("cached identification called provider again: %+v", provider)
	}
}

func TestManualMatchCreatesCatalogGameWithoutLocalFingerprint(t *testing.T) {
	ctx := context.Background()
	repository := storagetest.NewMemoryRepository()
	provider := &fakeProvider{}
	games := catalogservice.New(repository, repository, provider)

	candidates, err := games.Search(ctx, catalog.SearchGames{
		Title: "Super Mario World", Platform: "SNES",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].SelectionToken != "known-selection" {
		t.Fatalf("unexpected candidates: %v", candidates)
	}

	matched, err := games.Match(ctx, "debug-super-mario-world", catalog.MatchGame{
		SelectionToken: candidates[0].SelectionToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if matched.ID != "debug-super-mario-world" || matched.Title != "Super Mario World" {
		t.Fatalf("unexpected matched game: %v", matched)
	}
	if matched.Metadata["match_method"] != "manual" || len(matched.Media) != 1 {
		t.Fatalf("manual match was not cached: %v", matched)
	}
}

type fakeProvider struct {
	identifications int
	mediaRequests   int
}

func (p *fakeProvider) Identify(_ context.Context, fingerprint catalog.Fingerprint) (*catalog.ProviderMatch, error) {
	p.identifications++
	match := p.match()
	match.ROM.SHA1 = fingerprint.SHA1
	return match, nil
}

func (p *fakeProvider) Search(context.Context, catalog.SearchGames) ([]catalog.GameCandidate, error) {
	return []catalog.GameCandidate{{
		Provider:       "hasheous",
		ProviderID:     "962167",
		Title:          "Super Mario World",
		Edition:        "Super Mario World (USA)",
		Platform:       "Super Nintendo Entertainment System",
		SelectionToken: "known-selection",
	}}, nil
}

func (p *fakeProvider) Match(_ context.Context, selectionToken string) (*catalog.ProviderMatch, error) {
	if selectionToken != "known-selection" {
		return nil, catalog.ErrInvalid
	}
	return p.match(), nil
}

func (p *fakeProvider) match() *catalog.ProviderMatch {
	return &catalog.ProviderMatch{
		Provider:    "hasheous",
		ProviderID:  "337",
		Title:       "Super Mario World",
		SortTitle:   "Super Mario World",
		Platform:    "Super Nintendo Entertainment System",
		Publisher:   "Nintendo",
		Description: "Mario and Yoshi explore Dinosaur Land.",
		ROM: catalog.ROMMatch{
			ProviderID: "1628019",
			System:     "Nintendo - Super Nintendo Entertainment System",
			Name:       "Super Mario World.sfc",
			Source:     "no-intro",
		},
		Media: []catalog.MediaReference{{
			Kind:        "cover",
			ProviderID:  "FD5CF3F8CA683D9F7DC66484C3C6602332A0CD84",
			Attribution: "IGDB",
		}},
	}
}

func (p *fakeProvider) OpenMedia(context.Context, catalog.MediaReference) (string, io.ReadCloser, error) {
	p.mediaRequests++
	return "image/png", io.NopCloser(strings.NewReader(coverImage)), nil
}

var _ catalog.Provider = (*fakeProvider)(nil)
