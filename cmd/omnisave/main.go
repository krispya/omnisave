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
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/activity"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/discovery"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// errReported marks failures already rendered by the TUI: exit non-zero
// without printing a second, plainer copy of the error.
var errReported = errors.New("failure already reported")

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintf(os.Stderr, "omnisave: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	scanner := client.NewScanner(nil, retroarch.NewDefault(), steam.NewDefault())
	if len(arguments) == 0 {
		return runApp(ctx, scanner, nil)
	}
	switch arguments[0] {
	case "connect":
		return runConnect(ctx, arguments[1:])
	case "scan":
		return runScan(ctx, scanner, arguments[1:])
	case "track":
		return runTrack(ctx, scanner, arguments[1:])
	case "sync":
		return runSync(ctx, scanner, arguments[1:])
	case "watch":
		return runWatch(ctx, scanner, arguments[1:])
	case "bind":
		return runBind(ctx, scanner, arguments[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		// Bare flags belong to the commandless run.
		if strings.HasPrefix(arguments[0], "-") {
			return runApp(ctx, scanner, arguments)
		}
		return fmt.Errorf("unknown command %q; run omnisave with no command, or use track, sync, watch, connect, scan, or bind", arguments[0])
	}
}

// runConnect persists a server connection after successful approval.
func runConnect(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("connect", flag.ContinueOnError)
	statePath := flags.String("state", "", "path to local tracking state")
	server := flags.String("server", "", "Omnisave server URL; skips discovery")
	// Keep owner-token authentication as an explicit recovery path.
	token := flags.String("token", os.Getenv("OMNISAVE_API_TOKEN"), "owner token; skips pairing")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	serverURL := *server
	if serverURL == "" {
		serverURL = flags.Arg(0)
	}
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
		serverURL = os.Getenv("OMNISAVE_SERVER_URL")
	}

	if *token != "" {
		if serverURL == "" {
			serverURL = state.Server.URL
		}
		if serverURL == "" {
			return errors.New("connecting with a token needs --server")
		}
		_, err = establishServer(ctx, store, &state, serverURL, *token)
		return err
	}
	_, err = connectByPairing(ctx, store, &state, serverURL)
	if errors.Is(err, tui.ErrAborted) {
		// Calling off a connection is not a failure worth an error line.
		return nil
	}
	return err
}

// connectByPairing discovers a server and waits for owner approval.
func connectByPairing(
	ctx context.Context, store *tracking.Store, state *tracking.State, serverURL string,
) (*remote.Client, error) {
	if serverURL == "" {
		found, err := findServer(ctx)
		if err != nil {
			// Aborting is passed through rather than reported: calling off a
			// run is not a failure, and the caller ends it quietly.
			return nil, err
		}
		serverURL = found
	}
	serverURL, err := remote.NormalizeServerURL(serverURL)
	if err != nil {
		tui.ConnectFailed(err)
		return nil, errReported
	}

	device := state.EnsureDevice(deviceName())
	ticket, err := remote.RequestPairing(ctx, serverURL, access.RequestPairing{
		DeviceID:   device.ID,
		DeviceName: device.Name,
		Platform:   runtime.GOOS,
	}, nil)
	if err != nil {
		tui.ConnectFailed(err)
		return nil, errReported
	}

	// Capture the token from polling without exposing it to the display interface.
	var credential string
	outcome, err := tui.AwaitApproval(ctx, serverURL, ticket.Code,
		func(ctx context.Context) (access.CollectionStatus, error) {
			collection, err := remote.CollectPairing(ctx, serverURL, ticket.Handle, nil)
			if err != nil {
				return access.CollectionPending, err
			}
			if collection.Status == access.CollectionApproved {
				credential = collection.Token
			}
			return collection.Status, nil
		})
	if errors.Is(err, tui.ErrAborted) {
		return nil, err
	}
	if err != nil {
		tui.ConnectFailed(err)
		return nil, errReported
	}

	switch outcome {
	case access.CollectionApproved:
		return establishServer(ctx, store, state, serverURL, credential)
	case access.CollectionDenied:
		tui.ConnectDenied()
	default:
		tui.ConnectExpired()
	}
	return nil, errReported
}

// findServer discovers a server or asks for its address when discovery finds none.
func findServer(ctx context.Context) (string, error) {
	var servers []discovery.Server
	var browseErr error
	if err := tui.Wait(ctx, "Looking for Omnisave servers", func(ctx context.Context, _ *tui.WaitSession) {
		servers, browseErr = discovery.Browse(ctx, 0)
	}); err != nil {
		return "", err
	}
	if browseErr != nil {
		// Discovery is a convenience; a machine that cannot browse can still
		// be told where the server is.
		servers = nil
	}

	switch len(servers) {
	case 0:
		tui.NoServersFound()
		return tui.PromptServerURL("http://localhost:8080")
	case 1:
		tui.ServerFound(servers[0])
		return servers[0].URL, nil
	default:
		chosen, err := tui.ChooseServer(servers)
		if err != nil {
			return "", err
		}
		return chosen.URL, nil
	}
}

// establishServer verifies and persists a connection after registering the Device.
func establishServer(ctx context.Context, store *tracking.Store, state *tracking.State, serverURL, apiToken string) (*remote.Client, error) {
	server, err := remote.New(serverURL, apiToken, nil)
	if err != nil {
		tui.ConnectFailed(err)
		return nil, errReported
	}
	if err := verifyConnection(ctx, server); err != nil {
		tui.ConnectFailed(err)
		return nil, errReported
	}
	device := state.EnsureDevice(deviceName())
	if err := server.RegisterDevice(ctx, device.ID, catalog.RegisterDevice{Name: device.Name, Platform: runtime.GOOS}); err != nil {
		tui.ConnectFailed(err)
		return nil, errReported
	}

	state.Server = tracking.Server{URL: serverURL, Token: apiToken}
	if err := store.Save(*state); err != nil {
		return nil, err
	}
	tui.ConnectSuccess(serverURL, device.Name)
	return server, nil
}

// ensureServer returns a verified client, connecting first when necessary.
func ensureServer(ctx context.Context, store *tracking.Store, state *tracking.State, flagURL, flagToken string) (*remote.Client, error) {
	url, token := serverConnection(*state, flagURL, flagToken)
	if token != "" {
		server, err := remote.New(url, token, nil)
		if err != nil {
			return nil, err
		}
		if err := awaitServer(ctx, url, server); err != nil {
			if errors.Is(err, tui.ErrDisconnected) {
				return nil, errReported
			}
			return nil, err
		}
		return server, nil
	}
	// Use the pairing flow when the Device has no credential.
	return connectByPairing(ctx, store, state, flagURL)
}

// awaitServer verifies connectivity, offering retries only in an interactive terminal.
func awaitServer(ctx context.Context, serverURL string, server *remote.Client) error {
	if !isatty.IsTerminal(os.Stderr.Fd()) {
		err := verifyConnection(ctx, server)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, errRejectedToken):
			tui.ServerRejectedToken(serverURL)
		default:
			tui.ServerUnreachable(serverURL, err)
		}
		return tui.ErrDisconnected
	}
	return tui.AwaitConnection(ctx, serverURL, func(checkCtx context.Context) *tui.ConnectionFailure {
		err := verifyConnection(checkCtx, server)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, errRejectedToken):
			// Retrying re-asks with the same credentials; only connect helps.
			return &tui.ConnectionFailure{
				Cause: "the server rejected this token",
				Fix:   "Run omnisave connect to reconnect this device",
			}
		default:
			return &tui.ConnectionFailure{Cause: tui.Cause(err), Retry: true}
		}
	})
}

