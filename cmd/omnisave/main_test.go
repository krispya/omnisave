package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
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

func TestTrackingReattachesALocalSaveThatMatchesOneExistingCurrentRevision(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	digest := sha256.Sum256(fixture.content)
	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{ID: "omnisave-1", GameID: "server-game-1", CurrentRevisionID: &currentID}
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
			ID:         currentID,
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
	if !ok || bound.OmnisaveID != remoteSave.ID || bound.LastSyncedRevisionID == nil || *bound.LastSyncedRevisionID != currentID {
		t.Fatalf("expected the local save to reattach at the matching current revision, got %+v", bound)
	}
	if outcome.Rebound != 1 || outcome.Seeded != 0 || outcome.Unbound != 0 || outcome.Failed != 0 {
		t.Fatalf("expected one automatic rebind and no fallback, got %+v", outcome)
	}
}

func TestTrackingCanJumpAStaleLocalSaveToTheCurrentRevision(t *testing.T) {
	fixture := newBindingFixture(t, "old-progress")
	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "New Game+", CurrentRevisionID: &currentID,
	}
	matched := testRevision("revision-1", remoteSave.ID, "old-progress")
	current := testRevision(currentID, remoteSave.ID, "new-progress")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{matched, current})
		case "/api/v1/artifacts/" + current.Files[0].Artifact.SHA256:
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
		return tui.StaleBindingJump, nil
	}

	err = reconcileSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, testPrompts(choose, failingAmbiguousChooser(t), t), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new-progress" {
		t.Fatalf("expected the local save to jump to the current, got %q", content)
	}
	assertBinding(t, fixture, remoteSave.ID, currentID)
	if outcome.Jumped != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one jump to current, got %+v", outcome)
	}
}

func TestTrackingCanForkAStaleLocalSaveAtItsMatchingRevision(t *testing.T) {
	fixture := newBindingFixture(t, "old-progress")
	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "New Game+", CurrentRevisionID: &currentID,
	}
	matched := testRevision("revision-1", remoteSave.ID, "old-progress")
	current := testRevision(currentID, remoteSave.ID, "new-progress")
	forkTip := "fork-revision-1"
	fork := omnisave.ForkResult{
		Omnisave: omnisave.Omnisave{
			ID: "omnisave-fork", GameID: remoteSave.GameID, DisplayName: "New Game+ (fork)", CurrentRevisionID: &forkTip,
		},
		Revision: omnisave.Revision{ID: forkTip, OmnisaveID: "omnisave-fork", Files: matched.Files},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{matched, current})
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

	err = reconcileSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, testPrompts(choose, failingAmbiguousChooser(t), t), nil, 0)
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

// After a Dash rewind, an unbound device can hold content matching a
// DESCENDANT of current — the device is ahead, not behind. The unique-match
// stale prompt still owns the choice, and adopting applies the current
// (older) revision.
func TestAReverseStaleSaveGetsTheStalePromptAndCanAdoptTheOlderCurrent(t *testing.T) {
	fixture := newBindingFixture(t, "descendant-progress")
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "New Game+", CurrentRevisionID: &currentID,
	}
	current := testRevision(currentID, remoteSave.ID, "restored-progress")
	descendant := testRevision("revision-2", remoteSave.ID, "descendant-progress")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{current, descendant})
		case "/api/v1/artifacts/" + current.Files[0].Artifact.SHA256:
			_, _ = response.Write([]byte("restored-progress"))
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
	prompted := false
	choose := func(gameTitle, omnisaveName string) (tui.StaleBindingChoice, error) {
		if gameTitle != "Chrono Trigger" || omnisaveName != "New Game+" {
			t.Fatalf("unexpected stale-match prompt: %q %q", gameTitle, omnisaveName)
		}
		prompted = true
		return tui.StaleBindingJump, nil
	}

	err = reconcileSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, testPrompts(choose, failingAmbiguousChooser(t), t), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !prompted {
		t.Fatal("expected the descendant match to reach the stale prompt")
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "restored-progress" {
		t.Fatalf("expected adopting to place the current revision's older content, got %q", content)
	}
	assertBinding(t, fixture, remoteSave.ID, currentID)
	if outcome.Jumped != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one jump to current, got %+v", outcome)
	}
}

