package remote_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
)

func TestClientListsOmniSavesForBinding(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v1/omnisaves" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("expected the configured API token")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`[{"id":"save-a","game_id":"game-a","display_name":"Save 1","head_revision_id":null,"created_at":"2026-07-17T12:00:00Z"}]`)),
		}, nil
	})}

	client, err := remote.New("https://server.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	saves, err := client.ListOmniSaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || saves[0].ID != "save-a" || saves[0].DisplayName != "Save 1" {
		t.Fatalf("unexpected binding destinations: %+v", saves)
	}
}

func TestClientResolvesLocalGameEvidence(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/games/resolve" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"status":"created",
				"game":{"id":"server-game","title":"Stardew Valley","metadata_source":"client","identifiers":[{"namespace":"steam.app","value":"413150"}],"fingerprints":[],"media":[],"refreshed_at":"2026-07-17T12:00:00Z"}
			}`)),
		}, nil
	})}
	client, err := remote.New("https://server.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := client.ResolveGame(context.Background(), catalog.ResolveGame{
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "413150"}},
		TitleHint:   "Stardew Valley",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != catalog.ResolutionCreated || resolution.Game.ID != "server-game" {
		t.Fatalf("unexpected resolution: %+v", resolution)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