// errRejectedToken marks a server that answered and refused the credentials:
// the connection is wrong rather than absent, so retrying cannot fix it.
var errRejectedToken = errors.New("the server rejected this token")

func verifyConnection(ctx context.Context, server *remote.Client) error {
	_, err := server.ListOmnisaves(ctx)
	var response *remote.ResponseError
	if errors.As(err, &response) && response.StatusCode == http.StatusUnauthorized {
		return errRejectedToken
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
	server, err := ensureServer(ctx, store, &state, *serverURL, *token)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
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

// sessionMode is the difference between the two runs that reconcile.
type sessionMode int

const (
	// appSession is the commandless run: it asks only what this Device has
	// not answered yet and ends in the watch loop.
	appSession sessionMode = iota
	// trackSession is track: it exists to change which games this Device
	// protects, so it always asks, and it ends with its report.
	trackSession
)

// asks reports whether this run stops to ask which games to track.
func (m sessionMode) asks(state tracking.State) bool {
	return m == trackSession || len(state.Games) == 0
}

// keepsWatching reports whether this run hands off to the watch loop rather
// than ending once the pass is reported.
func (m sessionMode) keepsWatching() bool {
	return m == appSession
}

// runApp connects, initializes tracking when needed, reconciles, and watches.
func runApp(ctx context.Context, scanner *client.Scanner, arguments []string) error {
	return runSession(ctx, scanner, "omnisave", appSession, arguments)
}

// runTrack updates the tracked-game selection and runs one reconciliation pass.
func runTrack(ctx context.Context, scanner *client.Scanner, arguments []string) error {
	return runSession(ctx, scanner, "track", trackSession, arguments)
}

// runSession connects, updates tracking as needed, and runs one reconciliation pass.
func runSession(ctx context.Context, scanner *client.Scanner, name string, mode sessionMode, arguments []string) error {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
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
	server, err := ensureServer(ctx, store, &state, *serverURL, *token)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	if err != nil {
		return err
	}
	report := &tui.TrackReport{}
	reconciled := reconcileDeletedGames(ctx, server, &state, report)

	scans, err := tui.ScanForSelection(ctx, scanner)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	if err != nil {
		return err
	}
	asked := mode.asks(state)
	selected := standingSelection(state, scans)
	if asked {
		selected, err = tui.SelectTrackedGames(scans, state.TrackedIDs())
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
	}
	removed, err := state.ApplyVisible(tracking.FromScans(scans), selected)
	if err != nil {
		return err
	}
	if err := store.Save(state); err != nil {
		return err
	}

	var outcome tui.TrackOutcome
	var bindingErr error
	// Hand presence, the warmed detector, and deferred pulls to the watch phase.
	detector := running.PlatformDetector()
	var presence presenceWatch
	var deferredPulls []string
	waitErr := tui.Wait(ctx, "", func(taskCtx context.Context, session *tui.WaitSession) {
		taskCtx = activity.WithReporter(taskCtx, session.SetLabel)
		var confirmed map[string]bool
		outcome, confirmed = syncTracking(taskCtx, server, &state, scans, removed, report)
		// Build presence after resolving Library identities and reuse it in watch.
		presence = trackedPresence(scanner, &state, scans)
		if !outcome.Synced {
			return
		}
		// Prompts borrow the terminal from the activity line and hand it
		// back, so one spinner spans the whole server phase.
		prompts := &reconcilePrompts{
			empty: func(gameTitle string, options []tui.SyncToDeviceOption) (choice tui.SyncToDeviceChoice, err error) {
				session.Interact(func() {
					choice, err = tui.PromptSyncToDevice(gameTitle, options)
				})
				return choice, err
			},
			stale: func(gameTitle, omnisaveName string) (choice tui.StaleBindingChoice, err error) {
				session.Interact(func() {
					choice, err = tui.PromptStaleBinding(gameTitle, omnisaveName)
				})
				return choice, err
			},
			ambiguous: func(gameTitle string, options []tui.AmbiguousBindingOption) (choice tui.AmbiguousBindingChoice, err error) {
				session.Interact(func() {
					choice, err = tui.PromptAmbiguousBinding(gameTitle, options)
				})
				return choice, err
			},
			diverged: func(gameTitle, omnisaveName string) (choice tui.DivergedBindingChoice, err error) {
				session.Interact(func() {
					choice, err = tui.PromptDivergedBinding(gameTitle, omnisaveName)
				})
				return choice, err
			},
		}
		// Interactive pulls also defer while their game is running.
		var gate *pullGate
		if playing, sweepErr := playingNow(taskCtx, detector, presence.matchers); sweepErr == nil {
			gate = &pullGate{playing: playing}
		}
		bindingErr = reconcileSaves(taskCtx, server, &state, scans, confirmed, &outcome, report, prompts, gate, 0)
		if gate != nil {
			deferredPulls = gate.waiting
		}
		activity.Report(taskCtx, "finishing")
	})
	if waitErr != nil && !errors.Is(waitErr, tui.ErrAborted) {
		return waitErr
	}
	outcome.Untracked += reconciled
	pass := handoff{snapshot: report.Snapshot(), at: time.Now()}
	// A run that asked answers with its report; a run that asked nothing has
	// nothing to answer, so it lets the live view carry what the pass found.
	if asked {
		report.Print()
		tui.TrackSummary(outcome)
	}
	if err := store.Save(state); err != nil {
		return err
	}
	if errors.Is(waitErr, tui.ErrAborted) || errors.Is(bindingErr, tui.ErrAborted) {
		return nil
	}
	if bindingErr != nil {
		return bindingErr
	}
	if !mode.keepsWatching() {
		return nil
	}
	if asked {
		// The report is scrollback now; the live block gets its own space.
		fmt.Println()
	}
	return keepTracking(ctx, scanner, server, store, &state, scans,
		watchSeed{detector: detector, presence: presence, deferred: deferredPulls}, pass, *serverURL, *token)
}

// standingSelection refreshes visible tracked games without untracking missing ones.
func standingSelection(state tracking.State, scans []client.TargetScan) []string {
	tracked := state.TrackedIDs()
	selected := make([]string, 0, len(tracked))
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			if tracked[discovered.Game.ID] {
				selected = append(selected, discovered.Game.ID)
			}
		}
	}
	return selected
}

