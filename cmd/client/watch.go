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
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
)

// watchSink receives the watch loop's lifecycle. The live view renders it;
// plain mode logs it, so watch works identically under a service manager.
type watchSink interface {
	Watching(files int)
	PassStarted()
	Snapshot(lines []string)
	PassFinished(lines []string, summary string, changed bool, err error)
	Requests() <-chan tui.WatchRequest
}

// runWatch keeps saves synced continuously (FDR-005): a poll-based watcher
// notices save writes, waits for a quiet interval so write bursts settle,
// and runs the same headless pass sync uses. A periodic pass picks up
// server-side movement. Watch never prompts — a diverged save is reported
// as such and waits for an interactive track run. In a terminal it shows a
// live view; piped or under a service manager it logs plainly.
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

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	loop := watchLoop{
		scanner: scanner,
		server:  server,
		store:   store,
		poll:    *poll,
		pull:    *pullEvery,
		floor:   *floor,
	}

	if *plain || !isatty.IsTerminal(os.Stdout.Fd()) {
		loop.run(ctx, plainWatchSink{})
		return nil
	}

	url, _ := serverConnection(initial, *serverURL, *token)
	display := tui.NewWatchDisplay(tui.WatchConfig{ServerURL: url, Poll: *poll, PullEvery: *pullEvery})
	loopCtx, cancelLoop := context.WithCancel(ctx)
	go func() {
		<-loopCtx.Done()
		display.Quit()
	}()
	go loop.run(loopCtx, display)
	err = display.Run()
	cancelLoop()
	return err
}

type watchLoop struct {
	scanner *client.Scanner
	server  *remote.Client
	store   *tracking.Store
	poll    time.Duration
	pull    time.Duration
	floor   time.Duration
}

// run polls until ctx ends. State reloads every pass so a concurrent track
// run's changes are honored; a failed pass leaves work for the next one.
func (l watchLoop) run(ctx context.Context, sink watchSink) {
	pass := func() []string {
		sink.PassStarted()
		state, err := l.store.Load()
		if err != nil {
			sink.PassFinished(nil, "", false, err)
			return nil
		}
		report := &tui.TrackReport{OnUpdate: sink.Snapshot}
		outcome, files, err := syncPass(ctx, l.scanner, l.server, &state, report, l.floor)
		if err != nil {
			sink.PassFinished(report.Lines(), "", false, err)
			return files
		}
		if err := l.store.Save(state); err != nil {
			sink.PassFinished(report.Lines(), "", false, err)
			return files
		}
		sink.PassFinished(report.Lines(), tui.SummaryLine(outcome), outcome.Changed(), nil)
		return files
	}

	watched := pass()
	sink.Watching(len(watched))
	signature := statSignature(watched)
	dirty := false
	pollTicker := time.NewTicker(l.poll)
	defer pollTicker.Stop()
	pullTicker := time.NewTicker(l.pull)
	defer pullTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sink.Requests():
			watched = pass()
			sink.Watching(len(watched))
			signature = statSignature(watched)
			dirty = false
		case <-pullTicker.C:
			watched = pass()
			sink.Watching(len(watched))
			signature = statSignature(watched)
			dirty = false
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
				dirty = false
				watched = pass()
				sink.Watching(len(watched))
				signature = statSignature(watched)
			}
		}
	}
}

// plainWatchSink logs pass results line by line — the no-TTY behavior.
type plainWatchSink struct{}

func (plainWatchSink) Watching(files int) {
	fmt.Printf("Watching %d save paths\n", files)
}

func (plainWatchSink) PassStarted()            {}
func (plainWatchSink) Snapshot(lines []string) {}

func (plainWatchSink) PassFinished(lines []string, summary string, changed bool, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "omnisave-client watch: %v\n", err)
		return
	}
	if !changed {
		return
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	fmt.Println()
	fmt.Println(summary)
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
