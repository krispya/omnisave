package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	sqlitestorage "github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

func newRealServer(t *testing.T) *remote.Client {
	return newInterceptedServer(t, nil)
}

// newObservedServer is newRealServer with a hook that sees every request, so
// a test can count the reports a watch loop sends.
func newObservedServer(t *testing.T, observe func(*http.Request)) *remote.Client {
	t.Helper()
	return newInterceptedServer(t, func(_ http.ResponseWriter, request *http.Request) bool {
		observe(request)
		return false
	})
}

// newInterceptedServer is the real server behind a hook that may answer a
// request itself, so a test can break one endpoint mid-run and repair it
// again. A hook returning true has answered; false passes the request
// through to the real handler.
func newInterceptedServer(t *testing.T, intercept func(http.ResponseWriter, *http.Request) bool) *remote.Client {
	t.Helper()
	directory := t.TempDir()
	repository, err := sqlitestorage.Open(
		filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repository.Close() })
	var handler http.Handler = httpapi.New(accessservice.New(repository, "secret"), httpapi.Config{
		Saves:   omnisaveservice.New(repository),
		Catalog: catalogservice.New(repository, repository),
	})
	if intercept != nil {
		wrapped := handler
		handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if intercept(response, request) {
				return
			}
			wrapped.ServeHTTP(response, request)
		})
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	remoteClient, err := remote.New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return remoteClient
}

// newSyncFixture is a binding fixture whose game resolves through the real
// server instead of carrying a preset Library identity.
func newSyncFixture(t *testing.T, content string) bindingFixture {
	t.Helper()
	fixture := newBindingFixture(t, content)
	fixture.scans[0].Games[0].Game.Identity = target.GameIdentity{
		Title:       "Chrono Trigger",
		Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "424242"}},
	}
	game := fixture.state.Games["local-game-1"]
	game.ServerGameID = ""
	fixture.state.Games["local-game-1"] = game
	return fixture
}

// otherDeviceCommit pushes content onto the Omnisave's current revision as if another
// Device had played, and returns the new current revision.
func otherDeviceCommit(t *testing.T, server *remote.Client, omnisaveID, content string) omnisave.Revision {
	t.Helper()
	ctx := context.Background()
	otherPath := filepath.Join(t.TempDir(), "Chrono Trigger.srm")
	if err := os.WriteFile(otherPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	otherSave := target.Save{
		ID: "other-save", TargetID: "other-target", GameID: "other-game", Kind: "battery",
		Files: []target.File{{Path: otherPath, LocationID: "battery", RelativePath: "Chrono Trigger.srm"}},
	}
	saves, err := server.ListOmnisaves(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var current omnisave.Revision
	var currentID string
	for _, save := range saves {
		if save.ID == omnisaveID && save.CurrentRevisionID != nil {
			currentID = *save.CurrentRevisionID
		}
	}
	history, err := server.ListRevisions(ctx, omnisaveID)
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range history {
		if revision.ID == currentID {
			current = revision
		}
	}
	if current.ID == "" {
		t.Fatal("no current revision to commit on top of")
	}
	revision, err := binding.Push(ctx, server, omnisaveID, otherSave, current.ID, current.Files)
	if err != nil {
		t.Fatal(err)
	}
	return *revision
}

func syncOnce(t *testing.T, server *remote.Client, fixture *bindingFixture, prompts *reconcilePrompts, floor time.Duration) tui.TrackOutcome {
	t.Helper()
	return syncOnceGated(t, server, fixture, prompts, nil, floor)
}

// syncOnceGated is syncOnce with a pull gate, for passes that must see a
// game as being played.
func syncOnceGated(t *testing.T, server *remote.Client, fixture *bindingFixture, prompts *reconcilePrompts, gate *pullGate, floor time.Duration) tui.TrackOutcome {
	t.Helper()
	ctx := context.Background()
	outcome, confirmed := syncTracking(ctx, server, &fixture.state, fixture.scans, nil, &tui.TrackReport{})
	if !outcome.Synced {
		t.Fatal("expected the library sync to reach the server")
	}
	if err := reconcileSaves(ctx, nil, server, &fixture.state, fixture.scans, confirmed,
		&outcome, &tui.TrackReport{}, prompts, gate, floor); err != nil {
		t.Fatal(err)
	}
	return outcome
}

// rewriteInPlace replaces a file's bytes and restores the modification time
// it had, so nothing a stat can see about the file has changed. It is how a
// test asks whether a pass read the save or only looked at it.
func rewriteInPlace(t *testing.T, path, content string) {
	t.Helper()
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(content)) != before.Size() {
		t.Fatalf("rewriting in place needs the same size, had %d and want %d", before.Size(), len(content))
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
}

func revisionsOf(t *testing.T, server *remote.Client, omnisaveID string) []omnisave.Revision {
	t.Helper()
	history, err := server.ListRevisions(context.Background(), omnisaveID)
	if err != nil {
		t.Fatal(err)
	}
	return history
}

// A pass used to read every bound save in full every time it ran, which the
// periodic pull and any other Device's commit both trigger. A save whose
// files have not moved under an Omnisave that has not moved cannot need
// anything, and the pass now settles it without opening it.
func TestASettledSaveIsNotReadAgain(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "saved-game-content")
	// The first pass seeds the save; the second proves it equal the reading
	// way and remembers how its files stood.
	syncOnce(t, server, &fixture, nil, 0)
	syncOnce(t, server, &fixture, nil, 0)
	bound, isBound := fixture.state.BindingFor(tracking.LocalSaveFrom(
		fixture.scans[0], fixture.scans[0].Games[0], fixture.save))
	if !isBound {
		t.Fatal("expected the seeded save to be bound")
	}

	// Content the pass cannot see without reading the file.
	rewriteInPlace(t, fixture.localPath, "secretly-rewritten")

	outcome := syncOnce(t, server, &fixture, nil, 0)
	if outcome.Pushed != 0 {
		t.Fatalf("expected a save nothing touched to be settled unread, got %d pushed", outcome.Pushed)
	}
	if history := revisionsOf(t, server, bound.OmnisaveID); len(history) != 1 {
		t.Fatalf("expected the history to stand at the seed, got %d revisions", len(history))
	}
}

// The save's history is the other thing a pass spent on every bound save
// every time: one request per Omnisave, to re-read a history that only
// matters once something has moved.
func TestASettledSaveCostsNoHistoryRequest(t *testing.T) {
	var histories atomic.Int64
	server := newObservedServer(t, func(request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/revisions") && request.Method == http.MethodGet {
			histories.Add(1)
		}
	})
	fixture := newSyncFixture(t, "saved-game-content")
	syncOnce(t, server, &fixture, nil, 0)
	syncOnce(t, server, &fixture, nil, 0)

	histories.Store(0)
	syncOnce(t, server, &fixture, nil, 0)

	if asked := histories.Load(); asked != 0 {
		t.Fatalf("expected a settled save to ask the server for nothing, got %d history requests", asked)
	}
}