// keepTracking hands the reconciliation state to the watch loop until cancellation.
func keepTracking(
	ctx context.Context,
	scanner *client.Scanner,
	server *remote.Client,
	store *tracking.Store,
	state *tracking.State,
	scans []client.TargetScan,
	seed watchSeed,
	pass handoff,
	flagURL, flagToken string,
) error {
	settings := defaultWatchSettings()
	events := newAnnouncer()
	// The run's own pass is the view's opening state, not news: the loop
	// starts from it so the stream announces only what happens next.
	events.seen(pass.snapshot)
	loop := newWatchLoop(scanner, server, store, seed.detector, settings, events)
	loop.watched = watchedFiles(state, scans)
	loop.presence = seed.presence
	// Preserve deferred pulls when skipping the watch loop's opening pass.
	loop.deferred = seed.deferred
	url, _ := serverConnection(*state, flagURL, flagToken)
	return keepWatching(ctx, loop, url, settings, pass)
}

// reconcileDeletedGames untracks games confirmed deleted from the server Library.
func reconcileDeletedGames(ctx context.Context, server *remote.Client, state *tracking.State, report *tui.TrackReport) int {
	serverGameIDs, err := server.ListGameIDs(ctx)
	if err != nil {
		return 0
	}
	exists := make(map[string]bool, len(serverGameIDs))
	for _, id := range serverGameIDs {
		exists[id] = true
	}
	untracked := 0
	for _, id := range sortedGameIDs(state.Games) {
		game := state.Games[id]
		if game.ServerGameID == "" || exists[game.ServerGameID] {
			continue
		}
		state.Untrack(id)
		report.DeletedOnServer(game.Title)
		report.Removed(game.Title)
		untracked++
	}
	return untracked
}

// syncTracking registers tracked games and returns those confirmed by the server.
func syncTracking(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	scans []client.TargetScan,
	removed []tracking.Game,
	report *tui.TrackReport,
) (tui.TrackOutcome, map[string]bool) {
	device := state.EnsureDevice(deviceName())
	outcome := tui.TrackOutcome{Tracked: len(state.Games)}
	confirmed := make(map[string]bool)
	activity.Report(ctx, "registering device")
	err := server.RegisterDevice(ctx, device.ID, catalog.RegisterDevice{Name: device.Name, Platform: runtime.GOOS})
	if err != nil {
		report.SyncFailed(err)
		return outcome, confirmed
	}
	outcome.Synced = true

	identities := installedGameIdentities(scans)
	for _, id := range sortedGameIDs(state.Games) {
		game := state.Games[id]
		identity, visible := identities[id]
		if game.ServerGameID == "" && !visible {
			outcome.Pending++
			report.Pending(game.Title)
			continue
		}
		if game.ServerGameID == "" {
			activity.Report(ctx, "looking up "+game.Title)
			if !resolveIntoLibrary(ctx, server, state, &outcome, id, identity, report) {
				continue
			}
			game = state.Games[id]
		}
		activity.Report(ctx, "updating library")
		trackErr := server.TrackGame(ctx, game.ServerGameID, device.ID, catalog.TrackGame{
			Adapter:   game.Adapter,
			Installed: visible,
		})
		if isNotFound(trackErr) {
			// Untrack games whose stored Library identity was deleted by the server.
			state.Untrack(id)
			outcome.Untracked++
			outcome.Tracked--
			report.DeletedOnServer(game.Title)
			report.Removed(game.Title)
			continue
		}
		if trackErr != nil {
			outcome.Failed++
			report.Failed(game.Title, trackErr)
			continue
		}
		confirmed[id] = true
	}
	for _, game := range removed {
		if game.ServerGameID == "" {
			continue
		}
		// A game already deleted on the server has nothing left to untrack.
		if err := server.UntrackGame(ctx, game.ServerGameID, device.ID); err != nil && !isNotFound(err) {
			outcome.Failed++
			report.Failed(game.Title, err)
			continue
		}
		outcome.Untracked++
		report.Removed(game.Title)
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
	report *tui.TrackReport,
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
		report.Failed(game.Title, err)
		return false
	}
	state.SetServerGameID(id, resolution.Game.ID)
	if resolution.Status == catalog.ResolutionCreated {
		outcome.Added++
		report.Added(game.Title, resolution.Game.Title)
	} else {
		outcome.Linked++
		report.Linked(game.Title, resolution.Game.Title)
	}
	return true
}

