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

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
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
	scanner := client.NewScanner(nil, retroarch.NewDefault(), steam.NewDefault())
	if len(arguments) == 0 {
		return runTrack(ctx, scanner, nil)
	}
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
		// Bare flags belong to track, the default command.
		if strings.HasPrefix(arguments[0], "-") {
			return runTrack(ctx, scanner, arguments)
		}
		return fmt.Errorf("unknown command %q; use track (the default), connect, scan, or bind", arguments[0])
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

	_, err = establishServer(ctx, store, &state, serverURL, apiToken)
	return err
}

// establishServer verifies a connection, registers this device, and persists
// the server so later commands need no token or URL. Nothing is saved unless
// the token is accepted, so a failed attempt leaves any previous connection
// intact.
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

// ensureServer returns a client for the established server connection,
// running the connect flow first when this device has none: every command
// that records against a server knows which server that is, even when it is
// unreachable for the run itself.
func ensureServer(ctx context.Context, store *tracking.Store, state *tracking.State, flagURL, flagToken string) (*remote.Client, error) {
	url, token := serverConnection(*state, flagURL, flagToken)
	if token != "" {
		return remote.New(url, token, nil)
	}
	promptURL, promptToken, err := tui.PromptConnect(url)
	if err != nil {
		return nil, err
	}
	return establishServer(ctx, store, state, promptURL, promptToken)
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

func runTrack(ctx context.Context, scanner *client.Scanner, arguments []string) error {
	flags := flag.NewFlagSet("track", flag.ContinueOnError)
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

	var outcome tui.TrackOutcome
	var bindingErr error
	waitErr := tui.Wait(ctx, "", func(taskCtx context.Context, session *tui.WaitSession) {
		var confirmed map[string]bool
		outcome, confirmed = syncTracking(taskCtx, server, &state, scans, removed, report)
		if !outcome.Synced {
			return
		}
		// Stale-match questions borrow the terminal from the activity line
		// and hand it back, so one spinner spans the whole server phase.
		choose := func(gameTitle, omnisaveName string) (choice tui.StaleBindingChoice, err error) {
			session.Interact(func() {
				choice, err = tui.PromptStaleBinding(gameTitle, omnisaveName)
			})
			return choice, err
		}
		bindingErr = bindUnboundSavesWithChooser(taskCtx, server, &state, scans, confirmed, &outcome, report, choose)
	})
	if waitErr != nil && !errors.Is(waitErr, tui.ErrAborted) {
		return waitErr
	}
	outcome.Untracked += reconciled
	report.Print()
	tui.TrackSummary(outcome)
	if err := store.Save(state); err != nil {
		return err
	}
	if errors.Is(waitErr, tui.ErrAborted) || errors.Is(bindingErr, tui.ErrAborted) {
		return nil
	}
	return bindingErr
}

// reconcileDeletedGames untracks games whose Library records were deleted on
// the server, so the selection prompt reflects the deletion before it opens
// (FDR-002, decision 10). An unreachable server is not a deletion; when the
// list cannot be fetched, reconciliation waits for a run that can ask.
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
	report *tui.TrackReport,
) (tui.TrackOutcome, map[string]bool) {
	device := state.EnsureDevice(deviceName())
	outcome := tui.TrackOutcome{Tracked: len(state.Games)}
	confirmed := make(map[string]bool)
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
			if !resolveIntoLibrary(ctx, server, state, &outcome, id, identity, report) {
				continue
			}
			game = state.Games[id]
		}
		trackErr := server.TrackGame(ctx, game.ServerGameID, device.ID, catalog.TrackGame{
			Adapter:   game.Adapter,
			Installed: visible,
		})
		if isNotFound(trackErr) {
			// The stored Library identity no longer exists: the game was
			// deleted on the server, and the server is the authority, so the
			// deletion syncs back and this device stops tracking the game.
			// Re-tracking it in a later run resolves it fresh.
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

// bindUnboundSaves runs FDR-003's binding pass: conflict-free seeds and head
// matches are automatic, while a unique older match asks whether to advance
// or fork. Ambiguous and unmatched saves remain unbound.
func bindUnboundSaves(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	scans []client.TargetScan,
	confirmed map[string]bool,
	outcome *tui.TrackOutcome,
	report *tui.TrackReport,
) error {
	return bindUnboundSavesWithChooser(ctx, server, state, scans, confirmed, outcome, report, tui.PromptStaleBinding)
}

func bindUnboundSavesWithChooser(
	ctx context.Context,
	server *remote.Client,
	state *tracking.State,
	scans []client.TargetScan,
	confirmed map[string]bool,
	outcome *tui.TrackOutcome,
	report *tui.TrackReport,
	choose func(gameTitle, omnisaveName string) (tui.StaleBindingChoice, error),
) error {
	type candidate struct {
		local        tracking.LocalSave
		save         target.Save
		serverGameID string
	}
	var candidates []candidate
	type scannedGame struct {
		id    string
		title string
	}
	var scannedGames []scannedGame
	seenGames := make(map[string]bool)
	hasSave := make(map[string]bool)
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			game := state.Games[discovered.Game.ID]
			if !confirmed[discovered.Game.ID] || game.ServerGameID == "" {
				continue
			}
			if !seenGames[discovered.Game.ID] {
				seenGames[discovered.Game.ID] = true
				scannedGames = append(scannedGames, scannedGame{
					id:    discovered.Game.ID,
					title: game.Title,
				})
			}
			for _, save := range discovered.Saves {
				if len(save.Files) == 0 {
					continue
				}
				hasSave[discovered.Game.ID] = true
				candidates = append(candidates, candidate{
					local:        tracking.LocalSaveFrom(scan, discovered, save),
					save:         save,
					serverGameID: game.ServerGameID,
				})
			}
		}
	}
	for _, game := range scannedGames {
		if !hasSave[game.id] {
			report.NoSave(game.title)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	remoteSaves, err := server.ListOmnisaves(ctx)
	if err != nil {
		outcome.Failed++
		report.BindingFailed(err)
		return nil
	}
	saveExists := make(map[string]bool, len(remoteSaves))
	savesByGame := make(map[string][]omnisave.Omnisave)
	for _, save := range remoteSaves {
		saveExists[save.ID] = true
		savesByGame[save.GameID] = append(savesByGame[save.GameID], save)
	}
	histories := make(map[string][]omnisave.Revision)
	historyLoaded := make(map[string]bool)

	for _, candidate := range candidates {
		if _, tracked := state.Games[candidate.local.GameID]; !tracked {
			// An earlier candidate's server-side deletion untracked this
			// game mid-pass; its remaining saves have nothing to bind to.
			continue
		}
		if bound, isBound := state.BindingFor(candidate.local); isBound {
			if saveExists[bound.OmnisaveID] {
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
			// Another lineage survives, so only this mapping is dead. Drop
			// it and let the save match or wait for manual binding.
			state.Unbind(candidate.local)
		}

		gameSaves := savesByGame[candidate.serverGameID]
		if len(gameSaves) == 0 {
			created, revision, err := binding.Seed(ctx, server, candidate.serverGameID, candidate.save)
			if err != nil {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, err)
				continue
			}
			if err := state.Bind(candidate.local, created.ID); err != nil {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, err)
				continue
			}
			if err := state.RecordSynced(candidate.local, created.ID, revision.ID); err != nil {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, err)
				continue
			}
			outcome.Seeded++
			report.Seeded(candidate.local.GameTitle, created.DisplayName)
			continue
		}

		lineages := make([]binding.Lineage, 0, len(gameSaves))
		loadFailed := false
		for _, remoteSave := range gameSaves {
			if !historyLoaded[remoteSave.ID] {
				history, err := server.ListRevisions(ctx, remoteSave.ID)
				if err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					loadFailed = true
					break
				}
				histories[remoteSave.ID] = history
				historyLoaded[remoteSave.ID] = true
			}
			lineages = append(lineages, binding.Lineage{
				Omnisave:  remoteSave,
				Revisions: histories[remoteSave.ID],
			})
		}
		if loadFailed {
			continue
		}
		matches, err := binding.FindContentMatches(candidate.save, lineages)
		if err != nil {
			outcome.Failed++
			report.SaveFailed(candidate.local.GameTitle, err)
			continue
		}
		if len(matches) == 1 && matches[0].MatchesHead() {
			matched := matches[0].Omnisave
			if err := state.Bind(candidate.local, matched.ID); err != nil {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, err)
				continue
			}
			if err := state.RecordSynced(candidate.local, matched.ID, *matched.HeadRevisionID); err != nil {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, err)
				continue
			}
			outcome.Rebound++
			report.Rebound(candidate.local.GameTitle, omnisaveDisplayName(matched))
			continue
		}
		if len(matches) == 1 {
			matched := matches[0]
			head, headFound := revisionByID(histories[matched.Omnisave.ID], matched.Omnisave.HeadRevisionID)
			if !headFound {
				outcome.Failed++
				report.SaveFailed(candidate.local.GameTitle, errors.New("matching Omnisave has no readable head"))
				continue
			}
			matchedRevision := matched.Revisions[len(matched.Revisions)-1]
			choice, err := choose(candidate.local.GameTitle, omnisaveDisplayName(matched.Omnisave))
			if err != nil {
				return err
			}
			switch choice {
			case tui.StaleBindingFastForward:
				if err := binding.FastForward(ctx, server, candidate.save, matchedRevision, head); err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
				if err := state.Bind(candidate.local, matched.Omnisave.ID); err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
				if err := state.RecordSynced(candidate.local, matched.Omnisave.ID, head.ID); err != nil {
					outcome.Failed++
					report.SaveFailed(candidate.local.GameTitle, err)
					continue
				}
				outcome.Advanced++
				report.FastForwarded(candidate.local.GameTitle, omnisaveDisplayName(matched.Omnisave))
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
				report.Forked(candidate.local.GameTitle, omnisaveDisplayName(fork.Omnisave))
				continue
			default:
				return fmt.Errorf("unknown stale binding choice %q", choice)
			}
		}
		outcome.Unbound++
		report.Unbound(candidate.local.GameTitle)
	}
	return nil
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
  omnisave-client [track] [--state path]
  omnisave-client connect [URL] [--state path]
  omnisave-client scan [--verbose]
  omnisave-client bind [--state path]

Commands:
  track    Choose which discovered games should be synchronized; tracked games
           are added to your server library and record this device's provenance.
           Running omnisave-client with no command runs track; the first run
           asks for your server connection.
  connect  Link this device to your Omnisave server and remember the connection
  scan     Discover installed targets, games, and saves without changing state
  bind     Choose which Omnisave a discovered local save should synchronize with

The saved connection can be overridden per command with --server and --token,
or the OMNISAVE_SERVER_URL and OMNISAVE_API_TOKEN environment variables.`)
}
