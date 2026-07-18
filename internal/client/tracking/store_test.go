package tracking_test

import (
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
)

func TestTrackingSelectionsSurviveRestartsAndPreserveUnavailableGames(t *testing.T) {
	store := tracking.NewStore(filepath.Join(t.TempDir(), "client.json"))
	state := tracking.NewState()
	firstScan := []tracking.Game{
		{ID: "steam:a", Adapter: "steam", Title: "Game A"},
		{ID: "steam:b", Adapter: "steam", Title: "Game B"},
	}
	if _, err := state.ApplyVisible(firstScan, []string{"steam:a", "steam:b"}); err != nil {
		t.Fatal(err)
	}
	state.SetServerGameID("steam:a", "server-a")
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	secondScan := []tracking.Game{
		{ID: "steam:a", Adapter: "steam", Title: "Game A"},
		{ID: "steam:c", Adapter: "steam", Title: "Game C"},
	}
	removed, err := reloaded.ApplyVisible(secondScan, []string{"steam:c"})
	if err != nil {
		t.Fatal(err)
	}

	tracked := reloaded.TrackedIDs()
	if tracked["steam:a"] || !tracked["steam:b"] || !tracked["steam:c"] {
		t.Fatalf("expected A untracked, unavailable B preserved, and C tracked; got %+v", tracked)
	}
	if len(removed) != 1 || removed[0].ID != "steam:a" || removed[0].ServerGameID != "server-a" {
		t.Fatalf("expected untracked A reported with its Library identity, got %+v", removed)
	}
}

func TestOneLocalSaveBindsToOneOmnisaveWhileMachinesCanShareIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	store := tracking.NewStore(path)
	state := tracking.NewState()
	local := tracking.LocalSave{
		ID:       "steam:game:cloud:account",
		Adapter:  "steam",
		TargetID: "steam:installer",
		GameID:   "steam:game",
	}

	if err := state.Bind(local, "save-a"); err != nil {
		t.Fatal(err)
	}
	if err := state.RecordSynced(local, "save-a", "revision-a"); err != nil {
		t.Fatal(err)
	}
	if err := state.Bind(local, "save-b"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	binding, ok := reloaded.BindingFor(local)
	if !ok || binding.OmnisaveID != "save-b" {
		t.Fatalf("expected rebinding to replace the local save's destination, got %+v", reloaded.Bindings)
	}
	if binding.LastSyncedRevisionID != nil {
		t.Fatal("expected rebinding not to carry a sync baseline from the old Omnisave")
	}

	otherMachine := tracking.NewState()
	if err := otherMachine.Bind(tracking.LocalSave{
		ID:       "retroarch:chrono:battery",
		Adapter:  "retroarch",
		TargetID: "retroarch:macos",
		GameID:   "retroarch:chrono",
	}, "save-b"); err != nil {
		t.Fatal(err)
	}
	if otherMachine.Bindings[0].OmnisaveID != binding.OmnisaveID {
		t.Fatal("expected independent machines to be allowed to target the same Omnisave")
	}
}

func TestTrackingRemovalDropsVisibleBindingsAndPreservesUnavailableOnes(t *testing.T) {
	state := tracking.NewState()
	gameA := tracking.Game{ID: "game-a", Adapter: "steam", TargetID: "steam", Title: "Game A"}
	gameB := tracking.Game{ID: "game-b", Adapter: "steam", TargetID: "steam", Title: "Game B"}
	if _, err := state.ApplyVisible([]tracking.Game{gameA, gameB}, []string{gameA.ID, gameB.ID}); err != nil {
		t.Fatal(err)
	}
	localA := tracking.LocalSave{ID: "save-a", Adapter: "steam", TargetID: "steam", GameID: gameA.ID}
	localB := tracking.LocalSave{ID: "save-b", Adapter: "steam", TargetID: "steam", GameID: gameB.ID}
	if err := state.Bind(localA, "remote-a"); err != nil {
		t.Fatal(err)
	}
	if err := state.Bind(localB, "remote-b"); err != nil {
		t.Fatal(err)
	}

	if _, err := state.ApplyVisible([]tracking.Game{gameA}, nil); err != nil {
		t.Fatal(err)
	}
	if _, exists := state.BindingFor(localA); exists {
		t.Fatal("expected untracking a visible game to remove its active save binding")
	}
	if _, exists := state.BindingFor(localB); !exists {
		t.Fatal("expected an unavailable tracked game to retain its save binding")
	}
}
