package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
)

// watchSink receives the watch loop's lifecycle. The live view renders it;
// plain mode logs it, so watch works identically under a service manager.
type watchSink interface {
	Watching(files int)
	PassStarted()
	// Playing replaces the set of games this device currently sees being
	// played, by display title; an empty set clears every marker.
	Playing(titles []string)
	PassFinished(result tui.PassResult)
	Requests() <-chan tui.WatchRequest
}

// announcer turns each pass's standing state into the events worth writing
// out. A pass reports conditions as well as changes — a saveless game says
// so every pass — so a sentence is announced when it appears and forgotten
// when it clears, which is what lets it announce again if it comes back.
type announcer struct {
	sentences map[string]map[string]bool
	synced    map[string]time.Time
	general   map[string]bool
}

func newAnnouncer() *announcer {
	return &announcer{
		sentences: make(map[string]map[string]bool),
		synced:    make(map[string]time.Time),
		general:   make(map[string]bool),
	}
}

// announce returns what changed since the last snapshot. started is when
// this pass began: a save whose sync predates it was already synced when
// the pass found it, which is a baseline rather than news.
func (a *announcer) announce(snapshot tui.ReportSnapshot, started, at time.Time) []tui.Event {
	return a.diff(snapshot, started, at, true)
}

// seen records a snapshot without announcing it, so a hand-off from track
// never repeats the report that run already printed.
func (a *announcer) seen(snapshot tui.ReportSnapshot) {
	a.diff(snapshot, time.Time{}, time.Time{}, false)
}

func (a *announcer) diff(snapshot tui.ReportSnapshot, started, at time.Time, emit bool) []tui.Event {
	var events []tui.Event
	general := make(map[string]bool, len(snapshot.General))
	for _, sentence := range snapshot.General {
		general[sentence] = true
		if emit && !a.general[sentence] {
			events = append(events, tui.Event{Glyph: tui.FailureGlyph(), Sentence: sentence, At: at})
		}
	}
	a.general = general

	for _, game := range snapshot.Games {
		sentences := make(map[string]bool, len(game.Events))
		for _, sentence := range game.Events {
			sentences[sentence] = true
			if emit && !a.sentences[game.Title][sentence] {
				events = append(events, tui.Event{
					Glyph: game.Glyph, Title: game.Title, Sentence: sentence, At: at,
				})
			}
		}
		a.sentences[game.Title] = sentences

		// A push or a pull leaves no sentence behind — the save simply
		// synced — so the moment it happened is the event.
		known, seenBefore := a.synced[game.Title]
		if game.SyncedAt.After(known) {
			if emit && (seenBefore || game.SyncedAt.After(started)) {
				events = append(events, tui.Event{
					Glyph:    game.Glyph,
					Title:    game.Title,
					Sentence: "synced with " + game.SyncedWith,
					At:       at,
				})
			}
			a.synced[game.Title] = game.SyncedAt
		}
	}
	return events
}

// runWatch keeps saves synced continuously (FDR-005): a poll-based watcher
// notices save writes, waits for a quiet interval so write bursts settle,
// and runs the same headless pass sync uses. Server-side movement arrives
// over the server's event stream — a Dash restore reaches this device in
// seconds — with a periodic pass as the fallback when the stream is down.
// Watch never prompts — a diverged save is reported as such and waits for
// an interactive track run. In a terminal it shows a live view; piped or
// under a service manager it logs plainly.
func runWatch(ctx context.Context, scanner *client.Scanner, arguments []string) error {
	flags := flag.NewFlagSet("watch", flag.ContinueOnError)
	statePath := flags.String("state", "", "path to local tracking state")
	serverURL := flags.String("server", environmentOr("OMNISAVE_SERVER_URL", ""), "Omnisave server URL")
	token := flags.String("token", os.Getenv("OMNISAVE_API_TOKEN"), "Omnisave API token")
	poll := flags.Duration("poll", 10*time.Second, "how often to check save files for changes")
	pullEvery := flags.Duration("pull-every", 15*time.Minute, "how often to ask the server for new revisions")
	floor := flags.Duration("floor", 5*time.Minute, "minimum spacing between commits of the same save")
	plain := flags.Bool("plain", false, "log line by line instead of the live view")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	store, err := trackingStore(*statePath)
	if err != nil {
		return err
	}
	initial, err := store.Load()
	if err != nil {
		return err
	}
	server, err := syncConnection(initial, *serverURL, *token)
	if err != nil {
		return err
	}
	settings := watchSettings{poll: *poll, pull: *pullEvery, floor: *floor, plain: *plain}
	loop := newWatchLoop(scanner, server, store, settings, newAnnouncer())
	url, _ := serverConnection(initial, *serverURL, *token)
	// watch owns its first pass, so it opens with nothing established yet.
	return keepWatching(ctx, loop, url, settings, handoff{})
}

// handoff is what a finished run gives the watch loop: the table its
// reconcile pass established and when that pass reached the server, so the
// live view opens showing standing state and proving liveness rather than
// waiting a poll to fill itself in.
type handoff struct {
	snapshot tui.ReportSnapshot
	at       time.Time
}

