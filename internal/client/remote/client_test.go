package remote_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

func TestClientListsOmnisavesForBinding(t *testing.T) {
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
	saves, err := client.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 || saves[0].ID != "save-a" || saves[0].DisplayName != "Save 1" {
		t.Fatalf("unexpected binding destinations: %+v", saves)
	}
}

func TestClientListsACompleteRevisionHistoryForBinding(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/omnisaves/save-a/revisions" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`[
				{"id":"revision-1","omnisave_id":"save-a","parent_id":null,"created_at":"2026-07-17T12:00:00Z","files":[]},
				{"id":"revision-2","omnisave_id":"save-a","parent_id":"revision-1","created_at":"2026-07-17T13:00:00Z","files":[]}
			]`)),
		}, nil
	})}

	client, err := remote.New("https://server.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	history, err := client.ListRevisions(context.Background(), "save-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].ID != "revision-1" || history[1].ID != "revision-2" {
		t.Fatalf("expected the complete ordered history, got %+v", history)
	}
}

func TestClientForksAMatchingOlderRevision(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/omnisaves/save-a/forks" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		var input omnisave.ForkOmnisave
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.RevisionID != "revision-1" || input.DisplayName != "Farm (fork)" {
			t.Fatalf("unexpected fork input: %+v", input)
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"omnisave":{"id":"save-b","game_id":"game-a","display_name":"Farm (fork)","head_revision_id":"revision-b","created_at":"2026-07-17T14:00:00Z"},
				"revision":{"id":"revision-b","omnisave_id":"save-b","parent_id":null,"created_at":"2026-07-17T14:00:00Z","files":[]}
			}`)),
		}, nil
	})}
	client, err := remote.New("https://server.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	fork, err := client.ForkOmnisave(context.Background(), "save-a", omnisave.ForkOmnisave{
		RevisionID: "revision-1", DisplayName: "Farm (fork)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fork.Omnisave.ID != "save-b" || fork.Revision.ID != "revision-b" {
		t.Fatalf("unexpected fork result: %+v", fork)
	}
}

func TestClientStreamsAnArtifactForFastForward(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/artifacts/abc123" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("save-content")),
		}, nil
	})}
	client, err := remote.New("https://server.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	payload, err := client.OpenArtifact(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	defer payload.Close()
	content, err := io.ReadAll(payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "save-content" {
		t.Fatalf("unexpected artifact content: %q", content)
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