func TestAReverseStaleSaveCanForkToKeepItsDescendantContent(t *testing.T) {
	fixture := newBindingFixture(t, "descendant-progress")
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "New Game+", CurrentRevisionID: &currentID,
	}
	current := testRevision(currentID, remoteSave.ID, "restored-progress")
	descendant := testRevision("revision-2", remoteSave.ID, "descendant-progress")
	forkTip := "fork-revision-1"
	fork := omnisave.ForkResult{
		Omnisave: omnisave.Omnisave{
			ID: "omnisave-fork", GameID: remoteSave.GameID, DisplayName: "New Game+ (fork)", CurrentRevisionID: &forkTip,
		},
		Revision: omnisave.Revision{ID: forkTip, OmnisaveID: "omnisave-fork", Files: descendant.Files},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{current, descendant})
		case "/api/v1/omnisaves/omnisave-1/forks":
			var input omnisave.ForkOmnisave
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.RevisionID != descendant.ID {
				t.Fatalf("expected the fork to start at the matched descendant, got %+v", input)
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

	err = reconcileSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, testPrompts(choose, failingAmbiguousChooser(t), t), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertBinding(t, fixture, fork.Omnisave.ID, fork.Revision.ID)
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "descendant-progress" {
		t.Fatalf("expected forking to keep the descendant content, got %q", content)
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
	currentID := "seed-revision"
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
			writeTestJSON(t, response, omnisave.Revision{ID: currentID, OmnisaveID: "omnisave-new"})
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

func TestFreshDeviceCanChooseAndMaterializeAnExistingServerSave(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "saves", "Chrono Trigger.srm")
	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Main Playthrough", CurrentRevisionID: &currentID,
	}
	current := testRevision(currentID, remoteSave.ID, "server-progress")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{current})
		case "/api/v1/artifacts/" + current.Files[0].Artifact.SHA256:
			_, _ = response.Write([]byte("server-progress"))
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
	scans := []client.TargetScan{{
		Target: target.Target{ID: "target-1", Adapter: "retroarch"},
		Games: []client.GameScan{{
			Game: target.InstalledGame{
				ID: "local-game-1", TargetID: "target-1",
				Identity: target.GameIdentity{Title: "Chrono Trigger"},
			},
			Destinations: []target.SaveDestination{{
				ID: "local-save-1", TargetID: "target-1", GameID: "local-game-1", Kind: "battery",
				Locations: []target.SaveLocation{{
					ID: "battery", Path: filepath.Dir(destination), Kind: target.SaveLocationDirectory,
				}},
			}},
		}},
	}}
	state := tracking.NewState()
	state.Games["local-game-1"] = tracking.Game{
		ID: "local-game-1", Adapter: "retroarch", TargetID: "target-1",
		Title: "Chrono Trigger", ServerGameID: "server-game-1",
	}
	confirmed := map[string]bool{"local-game-1": true}

	// A headless pass sees the available save but never writes into the game.
	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	if err := reconcileSaves(context.Background(), remoteClient, &state, scans, confirmed,
		&outcome, &tui.TrackReport{}, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) || outcome.Pulled != 0 {
		t.Fatalf("expected the headless pass to leave the destination empty, outcome=%+v err=%v", outcome, err)
	}

	// Deciding later is also non-destructive and leaves the offer repeatable.
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.empty = func(gameTitle string, options []tui.SyncToDeviceOption) (tui.SyncToDeviceChoice, error) {
		if gameTitle != "Chrono Trigger" || len(options) != 1 || options[0].Name != "Main Playthrough" {
			t.Fatalf("unexpected sync-to-device prompt: %q %+v", gameTitle, options)
		}
		return tui.SyncToDeviceChoice{}, nil
	}
	outcome = tui.TrackOutcome{Tracked: 1, Synced: true}
	if err := reconcileSaves(context.Background(), remoteClient, &state, scans, confirmed,
		&outcome, &tui.TrackReport{}, prompts, nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("expected deciding later to leave the destination empty, got %v", err)
	}

	// Choosing the lineage verifies and places its current, then binds at it.
	prompts.empty = func(string, []tui.SyncToDeviceOption) (tui.SyncToDeviceChoice, error) {
		return tui.SyncToDeviceChoice{OmnisaveID: remoteSave.ID}, nil
	}
	outcome = tui.TrackOutcome{Tracked: 1, Synced: true}
	if err := reconcileSaves(context.Background(), remoteClient, &state, scans, confirmed,
		&outcome, &tui.TrackReport{}, prompts, nil, 0); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "server-progress" {
		t.Fatalf("expected the selected current on this Device, got %q (%v)", content, err)
	}
	local := tracking.LocalSave{ID: "local-save-1", Adapter: "retroarch", TargetID: "target-1"}
	bound, ok := state.BindingFor(local)
	if !ok || bound.OmnisaveID != remoteSave.ID || bound.LastSyncedRevisionID == nil || *bound.LastSyncedRevisionID != currentID {
		t.Fatalf("expected a binding at the selected current, got %+v", bound)
	}
	if outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one fresh-device sync, got %+v", outcome)
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
	handler := httpapi.New(accessservice.New(repository, "secret"), httpapi.Config{
		Saves:   omnisaveservice.New(repository),
		Catalog: catalogservice.New(repository, repository),
	})
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
	request.Header.Set("Authorization", "Bearer secret")
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

