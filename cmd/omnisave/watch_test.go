package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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

// passSink records finished passes, playing markers, and the games passes
// took in hand, so a test can wait for them.
type passSink struct {
	finished chan tui.PassResult
	playing  chan []string
	requests chan tui.WatchRequest

	mutex    sync.Mutex
	working  []string
	phases   []string
	watching []int
}

func newPassSink() *passSink {
	return &passSink{
		finished: make(chan tui.PassResult, 16),
		playing:  make(chan []string, 16),
		requests: make(chan tui.WatchRequest, 4),
	}
}

func (s *passSink) Watching(files int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.watching = append(s.watching, files)
}

func (s *passSink) PassStarted()                       {}
func (s *passSink) Playing(titles []string)            { s.playing <- titles }
func (s *passSink) PassFinished(result tui.PassResult) { s.finished <- result }

func (s *passSink) Working(title string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.working = append(s.working, title)
}

func (s *passSink) Phase(message string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.phases = append(s.phases, message)
}

// watchedCounts is how many files the loop said it was watching, after each
// pass, in order.
func (s *passSink) watchedCounts() []int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]int(nil), s.watching...)
}

// reported is every phase the pass described, in order.
func (s *passSink) reported() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]string(nil), s.phases...)
}

// worked is every game the sink was told about, in order, including the
// empty titles that clear the mark.
func (s *passSink) worked() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]string(nil), s.working...)
}

func (s *passSink) Requests() <-chan tui.WatchRequest { return s.requests }

// waitForPass takes the next finished pass, failing rather than hanging.
func waitForPass(t *testing.T, sink *passSink) tui.PassResult {
	t.Helper()
	select {
	case result := <-sink.finished:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a pass to finish")
		return tui.PassResult{}
	}
}

// pendingSave finds the question a pass left waiting, the way the view does.
func pendingSave(t *testing.T, result tui.PassResult) tui.GameStatus {
	t.Helper()
	for _, game := range result.Snapshot.Games {
		if game.Pending != nil {
			return game
		}
	}
	t.Fatalf("expected a pass to leave a question waiting, got %+v", result.Snapshot.Games)
	return tui.GameStatus{}
}

