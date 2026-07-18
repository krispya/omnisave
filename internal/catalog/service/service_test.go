package service_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/storage/storagetest"
)

const coverImage = "\x89PNG\r\n\x1a\ncover image"

func TestResolveBuildsOneCanonicalGameFromDifferentIdentifiers(t *testing.T) {
	ctx := context.Background()
	repository := storagetest.NewMemoryRepository()
	provider := &fakeProvider{}
	games := catalogservice.New(repository, repository, provider)

	created, err := games.Resolve(ctx, catalog.ResolveGame{
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "2560710"}},
		TitleHint:   "Super Mario World",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != catalog.ResolutionCreated || created.Game.ID == "" {
		t.Fatalf("expected a new server-owned game: %v", created)
	}
	if len(created.Game.Identifiers) != 3 || len(created.Game.Media) != 1 {
		t.Fatalf("expected all provider identities and cached media: %v", created.Game)
	}

	reused, err := games.Resolve(ctx, catalog.ResolveGame{
		Identifiers: []catalog.GameIdentifier{{Namespace: "igdb.game", Value: "1070"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reused.Status != catalog.ResolutionExisting || reused.Game.ID != created.Game.ID {
		t.Fatalf("external IDs did not converge on one game: %v", reused)
	}
	if provider.resolutions != 1 || provider.mediaRequests != 1 {
		t.Fatalf("cached identity called the provider again: %+v", provider)
	}

	media, payload, err := games.OpenMedia(ctx, created.Game.ID, created.Game.Media[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer payload.Close()
	contents, _ := io.ReadAll(payload)
	if media.Format != "image/png" || string(contents) != coverImage {
		t.Fatalf("unexpected stored media: %v %q", media, contents)
	}
}

func TestResolveRejectsEvidenceAssignedToDifferentGames(t *testing.T) {
	ctx := context.Background()
	repository := storagetest.NewMemoryRepository()
	for _, game := range []catalog.Game{
		{ID: "one", Title: "One", MetadataSource: "client", Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "1"}}, RefreshedAt: time.Now()},
		{ID: "two", Title: "Two", MetadataSource: "client", Identifiers: []catalog.GameIdentifier{{Namespace: "igdb.game", Value: "2"}}, RefreshedAt: time.Now()},
	} {
		if err := repository.SaveGame(ctx, game, nil); err != nil {
			t.Fatal(err)
		}
	}
	games := catalogservice.New(repository, repository, nil)

	_, err := games.Resolve(ctx, catalog.ResolveGame{Identifiers: []catalog.GameIdentifier{
		{Namespace: "steam.app", Value: "1"},
		{Namespace: "igdb.game", Value: "2"},
	}})
	var conflict *catalog.IdentityConflict
	if !errors.As(err, &conflict) || len(conflict.GameIDs) != 2 {
		t.Fatalf("expected an identity conflict, got %v", err)
	}
}

func TestManualMatchCreatesCatalogGameWithoutLocalFingerprint(t *testing.T) {
	ctx := context.Background()
	repository := storagetest.NewMemoryRepository()
	provider := &fakeProvider{}
	games := catalogservice.New(repository, repository, provider)

	candidates, err := games.Search(ctx, catalog.SearchGames{Title: "Super Mario World", Platform: "SNES"})
	if err != nil {
		t.Fatal(err)
	}
	matched, err := games.Match(ctx, "debug-super-mario-world", catalog.MatchGame{
		SelectionToken: candidates[0].SelectionToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if matched.ID != "debug-super-mario-world" || matched.Metadata["match_method"] != "manual" || len(matched.Media) != 1 {
		t.Fatalf("manual match was not cached: %v", matched)
	}
}

type fakeProvider struct {
	resolutions   int
	mediaRequests int
}

func (p *fakeProvider) Resolve(_ context.Context, evidence catalog.ResolveGame) (*catalog.ProviderMatch, error) {
	p.resolutions++
	match := p.match()
	match.Identifiers = append(match.Identifiers, evidence.Identifiers...)
	match.Fingerprints = append(match.Fingerprints, evidence.Fingerprints...)
	return match, nil
}

func (p *fakeProvider) Search(context.Context, catalog.SearchGames) ([]catalog.GameCandidate, error) {
	return []catalog.GameCandidate{{
		Provider: "hasheous", ProviderID: "962167", Title: "Super Mario World",
		Platform: "Super Nintendo Entertainment System", SelectionToken: "known-selection",
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
		Source: "hasheous",
		Identifiers: []catalog.GameIdentifier{
			{Namespace: "hasheous.game", Value: "337"},
			{Namespace: "igdb.game", Value: "1070"},
		},
		Title: "Super Mario World", SortTitle: "Super Mario World",
		Platform: "Super Nintendo Entertainment System", Publisher: "Nintendo",
		ROM:   catalog.ROMMatch{ProviderID: "1628019", Source: "no-intro"},
		Media: []catalog.MediaReference{{Kind: "cover", ProviderID: "cover-id"}},
	}
}

func (p *fakeProvider) OpenMedia(context.Context, catalog.MediaReference) (string, io.ReadCloser, error) {
	p.mediaRequests++
	return "image/png", io.NopCloser(strings.NewReader(coverImage)), nil
}

var _ catalog.Provider = (*fakeProvider)(nil)