// watchSettings are the loop's timings. Watch takes them from flags; the
// watch phase that follows a track run uses the same defaults.
type watchSettings struct {
	poll  time.Duration
	pull  time.Duration
	floor time.Duration
	plain bool
}

func defaultWatchSettings() watchSettings {
	return watchSettings{poll: 10 * time.Second, pull: 15 * time.Minute, floor: 5 * time.Minute}
}

// keepWatching runs the loop until the user quits or the service manager
// stops the process. In a terminal the live block stays pinned at the
// bottom while events scroll past above it; piped, the events are the log.
func keepWatching(
	ctx context.Context,
	loop watchLoop,
	serverURL string,
	settings watchSettings,
	initial handoff,
) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if settings.plain || !isatty.IsTerminal(os.Stdout.Fd()) {
		loop.run(ctx, plainWatchSink{})
		return nil
	}
	display := tui.NewWatchDisplay(tui.WatchConfig{
		ServerURL: serverURL,
		Poll:      settings.poll,
		PullEvery: settings.pull,
		Initial:   initial.snapshot,
		Synced:    initial.at,
	})
	loopCtx, cancelLoop := context.WithCancel(ctx)
	go func() {
		<-loopCtx.Done()
		display.Quit()
	}()
	go loop.run(loopCtx, display)
	err := display.Run()
	cancelLoop()
	return err
}

// serverSettle is how long the loop lets server events coalesce before the
// pass they trigger: a restore publishes movement more than once, and one
// pass should answer the burst. The first event opens the window and later
// events add nothing to it, so steady traffic cannot postpone the pass.
const serverSettle = 2 * time.Second

// presenceReaffirm is how often watch re-affirms which games are being
// played. It must stay comfortably inside the server's credibility window,
// so a quiet session keeps reading as playing and a closed one clears
// within about a minute.
const presenceReaffirm = time.Minute

type watchLoop struct {
	scanner *client.Scanner
	server  *remote.Client
	store   *tracking.Store
	// detector answers which tracked games have a live process; nil turns
	// presence off entirely, which is what loop tests want.
	detector *running.Detector
	poll     time.Duration
	pull     time.Duration
	floor    time.Duration
	settle   time.Duration
	events   *announcer
	// movement subscribes to the server's change feed; nil leaves the
	// periodic pull as the only way server-side movement is noticed.
	movement func(context.Context) <-chan string
	// watched seeds the loop with the files a preceding run already
	// discovered, so a hand-off from track watches immediately instead of
	// repeating the pass that run just finished.
	watched []string
	// presence seeds the loop with the presence picture that same run
	// established, so playing games report immediately instead of waiting
	// out the first pass.
	presence presenceWatch
}

// newWatchLoop wires the loop the way every real caller needs it: timings
// from settings, movement from the server's event stream. Keeping the
// wiring in one place is what keeps the two callers from drifting; tests
// build the struct directly to inject their own movement feed.
func newWatchLoop(scanner *client.Scanner, server *remote.Client, store *tracking.Store, settings watchSettings, events *announcer) watchLoop {
	return watchLoop{
		scanner:  scanner,
		server:   server,
		store:    store,
		detector: running.PlatformDetector(),
		poll:     settings.poll,
		pull:     settings.pull,
		floor:    settings.floor,
		settle:   serverSettle,
		events:   events,
		movement: server.ServerEvents,
	}
}

// finish hands one pass to the sink, with the events that pass produced.
func (l watchLoop) finish(sink watchSink, started time.Time, snapshot tui.ReportSnapshot, summary string, changed bool, err error) {
	at := time.Now()
	sink.PassFinished(tui.PassResult{
		Snapshot: snapshot,
		Events:   l.events.announce(snapshot, started, at),
		Summary:  summary,
		Changed:  changed,
		Err:      err,
		At:       at,
	})
}

