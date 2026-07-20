package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"sort"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
)

// syncConnection resolves the saved connection without prompting: headless
// commands refuse to run unestablished rather than ask (FDR-005).
func syncConnection(state tracking.State, flagURL, flagToken string) (*remote.Client, error) {
	url, token := serverConnection(state, flagURL, flagToken)
	if token == "" {
		return nil, errors.New("no server connection; run omnisave-client track or connect first")
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
	outcome, _, err := syncPass(ctx, scanner, server, &state, report, 0)
	if err != nil {
		return err
	}
	report.Print()
	tui.TrackSummary(outcome)
	return store.Save(state)
}

// syncPass runs the headless reconcile shared by sync and watch: deletion
// reconciliation, library sync, then the three-way save pass. It returns
// the tracked games' local save files so watchers know what to poll.
func syncPass(
	ctx context.Context,
	scanner *client.Scanner,
	server *remote.Client,
	state *tracking.State,
	report *tui.TrackReport,
	pushFloor time.Duration,
) (tui.TrackOutcome, []string, error) {
	reconciled := reconcileDeletedGames(ctx, server, state, report)
	scans, err := scanner.ScanWithProgress(ctx, func(client.ScanProgress) {})
	if err != nil {
		return tui.TrackOutcome{}, nil, err
	}
	outcome, confirmed := syncTracking(ctx, server, state, scans, nil, report)
	if outcome.Synced {
		if err := reconcileSaves(ctx, server, state, scans, confirmed, &outcome, report, nil, pushFloor); err != nil {
			return outcome, nil, err
		}
	}
	outcome.Untracked += reconciled
	return outcome, watchedFiles(state, scans), nil
}

// watchedFiles lists the local files of tracked games' discovered saves.
func watchedFiles(state *tracking.State, scans []client.TargetScan) []string {
	var files []string
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			if _, tracked := state.Games[discovered.Game.ID]; !tracked {
				continue
			}
			for _, save := range discovered.Saves {
				for _, file := range save.Files {
					files = append(files, file.Path)
				}
			}
		}
	}
	sort.Strings(files)
	return files
}
