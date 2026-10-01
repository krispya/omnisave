package savesync_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
	catalogservice "github.com/krisbaumgartner/omnisave/internal/catalog/service"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync"
	"github.com/krisbaumgartner/omnisave/internal/client/savesync/savesynctest"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	deviceservice "github.com/krisbaumgartner/omnisave/internal/device/service"
	"github.com/krisbaumgartner/omnisave/internal/httpapi"
	omnisaveservice "github.com/krisbaumgartner/omnisave/internal/omnisave/service"
	sqlitestorage "github.com/krisbaumgartner/omnisave/internal/storage/sqlite"
)

// dashServer is the real server with its address exposed, so a story can
// act as the Dash beside the Device.
func dashServer(t *testing.T) (*httptest.Server, *remote.Client) {
	t.Helper()
	directory := t.TempDir()
	repository, err := sqlitestorage.Open(
		filepath.Join(directory, "omnisave.db"), filepath.Join(directory, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repository.Close() })
	server := httptest.NewServer(httpapi.New(accessservice.New(repository, savesynctest.Token), httpapi.Config{
		Saves:   omnisaveservice.New(repository),
		Catalog: catalogservice.New(repository, repository),
		Devices: deviceservice.New(repository),
	}))
	t.Cleanup(server.Close)
	remoteClient, err := remote.New(server.URL, savesynctest.Token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return server, remoteClient
}

// deleteGameInTheDash deletes a Library game through the API the Dash uses.
func deleteGameInTheDash(t *testing.T, server *httptest.Server, gameID string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/games/"+gameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+savesynctest.Token)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK {
		t.Fatalf("expected the Dash-style delete to succeed, got %d", response.StatusCode)
	}
}

// playtest is a Device tracking one Steam game with no save yet.
func playtest() ([]client.TargetScan, tracking.State) {
	scans := []client.TargetScan{{
		Target: target.Target{ID: "target-1", Adapter: "steam"},
		Games: []client.GameScan{{
			Game: target.InstalledGame{
				ID:       "local-game-1",
				TargetID: "target-1",
				Identity: target.GameIdentity{
					Title:       "Draw Steel Codex Playtest",
					Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: "999999"}},
				},
			},
		}},
	}}
	state := tracking.NewState()
	state.Games["local-game-1"] = tracking.Game{
		ID: "local-game-1", Adapter: "steam", TargetID: "target-1", Title: "Draw Steel Codex Playtest",
	}
	return scans, state
}

// Resolve and track a game, delete it through the same API the Dash uses,
// and the next run untracks it before the selection prompt would preselect
// it.
func TestDeletingAGameInTheDashUntracksItBeforeTheNextPrompt(t *testing.T) {
	dash, server := dashServer(t)
	scans, state := playtest()
	outcome, confirmed := savesync.SyncTracking(context.Background(), server, &state, scans, nil, &savesynctest.Recorder{})
	if !outcome.Synced || outcome.Added != 1 || !confirmed["local-game-1"] {
		t.Fatalf("expected the first run to add and confirm the game, got %+v", outcome)
	}
	serverGameID := state.Games["local-game-1"].ServerGameID
	if serverGameID == "" {
		t.Fatal("expected a resolved Library identity")
	}

	deleteGameInTheDash(t, dash, serverGameID)

	report := &savesynctest.Recorder{}
	if reconciled := savesync.ReconcileDeletedGames(context.Background(), server, &state, report); reconciled != 1 {
		t.Fatalf("expected reconciliation to untrack the deleted game, got %d", reconciled)
	}
	if state.TrackedIDs()["local-game-1"] {
		t.Fatal("expected the selection prompt to no longer preselect the deleted game")
	}
	if len(report.Of("DeletedOnServer")) != 1 || len(report.Of("Removed")) != 1 {
		t.Fatalf("expected the deletion reported, got %+v", report.Events)
	}
}

// A game deleted between one run's tracking and the next is noticed however
// the run learns of it: telling the server what this Device tracks is
// refused for a game it no longer has, and the game is untracked here
// rather than reported as a failure.
func TestTrackingAGameTheServerDeletedUntracksIt(t *testing.T) {
	dash, server := dashServer(t)
	scans, state := playtest()
	savesync.SyncTracking(context.Background(), server, &state, scans, nil, &savesynctest.Recorder{})
	deleteGameInTheDash(t, dash, state.Games["local-game-1"].ServerGameID)

	// The game is uninstalled here, which changes the tracking claim, so the
	// run tells the server — and hears the game is gone.
	outcome, confirmed := savesync.SyncTracking(context.Background(), server, &state, nil, nil, &savesynctest.Recorder{})

	if outcome.Untracked != 1 || outcome.Failed != 0 || confirmed["local-game-1"] {
		t.Fatalf("expected the deleted game untracked without a failure, got %+v", outcome)
	}
	if state.TrackedIDs()["local-game-1"] {
		t.Fatal("expected the deleted game untracked here")
	}
}
