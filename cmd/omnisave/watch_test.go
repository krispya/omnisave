package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// passSink records finished passes and playing markers so a test can wait
// for them.
type passSink struct {
	finished chan tui.PassResult
	playing  chan []string
}

func newPassSink() *passSink {
	return &passSink{
		finished: make(chan tui.PassResult, 16),
		playing:  make(chan []string, 16),
	}
}

func (s *passSink) Watching(int)                       {}
func (s *passSink) PassStarted()                       {}
func (s *passSink) Playing(titles []string)            { s.playing <- titles }
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

// stubProcesses is a process source a test can break, the unreadable process
// table a detection sweep must not turn into an empty playing report.
type stubProcesses struct {
	mu  sync.Mutex
	err error
}

func (p *stubProcesses) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *stubProcesses) Processes(context.Context) ([]running.Process, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return nil, p.err
}

func (p *stubProcesses) Cmdline(context.Context, int32) ([]string, error)   { return nil, nil }
func (p *stubProcesses) OpenPaths(context.Context, int32) ([]string, error) { return nil, nil }

// failingScans wraps fixtureAdapter so a test can break scanning mid-run,
// the way an unreadable target fails a pass.
type failingScans struct {
	fixtureAdapter
	mu     sync.Mutex
	broken bool
}

func (a *failingScans) fail() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.broken = true
}

func (a *failingScans) DiscoverTargets(ctx context.Context) ([]target.Target, error) {
	a.mu.Lock()
	broken := a.broken
	a.mu.Unlock()
	if broken {
		return nil, errors.New("target unreadable")
	}
	return a.fixtureAdapter.DiscoverTargets(ctx)
}

// playingFixtureAdapter is a fixtureAdapter whose games always read as
// playing, giving presence sweeps a matcher to consult.
type playingFixtureAdapter struct {
	fixtureAdapter
}

func (a playingFixtureAdapter) RunningGames(_ context.Context, _ *running.Snapshot, _ target.Target, games []target.InstalledGame) (map[string]bool, error) {
	playing := make(map[string]bool, len(games))
	for _, game := range games {
		playing[game.ID] = true
	}
	return playing, nil
}

