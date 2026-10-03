package savesync

import (
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// OwnedElsewhere reports whether another Local Save of the same game
// installation owns the history: it is bound there, or a restore into it is
// journaled. A history follows one native boundary per Device, so equal bytes
// or an empty sibling slot never make a second owner (ADR-021). Only
// bindings whose save the scans still discover count, so a save whose
// identity changed can still rebind to its history.
func OwnedElsewhere(state *tracking.State, scans []client.TargetScan, local tracking.LocalSave, omnisaveID string) bool {
	sibling := func(targetID, gameID, saveID string) bool {
		return targetID == local.TargetID && gameID == local.GameID && saveID != local.ID
	}
	for _, pending := range state.PendingPlacements {
		if pending.OmnisaveID == omnisaveID && sibling(pending.Save.TargetID, pending.Save.GameID, pending.Save.ID) {
			return true
		}
	}
	for _, bound := range state.Bindings {
		if bound.OmnisaveID == omnisaveID && sibling(bound.TargetID, bound.LocalGameID, bound.LocalSaveID) && discovered(scans, bound) {
			return true
		}
	}
	return false
}

// unowned keeps the histories no other discovered save of this game
// installation owns.
func unowned(state *tracking.State, scans []client.TargetScan, local tracking.LocalSave, saves []omnisave.Omnisave) []omnisave.Omnisave {
	var available []omnisave.Omnisave
	for _, save := range saves {
		if !OwnedElsewhere(state, scans, local, save.ID) {
			available = append(available, save)
		}
	}
	return available
}

func discovered(scans []client.TargetScan, bound tracking.Binding) bool {
	for _, scan := range scans {
		if scan.Target.ID != bound.TargetID {
			continue
		}
		for _, game := range scan.Games {
			if game.Game.ID != bound.LocalGameID {
				continue
			}
			for _, save := range game.Saves {
				if save.ID == bound.LocalSaveID {
					return true
				}
			}
			for _, destination := range game.Destinations {
				if destination.ID == bound.LocalSaveID {
					return true
				}
			}
		}
	}
	return false
}
