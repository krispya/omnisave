package savesync_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

func TestTrackingReattachesALocalSaveThatMatchesOneExistingCurrentRevision(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{ID: "omnisave-1", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, CurrentRevisionID: &currentID}
	history := []omnisave.Revision{
		testRevision("revision-1", remoteSave.ID, "different"),
		testRevision(currentID, remoteSave.ID, "saved-game-content"),
	}
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeJSON(t, response, history)
		default:
			http.NotFound(response, request)
		}
	})

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: strictPrompts(t)})

	assertBinding(t, &fixture, remoteSave.ID, currentID)
	if outcome.Rebound != 1 || outcome.Seeded != 0 || outcome.Unbound != 0 || outcome.Failed != 0 {
		t.Fatalf("expected one automatic rebind and no fallback, got %+v", outcome)
	}
}

func TestTrackingCanJumpAStaleLocalSaveToTheCurrentRevision(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "old-progress")
	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "New Game+", CurrentRevisionID: &currentID,
	}
	matched := testRevision("revision-1", remoteSave.ID, "old-progress")
	current := testRevision(currentID, remoteSave.ID, "new-progress")
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeJSON(t, response, []omnisave.Revision{matched, current})
		case "/api/v1/artifacts/" + current.Files[0].Artifact.SHA256:
			_, _ = response.Write([]byte("new-progress"))
		default:
			http.NotFound(response, request)
		}
	})
	prompts := strictPrompts(t)
	prompts.Stale = func(question savesync.StaleQuestion) (savesync.DivergedChoice, error) {
		if question.GameTitle != "Chrono Trigger" || question.OmnisaveName != "New Game+" {
			t.Fatalf("unexpected stale-match prompt: %+v", question)
		}
		return savesync.DivergedJump, nil
	}

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})

	if content := fixture.Read(t); content != "new-progress" {
		t.Fatalf("expected the local save to jump to the current, got %q", content)
	}
	assertBinding(t, &fixture, remoteSave.ID, currentID)
	if outcome.Jumped != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one jump to current, got %+v", outcome)
	}
}

func TestTrackingCanForkAStaleLocalSaveAtItsMatchingRevision(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "old-progress")
	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "New Game+", CurrentRevisionID: &currentID,
	}
	matched := testRevision("revision-1", remoteSave.ID, "old-progress")
	current := testRevision(currentID, remoteSave.ID, "new-progress")
	forkTip := "fork-revision-1"
	fork := omnisave.ForkResult{
		Omnisave: omnisave.Omnisave{
			ID: "omnisave-fork", GameID: remoteSave.GameID, PathFormatVersion: omnisave.PathFormatNative, DisplayName: "New Game+ (fork)", CurrentRevisionID: &forkTip,
		},
		Revision: omnisave.Revision{ID: forkTip, OmnisaveID: "omnisave-fork", Files: matched.Files},
	}
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeJSON(t, response, []omnisave.Revision{matched, current})
		case "/api/v1/omnisaves/omnisave-1/forks":
			var input omnisave.ForkOmnisave
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			// This device has no name, so the fork asks for none and the
			// server's own "(fork)" default applies.
			if input.RevisionID != matched.ID || input.DisplayName != "" {
				t.Fatalf("unexpected fork request: %+v", input)
			}
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, fork)
		default:
			http.NotFound(response, request)
		}
	})
	prompts := strictPrompts(t)
	prompts.Stale = func(savesync.StaleQuestion) (savesync.DivergedChoice, error) {
		return savesync.DivergedFork, nil
	}

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})

	assertBinding(t, &fixture, fork.Omnisave.ID, fork.Revision.ID)
	if content := fixture.Read(t); content != "old-progress" {
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
	fixture := savesynctest.NewFixture(t, "descendant-progress")
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "New Game+", CurrentRevisionID: &currentID,
	}
	current := testRevision(currentID, remoteSave.ID, "restored-progress")
	descendant := testRevision("revision-2", remoteSave.ID, "descendant-progress")
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeJSON(t, response, []omnisave.Revision{current, descendant})
		case "/api/v1/artifacts/" + current.Files[0].Artifact.SHA256:
			_, _ = response.Write([]byte("restored-progress"))
		default:
			http.NotFound(response, request)
		}
	})
	prompted := false
	prompts := strictPrompts(t)
	prompts.Stale = func(question savesync.StaleQuestion) (savesync.DivergedChoice, error) {
		if question.GameTitle != "Chrono Trigger" || question.OmnisaveName != "New Game+" {
			t.Fatalf("unexpected stale-match prompt: %+v", question)
		}
		prompted = true
		return savesync.DivergedJump, nil
	}

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})

	if !prompted {
		t.Fatal("expected the descendant match to reach the stale prompt")
	}
	if content := fixture.Read(t); content != "restored-progress" {
		t.Fatalf("expected adopting to place the current revision's older content, got %q", content)
	}
	assertBinding(t, &fixture, remoteSave.ID, currentID)
	if outcome.Jumped != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one jump to current, got %+v", outcome)
	}
}

