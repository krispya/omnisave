package savesync

import (
	"fmt"
	"slices"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// SelectSaveScopes returns the scope-selected view of scans without changing
// state, leaving discovery's whole-save facts intact for diagnostics. Save
// slots are the default wherever a Game Save Adapter finds them: every slot is
// tracked, occupied ones as saves and empty ones as destinations. A game
// without slots here, or with a whole-save binding, keeps its whole save. A
// game whose slot discovery fails is hidden, never broadened to its whole save.
func SelectSaveScopes(state *tracking.State, scans []client.TargetScan) []client.TargetScan {
	selected, _ := selectSaveScopes(state, scans, scopeOptions{})
	return selected
}

// ChooseSaveScopes explicitly revisits every game whose adapter finds save
// slots here, or fails to, which is how a game opts into, or back out of, its
// whole save. Histories are
// kept; incompatible local mappings are retired only after an answered choice.
// It reports whether any game had a choice to make.
func ChooseSaveScopes(state *tracking.State, scans []client.TargetScan, prompt func(ScopeQuestion) (ScopeChoice, error)) (bool, error) {
	asked := false
	_, err := selectSaveScopes(state, scans, scopeOptions{prompt: func(question ScopeQuestion) (ScopeChoice, error) {
		asked = true
		return prompt(question)
	}})
	return asked, err
}

type scopeOptions struct {
	// prompt asks every game that offers slots, even with a recorded scope.
	prompt func(ScopeQuestion) (ScopeChoice, error)
	// record persists the slot default for these games, so a later adapter
	// failure reports their slots unavailable instead of broadening to the
	// whole save. Nil records nothing.
	record map[string]bool
	// failed hears saves the view must hide; nil stays quiet.
	failed func(gameID, title string, err error)
}

func selectSaveScopes(state *tracking.State, scans []client.TargetScan, options scopeOptions) ([]client.TargetScan, error) {
	selected := make([]client.TargetScan, len(scans))
	for i, scan := range scans {
		selected[i] = scan
		selected[i].Games = slices.Clone(scan.Games)
		for j, g := range scan.Games {
			if _, tracked := state.Games[g.Game.ID]; !tracked {
				continue
			}
			view := &selected[i].Games[j]
			title := g.Game.Identity.DisplayTitle(g.Game.ID)
			// An adapter either finds slots, finds none here (the whole save
			// applies), or fails to finish discovery. A failure is never read
			// as "no slots": it would silently and permanently pick the whole.
			offered := g.Slots.Adapter != "" && (len(g.Slots.Found) > 0 || g.Slots.Err != nil)
			if offered && options.prompt != nil {
				if err := askScope(state, g, title, options.prompt); err != nil {
					return nil, err
				}
			}
			selection, known := state.SaveSelections[g.Game.ID]
			if !known {
				// Existing bindings and restores keep their scope until a person
				// explicitly revisits it; otherwise slots are the default.
				if adapter, bound := slotsInUse(state, g.Game.ID); bound {
					selection, known = tracking.SaveSelection{Adapter: adapter}, true
				} else if offered && !wholeInUse(state, g.Game.ID) {
					selection, known = tracking.SaveSelection{Adapter: g.Slots.Adapter}, true
				}
				if known && g.Slots.Err == nil && options.record[g.Game.ID] {
					if err := state.SelectSaves(g.Game.ID, selection); err != nil {
						return nil, err
					}
				}
			}
			if !known || selection.Adapter == "" {
				continue
			}
			view.Saves = nil
			view.Destinations = nil
			if g.Slots.Adapter != selection.Adapter || g.Slots.Err != nil {
				view.ScopeUnavailable = true
				if options.failed != nil {
					options.failed(g.Game.ID, title, fmt.Errorf("slots unavailable"))
				}
				continue
			}
			found := make(map[string]bool, len(g.Slots.Found))
			for _, slot := range g.Slots.Found {
				found[slot.Save.ID] = true
				view.Destinations = append(view.Destinations, slot.Destination)
				if len(slot.Save.Files) > 0 {
					view.Saves = append(view.Saves, slot.Save)
				}
			}
			for _, bound := range state.Bindings {
				if bound.LocalGameID == g.Game.ID && bound.Scope.Kind == omnisave.ScopeSlot && !found[bound.LocalSaveID] && options.failed != nil {
					options.failed(g.Game.ID, tracking.LocalSave{GameTitle: title, Slot: bound.Slot}.DisplayTitle(), fmt.Errorf("slot unavailable"))
				}
			}
		}
	}
	return selected, nil
}

// askScope puts one game's scope to a person and records an answer. An empty
// answer keeps the current scope.
func askScope(state *tracking.State, g client.GameScan, title string, prompt func(ScopeQuestion) (ScopeChoice, error)) error {
	question := ScopeQuestion{GameTitle: title, Current: ScopeSaveSlots}
	if selection, known := state.SaveSelections[g.Game.ID]; known && selection.Adapter == "" || !known && wholeInUse(state, g.Game.ID) {
		question.Current = ScopeWholeSave
	}
	for _, slot := range g.Slots.Found {
		question.Slots = append(question.Slots, slot.Save.Slot)
	}
	choice, err := prompt(question)
	if err != nil {
		return err
	}
	switch choice {
	case "":
		return nil
	case ScopeWholeSave:
		return state.SelectSaves(g.Game.ID, tracking.SaveSelection{})
	case ScopeSaveSlots:
		return state.SelectSaves(g.Game.ID, tracking.SaveSelection{Adapter: g.Slots.Adapter})
	default:
		return fmt.Errorf("unknown save scope choice %q", choice)
	}
}

// slotsInUse reports the adapter of a slot binding made before the slot
// default was recorded, such as by manual bind, which must not broaden.
func slotsInUse(state *tracking.State, gameID string) (string, bool) {
	for _, bound := range state.Bindings {
		if bound.LocalGameID == gameID && bound.Scope.Kind == omnisave.ScopeSlot {
			return bound.Scope.Adapter, true
		}
	}
	return "", false
}

// wholeInUse reports a whole-save binding, or a whole-save restore in
// progress, that an unrecorded default must not reinterpret.
func wholeInUse(state *tracking.State, gameID string) bool {
	for _, bound := range state.Bindings {
		if bound.LocalGameID == gameID && bound.Scope == (omnisave.SaveScope{}) {
			return true
		}
	}
	for _, pending := range state.PendingPlacements {
		if pending.Save.GameID == gameID && pending.Save.Scope == (omnisave.SaveScope{}) {
			return true
		}
	}
	return false
}
