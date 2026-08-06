package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

func sentences(events []tui.Event) []string {
	spoken := make([]string, 0, len(events))
	for _, event := range events {
		spoken = append(spoken, event.Sentence)
	}
	return spoken
}

func TestAConditionIsAnnouncedOnceAndAgainOnlyIfItComesBack(t *testing.T) {
	saveless := tui.ReportSnapshot{Games: []tui.GameStatus{
		{Title: "Project Zomboid", Events: []string{"No save available"}},
	}}
	cleared := tui.ReportSnapshot{Games: []tui.GameStatus{{Title: "Project Zomboid"}}}
	watcher := newAnnouncer()
	now := time.Now()

	first := watcher.announce(saveless, now, now)
	repeat := watcher.announce(saveless, now, now)

	if len(first) != 1 || first[0].Sentence != "No save available" || first[0].Title != "Project Zomboid" {
		t.Fatalf("expected the condition announced once, got %v", sentences(first))
	}
	if len(repeat) != 0 {
		t.Fatalf("expected a standing condition to stay quiet, got %v", sentences(repeat))
	}

	watcher.announce(cleared, now, now)
	returned := watcher.announce(saveless, now, now)
	if len(returned) != 1 {
		t.Fatalf("expected a condition that cleared to announce again, got %v", sentences(returned))
	}
}

func TestASyncIsAnnouncedWhenItHappensAndNotWhenItIsMerelyFound(t *testing.T) {
	started := time.Now()
	old := tui.ReportSnapshot{Games: []tui.GameStatus{
		{Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: started.Add(-3 * time.Hour)},
	}}
	fresh := tui.ReportSnapshot{Games: []tui.GameStatus{
		{Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: started.Add(time.Second)},
	}}

	watcher := newAnnouncer()
	found := watcher.announce(old, started, started)
	if len(found) != 0 {
		t.Fatalf("expected a save synced hours ago to be a baseline, got %v", sentences(found))
	}

	moved := watcher.announce(fresh, started, started)
	if len(moved) != 1 || moved[0].Sentence != "synced with Save 1" {
		t.Fatalf("expected the sync announced when it moved, got %v", sentences(moved))
	}
	if quiet := watcher.announce(fresh, started, started); len(quiet) != 0 {
		t.Fatalf("expected the same sync to stay quiet, got %v", sentences(quiet))
	}
}

func TestASaveSeededDuringAPassIsAnnouncedOnFirstSight(t *testing.T) {
	started := time.Now()
	seeded := tui.ReportSnapshot{Games: []tui.GameStatus{
		{Title: "Chrono Trigger", SyncedWith: "Save 1", SyncedAt: started.Add(2 * time.Second)},
	}}

	events := newAnnouncer().announce(seeded, started, started)

	if len(events) != 1 || events[0].Sentence != "synced with Save 1" {
		t.Fatalf("expected a save synced during the pass to be announced, got %v", sentences(events))
	}
}

func TestATrackRunsReportIsNotAnnouncedAgainByTheWatcher(t *testing.T) {
	report := &tui.TrackReport{}
	report.Linked("Slay the Spire 2", "Slay the Spire II")
	report.SyncedWith("Slay the Spire 2", "Save 1", time.Now())
	snapshot := report.Snapshot()

	watcher := newAnnouncer()
	watcher.seen(snapshot)
	events := watcher.announce(snapshot, time.Now(), time.Now())

	if len(events) != 0 {
		t.Fatalf("expected the hand-off to stay quiet about what track printed, got %v", sentences(events))
	}
}

func TestARunWideFailureIsAnnouncedOnce(t *testing.T) {
	failing := tui.ReportSnapshot{General: []string{"Library sync failed — connection refused"}}
	watcher := newAnnouncer()
	now := time.Now()

	first := watcher.announce(failing, now, now)
	repeat := watcher.announce(failing, now, now)

	if len(first) != 1 || first[0].Title != "" {
		t.Fatalf("expected the run-wide failure announced without a game, got %+v", first)
	}
	if len(repeat) != 0 {
		t.Fatalf("expected a persisting failure to stay quiet, got %v", sentences(repeat))
	}
}

// fixtureAdapter serves a binding fixture's target and game through the real
// Scanner, statting save files fresh each scan so a pull's changes are seen.
type fixtureAdapter struct {
	fixture *bindingFixture
}

func (a fixtureAdapter) Name() string { return "retroarch" }