// testPrompts builds a prompt set whose diverged prompt fails the test —
// binding-era tests never expect divergence.
func testPrompts(
	stale func(string, string) (tui.StaleBindingChoice, error),
	ambiguous func(string, []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error),
	t *testing.T,
) *reconcilePrompts {
	return &reconcilePrompts{
		empty: failingSyncToDeviceChooser(t), stale: stale,
		ambiguous: ambiguous, diverged: failingDivergedChooser(t),
	}
}

func failingSyncToDeviceChooser(t *testing.T) func(string, []tui.SyncToDeviceOption) (tui.SyncToDeviceChoice, error) {
	return func(gameTitle string, _ []tui.SyncToDeviceOption) (tui.SyncToDeviceChoice, error) {
		t.Fatalf("unexpected sync-to-device prompt for %q", gameTitle)
		return tui.SyncToDeviceChoice{}, nil
	}
}

func failingDivergedChooser(t *testing.T) func(string, string) (tui.DivergedBindingChoice, error) {
	return func(gameTitle, _ string) (tui.DivergedBindingChoice, error) {
		t.Fatalf("unexpected diverged prompt for %q", gameTitle)
		return "", nil
	}
}

func failingAmbiguousChooser(t *testing.T) func(string, []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error) {
	return func(gameTitle string, _ []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error) {
		t.Fatalf("unexpected ambiguity prompt for %q", gameTitle)
		return tui.AmbiguousBindingChoice{}, nil
	}
}

func TestAmbiguousSaveCanBindToAChosenOmnisaveWithNothingSyncedYet(t *testing.T) {
	fixture := newBindingFixture(t, "brand-new-content")
	first := "revision-a"
	second := "revision-b"
	remoteSaves := []omnisave.Omnisave{
		{ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Save 1", CurrentRevisionID: &first},
		{ID: "omnisave-2", GameID: "server-game-1", DisplayName: "Save 2", CurrentRevisionID: &second},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, remoteSaves)
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{testRevision(first, "omnisave-1", "other-content")})
		case "/api/v1/omnisaves/omnisave-2/revisions":
			writeTestJSON(t, response, []omnisave.Revision{testRevision(second, "omnisave-2", "different-content")})
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
	chooseAmbiguous := func(gameTitle string, options []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error) {
		if gameTitle != "Chrono Trigger" || len(options) != 2 ||
			options[0].MatchedRevisionID != "" || options[1].MatchedRevisionID != "" {
			t.Fatalf("expected two unmatched options, got %q %+v", gameTitle, options)
		}
		return tui.AmbiguousBindingChoice{OmnisaveID: "omnisave-2"}, nil
	}

	err = reconcileSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{},
		testPrompts(failingStaleChooser(t), chooseAmbiguous, t), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, ok := fixture.state.BindingFor(local)
	if !ok || bound.OmnisaveID != "omnisave-2" || bound.LastSyncedRevisionID != nil {
		t.Fatalf("expected a baseline-free binding to the chosen Omnisave, got %+v", bound)
	}
	if outcome.Bound != 1 || outcome.Seeded != 0 || outcome.Unbound != 0 || outcome.Failed != 0 {
		t.Fatalf("expected one chosen binding, got %+v", outcome)
	}
}