// The live view spins the row a pass has in hand. A pass with nothing to do
// must therefore take nothing in hand: a row that spins for a game the pass
// only considered claims a sync is happening when none is, and at the speed
// those decisions are made it claims it as a flicker down the table.
func TestASettledPassTakesNoGameInHand(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "saved-game-content")
	syncOnce(t, server, &fixture, nil, 0)
	syncOnce(t, server, &fixture, nil, 0)

	var held []string
	report := &tui.TrackReport{OnWorking: func(title string) {
		if title != "" {
			held = append(held, title)
		}
	}}
	ctx := context.Background()
	outcome, confirmed := syncTracking(ctx, server, &fixture.state, fixture.scans, nil, report)
	if err := reconcileSaves(ctx, nil, server, &fixture.state, fixture.scans, confirmed,
		&outcome, report, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	if len(held) != 0 {
		t.Fatalf("expected a settled pass to work on nothing, got %v", held)
	}
}

// Telling the server what it already knows is a request per game per pass,
// and a tracked game that is still installed is not news.
func TestASettledPassRestatesNoTracking(t *testing.T) {
	var claims atomic.Int64
	server := newObservedServer(t, func(request *http.Request) {
		if strings.Contains(request.URL.Path, "/tracking/") && request.Method == http.MethodPut {
			claims.Add(1)
		}
	})
	fixture := newSyncFixture(t, "saved-game-content")
	syncOnce(t, server, &fixture, nil, 0)
	if claims.Load() == 0 {
		t.Fatal("expected the first pass to state what this device tracks")
	}

	claims.Store(0)
	syncOnce(t, server, &fixture, nil, 0)
	if stated := claims.Load(); stated != 0 {
		t.Fatalf("expected an unchanged claim to go unrepeated, got %d", stated)
	}

	// A game that stops being installed is a different claim, and the server
	// hears it.
	fixture.scans[0].Games = nil
	syncOnce(t, server, &fixture, nil, 0)
	if stated := claims.Load(); stated != 1 {
		t.Fatalf("expected a changed claim to reach the server, got %d", stated)
	}
}

// The summary is only ever an excuse to skip work, never a reason to miss
// it: both halves of what it stands for are checked against.
func TestASettledSaveIsReadAgainWhenEitherSideMoves(t *testing.T) {
	t.Run("local files move", func(t *testing.T) {
		server := newRealServer(t)
		fixture := newSyncFixture(t, "saved-game-content")
		syncOnce(t, server, &fixture, nil, 0)
		syncOnce(t, server, &fixture, nil, 0)

		if err := os.WriteFile(fixture.localPath, []byte("played-further"), 0o600); err != nil {
			t.Fatal(err)
		}
		if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Pushed != 1 {
			t.Fatalf("expected local progress to commit, got %d pushed", outcome.Pushed)
		}
	})

	t.Run("current revision moves", func(t *testing.T) {
		server := newRealServer(t)
		fixture := newSyncFixture(t, "saved-game-content")
		syncOnce(t, server, &fixture, nil, 0)
		syncOnce(t, server, &fixture, nil, 0)
		bound, _ := fixture.state.BindingFor(tracking.LocalSaveFrom(
			fixture.scans[0], fixture.scans[0].Games[0], fixture.save))

		// Another Device commits: the local files are untouched and their
		// summary still stands, but what it stood for has moved.
		otherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")

		if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Pulled != 1 {
			t.Fatalf("expected the moved current revision to pull, got %+v", outcome)
		}
		if content, _ := os.ReadFile(fixture.localPath); string(content) != "deck-progress" {
			t.Fatalf("expected the pull to land, got %q", content)
		}
	})
}