func (a fixtureAdapter) DiscoverTargets(context.Context) ([]target.Target, error) {
	return []target.Target{a.fixture.scans[0].Target}, nil
}

func (a fixtureAdapter) DiscoverGames(_ context.Context, _ target.Target) ([]target.InstalledGame, error) {
	return []target.InstalledGame{a.fixture.scans[0].Games[0].Game}, nil
}

func (a fixtureAdapter) DiscoverSaves(_ context.Context, _ target.Target, _ target.InstalledGame) ([]target.Save, error) {
	save := a.fixture.save
	files := make([]target.File, len(save.Files))
	copy(files, save.Files)
	for index, file := range files {
		info, err := os.Stat(file.Path)
		if err != nil {
			return nil, err
		}
		files[index].Size = info.Size()
		files[index].Modified = info.ModTime()
	}
	save.Files = files
	return []target.Save{save}, nil
}

func (a fixtureAdapter) DiscoverSaveDestinations(context.Context, target.Target, target.InstalledGame) ([]target.SaveDestination, error) {
	return nil, nil
}

// passSink records finished passes so a test can wait for one.
type passSink struct {
	finished chan tui.PassResult
}

func newPassSink() *passSink {
	return &passSink{finished: make(chan tui.PassResult, 16)}
}

func (s *passSink) Watching(int)                       {}
func (s *passSink) PassStarted()                       {}
func (s *passSink) Activity(string)                    {}
func (s *passSink) PassFinished(result tui.PassResult) { s.finished <- result }
func (s *passSink) Requests() <-chan tui.WatchRequest  { return nil }

func TestAServerEventTriggersAPassThatAppliesADashRewind(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")

	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	movement := make(chan string)
	loop := watchLoop{
		scanner: client.NewScanner(nil, fixtureAdapter{fixture: &fixture}),
		server:  server,
		store:   store,
		poll:    time.Hour,
		pull:    time.Hour,
		floor:   0,
		settle:  time.Millisecond,
		events:  newAnnouncer(),
		movement: func(context.Context) <-chan string {
			return movement
		},
		// Seeding watched files skips the loop's opening pass, so the pass
		// under test is the one the event triggers.
		watched: []string{fixture.localPath},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	// The Dash rewinds current to the seed; only the event stream can tell
	// the loop, since nothing local changed and the tickers are hours out.
	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                seed.ID,
	}); err != nil {
		t.Fatal(err)
	}
	movement <- remote.LibraryChangedEvent

	select {
	case result := <-sink.finished:
		if result.Err != nil {
			t.Fatalf("expected the triggered pass to succeed, got %v", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the server event to trigger a pass")
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "first-progress" {
		t.Fatalf("expected the pass to pull the restored revision, got %q", content)
	}
}

func TestADeferredPullAppliesOnceTheGameCloses(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	installRoot := t.TempDir()
	fixture.scans[0].Games[0].Game.InstallRoot = installRoot
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")

	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	lister := &stubLister{}
	lister.set(running.Process{PID: 7, Executable: filepath.Join(installRoot, "game")})
	movement := make(chan string)
	loop := watchLoop{
		scanner:  client.NewScanner(nil, fixtureAdapter{fixture: &fixture}),
		server:   server,
		store:    store,
		poll:     30 * time.Millisecond,
		pull:     time.Hour,
		floor:    0,
		settle:   time.Millisecond,
		events:   newAnnouncer(),
		detector: running.NewDetector(lister),
		movement: func(context.Context) <-chan string {
			return movement
		},
		watched: []string{fixture.localPath},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                seed.ID,
	}); err != nil {
		t.Fatal(err)
	}
	movement <- remote.LibraryChangedEvent

	// The event-triggered pass runs while the game is "playing": deferred.
	select {
	case result := <-sink.finished:
		if result.Err != nil {
			t.Fatalf("expected the deferring pass to succeed, got %v", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the deferring pass")
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "second-progress" {
		t.Fatalf("expected the local save untouched while playing, got %q", content)
	}

	// The game closes; the poll sweep notices the exit and applies the pull.
	lister.set()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case result := <-sink.finished:
			if result.Err != nil {
				t.Fatalf("expected the applying pass to succeed, got %v", result.Err)
			}
			content, _ := os.ReadFile(fixture.localPath)
			if string(content) == "first-progress" {
				return
			}
		case <-deadline:
			content, _ := os.ReadFile(fixture.localPath)
			t.Fatalf("timed out waiting for the deferred pull to apply, local save is %q", content)
		}
	}
}