// divergedLoop seeds a divergence and returns a loop watching it, so the
// tests below start where a user actually meets the question.
func divergedLoop(t *testing.T, server *remote.Client, fixture *bindingFixture) watchLoop {
	t.Helper()
	if outcome := syncOnce(t, server, fixture, nil, 0); outcome.Seeded != 1 {
		t.Fatalf("expected the first pass to seed, got %+v", outcome)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, ok := fixture.state.BindingFor(local)
	if !ok {
		t.Fatal("expected a binding after seeding")
	}
	if err := os.WriteFile(fixture.localPath, []byte("local-divergence"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherDeviceCommit(t, server, bound.OmnisaveID, "deck-divergence")

	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	return watchLoop{
		scanner: client.NewScanner(nil, fixtureAdapter{fixture: fixture}),
		server:  server,
		store:   store,
		poll:    time.Hour,
		pull:    time.Hour,
		floor:   0,
		settle:  time.Hour,
		events:  newAnnouncer(),
	}
}

// Watch reports a divergence and cannot resolve it alone, but the user
// watching can answer it in place: the answer replays into the next pass,
// which runs the same reconcile an interactive track run would have.
func TestAnAnsweredDivergenceResolvesWithoutLeavingWatch(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	loop := divergedLoop(t, server, &fixture)

	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	reported := waitForPass(t, sink)
	waiting := pendingSave(t, reported)
	if waiting.Pending.Kind != tui.PendingDiverged {
		t.Fatalf("expected the opening pass to report a divergence, got %+v", waiting.Pending)
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "local-divergence" {
		t.Fatalf("expected the reported divergence to leave local content, got %q", content)
	}

	sink.requests <- tui.WatchRequest{
		Kind:     tui.WatchAnswered,
		Title:    waiting.Title,
		Omnisave: waiting.Pending.OmnisaveName,
		Diverged: tui.DivergedBindingJump,
	}

	answered := waitForPass(t, sink)
	if answered.Err != nil {
		t.Fatalf("expected the replayed answer to reconcile, got %v", answered.Err)
	}
	content, _ = os.ReadFile(fixture.localPath)
	if string(content) != "deck-divergence" {
		t.Fatalf("expected the answer to adopt the current revision, got %q", content)
	}
	saves, err := server.ListOmnisaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(saves) != 1 {
		t.Fatalf("expected the jump to keep a branch instead of forking, got %d saves", len(saves))
	}
	history, err := server.ListRevisions(context.Background(), saves[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("local-divergence"))
	kept := false
	for _, revision := range history {
		for _, file := range revision.Files {
			if file.Artifact.SHA256 == hex.EncodeToString(digest[:]) {
				kept = true
			}
		}
	}
	if !kept {
		t.Fatal("expected the shelved local progress to survive as a branch revision")
	}
}

// An answer belongs to the question it was given for. Once replayed it is
// spent, so a later divergence waits to be asked about rather than
// inheriting a decision the user made about a different one.
func TestAReplayedAnswerIsSpentAndDoesNotResolveTheNextDivergence(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	loop := divergedLoop(t, server, &fixture)

	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	waiting := pendingSave(t, waitForPass(t, sink))
	sink.requests <- tui.WatchRequest{
		Kind:     tui.WatchAnswered,
		Title:    waiting.Title,
		Omnisave: waiting.Pending.OmnisaveName,
		Diverged: tui.DivergedBindingJump,
	}
	if answered := waitForPass(t, sink); answered.Err != nil {
		t.Fatalf("expected the first answer to reconcile, got %v", answered.Err)
	}

	// The same save diverges again. Nobody has answered this one.
	state, err := loop.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	local := tracking.LocalSaveFrom(fixture.scans[0], fixture.scans[0].Games[0], fixture.save)
	bound, ok := state.BindingFor(local)
	if !ok {
		t.Fatal("expected the binding to survive the answer")
	}
	if err := os.WriteFile(fixture.localPath, []byte("later-local"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherDeviceCommit(t, server, bound.OmnisaveID, "later-deck")

	sink.requests <- tui.WatchRequest{Kind: tui.WatchSyncNow}

	next := waitForPass(t, sink)
	if pendingSave(t, next).Pending.Kind != tui.PendingDiverged {
		t.Fatal("expected the new divergence to wait for its own answer")
	}
	content, _ := os.ReadFile(fixture.localPath)
	if string(content) != "later-local" {
		t.Fatalf("expected the unanswered divergence to leave local content, got %q", content)
	}
}

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
	// The pull is the longest thing a rewind does here, and the only place
	// the view can say so is the phase the transfer reports for itself.
	phases := sink.reported()
	if !slices.ContainsFunc(phases, func(phase string) bool {
		return strings.HasPrefix(phase, "downloading")
	}) {
		t.Fatalf("expected the pull to say it was downloading, got %v", phases)
	}
}

// A pass is otherwise silent until it finishes, which leaves a rewind
// arriving from the Dash looking like nothing is happening. It names the
// game it has in hand as it goes, so the live view can spin that row, and
// hands back an empty title when it is done holding one.
func TestAPassNamesTheGameItHasInHand(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	loop := watchLoop{
		scanner: client.NewScanner(nil, fixtureAdapter{fixture: &fixture}),
		server:  server,
		store:   store,
		poll:    time.Hour,
		pull:    time.Hour,
		floor:   0,
		settle:  time.Hour,
		events:  newAnnouncer(),
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	if result := waitForPass(t, sink); result.Err != nil {
		t.Fatalf("expected the opening pass to succeed, got %v", result.Err)
	}
	worked := sink.worked()
	if !slices.Contains(worked, "Chrono Trigger") {
		t.Fatalf("expected the pass to name the game it worked on, got %v", worked)
	}
	if len(worked) == 0 || worked[len(worked)-1] != "" {
		t.Fatalf("expected a finished pass to hold no game, got %v", worked)
	}
}

// A branch is news the pass already stated, so it reads as one line rather
// than its own sentence plus the generic synced line, and the game goes
// straight back to standing state.
func TestABranchIsAnnouncedOnceAndLeavesNoCondition(t *testing.T) {
	started := time.Now()
	established := &tui.TrackReport{}
	established.SyncedWith("Chrono Trigger", "Save 1", started.Add(-3*time.Hour))
	watcher := newAnnouncer()
	watcher.announce(established.Snapshot(), started, started)

	report := &tui.TrackReport{}
	report.Branched("Chrono Trigger", "Save 1")
	report.SyncedWith("Chrono Trigger", "Save 1", started.Add(time.Second))
	snapshot := report.Snapshot()

	events := watcher.announce(snapshot, started, started)
	if len(events) != 1 || events[0].Sentence != "Save branched from Save 1" {
		t.Fatalf("expected one branch line, got %v", sentences(events))
	}
	standing := tui.ComposeStanding(snapshot, tui.Marks{}, started.Add(2*time.Second))
	if len(standing) != 1 || !strings.Contains(standing[0], "synced just now") {
		t.Fatalf("expected the branched save to read as synced, got %v", standing)
	}
	if quiet := watcher.announce(snapshot, started, started); len(quiet) != 0 {
		t.Fatalf("expected a branched save to stay quiet afterwards, got %v", sentences(quiet))
	}
}

// Watch used to stall on a rewind it could not resolve: the save diverged,
// every later pass re-reported it, and nothing synced until someone ran
// track. Branching keeps the loop flowing without a prompt (FDR-005,
// decision 15).
func TestARewoundCurrentUnderLocalProgressKeepsWatchFlowing(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")

	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	loop := watchLoop{
		scanner: client.NewScanner(nil, fixtureAdapter{fixture: &fixture}),
		server:  server,
		store:   store,
		// The local write is the trigger under test, so the poll path — the
		// one that owns the quiet-interval rule — is what runs each pass.
		poll:    20 * time.Millisecond,
		pull:    time.Hour,
		floor:   0,
		settle:  time.Millisecond,
		events:  newAnnouncer(),
		watched: []string{fixture.localPath},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	// The game wrote while the Dash rewound underneath it, so the pass finds
	// progress on both sides with current below the baseline.
	if _, err := server.RestoreCurrentRevision(context.Background(), bound.OmnisaveID, omnisave.RestoreRevision{
		ExpectedCurrentRevisionID: bound.LastSyncedRevisionID,
		RevisionID:                seed.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.localPath, []byte("kept-playing"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := awaitPass(t, sink)
	if result.Err != nil {
		t.Fatalf("expected the branching pass to succeed, got %v", result.Err)
	}
	if !strings.Contains(result.Summary, "1 branched") {
		t.Fatalf("expected the pass to report a branch, got %q", result.Summary)
	}
	if len(result.Events) != 1 || !strings.Contains(result.Events[0].Sentence, "branched from") {
		t.Fatalf("expected one branch event, got %v", sentences(result.Events))
	}
	content, err := os.ReadFile(fixture.localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "kept-playing" {
		t.Fatalf("expected branching to leave the local save alone, got %q", content)
	}

	// The loop is not stuck: the next write commits the ordinary way.
	if err := os.WriteFile(fixture.localPath, []byte("still-playing"), 0o600); err != nil {
		t.Fatal(err)
	}
	next := awaitPass(t, sink)
	if next.Err != nil {
		t.Fatalf("expected the pass after a branch to succeed, got %v", next.Err)
	}
	if strings.Contains(next.Summary, "diverged") || strings.Contains(next.Summary, "branched") {
		t.Fatalf("expected the save to push normally after branching, got %q", next.Summary)
	}
	history, err := server.ListRevisions(context.Background(), bound.OmnisaveID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 {
		t.Fatalf("expected the branch and the push that followed it, got %d revisions", len(history))
	}
}

// awaitPass takes the next finished pass, failing the test rather than
// hanging when the loop never runs one.
func awaitPass(t *testing.T, sink *passSink) tui.PassResult {
	t.Helper()
	select {
	case result := <-sink.finished:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a watch pass")
		return tui.PassResult{}
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
	// heals clears the break as the scan it fails is attempted.
	heals bool
}

func (a *failingScans) fail() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.broken = true
}

// failOnce breaks the next scan and heals on its own, which is the shape of
// every reason a pass fails on a handheld: a network that is not up yet, a
// server restarting, a device carried out of range.
func (a *failingScans) failOnce() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.broken = true
	a.heals = true
}

func (a *failingScans) DiscoverTargets(ctx context.Context) ([]target.Target, error) {
	a.mu.Lock()
	broken := a.broken
	if broken && a.heals {
		a.broken, a.heals = false, false
	}
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

// brokenLibrarySync answers device registration with a server error while it
// is broken. The pass that meets it fails its library sync without failing
// itself, so it never reaches the save comparison — and knows nothing about
// which pulls are still held.
type brokenLibrarySync struct {
	mu     sync.Mutex
	broken bool
}

func (b *brokenLibrarySync) set(broken bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broken = broken
}

func (b *brokenLibrarySync) intercept(response http.ResponseWriter, request *http.Request) bool {
	b.mu.Lock()
	broken := b.broken
	b.mu.Unlock()
	// Registration only: the status endpoint lives under the same prefix,
	// and breaking presence reporting would break the sweeps under test.
	if !broken || request.Method != http.MethodPut ||
		!strings.HasPrefix(request.URL.Path, "/api/v1/devices/") ||
		strings.HasSuffix(request.URL.Path, "/status") {
		return false
	}
	http.Error(response, "device registration unavailable", http.StatusServiceUnavailable)
	return true
}

func TestAPassThatNeverReconciledKeepsTheHeldPullsExitTrigger(t *testing.T) {
	broken := &brokenLibrarySync{}
	server := newInterceptedServer(t, broken.intercept)
	fixture := newSyncFixture(t, "first-progress")
	seed, bound := pushSecondRevision(t, server, &fixture, "second-progress")
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
	movement := make(chan string)
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
		movement: func(context.Context) <-chan string {
			return movement
		},
		watched:  []string{fixture.localPath},
		presence: trackedPresence(scanner, &fixture.state, fixture.scans),
		deferred: []string{"local-game-1"},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sink.playing:
			}
		}
	}()

	// A pass runs while the game is still being played and cannot reach the
	// server to sync the library, so it never reaches the save comparison.
	// It has nothing to say about the held pull — and must not be taken to
	// say the pull is no longer held.
	broken.set(true)
	movement <- remote.LibraryChangedEvent
	select {
	case <-sink.finished:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the pass with the broken library sync")
	}

	// The server comes back and the game closes. The exit trigger the failed
	// pass left standing is what lands the pull; nothing else can, with the
	// pull ticker an hour out and no local writes to poll for.
	broken.set(false)
	adapter.set(false)
	deadline := time.After(20 * time.Second)
	for {
		select {
		case result := <-sink.finished:
			if result.Err != nil {
				t.Fatalf("expected the exit-triggered pass to succeed, got %v", result.Err)
			}
			content, err := os.ReadFile(fixture.localPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) == "first-progress" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the retained exit trigger to land the held pull")
		}
	}
}

// brokenScans is a toggledPlaying whose scanning can be broken, so a test
// can fail every pass while still steering whether the game reads as played.
type brokenScans struct {
	*toggledPlaying
	mu     sync.Mutex
	broken bool
}

func (a *brokenScans) breakScans() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.broken = true
}

func (a *brokenScans) DiscoverTargets(ctx context.Context) ([]target.Target, error) {
	a.mu.Lock()
	broken := a.broken
	a.mu.Unlock()
	if broken {
		return nil, errors.New("target unreadable")
	}
	return a.toggledPlaying.DiscoverTargets(ctx)
}

func TestAFailingExitTriggeredPassRunsOnceAndRearmsOnRelaunch(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	adapter := &brokenScans{
		toggledPlaying: &toggledPlaying{fixtureAdapter: fixtureAdapter{fixture: &fixture}, playing: true},
	}
	scanner := client.NewScanner(nil, adapter)
	presence := trackedPresence(scanner, &fixture.state, fixture.scans)
	adapter.breakScans()
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
		watched:  []string{fixture.localPath},
		presence: presence,
		deferred: []string{"local-game-1"},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sink.playing:
			}
		}
	}()

	// The game closes and its exit triggers the pass that would land the
	// held pull. Scanning is broken, so the pass fails.
	adapter.set(false)
	select {
	case result := <-sink.finished:
		if result.Err == nil {
			t.Fatal("expected the broken scan to fail the exit-triggered pass")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the exit to trigger a pass")
	}

	// The exit is still standing and the sweeps still see it, but the pull
	// waits for the pull ticker now: retrying a failing pass every poll is
	// what every other trigger already refuses to do.
	select {
	case result := <-sink.finished:
		t.Fatalf("expected the exit to trigger one pass, got a retry: %+v", result)
	case <-time.After(500 * time.Millisecond):
	}

	// The game is relaunched and quits again. That is a new exit, not the
	// one already answered, so it gets its own pass.
	adapter.set(true)
	select {
	case <-sink.finished:
		t.Fatal("expected no pass while the relaunched game plays")
	case <-time.After(200 * time.Millisecond):
	}
	adapter.set(false)
	select {
	case result := <-sink.finished:
		if result.Err == nil {
			t.Fatal("expected the broken scan to fail the second pass too")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the relaunched game's exit to re-arm the trigger")
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

// A pass fails for reasons that pass: a service started before the network is
// up, a handheld carried out of range, a server restarting. Until the failed
// pass is repeated this device is protecting nothing, so a failure has to be
// worth less than the pull interval — which is the fallback for a quiet
// server, not the answer to a broken pass.
func TestAFailedPassIsRetriedWithoutWaitingForThePullInterval(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	adapter := &failingScans{fixtureAdapter: fixtureAdapter{fixture: &fixture}}
	adapter.failOnce()
	loop := watchLoop{
		scanner: client.NewScanner(nil, adapter),
		server:  server,
		store:   store,
		// Every scheduled trigger is an hour away, so a second pass can only
		// have come from the retry.
		poll:   time.Hour,
		pull:   time.Hour,
		settle: time.Hour,
		retry:  10 * time.Millisecond,
		floor:  0,
		events: newAnnouncer(),
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	if result := waitForPass(t, sink); result.Err == nil {
		t.Fatal("expected the opening pass to fail on the broken scan")
	}
	if result := waitForPass(t, sink); result.Err != nil {
		t.Fatalf("expected the retry to reconcile, got %v", result.Err)
	}
}

// A failed pass discovered nothing about this device. Adopting its empty file
// list would leave the poll with nothing to compare, so a save written while
// the server was unreachable would go unnoticed even after it came back.
func TestAFailedPassKeepsTheFilesItWasAlreadyWatching(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	adapter := &failingScans{fixtureAdapter: fixtureAdapter{fixture: &fixture}}
	movement := make(chan string)
	loop := watchLoop{
		scanner: client.NewScanner(nil, adapter),
		server:  server,
		store:   store,
		poll:    time.Hour,
		pull:    time.Hour,
		settle:  time.Millisecond,
		// The retry must not run during the assertion: this is about what a
		// failed pass leaves behind, not about it being repeated.
		retry:  time.Hour,
		floor:  0,
		events: newAnnouncer(),
		movement: func(context.Context) <-chan string {
			return movement
		},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	if result := waitForPass(t, sink); result.Err != nil {
		t.Fatalf("expected the opening pass to succeed, got %v", result.Err)
	}
	counts := sink.watchedCounts()
	if len(counts) == 0 || counts[0] == 0 {
		t.Fatalf("expected the opening pass to watch files, got %v", counts)
	}
	established := counts[0]

	adapter.fail()
	movement <- remote.LibraryChangedEvent
	if result := waitForPass(t, sink); result.Err == nil {
		t.Fatal("expected the broken scan to fail the pass")
	}
	counts = sink.watchedCounts()
	if last := counts[len(counts)-1]; last != established {
		t.Errorf("a failed pass left %d files watched; want the %d it had proved", last, established)
	}
}

// The retry is automatic and the pass it repeats is a full pass: fired while
// the game is streaming its save out, it would commit a half-written file. It
// defers to the quiet-interval rule like every other automatic trigger, and
// the failure it carries is not forgotten — the first quiet poll runs the
// pass instead.
func TestTheRetryWaitsOutASaveStillBeingWritten(t *testing.T) {
	server := newRealServer(t)
	fixture := newSyncFixture(t, "first-progress")
	store := tracking.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.Save(fixture.state); err != nil {
		t.Fatal(err)
	}
	adapter := &failingScans{fixtureAdapter: fixtureAdapter{fixture: &fixture}}
	adapter.failOnce()
	movement := make(chan string)
	loop := watchLoop{
		scanner: client.NewScanner(nil, adapter),
		server:  server,
		store:   store,
		// Only the retry can fire on its own here: every ticker is hours
		// out, so a pass inside the window below could only be the retry
		// ignoring the write in flight.
		poll:   time.Hour,
		pull:   time.Hour,
		settle: time.Millisecond,
		retry:  100 * time.Millisecond,
		floor:  0,
		events: newAnnouncer(),
		movement: func(context.Context) <-chan string {
			return movement
		},
		watched: []string{fixture.localPath},
	}
	sink := newPassSink()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go loop.run(ctx, sink)

	// The pass fails while the scan hiccups, arming the retry.
	movement <- remote.LibraryChangedEvent
	if result := waitForPass(t, sink); result.Err == nil {
		t.Fatal("expected the broken scan to fail the pass")
	}

	// The game writes after the failed pass rebased its signature, so the
	// retry finds the save moving.
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(fixture.localPath, []byte("mid-write"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Several retry intervals pass; every one must wait the write out.
	select {
	case result := <-sink.finished:
		t.Fatalf("expected the retry to wait out the write, got a pass: %+v", result)
	case <-time.After(400 * time.Millisecond):
	}

	// The user asks; theirs is the one trigger that never waits, and the
	// loop must not be wedged behind the deferred retry.
	sink.requests <- tui.WatchRequest{Kind: tui.WatchSyncNow}
	if result := waitForPass(t, sink); result.Err != nil {
		t.Fatalf("expected the asked-for pass to reconcile, got %v", result.Err)
	}
}

func TestTheRetryGrowsFromItsFloorAndStopsAtThePullInterval(t *testing.T) {
	floor, pull := 30*time.Second, 2*time.Minute
	wait := nextPassRetry(0, floor, pull)
	if wait != floor {
		t.Fatalf("the first retry waited %s; want the floor %s", wait, floor)
	}
	for _, want := range []time.Duration{time.Minute, 2 * time.Minute, 2 * time.Minute} {
		wait = nextPassRetry(wait, floor, pull)
		if wait != want {
			t.Fatalf("the retry grew to %s; want %s", wait, want)
		}
	}
	// A pull interval shorter than the floor is the ceiling either way: there
	// is nothing to gain from retrying more often than the fallback asks.
	if wait := nextPassRetry(0, time.Minute, 10*time.Second); wait != 10*time.Second {
		t.Errorf("the first retry waited %s; want the shorter pull interval", wait)
	}
}