func isNotFound(err error) bool {
	var response *remote.ResponseError
	return errors.As(err, &response) && response.StatusCode == http.StatusNotFound
}

// reconcilePrompts provides optional interaction; nil leaves decisions for a later run.
type reconcilePrompts struct {
	empty     func(gameTitle string, options []tui.SyncToDeviceOption) (tui.SyncToDeviceChoice, error)
	stale     func(gameTitle, omnisaveName string) (tui.StaleBindingChoice, error)
	ambiguous func(gameTitle string, options []tui.AmbiguousBindingOption) (tui.AmbiguousBindingChoice, error)
	diverged  func(gameTitle, omnisaveName string) (tui.DivergedBindingChoice, error)
}

// errUnanswered is how a replayed answer says a question is not the one it
// holds. The pass then leaves that save waiting, which is what a pass with
// no answers at all does with every question it meets.
var errUnanswered = errors.New("no answer for this save")

// A pass can answer some questions and not others: the watch view replays
// one divergence answer and leaves every other question to a later run. So
// each question asks whether it in particular can be put, rather than
// whether the pass is interactive at all. A nil set answers nothing.
func (p *reconcilePrompts) asksEmpty() bool     { return p != nil && p.empty != nil }
func (p *reconcilePrompts) asksStale() bool     { return p != nil && p.stale != nil }
func (p *reconcilePrompts) asksAmbiguous() bool { return p != nil && p.ambiguous != nil }
func (p *reconcilePrompts) asksDiverged() bool  { return p != nil && p.diverged != nil }

func interactivePrompts() *reconcilePrompts {
	return &reconcilePrompts{
		empty:     tui.PromptSyncToDevice,
		stale:     tui.PromptStaleBinding,
		ambiguous: tui.PromptAmbiguousBinding,
		diverged:  tui.PromptDivergedBinding,
	}
}

// bindUnboundSaves automatically binds unambiguous matches and prompts for stale ones.
func bindUnboundSaves(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	scans []client.TargetScan,
	confirmed map[string]bool,
	outcome *tui.TrackOutcome,
	report *tui.TrackReport,
) error {
	return reconcileSaves(ctx, server, state, scans, confirmed, outcome, report, interactivePrompts(), nil, 0)
}

