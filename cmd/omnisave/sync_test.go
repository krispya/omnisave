package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/omnisave/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	sqlitestorage "github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

func newRealServer(t *testing.T) *remote.Client {
	t.Helper()
	directory := t.TempDir()
	repository, err := sqlitestorage.Open(
		filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repository.Close() })
	handler := httpapi.New(accessservice.New(repository, "secret"), httpapi.Config{
		Saves:   omnisaveservice.New(repository),
		Catalog: catalogservice.New(repository, repository),
	})
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
	ctx := context.Background()
	outcome, confirmed := syncTracking(ctx, server, &fixture.state, fixture.scans, nil, &tui.TrackReport{})
	if !outcome.Synced {
		t.Fatal("expected the library sync to reach the server")
	}
	if err := reconcileSaves(ctx, server, &fixture.state, fixture.scans, confirmed,
		&outcome, &tui.TrackReport{}, prompts, nil, floor); err != nil {
		t.Fatal(err)
	}
	return outcome
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

	// Jump-to-latest preserves the local progress as a fork, then adopts
	// the current revision; the binding stays on the original lineage.
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(string, string) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingJump, nil
	}
	outcome = syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Forked != 1 || outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected jump to preserve then pull, got %+v", outcome)
	}
	content, _ = os.ReadFile(fixture.localPath)
	if string(content) != "deck-divergence" {
		t.Fatalf("expected the jump to adopt the current revision, got %q", content)
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 2 {
		t.Fatalf("expected the original and one preservation fork, got %d saves", len(saves))
	}
	digest := sha256.Sum256([]byte("local-divergence"))
	preservedFound := false
	for _, save := range saves {
		if save.ID == bound.OmnisaveID {
			continue
		}
		if !strings.Contains(save.DisplayName, "(diverged)") {
			t.Fatalf("expected the preservation fork to be named as diverged, got %q", save.DisplayName)
		}
		history, err := server.ListRevisions(context.Background(), save.ID)
		if err != nil {
			t.Fatal(err)
		}
		current := history[len(history)-1]
		for _, file := range current.Files {
			if file.Artifact.SHA256 == hex.EncodeToString(digest[:]) {
				preservedFound = true
			}
		}
	}
	if !preservedFound {
		t.Fatal("expected the fork's current revision to carry the diverged local content")
	}
	rebound, _ := fixture.state.BindingFor(local)
	if rebound.OmnisaveID != bound.OmnisaveID {
		t.Fatalf("expected the binding to stay on the original lineage, got %+v", rebound)
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

func TestARestoreUnderLocalProgressDivergesAndJumpForksFirst(t *testing.T) {
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

	// Headless passes report the divergence and touch nothing.
	outcome := syncOnce(t, server, &fixture, nil, 0)
	if outcome.Diverged != 1 || outcome.Pushed != 0 || outcome.Pulled != 0 || outcome.Failed != 0 {
		t.Fatalf("expected a restored current under local progress to diverge, got %+v", outcome)
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "local-edit" {
		t.Fatalf("expected the headless pass to leave the local save alone, got %q", content)
	}

	// Jump preserves the local progress as a fork, then adopts the older
	// restored current.
	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(string, string) (tui.DivergedBindingChoice, error) {
		return tui.DivergedBindingJump, nil
	}
	outcome = syncOnce(t, server, &fixture, prompts, 0)
	if outcome.Forked != 1 || outcome.Pulled != 1 || outcome.Failed != 0 {
		t.Fatalf("expected the jump to preserve then adopt, got %+v", outcome)
	}
	content, _ = os.ReadFile(fixture.localPath)
	if string(content) != "first-progress" {
		t.Fatalf("expected the jump to place the restored current revision, got %q", content)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	rebound, _ := fixture.state.BindingFor(local)
	if rebound.OmnisaveID != bound.OmnisaveID || rebound.LastSyncedRevisionID == nil ||
		*rebound.LastSyncedRevisionID != seed.ID {
		t.Fatalf("expected the binding to stay on the lineage at the restored revision, got %+v", rebound)
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	preserved := false
	for _, save := range saves {
		if save.ID != bound.OmnisaveID && strings.Contains(save.DisplayName, "(diverged)") {
			preserved = true
		}
	}
	if !preserved {
		t.Fatal("expected the local edit to survive as a preservation fork")
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

	prompts := testPrompts(failingStaleChooser(t), failingAmbiguousChooser(t), t)
	prompts.diverged = func(string, string) (tui.DivergedBindingChoice, error) {
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

	// The next pass is quiet: the fork's current revision is exactly the local content.
	if outcome := syncOnce(t, server, &fixture, nil, 0); outcome.Changed() {
		t.Fatalf("expected the forked lineage to be in sync, got %+v", outcome)
	}
}

// stubLister serves a switchable process list, standing in for the platform
// process sweep so tests decide when a game is "playing".
type stubLister struct {
	mu        sync.Mutex
	processes []running.Process
}

func (l *stubLister) set(processes ...running.Process) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.processes = processes
}

func (l *stubLister) Processes(context.Context) ([]running.Process, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]running.Process(nil), l.processes...), nil
}

// syncOncePlaying mirrors syncOnce with a running-game detector in play.
func syncOncePlaying(t *testing.T, server *remote.Client, fixture *bindingFixture, detector *running.Detector) tui.TrackOutcome {
	t.Helper()
	ctx := context.Background()
	outcome, confirmed := syncTracking(ctx, server, &fixture.state, fixture.scans, nil, &tui.TrackReport{})
	if !outcome.Synced {
		t.Fatal("expected the library sync to reach the server")
	}
	playing := playingNow(ctx, detector, trackedRunningGames(&fixture.state, fixture.scans))
	if err := reconcileSaves(ctx, server, &fixture.state, fixture.scans, confirmed,
		&outcome, &tui.TrackReport{}, nil, playing, 0); err != nil {
		t.Fatal(err)
	}
	return outcome
}

func TestARestoreWhileTheGameIsPlayingDefersThePull(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	installRoot := filepath.Join(t.TempDir(), "chrono-trigger")
	fixture.scans[0].Games[0].Game.InstallRoot = installRoot
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")
	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                seed.ID,
	}); err != nil {
		t.Fatal(err)
	}
	lister := &stubLister{}
	lister.set(running.Process{PID: 7, Executable: filepath.Join(installRoot, "game")})
	detector := running.NewDetector(lister)

	outcome := syncOncePlaying(t, server, &fixture, detector)
	if outcome.Deferred != 1 || outcome.Pulled != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the pull deferred under the playing game, got %+v", outcome)
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "second-progress" {
		t.Fatalf("expected the local save untouched while playing, got %q", content)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	held, _ := fixture.state.BindingFor(local)
	if held.LastSyncedRevisionID == nil || *held.LastSyncedRevisionID != *bound.LastSyncedRevisionID {
		t.Fatalf("expected the baseline held while playing, got %+v", held)
	}

	// The game closes; the next pass applies the restore.
	lister.set()
	outcome = syncOncePlaying(t, server, &fixture, detector)
	if outcome.Pulled != 1 || outcome.Deferred != 0 || outcome.Failed != 0 {
		t.Fatalf("expected the pull once the game closed, got %+v", outcome)
	}
	content, _ = os.ReadFile(fixture.localPath)
	if string(content) != "first-progress" {
		t.Fatalf("expected the restored revision placed, got %q", content)
	}
	pulled, _ := fixture.state.BindingFor(local)
	if pulled.LastSyncedRevisionID == nil || *pulled.LastSyncedRevisionID != seed.ID {
		t.Fatalf("expected the baseline at the restored revision, got %+v", pulled)
	}
}