func TestAReverseStaleSaveCanForkToKeepItsDescendantContent(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "descendant-progress")
	currentID := "revision-1"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "New Game+", CurrentRevisionID: &currentID,
	}
	current := testRevision(currentID, remoteSave.ID, "restored-progress")
	descendant := testRevision("revision-2", remoteSave.ID, "descendant-progress")
	forkTip := "fork-revision-1"
	fork := omnisave.ForkResult{
		Omnisave: omnisave.Omnisave{
			ID: "omnisave-fork", GameID: remoteSave.GameID, PathFormatVersion: omnisave.PathFormatNative, DisplayName: "New Game+ (fork)", CurrentRevisionID: &forkTip,
		},
		Revision: omnisave.Revision{ID: forkTip, OmnisaveID: "omnisave-fork", Files: descendant.Files},
	}
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeJSON(t, response, []omnisave.Revision{current, descendant})
		case "/api/v1/omnisaves/omnisave-1/forks":
			var input omnisave.ForkOmnisave
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.RevisionID != descendant.ID {
				t.Fatalf("expected the fork to start at the matched descendant, got %+v", input)
			}
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, fork)
		default:
			http.NotFound(response, request)
		}
	})
	prompts := strictPrompts(t)
	prompts.Stale = func(savesync.StaleQuestion) (savesync.DivergedChoice, error) {
		return savesync.DivergedFork, nil
	}

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})

	assertBinding(t, &fixture, fork.Omnisave.ID, fork.Revision.ID)
	if content := fixture.Read(t); content != "descendant-progress" {
		t.Fatalf("expected forking to keep the descendant content, got %q", content)
	}
	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one fork, got %+v", outcome)
	}
}

func TestDeletingTheLastOmnisaveOnTheServerUntracksTheGameHere(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	device := fixture.State.EnsureDevice("test-device")
	if err := fixture.State.Bind(fixture.Local(), "omnisave-deleted"); err != nil {
		t.Fatal(err)
	}
	untracked := false
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{})
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
	})

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: strictPrompts(t)})

	if _, tracked := fixture.State.Games["local-game-1"]; tracked || !untracked {
		t.Fatalf("expected the server deletion to untrack the game here, tracked=%v serverUntracked=%v",
			tracked, untracked)
	}
	if _, stillBound := fixture.State.BindingFor(fixture.Local()); stillBound {
		t.Fatalf("expected the dead binding to be dropped")
	}
	if outcome.Untracked != 1 || outcome.Tracked != 0 || outcome.Seeded != 0 || outcome.Failed != 0 {
		t.Fatalf("expected one untrack and no reseed, got %+v", outcome)
	}
}

func TestAnUnboundSaveStillSeedsWhenTheServerHasNoSaves(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	seeded := false
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/api/v1/omnisaves" && request.Method == http.MethodGet:
			writeJSON(t, response, []omnisave.Omnisave{})
		case request.URL.Path == "/api/v1/omnisaves" && request.Method == http.MethodPost:
			seeded = true
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, omnisave.Omnisave{ID: "omnisave-new", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "Save 1"})
		case request.URL.Path == "/api/v1/omnisaves/omnisave-new/revisions":
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, omnisave.Revision{ID: "seed-revision", OmnisaveID: "omnisave-new"})
		default:
			// Artifact uploads and stats vary by content; accept them.
			response.WriteHeader(http.StatusCreated)
		}
	})

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: strictPrompts(t)})

	if !seeded || outcome.Seeded != 1 {
		t.Fatalf("expected a first-time save to still seed, seeded=%v outcome=%+v", seeded, outcome)
	}
	if _, tracked := fixture.State.Games["local-game-1"]; !tracked {
		t.Fatalf("expected the game to stay tracked after seeding")
	}
}