func TestASaveMatchingTwoForkedLineagesBindsAtTheChosenRevision(t *testing.T) {
	fixture := newBindingFixture(t, "shared-fork-content")
	first := "revision-a"
	second := "revision-b"
	remoteSaves := []omnisave.Omnisave{
		{ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Save 1", CurrentRevisionID: &first},
		{ID: "omnisave-2", GameID: "server-game-1", DisplayName: "Save 1 (fork)", CurrentRevisionID: &second},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeTestJSON(t, response, remoteSaves)
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{testRevision(first, "omnisave-1", "shared-fork-content")})
		case "/api/v1/omnisaves/omnisave-2/revisions":
			writeTestJSON(t, response, []omnisave.Revision{testRevision(second, "omnisave-2", "shared-fork-content")})
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
	chooseAmbiguous := func(_ string, options []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error) {
		if len(options) != 2 || options[0].MatchedRevisionID != first || options[1].MatchedRevisionID != second {
			t.Fatalf("expected both fork twins marked as matches, got %+v", options)
		}
		return tui.AmbiguousBindingChoice{OmnisaveID: "omnisave-2"}, nil
	}

	err = reconcileSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{},
		testPrompts(failingStaleChooser(t), chooseAmbiguous, t), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertBinding(t, fixture, "omnisave-2", second)
	if outcome.Bound != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one chosen binding at the matched revision, got %+v", outcome)
	}
}

func TestAnAmbiguousSaveCanSeedANewOmnisaveOrStayUnbound(t *testing.T) {
	fixture := newBindingFixture(t, "brand-new-content")
	current := "revision-a"
	seeded := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/api/v1/omnisaves" && request.Method == http.MethodGet:
			writeTestJSON(t, response, []omnisave.Omnisave{
				{ID: "omnisave-1", GameID: "server-game-1", DisplayName: "Save 1", CurrentRevisionID: &current},
			})
		case request.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			writeTestJSON(t, response, []omnisave.Revision{testRevision(current, "omnisave-1", "other-content")})
		case request.URL.Path == "/api/v1/omnisaves" && request.Method == http.MethodPost:
			seeded = true
			response.WriteHeader(http.StatusCreated)
			writeTestJSON(t, response, omnisave.Omnisave{ID: "omnisave-new", GameID: "server-game-1", DisplayName: "Save 2"})
		case request.URL.Path == "/api/v1/omnisaves/omnisave-new/revisions":
			response.WriteHeader(http.StatusCreated)
			writeTestJSON(t, response, omnisave.Revision{ID: "seed-revision", OmnisaveID: "omnisave-new"})
		default:
			response.WriteHeader(http.StatusCreated)
		}
	}))
	defer server.Close()
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	outcome := tui.TrackOutcome{Tracked: 1, Synced: true}
	deferring := func(string, []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error) {
		return tui.AmbiguousBindingChoice{}, nil
	}
	err = reconcileSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, testPrompts(failingStaleChooser(t), deferring, t), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Unbound != 1 || seeded {
		t.Fatalf("expected deferring to leave the save unbound, got %+v seeded=%v", outcome, seeded)
	}

	outcome = tui.TrackOutcome{Tracked: 1, Synced: true}
	seeding := func(string, []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error) {
		return tui.AmbiguousBindingChoice{Seed: true}, nil
	}
	err = reconcileSaves(context.Background(), remoteClient, &fixture.state, fixture.scans,
		map[string]bool{"local-game-1": true}, &outcome, &tui.TrackReport{}, testPrompts(failingStaleChooser(t), seeding, t), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, ok := fixture.state.BindingFor(local)
	if !seeded || outcome.Seeded != 1 || !ok || bound.OmnisaveID != "omnisave-new" {
		t.Fatalf("expected the prompt's seed choice to create and bind, got %+v outcome=%+v", bound, outcome)
	}
}

