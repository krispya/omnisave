package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/activity"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
)

// syncConnection resolves the saved connection without prompting: headless
// commands refuse to run unestablished rather than ask (FDR-005).
func syncConnection(state tracking.State, flagURL, flagToken string) (*remote.Client, error) {
	url, token := serverConnection(state, flagURL, flagToken)
	if token == "" {
		return nil, errors.New("no server connection; run omnisave track or connect first")
	}
	return remote.New(url, token, nil)
}

// runSync performs one headless reconcile over every tracked game and
// reports what moved (FDR-005). It never prompts: prompt-shaped situations
// are reported and wait for an interactive track run.
func runSync(ctx context.Context, scanner *client.Scanner, arguments []string) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	statePath := flags.String("state", "", "path to local tracking state")
	serverURL := flags.String("server", environmentOr("OMNISAVE_SERVER_URL", ""), "Omnisave server URL")
	token := flags.String("token", os.Getenv("OMNISAVE_API_TOKEN"), "Omnisave API token")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	store, err := trackingStore(*statePath)
	if err != nil {
		return err
	}
	state, err := store.Load()
	if err != nil {
		return err
	}
	server, err := syncConnection(state, *serverURL, *token)
	if err != nil {
		return err
	}
	report := &tui.TrackReport{}
	outcome, _, played, err := syncPass(ctx, scanner, server, running.PlatformDetector(), &state, report, 0)
	if err != nil {
		return err
	}
	// A failed sweep skips the report entirely: the server's picture ages
	// out on its own, and an empty report would clear a running game.
	if played.swept {
		reportPlaying(ctx, server, played.presence, played.playing)
	}
	report.Print()
	tui.TrackSummary(outcome)
	return store.Save(state)
}

// syncPass runs the headless reconcile shared by sync and watch: deletion
// reconciliation, library sync, then the three-way save pass. It returns
// the tracked games' local save files so watchers know what to poll, and
// what the pass learned about playing games — the presence picture the
// watch loop re-affirms between passes, the sweep behind it, and the games
// whose pulls the pass held back under a running game.
func syncPass(
	ctx context.Context,
	scanner *client.Scanner,
	server *remote.Client,
	detector *running.Detector,
	state *tracking.State,
	report *tui.TrackReport,
	pushFloor time.Duration,
) (tui.TrackOutcome, []string, passPlaying, error) {
	activity.Report(ctx, "checking library")
	reconciled := reconcileDeletedGames(ctx, server, state, report)
	activity.Report(ctx, "scanning")
	scans, err := scanner.ScanWithProgress(ctx, func(client.ScanProgress) {})
	if err != nil {
		return tui.TrackOutcome{}, nil, passPlaying{}, err
	}
	outcome, confirmed := syncTracking(ctx, server, state, scans, nil, report)
	// Presence maps through Library identities, which the library sync just
	// resolved, so a freshly tracked game reports on its very first pass.
	played := passPlaying{presence: trackedPresence(scanner, state, scans)}
	// One sweep serves two consumers with opposite failure policies: the
	// pull gate fails open — an unreadable process list must not defer
	// pulls forever — while the presence report fails closed, so swept
	// says whether there is a picture worth reporting at all.
	var gate *pullGate
	if detector != nil {
		if playing, sweepErr := playingNow(ctx, detector, played.presence.matchers); sweepErr == nil {
			played.playing = playing
			played.swept = true
			gate = &pullGate{playing: playing}
		}
	}
	if outcome.Synced {
		if err := reconcileSaves(ctx, server, state, scans, confirmed, &outcome, report, nil, gate, pushFloor); err != nil {
			return outcome, nil, played, err
		}
	}
	if gate != nil {
		played.waiting = gate.waiting
	}
	outcome.Untracked += reconciled
	return outcome, watchedFiles(state, scans), played, nil
}

// watchedFiles lists the local files of tracked games' discovered saves,
// plus their parent directories: a file appearing in a save's directory
// bumps the directory's mtime, so new files trigger a pass even though
// their own paths were never seen before.
func watchedFiles(state *tracking.State, scans []client.TargetScan) []string {
	paths := make(map[string]bool)
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			if _, tracked := state.Games[discovered.Game.ID]; !tracked {
				continue
			}
			for _, save := range discovered.Saves {
				for _, file := range save.Files {
					paths[file.Path] = true
					paths[filepath.Dir(file.Path)] = true
				}
			}
			for _, destination := range discovered.Destinations {
				for _, location := range destination.Locations {
					// A missing prospective path changes from "missing" when the
					// game creates its first save; a directory mtime also catches
					// new files beneath an existing save root.
					paths[location.Path] = true
				}
			}
		}
	}
	sorted := make([]string, 0, len(paths))
	for path := range paths {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)
	return sorted
}
