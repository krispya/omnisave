package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

func TestManualBindingOffersOnlySavesForTheSelectedGame(t *testing.T) {
	state := tracking.NewState()
	state.Games["local-zomboid"] = tracking.Game{
		ID: "local-zomboid", ServerGameID: "server-zomboid",
	}
	local := tracking.LocalSave{
		ID: "local-save", Adapter: "steam", TargetID: "steam", GameID: "local-zomboid",
	}
	remote := []omnisave.Omnisave{
		{ID: "zomboid-main", GameID: "server-zomboid", DisplayName: "Main"},
		{ID: "spire-main", GameID: "server-spire", DisplayName: "Main"},
		{ID: "undertale-main", GameID: "server-undertale", DisplayName: "Main"},
	}

	destinations, err := bindingDestinations(state, local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(destinations) != 1 || destinations[0].ID != "zomboid-main" {
		t.Fatalf("expected only the selected game's save, got %+v", destinations)
	}
}

func TestManualBindingRequiresAResolvedGameIdentity(t *testing.T) {
	state := tracking.NewState()
	state.Games["local-zomboid"] = tracking.Game{ID: "local-zomboid"}
	local := tracking.LocalSave{
		ID: "local-save", Adapter: "steam", TargetID: "steam", GameID: "local-zomboid",
	}

	if _, err := bindingDestinations(state, local, nil); err == nil {
		t.Fatal("expected an unresolved game to have no manual binding destinations")
	}
}

// A run that cannot reach its server can neither resolve games nor bind
// saves, so it stops at the connection instead of spending a scan and a
// selection prompt on work it has to throw away.
func TestATrackRunStopsAtTheConnectionWhenTheSavedServerIsDown(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	address := down.URL
	down.Close()
	state := tracking.NewState()
	state.Server = tracking.Server{URL: address, Token: "secret"}
	statePath := filepath.Join(t.TempDir(), "state.json")

	server, err := ensureServer(context.Background(), tracking.NewStore(statePath), &state, "", "")

	if server != nil || !errors.Is(err, errReported) {
		t.Fatalf("expected the run to stop with a reported failure, got server=%v err=%v", server, err)
	}
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected an unreachable server to leave local state untouched, got %v", statErr)
	}
}

func TestAServerThatRefusesTheTokenIsDistinguishedFromAnOutage(t *testing.T) {
	unauthorized := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
	}))
	defer unauthorized.Close()
	remoteClient, err := remote.New(unauthorized.URL, "stale-token", unauthorized.Client())
	if err != nil {
		t.Fatal(err)
	}

	if err := verifyConnection(context.Background(), remoteClient); !errors.Is(err, errRejectedToken) {
		t.Fatalf("expected a rejected token, got %v", err)
	}
}

// The commandless run is the app: it stops for the questions this device
// has not answered yet, and it is the run that stays open. Track is the
// selection step on its own — it always asks, and it ends with its report.
func TestTheCommandlessRunAsksWhatIsUnansweredAndKeepsWatching(t *testing.T) {
	fresh := tracking.NewState()
	settled := tracking.NewState()
	settled.Games["local-game-1"] = tracking.Game{ID: "local-game-1", Title: "Chrono Trigger"}

	if !appSession.asks(fresh) {
		t.Fatal("expected the first run to ask which games to track")
	}
	if appSession.asks(settled) {
		t.Fatal("expected a device with tracked games to go straight to syncing")
	}
	if !appSession.keepsWatching() {
		t.Fatal("expected the commandless run to stay open watching")
	}

	if !trackSession.asks(settled) {
		t.Fatal("expected track to ask even once games are tracked")
	}
	if trackSession.keepsWatching() {
		t.Fatal("expected track to end with its report")
	}
}

// A run that skips the prompt still applies a selection — the standing one —
// so the scan refreshes what it saw without untracking what it missed.
func TestARunThatDoesNotAskKeepsEveryTrackedGame(t *testing.T) {
	fixture := savesynctest.NewFixture(t, "saved-game-content")
	state := fixture.State
	stale := state.Games["local-game-1"]
	stale.Title = "chrono_trigger"
	state.Games["local-game-1"] = stale
	state.Games["uninstalled-game"] = tracking.Game{
		ID: "uninstalled-game", Title: "Slay the Spire 2", ServerGameID: "server-game-2",
	}

	removed, err := state.ApplyVisible(savesync.TrackableGames(fixture.Scans), standingSelection(state, fixture.Scans))
	if err != nil {
		t.Fatal(err)
	}

	if len(removed) != 0 {
		t.Fatalf("expected a run that did not ask to untrack nothing, got %+v", removed)
	}
	if _, tracked := state.Games["uninstalled-game"]; !tracked {
		t.Fatal("expected a game the scan could not see to stay tracked")
	}
	scanned := state.Games["local-game-1"]
	if scanned.Title != "Chrono Trigger" || scanned.ServerGameID != "server-game-1" {
		t.Fatalf("expected the scan to refresh the game and keep its library identity, got %+v", scanned)
	}
}
