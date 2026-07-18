package hasheous_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/catalog/hasheous"
)

func TestProviderIdentifiesNoIntroGameAndOpensArtwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/Lookup/ByHash":
			if r.URL.Query().Get("returnSources") != "NoIntros" {
				t.Fatal("lookup should request the No-Intro source")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
				"id":337,
				"name":"Super Mario World",
				"platform":{"name":"Super Nintendo Entertainment System"},
				"publisher":{"name":"Nintendo"},
				"metadata":[{"id":"1070","source":"IGDB","status":"Mapped"}],
				"signatures":{"NoIntros":[{"game":{"sortingName":"Super Mario World","system":"Nintendo - Super Nintendo Entertainment System"},"rom":{"id":"1628019","name":"Super Mario World.sfc","size":524288,"sha1":"6b47bb75d16514b6a476aa0c73a683a2a4c18765","country":{"US":"United States"},"language":{"en":"English"}}}]},
				"attributes":[
					{"attributeName":"AIDescription","value":"Mario and Yoshi explore Dinosaur Land."},
					{"attributeName":"LogoAttribution","value":"IGDB"},
					{"attributeName":"Logo","value":"FD5CF3F8CA683D9F7DC66484C3C6602332A0CD84"}
				]
			}`)
		case "/api/v1/images/FD5CF3F8CA683D9F7DC66484C3C6602332A0CD84":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "cover image")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider := hasheous.New(server.URL, server.Client())

	match, err := provider.Resolve(context.Background(), catalog.ResolveGame{Fingerprints: []catalog.GameFingerprint{{
		Platform: "snes", Algorithm: "sha1", Value: "6b47bb75d16514b6a476aa0c73a683a2a4c18765",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if match.Title != "Super Mario World" || match.ROM.Region != "United States" {
		t.Fatalf("unexpected match: %v", match)
	}
	if len(match.Identifiers) != 2 || match.Identifiers[1].Namespace != "igdb.game" {
		t.Fatalf("expected Hasheous and IGDB identities: %v", match.Identifiers)
	}
	if len(match.Media) != 1 || match.Media[0].Kind != "cover" || match.Media[0].Attribution != "IGDB" {
		t.Fatalf("unexpected media: %v", match.Media)
	}

	format, payload, err := provider.OpenMedia(context.Background(), match.Media[0])
	if err != nil {
		t.Fatal(err)
	}
	defer payload.Close()
	contents, err := io.ReadAll(payload)
	if err != nil {
		t.Fatal(err)
	}
	if format != "image/png" || string(contents) != "cover image" {
		t.Fatalf("unexpected media response: %q %q", format, contents)
	}
}

func TestProviderSearchesMCPAndResolvesASelection(t *testing.T) {
	var searchedPlatform string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/Mcp":
			var request struct {
				Params struct {
					Name      string `json:"name"`
					Arguments struct {
						Platform string `json:"platform"`
					} `json:"arguments"`
				} `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Params.Name != "hasheous_search_games" {
				t.Fatalf("unexpected MCP tool: %s", request.Params.Name)
			}
			searchedPlatform = request.Params.Arguments.Platform
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"games":[{
				"id":962167,"name":"Super Mario World","description":"Super Mario World (USA)",
				"year":"1991","publisher":{"name":"Nintendo"},
				"platform":{"name":"Nintendo - Super Nintendo Entertainment System"},
				"country":"US","language":"en","roms":[{
					"name":"Super Mario World (USA).sfc","sha1":"6b47bb75d16514b6a476aa0c73a683a2a4c18765"
				}]
			}]}}}`)
		case "/api/v1/Lookup/ByHash":
			fmt.Fprint(w, `{
				"id":337,"name":"Super Mario World",
				"platform":{"name":"Super Nintendo Entertainment System"},
				"publisher":{"name":"Nintendo"},
				"signatures":{"NoIntros":[{"game":{"sortingName":"Super Mario World","system":"Nintendo - Super Nintendo Entertainment System"},"rom":{"id":"1628019","name":"Super Mario World.sfc","sha1":"6b47bb75d16514b6a476aa0c73a683a2a4c18765"}}]},
				"attributes":[]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider := hasheous.New(server.URL, server.Client())

	candidates, err := provider.Search(context.Background(), catalog.SearchGames{
		Title: "Super Mario World", Platform: "Nintendo Super Nintendo Entertainment System", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if searchedPlatform != "Super Nintendo Entertainment System" {
		t.Fatalf("unexpected platform search: %q", searchedPlatform)
	}
	if len(candidates) != 1 || candidates[0].ProviderID != "962167" || candidates[0].SelectionToken == "" {
		t.Fatalf("unexpected candidates: %v", candidates)
	}

	match, err := provider.Match(context.Background(), candidates[0].SelectionToken)
	if err != nil {
		t.Fatal(err)
	}
	if match.Title != "Super Mario World" || match.Metadata["signature_game_id"] != int64(962167) {
		t.Fatalf("unexpected selected match: %v", match)
	}
}