// reconcileSaves binds unbound saves and runs three-way sync for bound saves.
// pushFloor spaces commits, and gate optionally defers pulls for running games.
func reconcileSaves(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	scans []client.TargetScan,
	confirmed map[string]bool,
	outcome *tui.TrackOutcome,
	report *tui.TrackReport,
	prompts *reconcilePrompts,
	gate *pullGate,
	pushFloor time.Duration,
) error {
	activity.Report(ctx, "checking saves")
	type candidate struct {
		local        tracking.LocalSave
		save         target.Save
		serverGameID string
	}
	var candidates []candidate
	type emptyCandidate struct {
		scan         client.TargetScan
		discovered   client.GameScan
		serverGameID string
	}
	var emptyCandidates []emptyCandidate
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			game := state.Games[discovered.Game.ID]
			if !confirmed[discovered.Game.ID] || game.ServerGameID == "" {
				continue
			}
			hasSave := false
			for _, save := range discovered.Saves {
				if len(save.Files) == 0 {
					continue
				}
				hasSave = true
				candidates = append(candidates, candidate{
					local:        tracking.LocalSaveFrom(scan, discovered, save),
					save:         save,
					serverGameID: game.ServerGameID,
				})
			}
			if !hasSave {
				emptyCandidates = append(emptyCandidates, emptyCandidate{
					scan: scan, discovered: discovered, serverGameID: game.ServerGameID,
				})
			}
		}
	}
	if len(candidates) == 0 && len(emptyCandidates) == 0 {
		return nil
	}

	remoteSaves, err := server.ListOmnisaves(ctx)
	if err != nil {
		outcome.Failed++
		report.BindingFailed(err)
		return nil
	}
	savesByID := make(map[string]omnisave.Omnisave, len(remoteSaves))
	savesByGame := make(map[string][]omnisave.Omnisave)
	for _, save := range remoteSaves {
		savesByID[save.ID] = save
		savesByGame[save.GameID] = append(savesByGame[save.GameID], save)
	}
	histories := make(map[string][]omnisave.Revision)
	historyLoaded := make(map[string]bool)
	loadHistory := func(omnisaveID string) ([]omnisave.Revision, error) {
		if !historyLoaded[omnisaveID] {
			history, err := server.ListRevisions(ctx, omnisaveID)
			if err != nil {
				return nil, err
			}
			histories[omnisaveID] = history
			historyLoaded[omnisaveID] = true
		}
		return histories[omnisaveID], nil
	}

	for _, candidate := range candidates {
		activity.Report(ctx, "checking "+candidate.local.GameTitle)
		if _, tracked := state.Games[candidate.local.GameID]; !tracked {
			// An earlier candidate's server-side deletion untracked this
			// game mid-pass; its remaining saves have nothing to bind to.
			continue
		}
		if bound, isBound := state.BindingFor(candidate.local); isBound {
			if remoteSave, exists := savesByID[bound.OmnisaveID]; exists {
				if err := syncBoundSave(ctx, server, state, candidate.local, candidate.save,
					bound, remoteSave, loadHistory, outcome, report, prompts, gate, pushFloor); err != nil {
					return err
				}
				continue
			}
			if len(savesByGame[candidate.serverGameID]) == 0 {
				// The Omnisave this device protected was deleted on the
				// authoritative server and no other lineage remains, so the
				// deletion syncs back as untracking — reseeding here would
				// resurrect the deleted content. Re-tracking starts fresh.
				state.Untrack(candidate.local.GameID)
				if err := server.UntrackGame(ctx, candidate.serverGameID, state.Device.ID); err != nil && !isNotFound(err) {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
				}
				outcome.Untracked++
				outcome.Tracked--
				report.SaveDeleted(candidate.local.GameTitle)
				report.Removed(candidate.local.GameTitle)
				continue
			}
			// Drop a dead mapping when another server lineage still survives.
			state.Unbind(candidate.local)
		}

		gameSaves := savesByGame[candidate.serverGameID]
		if len(gameSaves) == 0 {
			seedCandidateSave(ctx, server, state, candidate.local, candidate.save, candidate.serverGameID, outcome, report)
			continue
		}

		lineages := make([]binding.Lineage, 0, len(gameSaves))
		loadFailed := false
		for _, remoteSave := range gameSaves {
			history, err := loadHistory(remoteSave.ID)
			if err != nil {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, err)
				loadFailed = true
				break
			}
			lineages = append(lineages, binding.Lineage{
				Omnisave:  remoteSave,
				Revisions: history,
			})
		}
		if loadFailed {
			continue
		}
		matches, err := binding.FindContentMatchesContext(ctx, candidate.save, lineages)
		if err != nil {
			outcome.Failed++
			report.SaveFailed(candidate.local.GameTitle, err)
			continue
		}
		if len(matches) == 1 && matches[0].MatchesCurrent() {
			matched := matches[0].Omnisave
			if err := state.Bind(candidate.local, matched.ID); err != nil {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, err)
				continue
			}
			if err := state.RecordSynced(candidate.local, matched.ID, *matched.CurrentRevisionID); err != nil {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, err)
				continue
			}
			outcome.Rebound++
			report.SyncedWith(candidate.local.GameTitle, omnisaveDisplayName(matched), time.Now())
			continue
		}
		if len(matches) == 1 {
			matched := matches[0]
			current, currentFound := revisionByID(histories[matched.Omnisave.ID], matched.Omnisave.CurrentRevisionID)
			if !currentFound {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, errors.New("matching Omnisave has no readable current revision"))
				continue
			}
			matchedRevision := matched.Revisions[len(matched.Revisions)-1]
			if !prompts.asksStale() {
				// Headless: the stale question waits for an interactive run.
				outcome.Unbound++
				report.Stale(candidate.local.GameTitle, omnisaveDisplayName(matched.Omnisave))
				continue
			}
			choice, err := prompts.stale(candidate.local.GameTitle, omnisaveDisplayName(matched.Omnisave))
			if err != nil {
				return err
			}
			switch choice {
			case tui.StaleBindingJump:
				if err := binding.ApplyCurrent(ctx, server, candidate.save, matchedRevision, current); err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
				if err := state.Bind(candidate.local, matched.Omnisave.ID); err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
				if err := state.RecordSynced(candidate.local, matched.Omnisave.ID, current.ID); err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
				outcome.Jumped++
				report.SyncedWith(candidate.local.GameTitle, omnisaveDisplayName(matched.Omnisave), time.Now())
				continue
			case tui.StaleBindingFork:
				fork, err := server.ForkOmnisave(ctx, matched.Omnisave.ID, omnisave.ForkOmnisave{
					RevisionID:  matchedRevision.ID,
					DisplayName: forkDisplayName(matched.Omnisave),
				})
				if err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
				if err := state.Bind(candidate.local, fork.Omnisave.ID); err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
				if err := state.RecordSynced(candidate.local, fork.Omnisave.ID, fork.Revision.ID); err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
				outcome.Forked++
				report.SyncedWith(candidate.local.GameTitle, omnisaveDisplayName(fork.Omnisave), time.Now())
				continue
			default:
				return fmt.Errorf("unknown stale binding choice %q", choice)
			}
		}
		// Leave zero or multiple matches for an explicit choice.
		if !prompts.asksAmbiguous() {
			outcome.Unbound++
			report.Unbound(candidate.local.GameTitle)
			continue
		}
		matchedRevisions := make(map[string]string, len(matches))
		for _, match := range matches {
			matchedRevisions[match.Omnisave.ID] = match.Revisions[len(match.Revisions)-1].ID
		}
		options := make([]tui.AmbiguousBindingOption, 0, len(gameSaves))
		for _, remoteSave := range gameSaves {
			options = append(options, tui.AmbiguousBindingOption{
				OmnisaveID:        remoteSave.ID,
				Name:              omnisaveDisplayName(remoteSave),
				MatchedRevisionID: matchedRevisions[remoteSave.ID],
			})
		}
		choice, err := prompts.ambiguous(candidate.local.GameTitle, options)
		if err != nil {
			return err
		}
		switch {
		case choice.Seed:
			seedCandidateSave(ctx, server, state, candidate.local, candidate.save, candidate.serverGameID, outcome, report)
		case choice.OmnisaveID != "":
			if err := state.Bind(candidate.local, choice.OmnisaveID); err != nil {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, err)
				continue
			}
			matchedRevisionID := matchedRevisions[choice.OmnisaveID]
			if matchedRevisionID != "" {
				if err := state.RecordSynced(candidate.local, choice.OmnisaveID, matchedRevisionID); err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
			}
			name := choice.OmnisaveID
			for _, option := range options {
				if option.OmnisaveID == choice.OmnisaveID {
					name = option.Name
					break
				}
			}
			outcome.Bound++
			if matchedRevisionID != "" {
				report.SyncedWith(candidate.local.GameTitle, name, time.Now())
			} else {
				report.BoundUnsynced(candidate.local.GameTitle, name)
			}
		default:
			outcome.Unbound++
			report.Unbound(candidate.local.GameTitle)
		}
	}
	for _, candidate := range emptyCandidates {
		if _, tracked := state.Games[candidate.discovered.Game.ID]; !tracked {
			continue
		}
		gameSaves := savesByGame[candidate.serverGameID]
		if len(gameSaves) == 0 {
			report.NoSave(candidate.discovered.Game.Identity.DisplayTitle(candidate.discovered.Game.ID))
			continue
		}
		if err := syncSaveToDevice(ctx, server, state, candidate.scan, candidate.discovered,
			gameSaves, loadHistory, outcome, report, prompts); err != nil {
			return err
		}
	}
	return nil
}

