package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	sqlitestorage "github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

type bindingFixture struct {
	localPath string
	content   []byte
	save      target.Save
	scans     []client.TargetScan
	state     tracking.State
}

func newBindingFixture(t *testing.T, content string) bindingFixture {
	t.Helper()
	directory := t.TempDir()
	localPath := filepath.Join(directory, "Chrono Trigger.srm")
	payload := []byte(content)
	if err := os.WriteFile(localPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	save := target.Save{
		ID:       "local-save-1",
		TargetID: "target-1",
		GameID:   "local-game-1",
		Kind:     "battery",
		Files: []target.File{{
			Path:         localPath,
			LocationID:   "battery",
			RelativePath: "Chrono Trigger.srm",
			Size:         int64(len(payload)),
		}},
	}
	scans := []client.TargetScan{{
		Target: target.Target{ID: "target-1", Adapter: "retroarch"},
		Games: []client.GameScan{{
			Game: target.InstalledGame{
				ID:       "local-game-1",
				TargetID: "target-1",
				Identity: target.GameIdentity{Title: "Chrono Trigger"},
			},
			Saves: []target.Save{save},
		}},
	}}
	state := tracking.NewState()
	state.Games["local-game-1"] = tracking.Game{
		ID:           "local-game-1",
		Adapter:      "retroarch",
		TargetID:     "target-1",
		Title:        "Chrono Trigger",
		ServerGameID: "server-game-1",
	}
	return bindingFixture{localPath: localPath, content: payload, save: save, scans: scans, state: state}
}

func TestTrackingReattachesALocalSaveThatMatchesOneExistingHead(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	digest := sha256.Sum256(fixture.content)
	headID := "revision-2"
	remoteSave := omnisave.Omnisave{ID: "omnisave-1", GameID: "server-game-1", HeadRevisionID: &headID}
	history := []omnisave.Revision{
		{
			ID:         "revision-1",
			OmnisaveID: remoteSave.ID,
			Files: []omnisave.RevisionFile{{
				Path:     "battery/Chrono Trigger.srm",
				Artifact: omnisave.Artifact{Format: "application/octet-stream", SHA256: "different", Size: 9},
			}},
		},
		{
			ID:         headID,
			OmnisaveID: remoteSave.ID,
			Files: []omnisave.RevisionFile{{
				Path: "battery/Chrono Trigger.srm",
				Artifact: omnisave.Artifact{
					Format: "application/octet-stream",
					SHA256: hex.EncodeToString(digest[:]),
					Size:   int64(len(fixture.content)),
				},
			}},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			if err := json.NewEncoder(response).Encode([]omnisave.Omnisave{remoteSave}); err != nil {
				t.Error(err)
			}
		case "/api/v1/omnisaves/omnisave-1/revisions":
			if err := json.NewEncoder(response).Encode(history); err != nil {
				t.Error(err)
			}
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}

	if err := bindUnboundSaves(context.Background(), remoteClient, &fixture.state, fixture.scans, map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}); err != nil {
		t.Fatal(err)
	}

	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, ok := fixture.state.BindingFor(local)
	if !ok || bound.OmnisaveID != remoteSave.ID || bound.LastSyncedRevisionID == nil || *bound.LastSyncedRevisionID != headID {
		t.Fatalf("expected the local save to reattach at the matching head, got %+v", bound)
	}
	if outcome.Rebound != 1 || outcome.Seeded != 0 || outcome.Unbound != 0 || outcome.Failed != 0 {
		t.Fatalf("expected one automatic rebind and no fallback, got %+v", outcome)
	}
}

func TestTrackingCanJumpAStaleLocalSaveToTheMatchingLineageHead(t *testing.T) {
	fixture := newBindingFixture(t, "old-progress")
	headID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "New Game+", HeadRevisionID: &headID,
	}
	matched := testRevision("revision-1", remoteSave.ID, "old-progress")
	head := testRevision(headID, remoteSave.ID, "new-progress")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{matched, head})
		case "/api/v1/artifacts/" + head.Files[0].Artifact.SHA256:
			_, _ = response.Write([]byte("new-progress"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	choose := func(gameTitle, omnisaveName string) (tui.StaleBindingChoice, error) {
		if gameTitle != "Chrono Trigger" || omnisaveName != "New Game+" {
			t.Fatalf("unexpected stale-match prompt: %q %q", gameTitle, omnisaveName)
		}
		return tui.StaleBindingFastForward, nil
	}

	err = bindUnboundSavesWithChooser(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, choose)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new-progress" {
		t.Fatalf("expected the local save to jump to the head, got %q", content)
	}
	assertBinding(t, fixture, remoteSave.ID, headID)
	if outcome.Advanced != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one fast-forward, got %+v", outcome)
	}
}

