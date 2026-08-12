package main

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/activity"
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
	// Working names the game the running pass has in hand, by display title;
	// an empty title says the pass is between games.
	Working(title string)
	// Phase carries what the pass is doing right now, in the work's own
	// words, for whatever it currently has in hand.
	Phase(message string)
	PassFinished(result tui.PassResult)
	Requests() <-chan tui.WatchRequest
}

// announcer emits conditions when they appear and allows them to recur after clearing.
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

// announce returns changes since the last snapshot, excluding syncs older than started.
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
		spoken := false
		for _, sentence := range game.Events {
			sentences[sentence] = true
			if emit && !a.sentences[game.Title][sentence] {
				events = append(events, tui.Event{
					Glyph: game.Glyph, Title: game.Title, Sentence: sentence, At: at,
				})
				spoken = true
			}
		}
		a.sentences[game.Title] = sentences

		// Do not add a generic sync event when this pass emitted a more specific event.
		known, seenBefore := a.synced[game.Title]
		if game.SyncedAt.After(known) {
			if emit && !spoken && (seenBefore || game.SyncedAt.After(started)) {
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

// runWatch continuously runs non-interactive sync passes after local or server changes.
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
	loop := newWatchLoop(scanner, server, store, running.PlatformDetector(), settings, newAnnouncer())
	url, _ := serverConnection(initial, *serverURL, *token)
	// watch owns its first pass, so it opens with nothing established yet.
	return keepWatching(ctx, loop, url, settings, handoff{})
}

// handoff seeds the watch view from a completed reconciliation pass.
type handoff struct {
	snapshot tui.ReportSnapshot
	at       time.Time
}

// watchSeed carries presence, the warmed detector, and deferred pulls into watch.
type watchSeed struct {
	detector *running.Detector
	presence presenceWatch
	deferred []string
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

// keepWatching runs until cancellation, using a live TTY view or plain event log.
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

// serverSettle coalesces a burst of server events without extending the first deadline.
const serverSettle = 2 * time.Second

// presenceReaffirm stays within the server's presence-expiry window.
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
	// watched avoids repeating the handoff pass before polling begins.
	watched []string
	// presence makes the handoff report available immediately.
	presence presenceWatch
	// deferred keeps handoff pull gates active from the first sweep.
	deferred []string
}

// newWatchLoop wires configured timings, server events, and a reusable process detector.
func newWatchLoop(scanner *client.Scanner, server *remote.Client, store *tracking.Store, detector *running.Detector, settings watchSettings, events *announcer) watchLoop {
	return watchLoop{
		scanner:  scanner,
		server:   server,
		store:    store,
		detector: detector,
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
	// presence is replaced only by a pass with a valid process sweep.
	presence := l.presence
	// Only a reconciled pass may replace deferred pulls and their exit triggers.
	deferred := l.deferred
	// exitFired prevents failed exit-triggered passes from retrying every poll.
	exitFired := false
	// answers carries decisions the user gave the view, keyed by the game and
	// Omnisave the question named. They are replayed into one pass and then
	// forgotten: the pass validates each against fresh state, so an answer to
	// a divergence that resolved itself meanwhile is simply never reached,
	// and a stale answer can never land on a later, different question.
	answers := make(map[string]tui.DivergedBindingChoice)
	// held is set while a question is on screen. The answer has to land on
	// the table the question was built from, so the loop runs no pass until
	// it comes back — presence keeps re-affirming either way, because the
	// server's picture must not age out behind an open modal.
	held := false
	affirmPresence := func() (map[string]bool, bool) {
		// Skip process sweeps when there is no presence or deferred pull to update.
		if l.detector == nil || (presence.deviceID == "" && len(deferred) == 0) {
			return nil, false
		}
		playing, err := playingNow(ctx, l.detector, presence.matchers)
		if err != nil {
			// A failed sweep neither clears presence nor resolves deferred pulls.
			return nil, false
		}
		if presence.deviceID != "" {
			reportPlaying(ctx, l.server, presence, playing)
			sink.Playing(playingGames(presence.titles, playing))
		}
		return playing, true
	}
	// Reaffirm presence independently of save writes; poll faster while pulls await exit.
	nextAffirm := func() time.Duration {
		if len(deferred) > 0 && l.poll < presenceReaffirm {
			return l.poll
		}
		return presenceReaffirm
	}
	presenceTicker := time.NewTicker(nextAffirm())
	defer presenceTicker.Stop()
	pass := func() []string {
		started := time.Now()
		sink.PassStarted()
		state, err := l.store.Load()
		if err != nil {
			l.finish(sink, started, tui.ReportSnapshot{}, "", false, err)
			return nil
		}
		// The pass names the game it has in hand as it goes; only the live
		// view has anywhere to put that, and it marks the row rather than
		// disturbing the table, which stays settled until the pass finishes.
		report := &tui.TrackReport{OnWorking: sink.Working}
		// The work already describes itself for the one-line spinner an
		// interactive command shows; watch listens to the same reporting and
		// lands it on whatever the pass has in hand.
		passCtx := activity.WithReporter(ctx, sink.Phase)
		// Answers are spent by the pass they are replayed into, whatever it
		// makes of them. Holding one back for a later pass would let it apply
		// to a divergence the user never saw.
		prompts := replayedAnswers(answers)
		clear(answers)
		outcome, files, played, err := syncPass(passCtx, l.scanner, l.server, l.detector, &state, report, l.floor, prompts)
		// Preserve the last valid presence after a failed scan.
		if played.presence.deviceID != "" {
			presence = played.presence
		}
		if played.swept {
			// Reuse the pass's process sweep as the affirmation.
			if presence.deviceID != "" {
				reportPlaying(ctx, l.server, presence, played.playing)
				sink.Playing(playingGames(presence.titles, played.playing))
			}
		} else {
			affirmPresence()
		}
		// Only a reconciled pass may replace or re-arm deferred pull state.
		if err == nil && outcome.Synced {
			deferred = played.waiting
			exitFired = false
		}
		presenceTicker.Reset(nextAffirm())
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
		// Publish handoff presence before the first ticker interval.
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

	// All triggers share refresh so pass bookkeeping has one update path.
	refresh := func() {
		settleTimer.Stop()
		settleArmed = false
		watched = pass()
		sink.Watching(len(watched))
		signature = statSignature(watched)
		dirty = false
	}
	// Delay triggered passes until local writes have been quiet for one interval.
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
				// Periodic passes remain the fallback after the event feed closes.
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
			if held || stillWriting() {
				// Server movement noticed behind an open question falls back
				// to the pull ticker, the same fallback a failed pass takes.
				continue
			}
			refresh()
		case request := <-sink.Requests():
			switch request.Kind {
			case tui.WatchSyncNow:
				// The user asked; theirs is the one trigger that never waits.
				refresh()
			case tui.WatchQuestionOpened:
				held = true
			case tui.WatchQuestionDismissed:
				// Nothing was decided, so nothing runs: the save waits and
				// the question can be asked again.
				held = false
			case tui.WatchAnswered:
				held = false
				answers[answerKey(request.Title, request.Omnisave)] = request.Diverged
				refresh()
			}
		case <-presenceTicker.C:
			playing, swept := affirmPresence()
			if !swept || held {
				continue
			}
			if !deferredGameExited(deferred, playing) {
				// Every held pull's game is playing (or nothing is held):
				// the next exit gets a trigger of its own.
				exitFired = false
				continue
			}
			if exitFired {
				// A failed exit pass retries on the normal pull ticker.
				continue
			}
			// Apply an exit-triggered pull only after the game's final writes settle.
			if stillWriting() {
				continue
			}
			exitFired = true
			refresh()
		case <-pullTicker.C:
			if held || stillWriting() {
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
			// A save written behind an open question stays dirty, so the
			// commit lands on the first poll after the question closes.
			if dirty && !held {
				refresh()
			}
		}
	}
}

// answerKey identifies the save a question was asked about by the same pair
// the question showed: the game's title and the Omnisave's display name.
func answerKey(gameTitle, omnisaveName string) string {
	return gameTitle + "\x00" + omnisaveName
}

// replayedAnswers turns collected answers into a prompts set the pass can
// use. Only the divergence question is answerable this way, so every other
// question stays nil and waits for an interactive run exactly as before. A
// pass with nothing to replay gets no prompts at all and stays headless.
func replayedAnswers(answers map[string]tui.DivergedBindingChoice) *reconcilePrompts {
	if len(answers) == 0 {
		return nil
	}
	replayed := make(map[string]tui.DivergedBindingChoice, len(answers))
	maps.Copy(replayed, answers)
	return &reconcilePrompts{
		diverged: func(gameTitle, omnisaveName string) (tui.DivergedBindingChoice, error) {
			choice, answered := replayed[answerKey(gameTitle, omnisaveName)]
			if !answered {
				return "", errUnanswered
			}
			return choice, nil
		},
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

// Working is silent for the same reason: what a pass is doing right now is a
// marker on a row, and a log has no row to mark — only the outcome is worth
// a line.
func (plainWatchSink) Working(string) {}

// Phase is silent too. Every step of every pass as its own log line would
// bury the events that actually happened.
func (plainWatchSink) Phase(string) {}

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
