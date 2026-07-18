package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"slices"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
)

// errReported marks failures already rendered by the TUI: exit non-zero
// without printing a second, plainer copy of the error.
var errReported = errors.New("failure already reported")

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintf(os.Stderr, "omnisave-client: %v\n", err)
		}
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
	case "connect":
		return runConnect(ctx, arguments[1:])
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
		return fmt.Errorf("unknown command %q; use connect, scan, track, or bind", arguments[0])
	}
}

// runConnect verifies a server connection and persists it for later commands.
// Nothing is saved unless the token is accepted, so a failed attempt leaves
// any previous connection intact.
func runConnect(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("connect", flag.ContinueOnError)
	statePath := flags.String("state", "", "path to local tracking state")
	token := flags.String("token", os.Getenv("OMNISAVE_API_TOKEN"), "Omnisave API token")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	serverURL := flags.Arg(0)
	// The standard flag package stops at the first positional argument, so
	// flags written after the URL ("connect URL --state path") need a second
	// parsing pass over the remainder.
	if flags.NArg() > 1 {
		if err := flags.Parse(flags.Args()[1:]); err != nil {
			return err
		}
	}

	store, err := trackingStore(*statePath)
	if err != nil {
		return err
	}
	state, err := store.Load()
	if err != nil {
		return err
	}

	if serverURL == "" {
		serverURL = environmentOr("OMNISAVE_SERVER_URL", state.Server.URL)
	}
	apiToken := *token
	if serverURL == "" || apiToken == "" {
		defaultURL := serverURL
		if defaultURL == "" {
			defaultURL = "http://localhost:8080"
		}
		promptURL, promptToken, err := tui.PromptConnect(defaultURL)
		if errors.Is(err, tui.ErrAborted) {
			return nil
		}
		if err != nil {
			return err
		}
		serverURL = promptURL
		if apiToken == "" {
			apiToken = promptToken
		}
	}

	server, err := remote.New(serverURL, apiToken, nil)
	if err != nil {
		tui.ConnectFailed(err)
		return errReported
	}
	if err := verifyConnection(ctx, server); err != nil {
		tui.ConnectFailed(err)
		return errReported
	}
	device := state.EnsureDevice(deviceName())
	if err := server.RegisterDevice(ctx, device.ID, catalog.RegisterDevice{Name: device.Name, Platform: runtime.GOOS}); err != nil {
		tui.ConnectFailed(err)
		return errReported
	}

	state.Server = tracking.Server{URL: serverURL, Token: apiToken}
	if err := store.Save(state); err != nil {
		return err
	}
	tui.ConnectSuccess(serverURL, device.Name)
	return nil
}

func verifyConnection(ctx context.Context, server *remote.Client) error {
	_, err := server.ListOmnisaves(ctx)
	var response *remote.ResponseError
	if errors.As(err, &response) && response.StatusCode == http.StatusUnauthorized {
		return errors.New("the server rejected this token")
	}
	return err
}

// serverConnection resolves a command's server URL and token: explicit flags
// and environment first, then the connection saved by connect.
func serverConnection(state tracking.State, flagURL, flagToken string) (string, string) {
	url := flagURL
	if url == "" {
		url = state.Server.URL
	}
	if url == "" {
		url = "http://localhost:8080"
	}
	token := flagToken
	if token == "" {
		token = state.Server.Token
	}
	return url, token
}

func runBind(ctx context.Context, scanner *client.Scanner, arguments []string) error {
	flags := flag.NewFlagSet("bind", flag.ContinueOnError)
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
	url, apiToken := serverConnection(state, *serverURL, *token)
	if apiToken == "" {
		return errors.New("no server connection; run omnisave-client connect first")
	}
	server, err := remote.New(url, apiToken, nil)
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
	serverURL := flags.String("server", environmentOr("OMNISAVE_SERVER_URL", ""), "Omnisave server URL")
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

	url, apiToken := serverConnection(state, *serverURL, *token)
	if apiToken == "" {
		tui.TrackSummary(tui.TrackOutcome{Tracked: len(state.Games)})
		if len(state.Games) > 0 || len(removed) > 0 {
			tui.TrackHint("run omnisave-client connect to link this device to your server")
		}
		return nil
	}
	server, err := remote.New(url, apiToken, nil)
	if err != nil {
		return err
	}
	outcome, confirmed := syncTracking(ctx, server, &state, scans, removed)
	if outcome.Synced {
		seedUnboundSaves(ctx, server, &state, scans, confirmed, &outcome)
	}
	tui.TrackSummary(outcome)
	return store.Save(state)
}