func TestTrackingCanForkAStaleLocalSaveAtItsMatchingRevision(t *testing.T) {
	fixture := newBindingFixture(t, "old-progress")
	headID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "New Game+", HeadRevisionID: &headID,
	}
	matched := testRevision("revision-1", remoteSave.ID, "old-progress")
	head := testRevision(headID, remoteSave.ID, "new-progress")
	forkHead := "fork-revision-1"
	fork := omnisave.ForkResult{
		Omnisave: omnisave.Omnisave{
			ID: "omnisave-fork", GameID: remoteSave.GameID, DisplayName: "New Game+ (fork)", HeadRevisionID: &forkHead,
		},
		Revision: omnisave.Revision{ID: forkHead, OmnisaveID: "omnisave-fork", Files: matched.Files},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{matched, head})
		case "/api/v1/omnisaves/omnisave-1/forks":
			var input omnisave.ForkOmnisave
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.RevisionID != matched.ID || input.DisplayName != "New Game+ (fork)" {
				t.Fatalf("unexpected fork request: %+v", input)
			}
			response.WriteHeader(http.StatusCreated)
			writeTestJSON(t, response, fork)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	choose := func(_, _ string) (tui.StaleBindingChoice, error) {
		return tui.StaleBindingFork, nil
	}

	err = bindUnboundSavesWithChooser(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, choose)
	if err != nil {
		t.Fatal(err)
	}
	assertBinding(t, fixture, fork.Omnisave.ID, fork.Revision.ID)
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old-progress" {
		t.Fatalf("expected forking to preserve the local content, got %q", content)
	}
	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one fork, got %+v", outcome)
	}
}

func TestDeletingTheLastOmnisaveOnTheServerUntracksTheGameHere(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	device := fixture.state.EnsureDevice("test-device")
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	if err := fixture.state.Bind(local, "omnisave-deleted"); err != nil {
		t.Fatal(err)
	}
	untracked := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{})
		case "/api/v1/games/server-game-1/tracking/" + device.ID:
			if request.Method != http.MethodDelete {
				t.Fatalf("unexpected tracking call: %s", request.Method)
			}
			untracked = true
			response.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected server call: %s %s", request.Method, request.URL.Path)
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}

	err = bindUnboundSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{})
	if err != nil {
		t.Fatal(err)
	}
	if _, tracked := fixture.state.Games["local-game-1"]; tracked || !untracked {
		t.Fatalf("expected the server deletion to untrack the game here, tracked=%v serverUntracked=%v",
			tracked, untracked)
	}
	if _, stillBound := fixture.state.BindingFor(local); stillBound {
		t.Fatalf("expected the dead binding to be dropped")
	}
	if outcome.Untracked != 1 || outcome.Tracked != 0 || outcome.Seeded != 0 || outcome.Failed != 0 {
		t.Fatalf("expected one untrack and no reseed, got %+v", outcome)
	}
}

