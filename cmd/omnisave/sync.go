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

// runSync performs one non-interactive reconciliation over all tracked games.
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
	outcome, _, played, err := syncPass(ctx, scanner, server, running.PlatformDetector(), &state, report, 0, nil)
	if err != nil {
		return err
	}
	// Do not replace presence with an empty report when the sweep fails.
	if played.swept {
		reportPlaying(ctx, server, played.presence, played.playing)
	}
	report.Print()
	tui.TrackSummary(outcome)
	return store.Save(state)
}

// syncPass reconciles deletions, Library state, and saves for sync and watch.
// A nil prompts set leaves every question waiting, which is what a headless
// pass wants; watch passes one carrying the answers its view collected.
func syncPass(
	ctx context.Context,
	scanner *client.Scanner,
	server *remote.Client,
	detector *running.Detector,
	state *tracking.State,
	report *tui.TrackReport,
	pushFloor time.Duration,
	prompts *reconcilePrompts,
) (tui.TrackOutcome, []string, passPlaying, error) {
	activity.Report(ctx, "checking library")
	reconciled := reconcileDeletedGames(ctx, server, state, report)
	activity.Report(ctx, "scanning")
	scans, err := scanner.ScanWithProgress(ctx, func(client.ScanProgress) {})
	if err != nil {
		return tui.TrackOutcome{}, nil, passPlaying{}, err
	}
	outcome, confirmed := syncTracking(ctx, server, state, scans, nil, report)
	// Build presence after Library identities are resolved.
	played := passPlaying{presence: trackedPresence(scanner, state, scans)}
	// Pull gating fails open, while presence omits failed sweeps.
	var gate *pullGate
	if detector != nil {
		if playing, sweepErr := playingNow(ctx, detector, played.presence.matchers); sweepErr == nil {
			played.playing = playing
			played.swept = true
			gate = &pullGate{playing: playing}
		}
	}
	if outcome.Synced {
		if err := reconcileSaves(ctx, scanner, server, state, scans, confirmed, &outcome, report, prompts, gate, pushFloor); err != nil {
			return outcome, nil, played, err
		}
	}
	if gate != nil {
		played.waiting = gate.waiting
	}
	outcome.Untracked += reconciled
	return outcome, watchedFiles(state, scans), played, nil
}

// watchedFiles includes save files and parent directories so new files trigger a pass.
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
					// Watch missing paths and directory mtimes for newly created saves.
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
