package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/activity"
	"github.com/krisbaumgartner/omnisave/internal/client/host"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/ludusavi/embedded"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile/steamcloud"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/target/gamehub"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/device"
	"github.com/krisbaumgartner/omnisave/internal/discovery"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// errReported marks failures already rendered by the TUI: exit non-zero
// without printing a second, plainer copy of the error.
var errReported = errors.New("failure already reported")

// Build metadata is replaced at build time for distributed binaries.
var (
	version     = "dev"
	buildNumber string
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintf(os.Stderr, "omnisave: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	return runWithOutput(ctx, arguments, os.Stdout)
}

func runWithOutput(ctx context.Context, arguments []string, output io.Writer) error {
	// Community save-location knowledge answers first; Steam's own cloud
	// configuration answers for the games it has never heard of, so a game
	// missing from the manifest is not left unprotected (FDR-003, decision 10).
	profiles := saveprofile.Fallback{
		Primary:   embedded.Provider(),
		Secondary: steamcloud.NewDefault(),
	}
	scanner := client.NewScanner(profiles, retroarch.NewDefault(), steam.NewDefault(), gamehub.NewDefault())
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
	case "service":
		return runService(ctx, arguments[1:])
	case steamCloudHelperCommand:
		// Internal: the client re-executes itself here so each Steam Cloud
		// reconciliation gets its own process (see steamcloudhelper.go).
		return runSteamCloudHelper(os.Stdin, output)
	case "update":
		return runUpdate(ctx, arguments[1:])
	case "help", "-h", "--help":
		printUsage(output)
		return nil
	case "-v", "--version":
		fmt.Fprintln(output, formattedVersion())
		return nil
	default:
		// Bare flags belong to the commandless run.
		if strings.HasPrefix(arguments[0], "-") {
			return runApp(ctx, scanner, arguments)
		}
		return fmt.Errorf("unknown command %q; run omnisave with no command, or use track, sync, watch, connect, scan, bind, update, or service", arguments[0])
	}
}

func formattedVersion() string {
	if buildNumber == "" {
		return "omnisave " + version
	}
	return fmt.Sprintf("omnisave %s-%s", version, buildNumber)
}