func TestWatchedPathsIncludeParentDirectoriesSoNewFilesTrigger(t *testing.T) {
	fixture := newBindingFixture(t, "saved-game-content")
	prospective := filepath.Join(t.TempDir(), "future-save")
	fixture.scans[0].Games[0].Destinations = []target.SaveDestination{{
		ID: "future", Locations: []target.SaveLocation{{
			ID: "battery", Path: prospective, Kind: target.SaveLocationDirectory,
		}},
	}}
	paths := watchedFiles(&fixture.state, fixture.scans)

	wantFile := fixture.localPath
	wantDir := filepath.Dir(fixture.localPath)
	foundFile, foundDir, foundProspective := false, false, false
	for _, path := range paths {
		if path == wantFile {
			foundFile = true
		}
		if path == wantDir {
			foundDir = true
		}
		if path == prospective {
			foundProspective = true
		}
	}
	if !foundFile || !foundDir || !foundProspective {
		t.Fatalf("expected current and prospective save paths to be polled, got %q", paths)
	}
}

func TestSyncLifecyclePushesPullsAndReportsDivergence(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")

	// First pass seeds; the seed revision becomes the baseline.
	outcome := syncOnce(t, server, &fixture, nil, 0)
	if outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}

	// A pass with nothing new stays quiet.
	outcome = syncOnce(t, server, &fixture, nil, 0)
	if outcome.Changed() {
		t.Fatalf("expected an in-sync pass to report nothing, got %+v", outcome)
	}

	// Local progress pushes and advances the baseline.
	if err := os.WriteFile(fixture.localPath, []byte("second-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome = syncOnce(t, server, &fixture, nil, 0)
	if outcome.Pushed != 1 || outcome.Failed != 0 {
		t.Fatalf("expected local progress to push, got %+v", outcome)
	}

	// The spacing floor suppresses an immediate follow-up commit.
	if err := os.WriteFile(fixture.localPath, []byte("third-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome = syncOnce(t, server, &fixture, nil, time.Hour)
	if outcome.Changed() {
		t.Fatalf("expected the spacing floor to hold the commit, got %+v", outcome)
	}
	outcome = syncOnce(t, server, &fixture, nil, 0)
	if outcome.Pushed != 1 {
		t.Fatalf("expected the held commit to push without a floor, got %+v", outcome)
	}

	// Another device's progress pulls down automatically.
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, ok := fixture.state.BindingFor(local)
	if !ok {
		t.Fatal("expected a binding after seeding")
	}
	otherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	outcome = syncOnce(t, server, &fixture, nil, 0)
	if outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the current revision to sync down, got %+v", outcome)
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "deck-progress" {
		t.Fatalf("expected the pull to place the current revision's content, got %q", content)
	}

	// Progress on both sides diverges: headless passes report and skip.
	if err := os.WriteFile(fixture.localPath, []byte("local-divergence"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherDeviceCommit(t, server, bound.OmnisaveID, "deck-divergence")
	outcome = syncOnce(t, server, &fixture, nil, 0)
	if outcome.Diverged != 1 || outcome.Pushed != 0 || outcome.Pulled != 0 {
		t.Fatalf("expected headless divergence to only report, got %+v", outcome)
	}
	content, _ = os.ReadFile(fixture.localPath)
	if string(content) != "local-divergence" {
		t.Fatalf("expected headless divergence to leave local content, got %q", content)
	}

	// Jump-to-latest keeps the unsynced local progress as a branch inside
	// the same Omnisave — named for this device, left behind the current
	// pointer — then adopts the current revision. No second save appears.
	fixture.state.Device.Name = "Steam Deck"
	beforeJump, _ := fixture.state.BindingFor(local)
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(question tui.DivergedQuestion) (tui.DivergedBindingChoice, error) {
		// The question names the save forking would have created.
		if question.ForkName != "Save 1 (Steam Deck)" {
			t.Fatalf("expected the question to carry the fork name, got %+v", question)
		}
		return tui.DivergedBindingJump, nil
	}
	outcome = syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Branched != 1 || outcome.Pulled != 1 || outcome.Forked != 0 || outcome.Failed != 0 {
		t.Fatalf("expected jump to keep a branch then pull, got %+v", outcome)
	}
	content, _ = os.ReadFile(fixture.localPath)
	if string(content) != "deck-divergence" {
		t.Fatalf("expected the jump to adopt the current revision, got %q", content)
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 {
		t.Fatalf("expected the jump to leave a single save, got %d", len(saves))
	}
	digest := sha256.Sum256([]byte("local-divergence"))
	var branch omnisave.Revision
	for _, revision := range revisionsOf(t, server, bound.OmnisaveID) {
		for _, file := range revision.Files {
			if file.Artifact.SHA256 == hex.EncodeToString(digest[:]) {
				branch = revision
			}
		}
	}
	if branch.ID == "" {
		t.Fatal("expected the branch to carry the diverged local content")
	}
	if branch.DisplayName != "Steam Deck" {
		t.Fatalf("expected the branch revision named for the device, got %q", branch.DisplayName)
	}
	if branch.ParentID == nil || *branch.ParentID != *beforeJump.LastSyncedRevisionID {
		t.Fatalf("expected the branch to continue the baseline %v, got parent %v",
			beforeJump.LastSyncedRevisionID, branch.ParentID)
	}
	if saves[0].CurrentRevisionID == nil || *saves[0].CurrentRevisionID == branch.ID {
		t.Fatalf("expected the branch to stay behind the current pointer, got current %v", saves[0].CurrentRevisionID)
	}
	rebound, _ := fixture.state.BindingFor(local)
	if rebound.OmnisaveID != bound.OmnisaveID {
		t.Fatalf("expected the binding to stay on the original lineage, got %+v", rebound)
	}
	if rebound.LastSyncedRevisionID == nil || *rebound.LastSyncedRevisionID != *saves[0].CurrentRevisionID {
		t.Fatalf("expected the baseline to advance to current, got %+v", rebound)
	}
}

// pushSecondRevision advances the fixture past its seed so the history has
// an ancestor to rewind to. It returns the seed (ancestor) revision and the
// binding after the push.
func pushSecondRevision(t *testing.T, server *remote.Client, fixture *bindingFixture, content string) (omnisave.Revision, tracking.Binding) {
	t.Helper()
	if outcome := syncOnce(t, server, fixture, nil, 0); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	if err := os.WriteFile(fixture.localPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if outcome := syncOnce(t, server, fixture, nil, 0); outcome.Pushed != 1 {
		t.Fatalf("expected the second pass to push, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, ok := fixture.state.BindingFor(local)
	if !ok || bound.LastSyncedRevisionID == nil {
		t.Fatalf("expected a binding with a baseline after pushing, got %+v", bound)
	}
	history, err := server.ListRevisions(context.Background(), bound.OmnisaveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("expected a seed and one push, got %d revisions", len(history))
	}
	for _, revision := range history {
		if revision.ID != *bound.LastSyncedRevisionID {
			return revision, bound
		}
	}
	t.Fatal("expected an ancestor revision below the baseline")
	return omnisave.Revision{}, tracking.Binding{}
}

func TestSyncPullsARestoredCurrentRevisionBackDown(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")

	// The Dash rewinds current to the seed; this device is now ahead of it.
	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                seed.ID,
	}); err != nil {
		t.Fatal(err)
	}

	outcome := syncOnce(t, server, &fixture, nil, 0)
	if outcome.Pulled != 1 || outcome.Failed != 0 || outcome.Diverged != 0 {
		t.Fatalf("expected a clean device to adopt the restored revision, got %+v", outcome)
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "first-progress" {
		t.Fatalf("expected the local save to revert to the restored revision, got %q", content)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	rewound, _ := fixture.state.BindingFor(local)
	if rewound.LastSyncedRevisionID == nil || *rewound.LastSyncedRevisionID != seed.ID {
		t.Fatalf("expected the baseline to move to the restored revision, got %+v", rewound)
	}
}

func TestAPullWaitsWhileTheGameIsPlayedAndAppliesOnceItCloses(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")
	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                seed.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// The pass runs while the game is being played: the pull waits, the
	// local save is untouched, and the gate records whose exit resolves it.
	gate := &pullGate{playing: map[string]bool{"local-game-1": true}}
	outcome := syncOnceGated(t, server, &fixture, nil, gate, 0)
	if outcome.Deferred != 1 || outcome.Pulled != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the pull deferred under the playing game, got %+v", outcome)
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "second-progress" {
		t.Fatalf("expected the deferred pull to leave the local save alone, got %q", content)
	}
	if len(gate.waiting) != 1 || gate.waiting[0] != "local-game-1" {
		t.Fatalf("expected the gate to record the waiting game, got %v", gate.waiting)
	}

	// The game closed: the same comparison now pulls and moves the baseline.
	outcome = syncOnceGated(t, server, &fixture, nil, &pullGate{}, 0)
	if outcome.Pulled != 1 || outcome.Deferred != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the pull once the game closed, got %+v", outcome)
	}
	content, _ = os.ReadFile(fixture.localPath)
	if string(content) != "first-progress" {
		t.Fatalf("expected the restored revision placed after the game closed, got %q", content)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	rewound, _ := fixture.state.BindingFor(local)
	if rewound.LastSyncedRevisionID == nil || *rewound.LastSyncedRevisionID != seed.ID {
		t.Fatalf("expected the baseline to move to the restored revision, got %+v", rewound)
	}
}

// A rewind under unsynced local progress is not a conflict: the server moved
// its pointer back without adding anything this device lacks, so the local
// content commits as a branch off the baseline it continues, and current
// follows it (FDR-005, decision 15). No prompt, no preservation fork.
func TestARestoreUnderLocalProgressBranchesWithoutAsking(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")
	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                seed.ID,
	}); err != nil {
		t.Fatal(err)
	}
	// The baseline is now a descendant of current, and the game kept writing.
	if err := os.WriteFile(fixture.localPath, []byte("local-edit"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A headless pass resolves it on its own: no prompts are wired in, so a
	// pass that tried to ask would fail rather than branch.
	outcome := syncOnce(t, server, &fixture, nil, 0)
	if outcome.Branched != 1 || outcome.Diverged != 0 || outcome.Forked != 0 || outcome.Failed != 0 {
		t.Fatalf("expected a rewind under local progress to branch, got %+v", outcome)
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "local-edit" {
		t.Fatalf("expected the branch commit to leave the local save alone, got %q", content)
	}

	// The new revision continues the baseline it actually came from, not the
	// restored node, and it is what the Omnisave now points at.
	history, err := server.ListRevisions(context.Background(), bound.OmnisaveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("expected the seed, the baseline, and the branch commit, got %d revisions", len(history))
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	branched, _ := fixture.state.BindingFor(local)
	if branched.LastSyncedRevisionID == nil {
		t.Fatalf("expected the baseline to advance to the branch commit, got %+v", branched)
	}
	committed, found := revisionByID(history, branched.LastSyncedRevisionID)
	if !found {
		t.Fatal("expected the branch commit in the Omnisave's history")
	}
	if committed.ParentID == nil || *committed.ParentID != *bound.LastSyncedRevisionID {
		t.Fatalf("expected the branch commit to attach to the old baseline, got parent %v", committed.ParentID)
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 {
		t.Fatalf("expected branching to create no new Omnisave, got %d saves", len(saves))
	}
	if saves[0].CurrentRevisionID == nil || *saves[0].CurrentRevisionID != committed.ID {
		t.Fatalf("expected the branch commit to become current, got %v", saves[0].CurrentRevisionID)
	}
}

// A rewind that another device has already built on leaves current on a
// sibling branch. That is still not a conflict — nothing this device holds is
// at risk — so its progress branches off its own baseline, and the restored
// node ends up with two children.
func TestProgressBesideASiblingBranchBranchesRatherThanDiverging(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")
	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                seed.ID,
	}); err != nil {
		t.Fatal(err)
	}
	// Another device adopts the rewind and plays on from it, so current now
	// sits on a branch this device's baseline never touched.
	sibling := otherDeviceCommit(t, server, bound.OmnisaveID, "deck-after-rewind")
	if err := os.WriteFile(fixture.localPath, []byte("local-edit"), 0o600); err != nil {
		t.Fatal(err)
	}

	outcome := syncOnce(t, server, &fixture, nil, 0)
	if outcome.Branched != 1 || outcome.Diverged != 0 || outcome.Failed != 0 {
		t.Fatalf("expected progress beside a sibling branch to branch, got %+v", outcome)
	}
	history, err := server.ListRevisions(context.Background(), bound.OmnisaveID)
	if err != nil {
		t.Fatal(err)
	}
	children := 0
	for _, revision := range history {
		if revision.ParentID != nil && *revision.ParentID == seed.ID {
			children++
		}
	}
	if children != 2 {
		t.Fatalf("expected the restored node to carry two branches, got %d children", children)
	}
	// The sibling's work is untouched; it simply is no longer current.
	if _, found := revisionByID(history, &sibling.ID); !found {
		t.Fatal("expected the other device's revision to survive branching")
	}
}

func TestACommitAgainstAMovedCurrentRevisionReturnsTheTypedConflict(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, _ := fixture.state.BindingFor(local)
	moved := otherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	if err := os.WriteFile(fixture.localPath, []byte("stale-progress"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A commit still expecting the old baseline is refused with the typed
	// conflict carrying where current actually is.
	_, err := binding.Push(context.Background(), server, bound.OmnisaveID,
		fixture.save, *bound.LastSyncedRevisionID, nil)
	var conflict *remote.CurrentRevisionConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected a typed current revision conflict, got %v", err)
	}
	if conflict.ActualCurrentRevisionID == nil || *conflict.ActualCurrentRevisionID != moved.ID {
		t.Fatalf("expected the conflict to carry the moved revision, got %+v", conflict)
	}
}

func TestDivergedSaveCanForkHereAndContinueLocally(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, _ := fixture.state.BindingFor(local)
	otherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	if err := os.WriteFile(fixture.localPath, []byte("local-divergence"), 0o600); err != nil {
		t.Fatal(err)
	}

	fixture.state.Device.Name = "Steam Deck"
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(tui.DivergedQuestion) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingFork, nil
	}
	outcome := syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Forked != 1 || outcome.Pulled != 0 || outcome.Failed != 0 {
		t.Fatalf("expected fork-here to only fork, got %+v", outcome)
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "local-divergence" {
		t.Fatalf("expected fork-here to keep local content, got %q", content)
	}
	forked, _ := fixture.state.BindingFor(local)
	if forked.OmnisaveID == bound.OmnisaveID || forked.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to continue on a new lineage, got %+v", forked)
	}
	// The fork says which device it deconflicts.
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, save := range saves {
		// The name links the fork to the lineage it left and the device
		// whose progress it keeps.
		if save.ID == forked.OmnisaveID && save.DisplayName != "Save 1 (Steam Deck)" {
			t.Fatalf("expected the fork named for its source and device, got %q", save.DisplayName)
		}
	}

	// The next pass is quiet: the fork's current revision is exactly the local content.
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Changed() {
		t.Fatalf("expected the forked lineage to be in sync, got %+v", outcome)
	}
}

// divergeOntoOldContent builds a divergence whose local content the server
// already holds: the save reverts to the seed revision's bytes while another
// device moves current further along the same line. Nothing local is unsynced,
// so neither answer should have to preserve anything.
func divergeOntoOldContent(t *testing.T, server *remote.Client, fixture *bindingFixture) (tracking.LocalSave, tracking.Binding) {
	t.Helper()
	_, bound := pushSecondRevision(t, server, fixture, "second-progress")
	otherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	if err := os.WriteFile(fixture.localPath, []byte("first-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	if outcome := syncOnce(t, server, fixture, nil, 0); outcome.Diverged != 1 {
		t.Fatalf("expected a headless pass to report the divergence, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	return local, bound
}

func TestJumpingFromContentTheHistoryAlreadyHoldsKeepsNoBranch(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	local, bound := divergeOntoOldContent(t, server, &fixture)
	before := revisionsOf(t, server, bound.OmnisaveID)

	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(tui.DivergedQuestion) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingJump, nil
	}
	outcome := syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Pulled != 1 || outcome.Branched != 0 || outcome.Forked != 0 || outcome.Failed != 0 {
		t.Fatalf("expected jump to pull without preserving, got %+v", outcome)
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "deck-progress" {
		t.Fatalf("expected the jump to adopt the current revision, got %q", content)
	}
	// The matched revision was the proof the server already had the content.
	if after := revisionsOf(t, server, bound.OmnisaveID); len(after) != len(before) {
		t.Fatalf("expected no new revision, had %d and got %d", len(before), len(after))
	}
	rebound, _ := fixture.state.BindingFor(local)
	if rebound.OmnisaveID != bound.OmnisaveID || rebound.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to advance on the same lineage, got %+v", rebound)
	}
}

func TestForkingFromContentTheHistoryAlreadyHoldsSharesTheMatchedRevision(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	local, bound := divergeOntoOldContent(t, server, &fixture)
	before := revisionsOf(t, server, bound.OmnisaveID)

	fixture.state.Device.Name = "Steam Deck"
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(tui.DivergedQuestion) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingFork, nil
	}
	outcome := syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Forked != 1 || outcome.Pulled != 0 || outcome.Failed != 0 {
		t.Fatalf("expected fork-here to fork without pushing, got %+v", outcome)
	}
	forked, _ := fixture.state.BindingFor(local)
	if forked.OmnisaveID == bound.OmnisaveID || forked.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to continue on a new lineage, got %+v", forked)
	}
	// The fork begins at the matched revision — shared ancestry, no copy.
	matched := false
	for _, revision := range before {
		if revision.ID == *forked.LastSyncedRevisionID {
			matched = true
		}
	}
	if !matched {
		t.Fatal("expected the fork to share the matched revision instead of pushing a new one")
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, save := range saves {
		// The name links the fork to the lineage it left and the device
		// whose progress it keeps.
		if save.ID == forked.OmnisaveID && save.DisplayName != "Save 1 (Steam Deck)" {
			t.Fatalf("expected the fork named for its source and device, got %q", save.DisplayName)
		}
	}
	// The next pass is quiet: the fork's current revision is the local content.
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Changed() {
		t.Fatalf("expected the forked lineage to be in sync, got %+v", outcome)
	}
}

// A manual bind to non-matching content leaves a binding with no baseline,
// which is diverged from the start — but a local save that turns out to equal
// the Current Revision has nothing to resolve, so even a headless pass
// rebinds it silently instead of waiting for a question.
func TestABaselinelessBindingMatchingCurrentRebindsSilently(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, _ := fixture.state.BindingFor(local)
	// Rebinding drops the baseline, as a manual `omnisave bind` would.
	if err := fixture.state.Bind(local, bound.OmnisaveID); err != nil {
		t.Fatal(err)
	}

	outcome := syncOnce(t, server, &fixture, nil, 0)
	if outcome.Rebound != 1 || outcome.Diverged != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the matching save to rebind silently, got %+v", outcome)
	}
	rebound, _ := fixture.state.BindingFor(local)
	if rebound.LastSyncedRevisionID == nil {
		t.Fatalf("expected the rebind to restore a baseline, got %+v", rebound)
	}
}

// seedForeignLayoutLineage resolves the fixture's game and seeds it an
// Omnisave whose revisions live in another save's layout — the shape a Steam
// Cloud lineage has next to a native-folder Local Save. No Local Save of the
// fixture can ever adopt its Current Revision.
func seedForeignLayoutLineage(t *testing.T, server *remote.Client, fixture *bindingFixture) *omnisave.Omnisave {
	t.Helper()
	ctx := context.Background()
	outcome, _ := syncTracking(ctx, server, &fixture.state, fixture.scans, nil, &tui.TrackReport{})
	if !outcome.Synced {
		t.Fatal("expected the library sync to reach the server")
	}
	serverGameID := fixture.state.Games["local-game-1"].ServerGameID
	if serverGameID == "" {
		t.Fatal("expected tracking to resolve the game")
	}
	cloudPath := filepath.Join(t.TempDir(), "profile.save")
	if err := os.WriteFile(cloudPath, []byte("cloud-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	cloudSave := target.Save{
		ID: "cloud-save", TargetID: "cloud-target", GameID: "cloud-game", Kind: "cloud",
		Files: []target.File{{Path: cloudPath, LocationID: "remote", RelativePath: "profile.save"}},
	}
	seeded, _, err := binding.Seed(ctx, server, serverGameID, cloudSave, "")
	if err != nil {
		t.Fatal(err)
	}
	return seeded
}

// A binding whose lineage is spelled in another save's layout can never adopt
// that lineage's current: no jump can place its files here. The answer
// refuses before preserving anything, so repeating it stacks nothing.
func TestJumpToAForeignLayoutCurrentFailsBeforePreserving(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "local-progress")
	seeded := seedForeignLayoutLineage(t, server, &fixture)
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	if err := fixture.state.Bind(local, seeded.ID); err != nil {
		t.Fatal(err)
	}

	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(tui.DivergedQuestion) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingJump, nil
	}
	outcome := syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Failed != 1 || outcome.Forked != 0 || outcome.Pulled != 0 {
		t.Fatalf("expected the jump refused with nothing preserved, got %+v", outcome)
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "local-progress" {
		t.Fatalf("expected the local save untouched, got %q", content)
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 {
		t.Fatalf("expected no preservation minted for a refused jump, got %+v", saves)
	}
}

// A jump that fails after preserving — an outage the preservation itself
// survived — records what it created. The next answer resumes that exact
// preservation instead of minting a duplicate.
func TestARepeatedJumpAnswerReusesTheEarlierPreservation(t *testing.T) {
	var failDownloads atomic.Bool
	server := newInterceptedServer(t, func(response http.ResponseWriter, request *http.Request) bool {
		if failDownloads.Load() && request.Method == http.MethodGet &&
			strings.Contains(request.URL.Path, "/api/v1/artifacts/") {
			http.Error(response, "unavailable", http.StatusInternalServerError)
			return true
		}
		return false
	})
	fixture := newSyncFixture(t, "server-content")
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, _ := fixture.state.BindingFor(local)
	if err := os.WriteFile(fixture.localPath, []byte("local-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Rebinding drops the baseline: unmatched content, diverged from the start.
	if err := fixture.state.Bind(local, bound.OmnisaveID); err != nil {
		t.Fatal(err)
	}
	fixture.state.Device.Name = "Steam Deck"
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(tui.DivergedQuestion) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingJump, nil
	}

	failDownloads.Store(true)
	outcome := syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Forked != 1 || outcome.Failed != 1 {
		t.Fatalf("expected the preservation to land and the pull to fail, got %+v", outcome)
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 2 {
		t.Fatalf("expected the lineage and one preservation, got %+v", saves)
	}

	failDownloads.Store(false)
	outcome = syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Pulled != 1 || outcome.Forked != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the retry to reuse the preservation and pull, got %+v", outcome)
	}
	saves, err = server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 2 {
		t.Fatalf("expected the retry to mint nothing new, got %+v", saves)
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "server-content" {
		t.Fatalf("expected the jump to adopt the current revision, got %q", content)
	}
	rebound, _ := fixture.state.BindingFor(local)
	if rebound.OmnisaveID != bound.OmnisaveID || rebound.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding settled on the rejoined lineage, got %+v", rebound)
	}
}

// A preservation fork that failed before its push holds none of the progress
// it was made to keep, and reads as that progress saved when it is not. The
// failed answer removes it, so a retry starts clean instead of stacking
// empty forks.
func TestAFailedForkPreservationLeavesNoEmptyFork(t *testing.T) {
	var failCommits atomic.Bool
	server := newInterceptedServer(t, func(response http.ResponseWriter, request *http.Request) bool {
		if failCommits.Load() && request.Method == http.MethodPost &&
			strings.HasSuffix(request.URL.Path, "/revisions") {
			http.Error(response, "unavailable", http.StatusInternalServerError)
			return true
		}
		return false
	})
	fixture := newSyncFixture(t, "first-progress")
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, _ := fixture.state.BindingFor(local)
	otherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	if err := os.WriteFile(fixture.localPath, []byte("local-divergence"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.state.Device.Name = "Steam Deck"
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(tui.DivergedQuestion) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingFork, nil
	}

	failCommits.Store(true)
	outcome := syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Failed != 1 || outcome.Forked != 0 {
		t.Fatalf("expected the fork answer to fail cleanly, got %+v", outcome)
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 {
		t.Fatalf("expected the empty fork removed, got %+v", saves)
	}

	failCommits.Store(false)
	outcome = syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the retried fork to succeed, got %+v", outcome)
	}
	saves, err = server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 2 {
		t.Fatalf("expected exactly one preservation fork, got %+v", saves)
	}
	forked, _ := fixture.state.BindingFor(local)
	if forked.OmnisaveID == bound.OmnisaveID || forked.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to continue on the fork, got %+v", forked)
	}
}

// An outage can take down the fork's push and the cleanup that would remove
// the empty fork. The answer records the fork instead, and the retry pushes
// onto that same fork rather than stacking another beside it.
func TestAForkOutageThatAlsoBlocksCleanupResumesTheSameFork(t *testing.T) {
	var broken atomic.Bool
	server := newInterceptedServer(t, func(response http.ResponseWriter, request *http.Request) bool {
		blocked := (request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/revisions")) ||
			request.Method == http.MethodDelete
		if broken.Load() && blocked {
			http.Error(response, "unavailable", http.StatusInternalServerError)
			return true
		}
		return false
	})
	fixture := newSyncFixture(t, "first-progress")
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, _ := fixture.state.BindingFor(local)
	otherDeviceCommit(t, server, bound.OmnisaveID, "deck-progress")
	if err := os.WriteFile(fixture.localPath, []byte("local-divergence"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.state.Device.Name = "Steam Deck"
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(tui.DivergedQuestion) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingFork, nil
	}

	broken.Store(true)
	outcome := syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Failed != 1 || outcome.Forked != 0 {
		t.Fatalf("expected the fork answer to fail, got %+v", outcome)
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 2 {
		t.Fatalf("expected the unremovable empty fork to stand, got %+v", saves)
	}
	var emptyFork omnisave.Omnisave
	for _, save := range saves {
		if save.ID != bound.OmnisaveID {
			emptyFork = save
		}
	}
	if recorded, ok := fixture.state.PendingPreservationFor(local); !ok || recorded != emptyFork.ID {
		t.Fatalf("expected the empty fork recorded for resumption, got %q %v", recorded, ok)
	}

	broken.Store(false)
	outcome = syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the retried fork to succeed, got %+v", outcome)
	}
	saves, err = server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 2 {
		t.Fatalf("expected the retry to resume the recorded fork, got %+v", saves)
	}
	forked, _ := fixture.state.BindingFor(local)
	if forked.OmnisaveID != emptyFork.ID || forked.LastSyncedRevisionID == nil {
		t.Fatalf("expected the binding to continue on the resumed fork, got %+v", forked)
	}
	if _, ok := fixture.state.PendingPreservationFor(local); ok {
		t.Fatal("expected the settled answer to clear the recorded preservation")
	}
}

// Another lineage holding the same bytes is not this Device's preservation.
// A fork answer must never adopt it — only a preservation the answer itself
// recorded — because the twin is an independent playthrough whose future
// updates would otherwise cross into this Device's line.
func TestAForkAnswerNeverAdoptsATwinLineage(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "server-content")
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, _ := fixture.state.BindingFor(local)
	if err := os.WriteFile(fixture.localPath, []byte("local-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Rebinding drops the baseline: unmatched content, diverged from the start.
	if err := fixture.state.Bind(local, bound.OmnisaveID); err != nil {
		t.Fatal(err)
	}
	// An independent lineage that happens to hold the same bytes right now.
	serverGameID := fixture.state.Games["local-game-1"].ServerGameID
	twinPath := filepath.Join(t.TempDir(), "Chrono Trigger.srm")
	if err := os.WriteFile(twinPath, []byte("local-progress"), 0o600); err != nil {
		t.Fatal(err)
	}
	twinSave := target.Save{
		ID: "twin-save", TargetID: "twin-target", GameID: "twin-game", Kind: "battery",
		Files: []target.File{{Path: twinPath, LocationID: "battery", RelativePath: "Chrono Trigger.srm"}},
	}
	twin, _, err := binding.Seed(context.Background(), server, serverGameID, twinSave, "Twin")
	if err != nil {
		t.Fatal(err)
	}

	fixture.state.Device.Name = "Steam Deck"
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(tui.DivergedQuestion) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingFork, nil
	}
	outcome := syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Forked != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the fork to create its own preservation, got %+v", outcome)
	}
	forked, _ := fixture.state.BindingFor(local)
	if forked.OmnisaveID == twin.ID {
		t.Fatal("expected the fork to leave the twin lineage alone")
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 3 {
		t.Fatalf("expected the twin untouched beside a fresh preservation, got %+v", saves)
	}
}

// A game whose only lineages live in another layout offers nothing this save
// could adopt, so an unmatched Local Save creates its own Omnisave without a
// question: one safe outcome remains (FDR-003, decision 1).
func TestAnUnmatchedSaveWithOnlyForeignLayoutLineagesSeedsWithoutAsking(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "local-progress")
	seeded := seedForeignLayoutLineage(t, server, &fixture)

	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	outcome := syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Seeded != 1 || outcome.Failed != 0 || outcome.Unbound != 0 {
		t.Fatalf("expected the unmatched save seeded without asking, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, ok := fixture.state.BindingFor(local)
	if !ok || bound.OmnisaveID == seeded.ID || bound.LastSyncedRevisionID == nil {
		t.Fatalf("expected a fresh lineage bound with a baseline, got %+v", bound)
	}
}