// syncSaveToDevice places one selected server lineage at an unambiguous local destination.
func syncSaveToDevice(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	scan client.TargetScan,
	discovered client.GameScan,
	gameSaves []omnisave.Omnisave,
	loadHistory func(string) ([]omnisave.Revision, error),
	outcome *tui.TrackOutcome,
	report *tui.TrackReport,
	prompts *reconcilePrompts,
) error {
	title := discovered.Game.Identity.DisplayTitle(discovered.Game.ID)
	if len(discovered.Destinations) == 0 {
		report.SaveLocationUnavailable(title)
		return nil
	}
	type availableSave struct {
		save        omnisave.Omnisave
		current     omnisave.Revision
		destination target.SaveDestination
	}
	options := make([]tui.SyncToDeviceOption, 0, len(gameSaves))
	available := make(map[string]availableSave, len(gameSaves))
	for _, save := range gameSaves {
		if save.CurrentRevisionID == nil {
			continue
		}
		history, err := loadHistory(save.ID)
		if err != nil {
			outcome.Failed++
			report.SaveFailed(title, err)
			return nil
		}
		current, exists := revisionByID(history, save.CurrentRevisionID)
		if !exists {
			outcome.Failed++
			report.SaveFailed(title, errors.New("Omnisave has no readable current revision"))
			return nil
		}
		var compatible []target.SaveDestination
		for _, destination := range discovered.Destinations {
			if binding.CanMaterialize(destination, current) == nil {
				compatible = append(compatible, destination)
			}
		}
		if len(compatible) != 1 {
			continue
		}
		available[save.ID] = availableSave{save: save, current: current, destination: compatible[0]}
		options = append(options, tui.SyncToDeviceOption{OmnisaveID: save.ID, Name: omnisaveDisplayName(save)})
	}
	if len(options) == 0 {
		report.SaveLocationUnavailable(title)
		return nil
	}
	if !prompts.asksEmpty() {
		report.SaveAvailable(title)
		return nil
	}
	choice, err := prompts.empty(title, options)
	if err != nil {
		return err
	}
	if choice.OmnisaveID == "" {
		report.SaveAvailable(title)
		return nil
	}
	selected, exists := available[choice.OmnisaveID]
	if !exists {
		return fmt.Errorf("unknown sync-to-device choice %q", choice.OmnisaveID)
	}
	materialized, err := binding.Materialize(ctx, server, selected.destination, selected.current)
	if err != nil {
		outcome.Failed++
		report.SaveFailed(title, err)
		return nil
	}
	local := tracking.LocalSaveFrom(scan, discovered, materialized)
	if err := state.Bind(local, selected.save.ID); err != nil {
		outcome.Failed++
		report.SaveFailed(title, err)
		return nil
	}
	if err := state.RecordSynced(local, selected.save.ID, selected.current.ID); err != nil {
		outcome.Failed++
		report.SaveFailed(title, err)
		return nil
	}
	outcome.Pulled++
	report.SyncedWith(title, omnisaveDisplayName(selected.save), time.Now())
	return nil
}

// seedCandidateSave creates a new Omnisave from one local save and records
// the seed revision as the binding's sync baseline.
func seedCandidateSave(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	local tracking.LocalSave,
	save target.Save,
	serverGameID string,
	outcome *tui.TrackOutcome,
	report *tui.TrackReport,
) {
	created, revision, err := binding.Seed(ctx, server, serverGameID, save)
	if err != nil {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, err)
		return
	}
	if err := state.Bind(local, created.ID); err != nil {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, err)
		return
	}
	if err := state.RecordSynced(local, created.ID, revision.ID); err != nil {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, err)
		return
	}
	outcome.Seeded++
	report.SyncedWith(local.GameTitle, omnisaveDisplayName(*created), time.Now())
}

func revisionByID(history []omnisave.Revision, id *string) (omnisave.Revision, bool) {
	if id == nil {
		return omnisave.Revision{}, false
	}
	for _, revision := range history {
		if revision.ID == *id {
			return revision, true
		}
	}
	return omnisave.Revision{}, false
}

// descendsFrom reports strict ancestry and whether the available history proves it.
func descendsFrom(history []omnisave.Revision, node, ancestor omnisave.Revision) (descends, resolved bool) {
	byID := make(map[string]omnisave.Revision, len(history))
	for _, revision := range history {
		byID[revision.ID] = revision
	}
	// Bound the walk so a parent cycle resolves as unknown.
	for step := 0; step <= len(history); step++ {
		if node.ParentID == nil {
			return false, true
		}
		if *node.ParentID == ancestor.ID {
			return true, true
		}
		parent, ok := byID[*node.ParentID]
		if !ok {
			return false, false
		}
		node = parent
	}
	return false, false
}