// syncTracking makes the server aware of this device's tracked games: it
// registers the device, resolves newly tracked games into the Library, and
// refreshes provenance. Failures never undo local tracking — unresolved games
// retry on the next track run. The returned set holds the games whose server
// records were confirmed this run; only those are safe to bind against.
func syncTracking(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	scans []client.TargetScan,
	removed []tracking.Game,
) (tui.TrackOutcome, map[string]bool) {
	device := state.EnsureDevice(deviceName())
	outcome := tui.TrackOutcome{Tracked: len(state.Games)}
	confirmed := make(map[string]bool)
	err := server.RegisterDevice(ctx, device.ID, catalog.RegisterDevice{Name: device.Name, Platform: runtime.GOOS})
	if err != nil {
		tui.TrackSyncFailed(err)
		return outcome, confirmed
	}
	outcome.Synced = true

	identities := installedGameIdentities(scans)
	for _, id := range sortedGameIDs(state.Games) {
		game := state.Games[id]
		identity, visible := identities[id]
		if game.ServerGameID == "" && !visible {
			outcome.Pending++
			tui.TrackPending(game.Title)
			continue
		}
		if game.ServerGameID == "" {
			if !resolveIntoLibrary(ctx, server, state, &outcome, id, identity) {
				continue
			}
			game = state.Games[id]
		}
		trackErr := server.TrackGame(ctx, game.ServerGameID, device.ID, catalog.TrackGame{
			Adapter:   game.Adapter,
			Installed: visible,
		})
		if isNotFound(trackErr) {
			// The stored Library identity no longer exists on this server —
			// its data was reset or the game was deleted there. The server is
			// the authority: discard the stale resolution, and with scan
			// evidence in hand resolve and track again.
			state.SetServerGameID(id, "")
			if !visible {
				outcome.Pending++
				tui.TrackPending(game.Title)
				continue
			}
			if !resolveIntoLibrary(ctx, server, state, &outcome, id, identity) {
				continue
			}
			game = state.Games[id]
			trackErr = server.TrackGame(ctx, game.ServerGameID, device.ID, catalog.TrackGame{
				Adapter:   game.Adapter,
				Installed: visible,
			})
		}
		if trackErr != nil {
			outcome.Failed++
			tui.TrackFailed(game.Title, trackErr)
			continue
		}
		confirmed[id] = true
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
	return outcome, confirmed
}

// resolveIntoLibrary resolves one tracked game's Library identity from this
// scan's evidence, records it, and reports the change line.
func resolveIntoLibrary(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	outcome *tui.TrackOutcome,
	id string,
	identity target.GameIdentity,
) bool {
	game := state.Games[id]
	resolution, err := server.ResolveGame(ctx, catalog.ResolveGame{
		Identifiers:  identity.Identifiers,
		Fingerprints: identity.Fingerprints,
		TitleHint:    identity.DisplayTitle(game.Title),
		PlatformHint: identity.Platform,
	})
	if err != nil {
		outcome.Failed++
		tui.TrackFailed(game.Title, err)
		return false
	}
	state.SetServerGameID(id, resolution.Game.ID)
	if resolution.Status == catalog.ResolutionCreated {
		outcome.Added++
		tui.TrackAdded(game.Title, resolution.Game.Title)
	} else {
		outcome.Linked++
		tui.TrackLinked(game.Title, resolution.Game.Title)
	}
	return true
}

func isNotFound(err error) bool {
	var response *remote.ResponseError
	return errors.As(err, &response) && response.StatusCode == http.StatusNotFound
}

// seedUnboundSaves runs FDR-003's binding pass for the conflict-free case:
// a tracked game the server has no saves for gets a new Omnisave seeded from
// this device's local save, and the binding starts at that revision. Only
// games whose server records were confirmed this run participate. Games
// whose saves the server already knows stay unbound here — matching them is
// a later step; manual bind covers them until then.
func seedUnboundSaves(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	scans []client.TargetScan,
	confirmed map[string]bool,
	outcome *tui.TrackOutcome,
) {
	type candidate struct {
		local        tracking.LocalSave
		save         target.Save
		serverGameID string
	}
	var candidates []candidate
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			game := state.Games[discovered.Game.ID]
			if !confirmed[discovered.Game.ID] || game.ServerGameID == "" {
				continue
			}
			for _, save := range discovered.Saves {
				if len(save.Files) == 0 {
					continue
				}
				candidates = append(candidates, candidate{
					local:        tracking.LocalSaveFrom(scan, discovered, save),
					save:         save,
					serverGameID: game.ServerGameID,
				})
			}
		}
	}
	if len(candidates) == 0 {
		return
	}

	remoteSaves, err := server.ListOmnisaves(ctx)
	if err != nil {
		outcome.Failed++
		tui.TrackFailed("save binding", err)
		return
	}
	saveExists := make(map[string]bool, len(remoteSaves))
	hasSaves := make(map[string]bool, len(remoteSaves))
	for _, save := range remoteSaves {
		saveExists[save.ID] = true
		hasSaves[save.GameID] = true
	}

	for _, seed := range candidates {
		if bound, isBound := state.BindingFor(seed.local); isBound {
			if saveExists[bound.OmnisaveID] {
				continue
			}
			// The bound Omnisave is gone from the authoritative server, so
			// the mapping is dead. Drop it and let this save bind fresh.
			state.Unbind(seed.local)
		}
		if hasSaves[seed.serverGameID] {
			outcome.Unbound++
			tui.BindSkipped(seed.local.GameTitle)
			continue
		}
		created, revision, err := binding.Seed(ctx, server, seed.serverGameID, seed.save)
		if err != nil {
			outcome.Failed++
			tui.TrackFailed(seed.local.GameTitle, err)
			continue
		}
		if err := state.Bind(seed.local, created.ID); err != nil {
			outcome.Failed++
			tui.TrackFailed(seed.local.GameTitle, err)
			continue
		}
		if err := state.RecordSynced(seed.local, created.ID, revision.ID); err != nil {
			outcome.Failed++
			tui.TrackFailed(seed.local.GameTitle, err)
			continue
		}
		outcome.Seeded++
		tui.BindSeeded(seed.local.GameTitle, seed.local.Kind)
	}
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
  omnisave-client connect [URL] [--state path]
  omnisave-client scan [--verbose]
  omnisave-client track [--state path]
  omnisave-client bind [--state path]

Commands:
  connect  Link this device to your Omnisave server and remember the connection
  scan     Discover installed targets, games, and saves without changing state
  track    Choose which discovered games should be synchronized; tracked games
           are added to your server library and record this device's provenance
  bind     Choose which Omnisave a discovered local save should synchronize with

The saved connection can be overridden per command with --server and --token,
or the OMNISAVE_SERVER_URL and OMNISAVE_API_TOKEN environment variables.`)
}
