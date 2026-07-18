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
	if len(first) != 1 || len(second) != 1 || first[0].SelectionToken != "17000" || searchRequests.Load() != 1 {
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

func assertAPIRequest(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Header.Get("Client-ID") != "client" || request.Header.Get("Authorization") != "Bearer access" {
		t.Errorf("missing IGDB authentication headers")
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