func omnisaveDisplayName(save omnisave.Omnisave) string {
	if name := strings.TrimSpace(save.DisplayName); name != "" {
		return name
	}
	id := save.ID
	if len(id) > 8 {
		id = id[:8]
	}
	return "Omnisave " + id
}

// syncBoundSave compares local content, its baseline, and the Current Revision.
func syncBoundSave(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	local tracking.LocalSave,
	save target.Save,
	bound tracking.Binding,
	remoteSave omnisave.Omnisave,
	loadHistory func(string) ([]omnisave.Revision, error),
	outcome *tui.TrackOutcome,
	report *tui.TrackReport,
	prompts *reconcilePrompts,
	gate *pullGate,
	pushFloor time.Duration,
) error {
	history, err := loadHistory(remoteSave.ID)
	if err != nil {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, err)
		return nil
	}
	manifest, err := binding.ManifestContext(ctx, save)
	if err != nil {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, err)
		return nil
	}
	current, currentOK := revisionByID(history, remoteSave.CurrentRevisionID)
	if !currentOK {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, errors.New("bound Omnisave has no readable current revision"))
		return nil
	}
	name := omnisaveDisplayName(remoteSave)
	var baseline omnisave.Revision
	baselineOK := false
	if bound.LastSyncedRevisionID != nil {
		baseline, baselineOK = revisionByID(history, bound.LastSyncedRevisionID)
	}
	if !baselineOK {
		// A binding without a baseline (a manual bind to non-matching
		// content) is diverged from the start (FDR-005, decision 1).
		return resolveDivergence(ctx, server, state, local, save, remoteSave, current, nil, outcome, report, prompts)
	}

	if current.ID == baseline.ID {
		if binding.MatchesManifest(manifest, baseline) {
			report.SyncedWith(local.GameTitle, name, lastSyncedAt(bound))
			return nil
		}
		if pushFloor > 0 && bound.LastSyncedAt != nil && time.Since(*bound.LastSyncedAt) < pushFloor {
			// Spacing floor: too soon after the last commit.
			report.SyncedWith(local.GameTitle, name, lastSyncedAt(bound))
			return nil
		}
		revision, err := binding.Push(ctx, server, remoteSave.ID, save, baseline.ID, current.Files)
		if err != nil {
			var conflict *remote.CurrentRevisionConflict
			if errors.As(err, &conflict) {
				// Defer reconciliation when Current Revision moves during the commit.
				outcome.Conflicted++
				report.CurrentMoved(local.GameTitle, name)
				return nil
			}
			outcome.Failed++
			report.SaveFailed(local.GameTitle, err)
			return nil
		}
		if err := state.RecordSynced(local, remoteSave.ID, revision.ID); err != nil {
			outcome.Failed++
			report.SaveFailed(local.GameTitle, err)
			return nil
		}
		outcome.Pushed++
		report.SyncedWith(local.GameTitle, name, time.Now())
		return nil
	}

	// The Current Revision moved away from the baseline.
	if binding.MatchesManifest(manifest, current) {
		// Local already carries the Current Revision's content; only the baseline lags.
		if err := state.RecordSynced(local, remoteSave.ID, current.ID); err != nil {
			outcome.Failed++
			report.SaveFailed(local.GameTitle, err)
			return nil
		}
		outcome.Rebound++
		report.SyncedWith(local.GameTitle, name, time.Now())
		return nil
	}
	if binding.MatchesManifest(manifest, baseline) {
		if gate.holdPull(local.GameID) {
			// Defer pulls that a running game could overwrite from memory.
			outcome.Deferred++
			report.PullDeferred(local.GameTitle, name)
			return nil
		}
		// Pull. Lossless: the replaced content is the baseline revision,
		// which the server keeps; placement re-verifies local is unchanged.
		if err := binding.ApplyCurrent(ctx, server, save, baseline, current); err != nil {
			outcome.Failed++
			report.SaveFailed(local.GameTitle, err)
			return nil
		}
		if err := state.RecordSynced(local, remoteSave.ID, current.ID); err != nil {
			outcome.Failed++
			report.SaveFailed(local.GameTitle, err)
			return nil
		}
		outcome.Pulled++
		report.SyncedWith(local.GameTitle, name, time.Now())
		return nil
	}
	// Diverge only when Current descends from the baseline; rewinds and sibling
	// branches can accept local progress as a new branch.
	descends, resolved := descendsFrom(history, current, baseline)
	if resolved && !descends {
		revision, err := binding.PushBranch(ctx, server, remoteSave.ID, save, current.ID, baseline)
		if err != nil {
			var conflict *remote.CurrentRevisionConflict
			if errors.As(err, &conflict) {
				outcome.Conflicted++
				report.CurrentMoved(local.GameTitle, name)
				return nil
			}
			outcome.Failed++
			report.SaveFailed(local.GameTitle, err)
			return nil
		}
		if err := state.RecordSynced(local, remoteSave.ID, revision.ID); err != nil {
			outcome.Failed++
			report.SaveFailed(local.GameTitle, err)
			return nil
		}
		outcome.Branched++
		report.Branched(local.GameTitle, name)
		report.SyncedWith(local.GameTitle, name, time.Now())
		return nil
	}
	return resolveDivergence(ctx, server, state, local, save, remoteSave, current, &baseline, outcome, report, prompts)
}