func TestFreshDeviceCanChooseAndMaterializeAnExistingServerSave(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "saves", "Chrono Trigger.srm")
	currentID := "revision-2"
	remoteSave := omnisave.Omnisave{
		ID: "omnisave-1", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "Main Playthrough", CurrentRevisionID: &currentID,
	}
	current := testRevision(currentID, remoteSave.ID, "server-progress")
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeJSON(t, response, []omnisave.Omnisave{remoteSave})
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeJSON(t, response, []omnisave.Revision{current})
		case "/api/v1/artifacts/" + current.Files[0].Artifact.SHA256:
			_, _ = response.Write([]byte("server-progress"))
		default:
			t.Errorf("unexpected server call: %s %s", request.Method, request.URL.Path)
			http.NotFound(response, request)
		}
	})
	// A Device with the game installed and nothing saved yet.
	fixture := savesynctest.Fixture{
		Scans: []client.TargetScan{{
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
		}},
		State: tracking.NewState(),
	}
	fixture.State.Games["local-game-1"] = tracking.Game{
		ID: "local-game-1", Adapter: "retroarch", TargetID: "target-1",
		Title: "Chrono Trigger", ServerGameID: "server-game-1",
	}

	// A headless pass sees the available save but never writes into the game.
	outcome, report := reconcile(t, server, &fixture, savesync.Options{})
	if _, err := os.Stat(destination); !os.IsNotExist(err) || outcome.Pulled != 0 {
		t.Fatalf("expected the headless pass to leave the destination empty, outcome=%+v err=%v", outcome, err)
	}
	if len(report.Of("SaveAvailable")) != 1 {
		t.Fatalf("expected the headless pass to report the waiting save, got %+v", report.Events)
	}

	// Deciding later is also non-destructive and leaves the offer repeatable.
	prompts := strictPrompts(t)
	prompts.SyncToDevice = func(gameTitle string, options []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
		if gameTitle != "Chrono Trigger" || len(options) != 1 || options[0].Name != "Main Playthrough" {
			t.Fatalf("unexpected sync-to-device prompt: %q %+v", gameTitle, options)
		}
		return savesync.SyncToDeviceChoice{}, nil
	}
	reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("expected deciding later to leave the destination empty, got %v", err)
	}

	// Choosing the lineage verifies and places its current, then binds at it.
	prompts.SyncToDevice = func(string, []savesync.SyncToDeviceOption) (savesync.SyncToDeviceChoice, error) {
		return savesync.SyncToDeviceChoice{OmnisaveID: remoteSave.ID}, nil
	}
	outcome, _ = reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "server-progress" {
		t.Fatalf("expected the selected current on this Device, got %q (%v)", content, err)
	}
	local := tracking.LocalSave{ID: "local-save-1", Adapter: "retroarch", TargetID: "target-1"}
	bound, ok := fixture.State.BindingFor(local)
	if !ok || bound.OmnisaveID != remoteSave.ID || bound.LastSyncedRevisionID == nil || *bound.LastSyncedRevisionID != currentID {
		t.Fatalf("expected a binding at the selected current, got %+v", bound)
	}
	if outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one fresh-device sync, got %+v", outcome)
	}
}

// unmatchedBeside seeds the server's save from the fixture, then unbinds it
// and writes new content, leaving an unmatched local save beside the
// returned binding's omnisave.
func unmatchedBeside(t *testing.T, server savesync.Server, fixture *savesynctest.Fixture) tracking.Binding {
	t.Helper()
	if outcome := savesynctest.SyncOnce(t, server, fixture, savesync.Options{}); outcome.Seeded != 1 {
		t.Fatalf("expected the server save seeded, got %+v", outcome)
	}
	original, ok := fixture.State.BindingFor(fixture.Local())
	if !ok || !fixture.State.Unbind(fixture.Local()) {
		t.Fatal("expected the seeded save to be unbound for the unmatched-save story")
	}
	fixture.Write(t, "local-progress")
	return original
}

