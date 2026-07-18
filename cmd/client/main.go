package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"slices"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
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
	case "bind":
		return runBind(ctx, scanner, arguments[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q; use scan, track, or bind", arguments[0])
	}
}

func runBind(ctx context.Context, scanner *client.Scanner, arguments []string) error {
	flags := flag.NewFlagSet("bind", flag.ContinueOnError)
	statePath := flags.String("state", "", "path to local tracking state")
	serverURL := flags.String("server", environmentOr("OMNISAVE_SERVER_URL", "http://localhost:8080"), "Omnisave server URL")
	token := flags.String("token", os.Getenv("OMNISAVE_API_TOKEN"), "Omnisave API token")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	server, err := remote.New(*serverURL, *token, nil)
	if err != nil {
		return err
	}

	scans, err := tui.ScanForSelection(ctx, scanner)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	if err != nil {
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
	local := trackedLocalSaves(state, tracking.SavesFromScans(scans))
	if len(local) == 0 {
		fmt.Println("No saves were discovered for tracked games. Run track first or create a save in the game.")
		return nil
	}
	remoteSaves, err := server.ListOmnisaves(ctx)
	if err != nil {
		return err
	}
	selection, err := tui.SelectBinding(local, remoteSaves, state.Bindings)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	if errors.Is(err, tui.ErrNoOmnisaves) {
		fmt.Println("The server has no Omnisaves to bind. Create one in the dashboard first.")
		return nil
	}
	if err != nil {
		return err
	}
	if err := state.Bind(selection.Local, selection.Omnisave.ID); err != nil {
		return err
	}
	if err := store.Save(state); err != nil {
		return err
	}
	fmt.Printf("✓ %s (%s) will sync with %s.\n", selection.Local.GameTitle, selection.Local.Kind, selection.Omnisave.DisplayName)
	return nil
}

func trackingStore(path string) (*tracking.Store, error) {
	if path != "" {
		return tracking.NewStore(path), nil
	}
	return tracking.DefaultStore()
}

func trackedLocalSaves(state tracking.State, saves []tracking.LocalSave) []tracking.LocalSave {
	tracked := state.TrackedIDs()
	selected := make([]tracking.LocalSave, 0, len(saves))
	for _, save := range saves {
		if tracked[save.GameID] {
			selected = append(selected, save)
		}
	}
	return selected
}

func environmentOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
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
	serverURL := flags.String("server", environmentOr("OMNISAVE_SERVER_URL", "http://localhost:8080"), "Omnisave server URL")
	token := flags.String("token", os.Getenv("OMNISAVE_API_TOKEN"), "Omnisave API token")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	scans, err := tui.ScanForSelection(ctx, scanner)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	if err != nil {
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
	removed, err := state.ApplyVisible(tracking.FromScans(scans), selected)
	if err != nil {
		return err
	}
	if err := store.Save(state); err != nil {
		return err
	}

	if *token == "" {
		tui.TrackSummary(tui.TrackOutcome{Tracked: len(state.Games)})
		if len(state.Games) > 0 || len(removed) > 0 {
			tui.TrackHint("set OMNISAVE_API_TOKEN to add tracked games to your library")
		}
		return nil
	}
	server, err := remote.New(*serverURL, *token, nil)
	if err != nil {
		return err
	}
	syncTracking(ctx, server, &state, scans, removed)
	return store.Save(state)
}

// syncTracking makes the server aware of this device's tracked games: it
// registers the device, resolves newly tracked games into the Library, and
// refreshes provenance. Failures never undo local tracking — unresolved games
// retry on the next track run.
func syncTracking(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	scans []client.TargetScan,
	removed []tracking.Game,
) {
	device := state.EnsureDevice(deviceName())
	outcome := tui.TrackOutcome{Tracked: len(state.Games)}
	err := server.RegisterDevice(ctx, device.ID, catalog.RegisterDevice{Name: device.Name, Platform: runtime.GOOS})
	if err != nil {
		tui.TrackSummary(outcome)
		tui.TrackSyncFailed(err)
		return
	}
	outcome.Synced = true

	identities := installedGameIdentities(scans)
	for _, id := range sortedGameIDs(state.Games) {
		game := state.Games[id]
		identity, visible := identities[id]
		if game.ServerGameID == "" {
			if !visible {
				outcome.Pending++
				tui.TrackPending(game.Title)
				continue
			}
			resolution, err := server.ResolveGame(ctx, catalog.ResolveGame{
				Identifiers:  identity.Identifiers,
				Fingerprints: identity.Fingerprints,
				TitleHint:    identity.DisplayTitle(game.Title),
				PlatformHint: identity.Platform,
			})
			if err != nil {
				outcome.Failed++
				tui.TrackFailed(game.Title, err)
				continue
			}
			state.SetServerGameID(id, resolution.Game.ID)
			game = state.Games[id]
			if resolution.Status == catalog.ResolutionCreated {
				outcome.Added++
				tui.TrackAdded(game.Title, resolution.Game.Title)
			} else {
				outcome.Linked++
				tui.TrackLinked(game.Title, resolution.Game.Title)
			}
		}
		if err := server.TrackGame(ctx, game.ServerGameID, device.ID, catalog.TrackGame{
			Adapter:   game.Adapter,
			Installed: visible,
		}); err != nil {
			outcome.Failed++
			tui.TrackFailed(game.Title, err)
		}
	}
	for _, game := range removed {
		if game.ServerGameID == "" {
			continue
		}
		if err := server.UntrackGame(ctx, game.ServerGameID, device.ID); err != nil {
			outcome.Failed++
			tui.TrackFailed(game.Title, err)
			continue
		}
		outcome.Untracked++
		tui.TrackRemoved(game.Title)
	}
	tui.TrackSummary(outcome)
}

func deviceName() string {
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	return "this-device"
}

func installedGameIdentities(scans []client.TargetScan) map[string]target.GameIdentity {
	identities := make(map[string]target.GameIdentity, len(scans))
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			identities[discovered.Game.ID] = discovered.Game.Identity
		}
	}
	return identities
}

func sortedGameIDs(games map[string]tracking.Game) []string {
	ids := make([]string, 0, len(games))
	for id := range games {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func printUsage() {
	fmt.Println(`Omnisave client

Usage:
  omnisave-client scan [--verbose]
  OMNISAVE_API_TOKEN=... omnisave-client track [--server URL] [--state path]
  OMNISAVE_API_TOKEN=... omnisave-client bind [--server URL] [--state path]

Commands:
  scan   Discover installed targets, games, and saves without changing state
  track  Choose which discovered games should be synchronized; tracked games
         are added to your server library and record this device's provenance
  bind   Choose which Omnisave a discovered local save should synchronize with`)
}
