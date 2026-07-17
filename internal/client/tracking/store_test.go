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
	if err := state.ApplyVisible(firstScan, []string{"steam:a", "steam:b"}); err != nil {
		t.Fatal(err)
	}
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
	if err := reloaded.ApplyVisible(secondScan, []string{"steam:c"}); err != nil {
		t.Fatal(err)
	}

	tracked := reloaded.TrackedIDs()
	if tracked["steam:a"] || !tracked["steam:b"] || !tracked["steam:c"] {
		t.Fatalf("expected A untracked, unavailable B preserved, and C tracked; got %+v", tracked)
	}
}
