package savesync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/activity"
	"github.com/krisbaumgartner/omnisave/internal/client/host"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/device"
)

// ReconcileDeletedGames untracks every game whose Library record the server
// no longer lists, and returns how many it untracked. It runs before a scan
// so a game deleted in the Dash is never offered for tracking again. A server
// that cannot list its games untracks nothing.
func ReconcileDeletedGames(ctx context.Context, server Server, state *tracking.State, report Reporter) int {
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

// SyncTracking registers this Device, resolves each tracked game into the
// Library, tells the server what this Device tracks, and untracks the games
// in removed. It returns the run's opening Outcome and the local game IDs
// the server confirmed, which are the only games Reconcile may bind saves
// for. An Outcome with Synced false means the Device never reached the
// server and nothing past registration ran.
func SyncTracking(
	ctx context.Context,
	server Server,
	state *tracking.State,
	scans []client.TargetScan,
	removed []tracking.Game,
	report Reporter,
) (Outcome, map[string]bool) {
	local := state.EnsureDevice(host.DeviceName())
	outcome := Outcome{Tracked: len(state.Games)}
	confirmed := make(map[string]bool)
	activity.Report(ctx, "registering device")
	err := server.RegisterDevice(ctx, local.ID, device.Registration{Name: local.Name, Platform: host.Platform()})
	if err != nil {
		report.SyncFailed(err)
		return outcome, confirmed
	}
	outcome.Synced = true

	identities := installedGameIdentities(scans)
	now := time.Now()
	for _, id := range sortedGameIDs(state.Games) {
		game := state.Games[id]
		identity, visible := identities[id]
		if game.ServerGameID == "" && !visible {
			outcome.Pending++
			report.Pending(game.Title)
			continue
		}
		if game.ServerGameID == "" {
			report.Working(game.Title)
			activity.Report(ctx, "looking up "+game.Title)
			if !resolveIntoLibrary(ctx, server, state, &outcome, id, identity, report) {
				continue
			}
			game = state.Games[id]
		}
		tracked := catalog.TrackGame{Adapter: game.Adapter, Installed: visible}
		claim := trackingClaim(game.ServerGameID, tracked)
		if state.TrackingIsCurrent(id, claim, now) {
			// The server was told this, and nothing about it has changed. A
			// game that is simply still installed and still tracked is not
			// news, and saying it again is a round trip per game per pass.
			confirmed[id] = true
			continue
		}
		report.Working(game.Title)
		activity.Report(ctx, "updating library")
		trackErr := server.TrackGame(ctx, game.ServerGameID, local.ID, tracked)
		if errors.Is(trackErr, catalog.ErrNotFound) {
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
		state.RecordTracking(id, claim, now)
		confirmed[id] = true
	}
	report.Idle()
	for _, game := range removed {
		if game.ServerGameID == "" {
			continue
		}
		// A game already deleted on the server has nothing left to untrack.
		if err := server.UntrackGame(ctx, game.ServerGameID, local.ID); err != nil && !errors.Is(err, catalog.ErrNotFound) {
			outcome.Failed++
			report.Failed(game.Title, err)
			continue
		}
		outcome.Untracked++
		report.Removed(game.Title)
	}
	return outcome, confirmed
}

// trackingClaim is everything one tracking call tells the server, as one
// string. Two claims that read the same say the same thing, so the second
// of them is worth nothing — including the Library identity, since a game
// re-resolved to a different one is a different claim entirely.
func trackingClaim(serverGameID string, tracked catalog.TrackGame) string {
	return fmt.Sprintf("%s|%s|%t", serverGameID, tracked.Adapter, tracked.Installed)
}

// resolveIntoLibrary resolves one tracked game's Library identity from this
// scan's evidence, records it, and reports the change line.
func resolveIntoLibrary(
	ctx context.Context,
	server Server,
	state *tracking.State,
	outcome *Outcome,
	id string,
	identity target.GameIdentity,
	report Reporter,
) bool {
	game := state.Games[id]
	resolution, err := server.ResolveGame(ctx, catalog.Evidence{
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
