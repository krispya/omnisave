package remote_test

import (
	"context"
	"encoding/json"
	"errors"
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
			Body:       io.NopCloser(strings.NewReader(`[{"id":"save-a","game_id":"game-a","display_name":"Save 1","current_revision_id":null,"created_at":"2026-07-17T12:00:00Z"}]`)),
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
				"omnisave":{"id":"save-b","game_id":"game-a","display_name":"Farm (fork)","current_revision_id":"revision-b","created_at":"2026-07-17T14:00:00Z"},
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

func TestClientDecodesACurrentRevisionConflictFromARefusedCommit(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/omnisaves/save-a/revisions" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusConflict,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"error":"current_revision_conflict",
				"status":409,
				"expected_current_revision_id":"revision-1",
				"actual_current_revision_id":"revision-9"
			}`)),
		}, nil
	})}
	client, err := remote.New("https://server.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	expected := "revision-1"
	_, err = client.CommitRevision(context.Background(), "save-a", omnisave.CreateRevision{
		ExpectedCurrentRevisionID: &expected,
	})
	var conflict *remote.CurrentRevisionConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected the conflict payload to decode to its typed error, got %v", err)
	}
	if conflict.ActualCurrentRevisionID == nil || *conflict.ActualCurrentRevisionID != "revision-9" {
		t.Fatalf("expected the conflict to carry where the current revision actually is, got %+v", conflict)
	}
}

func TestClientLeavesOtherConflictBodiesAsPlainResponseErrors(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusConflict,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"error":"identity_conflict",
				"status":409,
				"game_ids":["game-a","game-b"]
			}`)),
		}, nil
	})}
	client, err := remote.New("https://server.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.CommitRevision(context.Background(), "save-a", omnisave.CreateRevision{})
	var conflict *remote.CurrentRevisionConflict
	if errors.As(err, &conflict) {
		t.Fatalf("expected a different 409 body to stay generic, got %+v", conflict)
	}
	var response *remote.ResponseError
	if !errors.As(err, &response) || response.StatusCode != http.StatusConflict {
		t.Fatalf("expected a plain conflict response error, got %v", err)
	}
}

func TestClientRestoresTheCurrentRevision(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPut || request.URL.Path != "/api/v1/omnisaves/save-a/current-revision" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		var input omnisave.RestoreRevision
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if input.RevisionID != "revision-1" {
			t.Fatalf("unexpected restore input: %+v", input)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"id":"save-a","game_id":"game-a","display_name":"Save 1",
				"current_revision_id":"revision-1","created_at":"2026-07-17T12:00:00Z"
			}`)),
		}, nil
	})}
	client, err := remote.New("https://server.example", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := client.RestoreCurrentRevision(context.Background(), "save-a", omnisave.RestoreRevision{
		RevisionID: "revision-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if restored.CurrentRevisionID == nil || *restored.CurrentRevisionID != "revision-1" {
		t.Fatalf("expected the restored save to carry the moved pointer, got %+v", restored)
	}
}

func TestClientStreamsAnArtifactForApply(t *testing.T) {
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

func TestClientReportsDeviceStatus(t *testing.T) {
	var requested struct {
		method, path, body string
	}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		payload, _ := io.ReadAll(request.Body)
		requested.method = request.Method
		requested.path = request.URL.Path
		requested.body = string(payload)
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})}
	client, err := remote.New("http://server", "secret", httpClient)
	if err != nil {
		t.Fatal(err)
	}

	if err := client.ReportDeviceStatus(context.Background(), "device-1", []string{"game-1"}); err != nil {
		t.Fatal(err)
	}
	if requested.method != http.MethodPut || requested.path != "/api/v1/devices/device-1/status" {
		t.Fatalf("unexpected request: %+v", requested)
	}
	if requested.body != `{"playing_game_ids":["game-1"]}` {
		t.Fatalf("unexpected body: %s", requested.body)
	}

	if err := client.ReportDeviceStatus(context.Background(), "device-1", nil); err != nil {
		t.Fatal(err)
	}
	if requested.body != `{"playing_game_ids":[]}` {
		t.Fatalf("expected an explicit empty clear, got %s", requested.body)
	}
}