// adopting answers the unmatched-save question by syncing with omnisaveID,
// keeping its current save or using this Device's.
func adopting(t *testing.T, omnisaveID string, useLocal bool) savesync.Prompts {
	prompts := strictPrompts(t)
	prompts.Ambiguous = func(_ string, options []savesync.AmbiguousOption) (savesync.AmbiguousChoice, error) {
		if len(options) != 1 || options[0].MatchedRevisionID != "" {
			t.Fatalf("expected one unmatched server save, got %+v", options)
		}
		return savesync.AmbiguousChoice{OmnisaveID: omnisaveID, UseLocal: useLocal}, nil
	}
	return prompts
}

// Keeping the chosen save's current keeps the unmatched progress as a branch
// of that save, named for the Device, before taking current. Nothing is lost
// and no omnisave appears.
func TestAnUnmatchedSaveKeepingTheCurrentSaveKeepsItsProgressAsABranch(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "server-content")
	fixture.State.Device.Name = "Steam Deck"
	original := unmatchedBeside(t, server, &fixture)
	current := *original.LastSyncedRevisionID

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: adopting(t, original.OmnisaveID, false)})

	if outcome.Branched != 1 || outcome.Pulled != 1 || outcome.Seeded != 0 || outcome.Failed != 0 {
		t.Fatalf("expected a branch followed by a completed sync, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "server-content" {
		t.Fatalf("expected the chosen save applied locally, got %q", content)
	}
	assertBinding(t, &fixture, original.OmnisaveID, current)
	if saves := savesynctest.Saves(t, server); len(saves) != 1 || *saves[0].CurrentRevisionID != current {
		t.Fatalf("expected one save, still at its current, got %+v", saves)
	}
	for _, revision := range savesynctest.Revisions(t, server, original.OmnisaveID) {
		if revision.ID == current {
			continue
		}
		if revision.ParentID == nil || *revision.ParentID != current || revision.DisplayName != "Steam Deck" ||
			revision.Files[0].Artifact.SHA256 != artifactOf("local-progress").SHA256 {
			t.Fatalf("expected the local progress as a Steam Deck branch of current, got %+v", revision)
		}
	}
}

// Using this Device's save commits the unmatched content on top of the chosen
// save's current instead, so it becomes current there and the local files
// stay as they are.
func TestAnUnmatchedSaveCanBecomeTheChosenSavesCurrent(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "server-content")
	original := unmatchedBeside(t, server, &fixture)
	replaced := *original.LastSyncedRevisionID

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: adopting(t, original.OmnisaveID, true)})

	if outcome.Pushed != 1 || outcome.Pulled != 0 || outcome.Branched != 0 || outcome.Failed != 0 {
		t.Fatalf("expected using local to only commit, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "local-progress" {
		t.Fatalf("expected the local save untouched, got %q", content)
	}
	saves := savesynctest.Saves(t, server)
	if len(saves) != 1 {
		t.Fatalf("expected no new omnisave, got %+v", saves)
	}
	committed, _ := findRevision(savesynctest.Revisions(t, server, original.OmnisaveID), *saves[0].CurrentRevisionID)
	if committed.ParentID == nil || *committed.ParentID != replaced {
		t.Fatalf("expected the local save on top of the replaced current, got %+v", committed)
	}
	assertBinding(t, &fixture, original.OmnisaveID, committed.ID)
}