// resolveDivergence preserves both sides, prompting only during interactive runs.
func resolveDivergence(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	local tracking.LocalSave,
	save target.Save,
	remoteSave omnisave.Omnisave,
	current omnisave.Revision,
	baseline *omnisave.Revision,
	outcome *tui.TrackOutcome,
	report *tui.TrackReport,
	prompts *reconcilePrompts,
) error {
	name := omnisaveDisplayName(remoteSave)
	waiting := func() error {
		outcome.Diverged++
		report.Diverged(local.GameTitle, name)
		return nil
	}
	if !prompts.asksDiverged() {
		return waiting()
	}
	choice, err := prompts.diverged(local.GameTitle, name)
	// A replayed answer belongs to one save. Every other divergence the pass
	// meets is one nobody has answered, so it waits exactly as it would have
	// under a headless pass.
	if errors.Is(err, errUnanswered) {
		return waiting()
	}
	if err != nil {
		return err
	}
	preservedName := forkDisplayName(remoteSave)
	if choice == tui.DivergedBindingJump {
		preservedName = divergedDisplayName(remoteSave)
	}
	preserved, preservedRevision, err := preserveLocalProgress(ctx, server, save, remoteSave, baseline, preservedName)
	if err != nil {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, err)
		return nil
	}
	outcome.Forked++
	if choice == tui.DivergedBindingFork {
		if err := state.Bind(local, preserved.ID); err != nil {
			outcome.Failed++
			report.SaveFailed(local.GameTitle, err)
			return nil
		}
		if err := state.RecordSynced(local, preserved.ID, preservedRevision.ID); err != nil {
			outcome.Failed++
			report.SaveFailed(local.GameTitle, err)
			return nil
		}
		report.SyncedWith(local.GameTitle, omnisaveDisplayName(*preserved), time.Now())
		return nil
	}
	// Jump: name the preservation fork, then adopt the Current Revision.
	report.Forked(local.GameTitle, omnisaveDisplayName(*preserved))
	// The preserved revision carries exactly the local content, so the
	// staged placement can verify against it.
	if err := binding.ApplyCurrent(ctx, server, save, *preservedRevision, current); err != nil {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, err)
		return nil
	}
	if err := state.Bind(local, remoteSave.ID); err != nil {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, err)
		return nil
	}
	if err := state.RecordSynced(local, remoteSave.ID, current.ID); err != nil {
		outcome.Failed++
		report.SaveFailed(local.GameTitle, err)
		return nil
	}
	outcome.Pulled++
	report.SyncedWith(local.GameTitle, name, time.Now())
	return nil
}

// preserveLocalProgress stores unsynced content as a fork or a new seed.
func preserveLocalProgress(
	ctx context.Context,
	server *remote.Client,
	save target.Save,
	remoteSave omnisave.Omnisave,
	baseline *omnisave.Revision,
	name string,
) (*omnisave.Omnisave, *omnisave.Revision, error) {
	if baseline == nil {
		return binding.Seed(ctx, server, remoteSave.GameID, save)
	}
	fork, err := server.ForkOmnisave(ctx, remoteSave.ID, omnisave.ForkOmnisave{
		RevisionID:  baseline.ID,
		DisplayName: name,
	})
	if err != nil {
		return nil, nil, err
	}
	revision, err := binding.Push(ctx, server, fork.Omnisave.ID, save, fork.Revision.ID, fork.Revision.Files)
	if err != nil {
		return nil, nil, err
	}
	return &fork.Omnisave, revision, nil
}

func lastSyncedAt(bound tracking.Binding) time.Time {
	if bound.LastSyncedAt == nil {
		return time.Time{}
	}
	return *bound.LastSyncedAt
}

func divergedDisplayName(source omnisave.Omnisave) string {
	name := strings.TrimSpace(source.DisplayName)
	if name == "" {
		return "Diverged"
	}
	suffix := " (diverged)"
	runes := []rune(name)
	if len(runes)+len([]rune(suffix)) > 100 {
		runes = runes[:100-len([]rune(suffix))]
	}
	return string(runes) + suffix
}

func forkDisplayName(source omnisave.Omnisave) string {
	name := strings.TrimSpace(source.DisplayName)
	if name == "" {
		return "Fork"
	}
	suffix := " (fork)"
	runes := []rune(name)
	if len(runes)+len([]rune(suffix)) > 100 {
		runes = runes[:100-len([]rune(suffix))]
	}
	return string(runes) + suffix
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
  omnisave [--state path]
  omnisave track [--state path]
  omnisave sync [--state path]
  omnisave watch [--poll 10s] [--pull-every 15m] [--floor 5m]
  omnisave connect [--server URL] [--state path]
  omnisave scan [--verbose]
  omnisave bind [--state path]

Run omnisave with no command to run Omnisave. It skips whatever this
device already did: it connects this device only if it holds no credential,
asks which games to track only if none are tracked, then always syncs once and
keeps watching until you quit.

Commands:
  track    Choose which discovered games should be synchronized; tracked games
           are added to your server library and record this device's provenance.
           It always asks, so a game installed since the last run can join,
           then syncs once, reports what happened, and exits. Protecting those
           saves from then on is what a commandless run does.
  sync     Sync every tracked save once: local progress uploads, server
           progress downloads, and anything needing a decision is reported
           for the next track run. Never prompts.
  watch    Keep syncing continuously: commits shortly after the game stops
           writing its save and checks the server periodically. Run it under
           your service manager to survive reboots.
  connect  Connect this device to your Omnisave server. With no arguments it
           looks for a server announcing itself on the local network, then
           shows a code to approve in the Dash's server settings. Use --server
           when nothing announces, such as from outside the network.
  scan     Discover installed targets, games, and saves without changing state
  bind     Choose which Omnisave a discovered local save should synchronize with

Once connected, this device holds its own credential and later commands need
no token or address. The connection can still be overridden per command with
--server and --token, or the OMNISAVE_SERVER_URL and OMNISAVE_API_TOKEN
environment variables; the owner token remains the way in when nothing else
works.`)
}