// run polls until ctx ends. State reloads every pass so a concurrent track
// run's changes are honored; a failed pass leaves work for the next one.
func (l watchLoop) run(ctx context.Context, sink watchSink) {
	// presence carries what the re-affirm ticker sweeps and reports between
	// passes; a pass that produced a picture replaces it wholesale. A
	// hand-off from track seeds it, the same way it seeds the watched files.
	presence := l.presence
	affirmPresence := func() {
		if l.detector == nil || presence.deviceID == "" {
			return
		}
		playing, err := playingNow(ctx, l.detector, presence.matchers)
		if err != nil {
			// A failed sweep skips the report: the server's picture holds
			// through its credibility window, while an empty report would
			// clear a game still being played.
			return
		}
		reportPlaying(ctx, l.server, presence, playing)
		sink.Playing(playingGames(presence.titles, playing))
	}
	// Presence re-affirms on its own cadence: a game can run for an hour
	// without writing its save, and the server's picture of "playing" must
	// not age out just because no pass had a reason to run. A pass affirms
	// too, so each one pushes the next re-affirm a full interval out.
	presenceTicker := time.NewTicker(presenceReaffirm)
	defer presenceTicker.Stop()
	pass := func() []string {
		started := time.Now()
		sink.PassStarted()
		state, err := l.store.Load()
		if err != nil {
			l.finish(sink, started, tui.ReportSnapshot{}, "", false, err)
			return nil
		}
		report := &tui.TrackReport{}
		outcome, files, passPresence, err := syncPass(ctx, l.scanner, l.server, &state, report, l.floor)
		// A failed scan yields no picture; the presence already standing
		// keeps re-affirming rather than being zeroed by the failure.
		if passPresence.deviceID != "" {
			presence = passPresence
		}
		affirmPresence()
		presenceTicker.Reset(presenceReaffirm)
		if err != nil {
			l.finish(sink, started, report.Snapshot(), "", false, err)
			return files
		}
		if err := l.store.Save(state); err != nil {
			l.finish(sink, started, report.Snapshot(), "", false, err)
			return files
		}
		l.finish(sink, started, report.Snapshot(), tui.SummaryLine(outcome), outcome.Changed(), nil)
		return files
	}

	watched := l.watched
	if watched == nil {
		watched = pass()
	} else {
		// A hand-off skipped the opening pass, so the run's presence picture
		// goes live now instead of waiting out the first ticker interval.
		affirmPresence()
	}
	sink.Watching(len(watched))
	signature := statSignature(watched)
	dirty := false
	pollTicker := time.NewTicker(l.poll)
	defer pollTicker.Stop()
	pullTicker := time.NewTicker(l.pull)
	defer pullTicker.Stop()
	var movement <-chan string
	if l.movement != nil {
		movement = l.movement(ctx)
	}
	// The settle timer coalesces one burst of server events into one pass.
	// It starts stopped and only ever runs after movement arrives.
	settleTimer := time.NewTimer(time.Hour)
	settleTimer.Stop()
	defer settleTimer.Stop()
	settleArmed := false

	// refresh is the one way a pass lands: every trigger shares it, so the
	// bookkeeping cannot drift between them. A pass answers any movement
	// still settling, so the settle timer is disarmed too.
	refresh := func() {
		settleTimer.Stop()
		settleArmed = false
		watched = pass()
		sink.Watching(len(watched))
		signature = statSignature(watched)
		dirty = false
	}
	// stillWriting reports whether local save writes are still landing.
	// While they are, a triggered pass waits — committing mid-burst is how
	// a torn save reaches the server — and the poll path, which owns the
	// quiet-interval rule, runs the deferred pass once the burst settles.
	stillWriting := func() bool {
		if next := statSignature(watched); next != signature {
			signature = next
			dirty = true
		}
		return dirty
	}
	for {
		select {
		case <-ctx.Done():
			return
		case eventType, open := <-movement:
			if !open {
				// The feed is gone for good — a refused stream does not
				// come back — leaving the periodic pull as the fallback
				// it already is; the passes' own errors say why.
				movement = nil
				continue
			}
			if eventType != remote.LibraryChangedEvent {
				continue
			}
			if !settleArmed {
				settleArmed = true
				settleTimer.Reset(l.settle)
			}
		case <-settleTimer.C:
			settleArmed = false
			if stillWriting() {
				continue
			}
			refresh()
		case <-sink.Requests():
			// The user asked; theirs is the one trigger that never waits.
			refresh()
		case <-presenceTicker.C:
			affirmPresence()
		case <-pullTicker.C:
			if stillWriting() {
				continue
			}
			refresh()
		case <-pollTicker.C:
			next := statSignature(watched)
			if next != signature {
				// Writes are still landing; wait for one quiet interval so
				// a burst becomes a single commit.
				signature = next
				dirty = true
				continue
			}
			if dirty {
				refresh()
			}
		}
	}
}

// plainWatchSink logs events line by line — the no-TTY behavior. There is
// no view to keep, so the stream is the whole record.
type plainWatchSink struct{}

func (plainWatchSink) Watching(files int) {
	fmt.Println("Watching " + tui.SavePaths(files))
}

func (plainWatchSink) PassStarted() {}

// Playing is silent in plain mode: presence is a live marker, and reprinting
// it every re-affirmation would be noise in a stream of one-time events.
func (plainWatchSink) Playing([]string) {}

func (plainWatchSink) PassFinished(result tui.PassResult) {
	if result.Err != nil {
		fmt.Fprintf(os.Stderr, "omnisave watch: %v\n", result.Err)
		return
	}
	for _, event := range result.Events {
		fmt.Println(tui.EventLine(event))
	}
}

func (plainWatchSink) Requests() <-chan tui.WatchRequest {
	return nil
}

// statSignature summarizes the watched files' size and modification time;
// any difference between polls means the game wrote its save.
func statSignature(paths []string) string {
	var summary strings.Builder
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(&summary, "%s:missing;", path)
			continue
		}
		fmt.Fprintf(&summary, "%s:%d:%d;", path, info.Size(), info.ModTime().UnixNano())
	}
	return summary.String()
}