// An adoption whose placement fails has already kept the progress as a
// branch, so the next pass finds the content in the chosen save's history.
// An unattended pass waits on it as a stale match rather than binding, and
// jumping there finishes the adoption without a second branch.
func TestAFailedAdoptionResumesAsAStaleMatch(t *testing.T) {
	var failDownloads atomic.Bool
	server := savesynctest.NewInterceptedServer(t, func(response http.ResponseWriter, request *http.Request) bool {
		if failDownloads.Load() && request.Method == http.MethodGet &&
			strings.Contains(request.URL.Path, "/api/v1/artifacts/") {
			http.Error(response, "unavailable", http.StatusInternalServerError)
			return true
		}
		return false
	})
	fixture := savesynctest.NewSyncFixture(t, "server-content")
	original := unmatchedBeside(t, server, &fixture)

	failDownloads.Store(true)
	if outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: adopting(t, original.OmnisaveID, false)}); outcome.Branched != 1 || outcome.Failed != 1 {
		t.Fatalf("expected the progress kept and the adoption to fail, got %+v", outcome)
	}
	failDownloads.Store(false)
	if outcome := syncOnce(t, server, &fixture); outcome.Unbound != 1 || outcome.Rebound != 0 {
		t.Fatalf("expected the stale match to wait, not rebind by content, got %+v", outcome)
	}
	before := savesynctest.Revisions(t, server, original.OmnisaveID)

	jump := strictPrompts(t)
	jump.Stale = func(savesync.StaleQuestion) (savesync.DivergedChoice, error) { return savesync.DivergedJump, nil }
	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: jump})

	if outcome.Jumped != 1 || outcome.Branched != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the jump to finish the adoption, got %+v", outcome)
	}
	if after := savesynctest.Revisions(t, server, original.OmnisaveID); len(after) != len(before) {
		t.Fatalf("expected no second branch, had %d revisions and got %d", len(before), len(after))
	}
	if content := fixture.Read(t); content != "server-content" {
		t.Fatalf("expected the chosen save applied, got %q", content)
	}
	assertBinding(t, &fixture, original.OmnisaveID, *original.LastSyncedRevisionID)
}

// A stale save can become current too: its content is committed on top of
// the Current Revision, so the newer revision it had fallen behind stays in
// history as the parent.
func TestAStaleSaveCanBecomeCurrent(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	_, bound := savesynctest.PushSecondRevision(t, server, &fixture, "second-progress")
	newer := *bound.LastSyncedRevisionID
	fixture.State.Unbind(fixture.Local())
	fixture.Write(t, "first-progress")
	prompts := strictPrompts(t)
	prompts.Stale = func(savesync.StaleQuestion) (savesync.DivergedChoice, error) { return savesync.DivergedUseLocal, nil }

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: prompts})

	if outcome.Pushed != 1 || outcome.Jumped != 0 || outcome.Failed != 0 {
		t.Fatalf("expected using local to only commit, got %+v", outcome)
	}
	if content := fixture.Read(t); content != "first-progress" {
		t.Fatalf("expected the local save untouched, got %q", content)
	}
	saves := savesynctest.Saves(t, server)
	committed, _ := findRevision(savesynctest.Revisions(t, server, bound.OmnisaveID), *saves[0].CurrentRevisionID)
	if len(saves) != 1 || committed.ParentID == nil || *committed.ParentID != newer {
		t.Fatalf("expected the stale content on top of the newer revision, got %+v", committed)
	}
	assertBinding(t, &fixture, bound.OmnisaveID, committed.ID)
}

func TestASaveMatchingTwoForkedLineagesBindsAtTheChosenRevision(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "shared-fork-content")
	first := "revision-a"
	second := "revision-b"
	remoteSaves := []omnisave.Omnisave{
		{ID: "omnisave-1", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "Save 1", CurrentRevisionID: &first},
		{ID: "omnisave-2", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "Save 1 (fork)", CurrentRevisionID: &second},
	}
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/omnisaves":
			writeJSON(t, response, remoteSaves)
		case "/api/v1/omnisaves/omnisave-1/revisions":
			writeJSON(t, response, []omnisave.Revision{testRevision(first, "omnisave-1", "shared-fork-content")})
		case "/api/v1/omnisaves/omnisave-2/revisions":
			writeJSON(t, response, []omnisave.Revision{testRevision(second, "omnisave-2", "shared-fork-content")})
		default:
			t.Errorf("unexpected server call: %s %s", request.Method, request.URL.Path)
			http.NotFound(response, request)
		}
	})
	prompts := strictPrompts(t)
	prompts.Ambiguous = func(_ string, options []savesync.AmbiguousOption) (savesync.AmbiguousChoice, error) {
		if len(options) != 2 || options[0].MatchedRevisionID != first || options[1].MatchedRevisionID != second {
			t.Fatalf("expected both fork twins marked as matches, got %+v", options)
		}
		return savesync.AmbiguousChoice{OmnisaveID: "omnisave-2"}, nil
	}

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})

	assertBinding(t, &fixture, "omnisave-2", second)
	if outcome.Bound != 1 || outcome.Failed != 0 {
		t.Fatalf("expected one chosen binding at the matched revision, got %+v", outcome)
	}
}

