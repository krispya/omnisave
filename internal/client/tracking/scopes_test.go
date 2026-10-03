package tracking_test

import (
	"path/filepath"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

func TestChangingSaveScopeRetiresMappingsWithoutReinterpretingHistory(t *testing.T) {
	state := tracking.NewState()
	state.Games["game"] = tracking.Game{ID: "game"}
	whole := tracking.LocalSave{ID: "whole", Adapter: "steam", TargetID: "target", GameID: "game"}
	slot := tracking.LocalSave{ID: "profile1", Adapter: "steam", TargetID: "target", GameID: "game", Scope: omnisave.SaveScope{Kind: omnisave.ScopeSlot, Adapter: "sts2.vanilla"}, Slot: "Profile 1"}
	if err := state.Bind(whole, "aggregate-history"); err != nil {
		t.Fatal(err)
	}
	if err := state.Bind(slot, "slot-history"); err == nil {
		t.Fatal("overlapping scope was accepted")
	}
	if err := state.SelectSaves("game", tracking.SaveSelection{Adapter: "sts2.vanilla"}); err != nil {
		t.Fatal(err)
	}
	if len(state.Bindings) != 0 {
		t.Fatal("aggregate mapping was reinterpreted")
	}
	if err := state.Bind(slot, "slot-history"); err != nil {
		t.Fatal(err)
	}
	if err := state.Bind(whole, "aggregate-history"); err == nil {
		t.Fatal("whole binding ignored the selected slot boundary")
	}
	if err := state.SelectSaves("game", tracking.SaveSelection{}); err != nil {
		t.Fatal(err)
	}
	if len(state.Bindings) != 0 {
		t.Fatal("slot mapping survived a whole-save selection")
	}
}

func TestPersistedSelectionsCannotBroadenOrContradictBindings(t *testing.T) {
	state := tracking.NewState()
	state.Games["game"] = tracking.Game{ID: "game"}
	slot := tracking.LocalSave{ID: "profile1", Adapter: "steam", TargetID: "target", GameID: "game", Scope: omnisave.SaveScope{Kind: omnisave.ScopeSlot, Adapter: "fixture.profiles"}}
	if err := state.SelectSaves("game", tracking.SaveSelection{Adapter: slot.Scope.Adapter}); err != nil {
		t.Fatal(err)
	}
	if err := state.Bind(slot, "history"); err != nil {
		t.Fatal(err)
	}
	store := tracking.NewStore(filepath.Join(t.TempDir(), "client.json"))
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	state.SaveSelections["game"] = tracking.SaveSelection{Adapter: "Not an adapter"}
	if err := store.Save(state); err == nil {
		t.Fatal("malformed slot adapter was accepted")
	}
	state.SaveSelections["game"] = tracking.SaveSelection{}
	if err := store.Save(state); err == nil {
		t.Fatal("slot binding contradicted persisted whole-save scope")
	}
	loaded, err := store.Load()
	if err != nil || loaded.SaveSelections["game"].Adapter != slot.Scope.Adapter {
		t.Fatal("refused write replaced valid selection")
	}
}