func TestAnUnboundSaveStillSeedsWhenTheServerHasNoSaves(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	seeded := false
	headID := "seed-revision"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/api/v1/omnisaves" && request.Method == http.MethodGet:
			writeTestJSON(t, response, []omnisave.Omnisave{})
		case request.URL.Path == "/api/v1/omnisaves" && request.Method == http.MethodPost:
			seeded = true
			response.WriteHeader(http.StatusCreated)
			writeTestJSON(t, response, omnisave.Omnisave{ID: "omnisave-new", GameID: "server-game-1", DisplayName: "Save 1"})
		case request.URL.Path == "/api/v1/omnisaves/omnisave-new/revisions":
			response.WriteHeader(http.StatusCreated)
			writeTestJSON(t, response, omnisave.Revision{ID: headID, OmnisaveID: "omnisave-new"})
		default:
			// Artifact uploads and stats vary by content; accept them.
			response.WriteHeader(http.StatusCreated)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}

	err = bindUnboundSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{})
	if err != nil {
		t.Fatal(err)
	}
	if !seeded || outcome.Seeded != 1 {
		t.Fatalf("expected a first-time save to still seed, seeded=%v outcome=%+v", seeded, outcome)
	}
	if _, tracked := fixture.state.Games["local-game-1"]; !tracked {
		t.Fatalf("expected the game to stay tracked after seeding")
	}
}

// TestDeletingAGameInTheDashUntracksItBeforeTheNextPrompt drives the real
// server stack end to end: resolve and track a game, delete it through the
// same API the Dash uses, and confirm the next track run untracks it before
// the selection prompt would preselect it.
func TestDeletingAGameInTheDashUntracksItBeforeTheNextPrompt(t *testing.T) {
	directory := t.TempDir()
	repository, err := sqlitestorage.Open(
		filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	handler := httpapi.New(omnisaveservice.New(repository), catalogservice.New(repository, repository))
	server := httptest.NewServer(handler)
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	scans := []client.TargetScan{{
		Target: target.Target{ID: "target-1", Adapter: "steam"},
		Games: []client.GameScan{{
			Game: target.InstalledGame{
				ID:       "local-game-1",
				TargetID: "target-1",
				Identity: target.GameIdentity{
					Title:       "Draw Steel Codex Playtest",
					Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "999999"}},
				},
			},
		}},
	}}
	state := tracking.NewState()
	state.Games["local-game-1"] = tracking.Game{
		ID: "local-game-1", Adapter: "steam", TargetID: "target-1", Title: "Draw Steel Codex Playtest",
	}

	outcome, confirmed := syncTracking(
		context.Background(), remoteClient, &state, scans, nil, &tui.TrackReport{})
	if !outcome.Synced || outcome.Added != 1 || !confirmed["local-game-1"] {
		t.Fatalf("expected the first run to add and confirm the game, got %+v", outcome)
	}
	serverGameID := state.Games["local-game-1"].ServerGameID
	if serverGameID == "" {
		t.Fatal("expected a resolved Library identity")
	}

	request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/games/"+serverGameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK {
		t.Fatalf("expected the Dash-style delete to succeed, got %d", response.StatusCode)
	}

	reconciled := reconcileDeletedGames(context.Background(), remoteClient, &state, &tui.TrackReport{})
	if reconciled != 1 {
		t.Fatalf("expected reconciliation to untrack the deleted game, got %d", reconciled)
	}
	if state.TrackedIDs()["local-game-1"] {
		t.Fatal("expected the selection prompt to no longer preselect the deleted game")
	}
}

func testRevision(id, omnisaveID, content string) omnisave.Revision {
	digest := sha256.Sum256([]byte(content))
	return omnisave.Revision{
		ID: id, OmnisaveID: omnisaveID,
		Files: []omnisave.RevisionFile{{
			Path: "battery/Chrono Trigger.srm",
			Artifact: omnisave.Artifact{
				Format: "application/octet-stream", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(content)),
			},
		}},
	}
}

func writeTestJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(value); err != nil {
		t.Error(err)
	}
}

func assertBinding(t *testing.T, fixture bindingFixture, omnisaveID, revisionID string) {
	t.Helper()
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, ok := fixture.state.BindingFor(local)
	if !ok || bound.OmnisaveID != omnisaveID || bound.LastSyncedRevisionID == nil || *bound.LastSyncedRevisionID != revisionID {
		t.Fatalf("expected binding to %s at %s, got %+v", omnisaveID, revisionID, bound)
	}
}