func TestAnUnmatchedLocalSaveCanCreateANewSave(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "brand-new-content")
	current := "revision-a"
	seeded := false
	server := stubServer(t, func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/api/v1/omnisaves" && request.Method == http.MethodGet:
			writeJSON(t, response, []omnisave.Omnisave{
				{ID: "omnisave-1", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "Save 1", CurrentRevisionID: &current},
			})
		case request.URL.Path == "/api/v1/omnisaves/omnisave-1/revisions":
			writeJSON(t, response, []omnisave.Revision{testRevision(current, "omnisave-1", "other-content")})
		case request.URL.Path == "/api/v1/omnisaves" && request.Method == http.MethodPost:
			seeded = true
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, omnisave.Omnisave{ID: "omnisave-new", GameID: "server-game-1", PathFormatVersion: omnisave.PathFormatNative, DisplayName: "Save 2"})
		case request.URL.Path == "/api/v1/omnisaves/omnisave-new/revisions":
			response.WriteHeader(http.StatusCreated)
			writeJSON(t, response, omnisave.Revision{ID: "seed-revision", OmnisaveID: "omnisave-new"})
		default:
			response.WriteHeader(http.StatusCreated)
		}
	})
	prompts := strictPrompts(t)
	prompts.Ambiguous = func(string, []savesync.AmbiguousOption) (savesync.AmbiguousChoice, error) {
		return savesync.AmbiguousChoice{Create: true}, nil
	}

	outcome, _ := reconcile(t, server, &fixture, savesync.Options{Prompts: prompts})

	bound, ok := fixture.State.BindingFor(fixture.Local())
	if !seeded || outcome.Seeded != 1 || !ok || bound.OmnisaveID != "omnisave-new" {
		t.Fatalf("expected the create choice to create and bind, got %+v outcome=%+v", bound, outcome)
	}
}

// The Device is what a fork's name exists to record. A Device can be named
// longer than a save may be — a long hostname — so its fork is named for
// the Device alone, trimmed to what the server accepts rather than refused on
// every retry.
func TestADeviceNameTooLongForASaveStillForks(t *testing.T) {
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	fixture.State.Device.Name = strings.Repeat("D", 120)
	savesynctest.SyncOnce(t, server, &fixture, savesync.Options{})
	bound, _ := fixture.State.BindingFor(fixture.Local())
	savesynctest.OtherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	fixture.Write(t, "local-divergence")

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: answering(t, savesync.DivergedFork)})

	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the server to accept the trimmed fork name, got %+v", outcome)
	}
	// Trimmed to exactly the server's 100-rune limit, and to the Device alone.
	forkNamed(t, server, &fixture, strings.Repeat("D", 100))
}

// A source name that leaves no room for the Device loses its own text
// instead: the Device is the whole point of the name, and two Devices trimmed
// to the same text would be indistinguishable.
func TestALongSaveNameIsTrimmedBeforeTheDeviceIs(t *testing.T) {
	ctx := context.Background()
	server := savesynctest.NewServer(t)
	fixture := savesynctest.NewSyncFixture(t, "first-progress")
	savesync.SyncTracking(ctx, server, &fixture.State, fixture.Scans, nil, &savesynctest.Recorder{})
	serverGameID := fixture.State.Games["local-game-1"].ServerGameID
	long, seed, err := binding.Seed(ctx, server, serverGameID, fixture.Save, strings.Repeat("A", 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.State.Bind(fixture.Local(), long.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.State.RecordSynced(fixture.Local(), long.ID, seed.ID); err != nil {
		t.Fatal(err)
	}
	savesynctest.OtherDeviceCommit(t, server, long.ID, "deck-progress")
	fixture.Write(t, "local-divergence")
	fixture.State.Device.Name = "Steam Deck"

	outcome := savesynctest.SyncOnce(t, server, &fixture, savesync.Options{Prompts: answering(t, savesync.DivergedFork)})

	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the server to accept the trimmed fork name, got %+v", outcome)
	}
	// The source gives up exactly the room the Device needs.
	forkNamed(t, server, &fixture, strings.Repeat("A", 87)+" (Steam Deck)")
}
