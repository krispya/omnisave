package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "omnisave-client: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 {
		printUsage()
		return nil
	}

	scanner := client.NewScanner(nil, retroarch.NewDefault(), steam.NewDefault())
	switch arguments[0] {
	case "scan":
		return runScan(ctx, scanner, arguments[1:])
	case "track":
		return runTrack(ctx, scanner, arguments[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q; use scan or track", arguments[0])
	}
}

func runScan(ctx context.Context, scanner *client.Scanner, arguments []string) error {
	var verbose bool
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.BoolVar(&verbose, "verbose", false, "show targets, save sets, and individual files")
	flags.BoolVar(&verbose, "v", false, "show targets, save sets, and individual files")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	_, err := tui.Scan(ctx, scanner, verbose)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	return err
}

func runTrack(ctx context.Context, scanner *client.Scanner, arguments []string) error {
	flags := flag.NewFlagSet("track", flag.ContinueOnError)
	statePath := flags.String("state", "", "path to local tracking state")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	scans, err := tui.Scan(ctx, scanner, false)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Println()

	var store *tracking.Store
	if *statePath != "" {
		store = tracking.NewStore(*statePath)
	} else {
		store, err = tracking.DefaultStore()
		if err != nil {
			return err
		}
	}
	state, err := store.Load()
	if err != nil {
		return err
	}
	selected, err := tui.SelectTrackedGames(scans, state.TrackedIDs())
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	if errors.Is(err, tui.ErrNoGames) {
		fmt.Println("No games were discovered to track.")
		return nil
	}
	if err != nil {
		return err
	}
	if err := state.ApplyVisible(tracking.FromScans(scans), selected); err != nil {
		return err
	}
	if err := store.Save(state); err != nil {
		return err
	}
	noun := "games"
	if len(state.Games) == 1 {
		noun = "game"
	}
	fmt.Printf("✓ %d %s tracked on this machine.\n", len(state.Games), noun)
	return nil
}

func printUsage() {
	fmt.Println(`OmniSave client

Usage:
  omnisave-client scan [--verbose]
  omnisave-client track [--state path]

Commands:
  scan   Discover installed targets, games, and saves without changing state
  track  Choose which discovered games should be synchronized`)
}