// A run that cannot reach its server can neither resolve games nor bind
// saves, so it stops at the connection instead of spending a scan and a
// selection prompt on work it has to throw away.
func TestATrackRunStopsAtTheConnectionWhenTheSavedServerIsDown(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	address := down.URL
	down.Close()
	state := tracking.NewState()
	state.Server = tracking.Server{URL: address, Token: "secret"}
	statePath := filepath.Join(t.TempDir(), "state.json")

	server, err := ensureServer(context.Background(), tracking.NewStore(statePath), &state, "", "")

	if server != nil || !errors.Is(err, errReported) {
		t.Fatalf("expected the run to stop with a reported failure, got server=%v err=%v", server, err)
	}
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected an unreachable server to leave local state untouched, got %v", statErr)
	}
}

func TestAServerThatRefusesTheTokenIsDistinguishedFromAnOutage(t *testing.T) {
	unauthorized := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
	}))
	defer unauthorized.Close()
	remoteClient, err := remote.New(unauthorized.URL, "stale-token", unauthorized.Client())
	if err != nil {
		t.Fatal(err)
	}

	if err := verifyConnection(context.Background(), remoteClient); !errors.Is(err, errRejectedToken) {
		t.Fatalf("expected a rejected token, got %v", err)
	}
}

// The commandless run is the app: it stops for the questions this device
// has not answered yet, and it is the run that stays open. Track is the
// selection step on its own — it always asks, and it ends with its report.
func TestTheCommandlessRunAsksWhatIsUnansweredAndKeepsWatching(t *testing.T) {
	fresh := tracking.NewState()
	settled := tracking.NewState()
	settled.Games["local-game-1"] = tracking.Game{ID: "local-game-1", Title: "Chrono Trigger"}

	if !appSession.asks(fresh) {
		t.Fatal("expected the first run to ask which games to track")
	}
	if appSession.asks(settled) {
		t.Fatal("expected a device with tracked games to go straight to syncing")
	}
	if !appSession.keepsWatching() {
		t.Fatal("expected the commandless run to stay open watching")
	}

	if !trackSession.asks(settled) {
		t.Fatal("expected track to ask even once games are tracked")
	}
	if trackSession.keepsWatching() {
		t.Fatal("expected track to end with its report")
	}
}

// A run that skips the prompt still applies a selection — the standing one —
// so the scan refreshes what it saw without untracking what it missed.
func TestARunThatDoesNotAskKeepsEveryTrackedGame(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	state := fixture.state
	stale := state.Games["local-game-1"]
	stale.Title = "chrono_trigger"
	state.Games["local-game-1"] = stale
	state.Games["uninstalled-game"] = tracking.Game{
		ID: "uninstalled-game", Title: "Slay the Spire 2", ServerGameID: "server-game-2",
	}

	removed, err := state.ApplyVisible(tracking.FromScans(fixture.scans), standingSelection(state, fixture.scans))
	if err != nil {
		t.Fatal(err)
	}

	if len(removed) != 0 {
		t.Fatalf("expected a run that did not ask to untrack nothing, got %+v", removed)
	}
	if _, tracked := state.Games["uninstalled-game"]; !tracked {
		t.Fatal("expected a game the scan could not see to stay tracked")
	}
	scanned := state.Games["local-game-1"]
	if scanned.Title != "Chrono Trigger" || scanned.ServerGameID != "server-game-1" {
		t.Fatalf("expected the scan to refresh the game and keep its library identity, got %+v", scanned)
	}
}

func failingStaleChooser(t *testing.T) func(string, string) (tui.StaleBindingChoice, error) {
	return func(gameTitle, _ string) (tui.StaleBindingChoice, error) {
		t.Fatalf("unexpected stale prompt for %q", gameTitle)
		return "", nil
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
