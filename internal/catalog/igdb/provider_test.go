package igdb_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/catalog/igdb"
)

func TestProviderResolvesSteamAndCachesSearches(t *testing.T) {
	var tokenRequests atomic.Int32
	var searchRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			tokenRequests.Add(1)
			if request.URL.Query().Get("client_id") != "client" || request.URL.Query().Get("client_secret") != "secret" {
				t.Error("missing client credentials")
			}
			writeJSON(response, map[string]any{"access_token": "access", "expires_in": 3600})
		case "/v4/external_games":
			assertAPIRequest(t, request)
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), `uid = "413150"`) {
				t.Errorf("Steam AppID was not queried: %s", body)
			}
			writeJSON(response, []any{map[string]any{"uid": "413150", "game": gameResponse()}})
		case "/v4/games":
			assertAPIRequest(t, request)
			body, _ := io.ReadAll(request.Body)
			if strings.Contains(string(body), "search") {
				searchRequests.Add(1)
			}
			writeJSON(response, []any{gameResponse()})
		case "/images/t_cover_big/co1abc.jpg":
			response.Header().Set("Content-Type", "image/jpeg")
			_, _ = response.Write([]byte("cover"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	provider, err := igdb.New(igdb.Config{
		ClientID: "client", ClientSecret: "secret",
		BaseURL: server.URL + "/v4", TokenURL: server.URL + "/token", ImageBaseURL: server.URL + "/images",
		RequestsPerSecond: 4, SearchCacheTTL: time.Minute,
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	match, err := provider.Resolve(context.Background(), catalog.ResolveGame{
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "413150"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if match.Title != "Stardew Valley" || match.Publisher != "ConcernedApe" || len(match.Identifiers) != 2 || len(match.Media) != 1 {
		t.Fatalf("unexpected Steam resolution: %+v", match)
	}

	search := catalog.SearchGames{Title: "Stardew Valley", Platform: "PC (Microsoft Windows)", Limit: 10}
	first, err := provider.Search(context.Background(), search)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Search(context.Background(), search)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].SelectionToken != "17000|PC" || searchRequests.Load() != 1 {
		t.Fatalf("search was not cached: first=%v second=%v requests=%d", first, second, searchRequests.Load())
	}
	selected, err := provider.Match(context.Background(), first[0].SelectionToken)
	if err != nil || selected.Title != "Stardew Valley" {
		t.Fatalf("could not apply search selection: match=%v err=%v", selected, err)
	}
	format, image, err := provider.OpenMedia(context.Background(), match.Media[0])
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	contents, _ := io.ReadAll(image)
	if format != "image/jpeg" || string(contents) != "cover" || tokenRequests.Load() != 1 {
		t.Fatalf("unexpected media or token reuse: format=%q image=%q tokens=%d", format, contents, tokenRequests.Load())
	}
}

// A game reached through its Steam listing is the store-level "PC" release,
// not whichever console IGDB lists first, and the matcher shows the same
// vocabulary: one selectable row per platform, Steam's OSes collapsed into
// one "PC" row whose selection stamps the same platform resolution does.
func TestSteamResolutionAndMatcherAgreeOnPC(t *testing.T) {
	undertale := func() map[string]any {
		game := gameResponse()
		game["name"] = "Undertale"
		game["platforms"] = []any{
			map[string]any{"name": "PlayStation 4"},
			map[string]any{"name": "Nintendo Switch"},
			map[string]any{"name": "Linux"},
			map[string]any{"name": "Mac"},
			map[string]any{"name": "PC (Microsoft Windows)"},
		}
		return game
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			writeJSON(response, map[string]any{"access_token": "access", "expires_in": 3600})
		case "/v4/external_games":
			writeJSON(response, []any{map[string]any{"uid": "391540", "game": undertale()}})
		case "/v4/games":
			writeJSON(response, []any{undertale()})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	provider, err := igdb.New(igdb.Config{
		ClientID: "client", ClientSecret: "secret",
		BaseURL: server.URL + "/v4", TokenURL: server.URL + "/token",
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	match, err := provider.Resolve(context.Background(), catalog.ResolveGame{
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "391540"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if match.Platform != "PC" || match.PlatformCompany != "" {
		t.Fatalf("Steam resolution did not claim the store platform: %+v", match)
	}

	candidates, err := provider.Search(context.Background(), catalog.SearchGames{Title: "Undertale", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var platforms []string
	for _, candidate := range candidates {
		platforms = append(platforms, candidate.Platform)
	}
	expected := []string{"PlayStation 4", "Nintendo Switch", "PC"}
	if len(platforms) != 3 || platforms[0] != expected[0] || platforms[1] != expected[1] || platforms[2] != expected[2] {
		t.Fatalf("expected one row per platform with Steam's OSes as PC, got %v", platforms)
	}
	if candidates[2].SelectionToken != "17000|PC" {
		t.Fatalf("PC row does not carry its platform: %q", candidates[2].SelectionToken)
	}

	selected, err := provider.Match(context.Background(), candidates[2].SelectionToken)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Platform != "PC" || selected.PlatformCompany != "" {
		t.Fatalf("choosing the PC row did not stamp PC: %+v", selected)
	}
	console, err := provider.Match(context.Background(), candidates[0].SelectionToken)
	if err != nil {
		t.Fatal(err)
	}
	if console.Platform != "PlayStation 4" {
		t.Fatalf("choosing a console row did not stamp it: %+v", console)
	}
}

func assertAPIRequest(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Header.Get("Client-ID") != "client" || request.Header.Get("Authorization") != "Bearer access" {
		t.Errorf("missing IGDB authentication headers")
	}
}

// A resolved game carries the landscape key art as well as the portrait cover,
// and each is fetched at the size it is drawn at: covers small enough to fill a
// grid, artwork large enough to sit behind text full-bleed.
func TestProviderSuppliesArtworkAlongsideCover(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			writeJSON(response, map[string]any{"access_token": "access", "expires_in": 3600})
		case "/v4/games":
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), "artworks.image_id") {
				t.Errorf("artwork was not requested: %s", body)
			}
			game := gameResponse()
			game["artworks"] = []any{
				map[string]any{"image_id": "ar1aaa"},
				map[string]any{"image_id": "ar2bbb"},
			}
			writeJSON(response, []any{game})
		case "/images/t_1080p/ar1aaa.jpg":
			response.Header().Set("Content-Type", "image/jpeg")
			_, _ = response.Write([]byte("wide art"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	provider, err := igdb.New(igdb.Config{
		ClientID: "client", ClientSecret: "secret",
		BaseURL: server.URL + "/v4", TokenURL: server.URL + "/token", ImageBaseURL: server.URL + "/images",
		RequestsPerSecond: 4, SearchCacheTTL: time.Minute,
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	match, err := provider.Match(context.Background(), "17000")
	if err != nil {
		t.Fatal(err)
	}

	var kinds []string
	for _, media := range match.Media {
		kinds = append(kinds, media.Kind)
	}
	if len(match.Media) != 3 || kinds[0] != "cover" || kinds[1] != "artwork" || kinds[2] != "artwork" {
		t.Fatalf("expected a cover and two artworks, got %v", kinds)
	}
	// Position numbers artwork among artwork, so the first one a caller reaches
	// for is IGDB's own first choice rather than whatever followed the cover.
	if match.Media[1].Position != 0 || match.Media[2].Position != 1 {
		t.Fatalf("artwork positions did not start at zero: %+v", match.Media[1:])
	}

	format, image, err := provider.OpenMedia(context.Background(), match.Media[1])
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	contents, _ := io.ReadAll(image)
	if format != "image/jpeg" || string(contents) != "wide art" {
		t.Fatalf("artwork was not fetched at its own size: format=%q image=%q", format, contents)
	}
}

func writeJSON(response http.ResponseWriter, value any) {
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(value)
}

func gameResponse() map[string]any {
	return map[string]any{
		"id": 17000, "name": "Stardew Valley", "summary": "A farming role-playing game.",
		"first_release_date": 1456444800,
		"platforms":          []any{map[string]any{"name": "PC (Microsoft Windows)"}},
		"cover":              map[string]any{"image_id": "co1abc"},
		"involved_companies": []any{map[string]any{
			"publisher": true, "company": map[string]any{"name": "ConcernedApe"},
		}},
		"genres": []any{map[string]any{"name": "Role-playing (RPG)"}},
	}
}
