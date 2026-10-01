package main

import (
	"context"
	"errors"
	"flag"
	"os"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
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
	ports := savesync.Ports{Server: server, Adapters: scanner, Report: report}
	pass, err := savesync.Pass(ctx, ports, &state, savesync.PassOptions{Detector: running.PlatformDetector()})
	if err != nil {
		return err
	}
	// Do not replace presence with an empty report when the sweep fails.
	if pass.Played.Swept {
		pass.Played.Presence.Report(ctx, server, pass.Played.Playing)
	}
	report.Print()
	tui.TrackSummary(pass.Outcome)
	return store.Save(state)
}