func TestAFailedScanKeepsTheStandingPresenceReaffirming(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	adapter := &failingScans{fixtureAdapter: fixtureAdapter{fixture: &fixture}}
	movement := make(chan string)
	loop := watchLoop{
		scanner:  client.NewScanner(nil, adapter),
		server:   server,
		store:    store,
		detector: running.NewDetector(&stubProcesses{}),
		poll:     time.Hour,
		pull:     time.Hour,
		floor:    0,
		settle:   time.Millisecond,
		events:   newAnnouncer(),
		movement: func(context.Context) <-chan string {
			return movement
		},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	// The opening pass establishes presence and affirms it.
	select {
	case result := <-sink.finished:
		if result.Err != nil {
			t.Fatalf("expected the opening pass to succeed, got %v", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the opening pass")
	}
	select {
	case <-sink.playing:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the opening pass to affirm presence")
	}

	// The next pass fails to scan; the standing presence must survive it.
	adapter.fail()
	movement <- remote.LibraryChangedEvent
	select {
	case result := <-sink.finished:
		if result.Err == nil {
			t.Fatal("expected the broken scan to fail the pass")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the failing pass")
	}
	// A pass affirms before it finishes, so the marker is already here — its
	// absence would mean the failure zeroed the presence picture.
	select {
	case <-sink.playing:
	default:
		t.Fatal("expected the failed pass to keep re-affirming the standing presence")
	}
}

func TestADetectorErrorSkipsTheReportInsteadOfClearingIt(t *testing.T) {
	var statusReports atomic.Int32
	server := newObservedServer(t, func(request *http.Request) {
		if request.Method == http.MethodPut && strings.HasSuffix(request.URL.Path, "/status") {
			statusReports.Add(1)
		}
	})
	fixture := newSyncFixture(t, "first-progress")
	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	processes := &stubProcesses{}
	movement := make(chan string)
	loop := watchLoop{
		scanner:  client.NewScanner(nil, playingFixtureAdapter{fixtureAdapter{fixture: &fixture}}),
		server:   server,
		store:    store,
		detector: running.NewDetector(processes),
		poll:     time.Hour,
		pull:     time.Hour,
		floor:    0,
		settle:   time.Millisecond,
		events:   newAnnouncer(),
		movement: func(context.Context) <-chan string {
			return movement
		},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	// The opening pass sees the game playing and reports it.
	select {
	case result := <-sink.finished:
		if result.Err != nil {
			t.Fatalf("expected the opening pass to succeed, got %v", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the opening pass")
	}
	select {
	case titles := <-sink.playing:
		if len(titles) != 1 || titles[0] != "Chrono Trigger" {
			t.Fatalf("expected the playing marker for Chrono Trigger, got %v", titles)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the opening playing marker")
	}
	if reports := statusReports.Load(); reports != 1 {
		t.Fatalf("expected one playing report from the opening pass, got %d", reports)
	}

	// The process table becomes unreadable; the next affirm must skip its
	// report — an empty one would clear a game still being played.
	processes.fail(errors.New("process table unreadable"))
	movement <- remote.LibraryChangedEvent
	select {
	case result := <-sink.finished:
		if result.Err != nil {
			t.Fatalf("expected the pass itself to succeed, got %v", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the second pass")
	}
	if reports := statusReports.Load(); reports != 1 {
		t.Fatalf("expected the failed sweep to send no report, got %d", reports)
	}
	select {
	case titles := <-sink.playing:
		t.Fatalf("expected the failed sweep to leave the marker alone, got %v", titles)
	default:
	}
}

// toggledPlaying is a fixtureAdapter whose games read as playing only while
// the switch is on, so a test can close a game mid-run.
type toggledPlaying struct {
	fixtureAdapter
	mu      sync.Mutex
	playing bool
}

func (a *toggledPlaying) set(playing bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.playing = playing
}

func (a *toggledPlaying) RunningGames(_ context.Context, _ *running.Snapshot, _ target.Target, games []target.InstalledGame) (map[string]bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	matched := make(map[string]bool, len(games))
	for _, game := range games {
		matched[game.ID] = a.playing
	}
	return matched, nil
}

func TestAHandedOffDeferredPullAppliesOnceTheGameCloses(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")
	// The Dash rewound current to the seed while the game was being played:
	// the track run held the pull and handed the waiting game to the loop.
	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                seed.ID,
	}); err != nil {
		t.Fatal(err)
	}
	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	adapter := &toggledPlaying{fixtureAdapter: fixtureAdapter{fixture: &fixture}, playing: true}
	scanner := client.NewScanner(nil, adapter)
	loop := watchLoop{
		scanner:  scanner,
		server:   server,
		store:    store,
		detector: running.NewDetector(&stubProcesses{}),
		poll:     20 * time.Millisecond,
		pull:     time.Hour,
		floor:    0,
		settle:   time.Millisecond,
		events:   newAnnouncer(),
		// The hand-off seeds everything the run established, skipping the
		// opening pass: the files to watch, the presence picture, and the
		// game whose exit resolves the held pull.
		watched:  []string{fixture.localPath},
		presence: trackedPresence(scanner, &fixture.state, fixture.scans),
		deferred: []string{"local-game-1"},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)
	// Sweeps run every poll interval here and each pushes a playing marker;
	// drain them so the sink never backs the loop up.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sink.playing:
			}
		}
	}()

	// While the game keeps playing, the sweeps keep waiting: no file
	// changes, no server events, and the pull ticker is an hour out, so a
	// pass now could only come from the exit watch firing early.
	select {
	case result := <-sink.finished:
		t.Fatalf("expected no pass while the game plays, got %+v", result)
	case <-time.After(300 * time.Millisecond):
	}

	// The game closes. Once the stop grace expires, the next sweep notices
	// and the pass it triggers is what lands the held pull.
	adapter.set(false)
	select {
	case result := <-sink.finished:
		if result.Err != nil {
			t.Fatalf("expected the exit-triggered pass to succeed, got %v", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the game's exit to trigger a pass")
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "first-progress" {
		t.Fatalf("expected the deferred pull to apply once the game closed, got %q", content)
	}
}

func TestSteadyServerEventsCoalesceIntoAPassInsteadOfStarvingIt(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")

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
		settle:  50 * time.Millisecond,
		events:  newAnnouncer(),
		movement: func(context.Context) <-chan string {
			return movement
		},
		watched: []string{fixture.localPath},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	// Events keep arriving faster than the settle window closes; the first
	// one must still win a pass rather than be postponed by the rest.
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case movement <- remote.LibraryChangedEvent:
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	select {
	case result := <-sink.finished:
		if result.Err != nil {
			t.Fatalf("expected the coalesced pass to succeed, got %v", result.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out: steady events starved the settle window")
	}
}