// runConnect persists a server connection after successful approval.
func runConnect(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("connect", flag.ContinueOnError)
	statePath := flags.String("state", "", "path to local tracking state")
	server := flags.String("server", "", "Omnisave server URL; skips discovery")
	// The owner token skips pairing: it is traded for this Device's own
	// credential and never stored.
	token := flags.String("token", os.Getenv("OMNISAVE_API_TOKEN"), "owner token to connect without pairing")
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
		local := state.EnsureDevice(host.DeviceName())
		issued, err := remote.ExchangeOwnerToken(ctx, serverURL, *token,
			access.TokenExchange{Name: local.Name, DeviceID: local.ID}, nil)
		if err != nil {
			tui.ConnectFailed(err)
			return errReported
		}
		_, err = establishServer(ctx, store, &state, serverURL, issued.Token)
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

	device := state.EnsureDevice(host.DeviceName())
	ticket, err := remote.RequestPairing(ctx, serverURL, access.RequestPairing{
		DeviceID:   device.ID,
		DeviceName: device.Name,
		Platform:   host.Platform(),
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
	local := state.EnsureDevice(host.DeviceName())
	if err := server.RegisterDevice(ctx, local.ID, device.Registration{Name: local.Name, Platform: host.Platform()}); err != nil {
		tui.ConnectFailed(err)
		return nil, errReported
	}

	state.Server = tracking.Server{URL: serverURL, Token: apiToken}
	if err := store.Save(*state); err != nil {
		return nil, err
	}
	tui.ConnectSuccess(serverURL, local.Name)
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
	local := trackedLocalSaves(state, savesync.LocalSaves(scans))
	if len(local) == 0 {
		fmt.Println("No saves were discovered for tracked games. Run track first or create a save in the game.")
		return nil
	}
	remoteSaves, err := server.ListOmnisaves(ctx)
	if err != nil {
		return err
	}
	if len(remoteSaves) == 0 {
		fmt.Println("The server has no Omnisaves to bind. Create one in the dashboard first.")
		return nil
	}
	selectedLocal, err := tui.SelectLocalSaveForBinding(local, remoteSaves, state.Bindings)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	if err != nil {
		return err
	}
	destinations, err := bindingDestinations(state, selectedLocal, remoteSaves)
	if err != nil {
		return err
	}
	if len(destinations) == 0 {
		fmt.Printf("The server has no Omnisaves for %s to bind.\n", selectedLocal.GameTitle)
		return nil
	}
	currentID := ""
	if current, exists := state.BindingFor(selectedLocal); exists {
		currentID = current.OmnisaveID
	}
	selectedOmnisave, err := tui.SelectOmnisaveForBinding(selectedLocal.GameTitle, destinations, currentID)
	if errors.Is(err, tui.ErrAborted) {
		return nil
	}
	if err != nil {
		return err
	}
	game := state.Games[selectedLocal.GameID]
	if selectedOmnisave.GameID != game.ServerGameID {
		return errors.New("cannot bind a local save to an Omnisave from another game")
	}
	if currentID == selectedOmnisave.ID {
		fmt.Printf("✓ %s (%s) already syncs with %s.\n", selectedLocal.GameTitle, selectedLocal.Kind, selectedOmnisave.DisplayName)
		return nil
	}
	if err := state.Bind(selectedLocal, selectedOmnisave.ID); err != nil {
		return err
	}
	if err := store.Save(state); err != nil {
		return err
	}
	fmt.Printf("✓ %s (%s) will sync with %s.\n", selectedLocal.GameTitle, selectedLocal.Kind, selectedOmnisave.DisplayName)
	return nil
}

// bindingDestinations limits a manual mapping to the selected Local Save's
// resolved Library game. A binding is never meaningful across game identities.
func bindingDestinations(state tracking.State, local tracking.LocalSave, remote []omnisave.Omnisave) ([]omnisave.Omnisave, error) {
	game, tracked := state.Games[local.GameID]
	if !tracked || game.ServerGameID == "" {
		return nil, errors.New("local save has no resolved server game; run omnisave track first")
	}
	destinations := make([]omnisave.Omnisave, 0, len(remote))
	for _, save := range remote {
		if save.GameID == game.ServerGameID {
			destinations = append(destinations, save)
		}
	}
	return destinations, nil
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
	flags.BoolVar(&verbose, "verbose", false, "explain where discovery looked for every game's save")
	flags.BoolVar(&verbose, "v", false, "explain where discovery looked for every game's save")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	_, err := tui.Scan(ctx, scanner, verbose, formattedVersion())
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
	reconciled := savesync.ReconcileDeletedGames(ctx, server, &state, report)

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
	removed, err := state.ApplyVisible(savesync.TrackableGames(scans), selected)
	if err != nil {
		return err
	}
	if err := store.Save(state); err != nil {
		return err
	}

	var outcome savesync.Outcome
	var bindingErr error
	// Hand presence, the warmed detector, and deferred pulls to the watch phase.
	detector := running.PlatformDetector()
	var presence savesync.Presence
	var deferredPulls []string
	waitErr := tui.Wait(ctx, "", func(taskCtx context.Context, session *tui.WaitSession) {
		// The one-line spinner has no game row for a phase to land on, so
		// while a pass has a game in hand the label leads with it — "Hollow
		// Knight · uploading (1/3)" — the way a watch row reads. Working and
		// Report both come from the task goroutine, so a plain string carries
		// the title between them.
		working := ""
		report.OnWorking = func(title string) { working = title }
		taskCtx = activity.WithReporter(taskCtx, func(message string) {
			if working != "" {
				message = working + " · " + tui.RowPhase(message, working)
			}
			session.SetLabel(message)
		})
		var confirmed map[string]bool
		outcome, confirmed = savesync.SyncTracking(taskCtx, server, &state, scans, removed, report)
		// Build presence after resolving Library identities and reuse it in watch.
		presence = savesync.TrackedPresence(scanner, &state, scans)
		if !outcome.Synced {
			return
		}
		// Interactive pulls also defer while their game is running.
		var gate *savesync.PullGate
		if playing, sweepErr := presence.Sweep(taskCtx, detector); sweepErr == nil {
			gate = savesync.NewPullGate(playing)
		}
		ports := savesync.Ports{Server: server, Adapters: scanner, Report: report, Checkpoint: store.Save}
		bindingErr = savesync.Reconcile(taskCtx, ports, &state, scans, confirmed, &outcome,
			savesync.Options{Prompts: sessionPrompts(session), Gate: gate})
		deferredPulls = gate.Waiting()
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
		// The run that set tracking up is the one moment worth interrupting
		// to ask, and on a device that is about to stop having a terminal it
		// is the last one. Taking the offer ends the run: the service is
		// watching from here, and a foreground loop beside it would be a
		// second pass writing the same tracking state.
		if isatty.IsTerminal(os.Stdout.Fd()) && offerService(ctx, *statePath) {
			return nil
		}
	} else if isatty.IsTerminal(os.Stdout.Fd()) {
		suggestService(ctx, *statePath)
	}
	return keepTracking(ctx, scanner, server, store, &state, scans,
		watchSeed{detector: detector, presence: presence, deferred: deferredPulls}, pass, *serverURL, *token)
}

// sessionPrompts asks every reconciliation question in the terminal. Each
// prompt borrows the terminal from the activity line and hands it back, so
// one spinner spans the whole server phase.
func sessionPrompts(session *tui.WaitSession) savesync.Prompts {
	return savesync.Prompts{
		SyncToDevice: func(gameTitle string, options []savesync.SyncToDeviceOption) (choice savesync.SyncToDeviceChoice, err error) {
			session.Interact(func() { choice, err = tui.PromptSyncToDevice(gameTitle, options) })
			return choice, err
		},
		Stale: func(question savesync.StaleQuestion) (choice savesync.StaleChoice, err error) {
			session.Interact(func() { choice, err = tui.PromptStaleBinding(question) })
			return choice, err
		},
		Ambiguous: func(gameTitle string, options []savesync.AmbiguousOption) (choice savesync.AmbiguousChoice, err error) {
			session.Interact(func() { choice, err = tui.PromptAmbiguousBinding(gameTitle, options) })
			return choice, err
		},
		HeldSeed: func(gameTitle string) (create bool, err error) {
			session.Interact(func() { create, err = tui.PromptHeldLineageSeed(gameTitle) })
			return create, err
		},
		Diverged: func(question savesync.DivergedQuestion) (choice savesync.DivergedChoice, err error) {
			session.Interact(func() { choice, err = tui.PromptDivergedBinding(question) })
			return choice, err
		},
	}
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
	loop.watched = savesync.WatchedFiles(state, scans)
	loop.presence = seed.presence
	// Preserve deferred pulls when skipping the watch loop's opening pass.
	loop.deferred = seed.deferred
	url, _ := serverConnection(*state, flagURL, flagToken)
	return keepWatching(ctx, loop, url, settings, pass)
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `Omnisave client

Usage:
  omnisave [--state path]
  omnisave -v | --version
  omnisave track [--state path]
  omnisave sync [--state path]
  omnisave watch [--poll 10s] [--pull-every 15m] [--floor 5m]
  omnisave connect [--server URL] [--state path]
  omnisave scan [--verbose]
  omnisave bind [--state path]
  omnisave update [--check] [--version 0.2.0]
  omnisave service install | uninstall | status

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
           writing its save and checks the server periodically. This is what
           the background service runs; omnisave service installs it.
  connect  Connect this device to your Omnisave server. With no arguments it
           looks for a server announcing itself on the local network, then
           shows a code to approve in the Dash's server settings. Use --server
           when nothing announces, such as from outside the network.
  scan     Discover installed targets, games, and saves without changing state.
           Use --verbose when a game is found but its save is not: it explains
           where discovery looked for every game and what was there, which is
           what a report about a missing save needs to say.
  bind     Choose which Omnisave a discovered local save should synchronize with
  update   Replace this client with the newest published release, verified
           against the release checksums before anything is installed. Use
           --check to see whether one is available without installing it.
  service  Run watch in the background, started by your session and restarted
           if it stops, so a device with no terminal keeps syncing. install
           needs this device connected first; status is how a device with
           nowhere to print says whether it is actually running.

Once connected, this device holds its own credential and later commands need
no token or address. The connection can still be overridden per command with
--server and --token, or the OMNISAVE_SERVER_URL and OMNISAVE_API_TOKEN
environment variables; the owner token remains the way in when nothing else
works.`)
}
