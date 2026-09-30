package savesync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// placementFinisher completes the store half of a restore. A failure leaves
// the placement journal pending and must never be reported as synced.
type placementFinisher func(context.Context, target.Save, target.PlacementEvidence) error

func finishPlacement(adapters Adapters, discovered target.Target, game target.InstalledGame, title string, report Reporter) placementFinisher {
	return func(ctx context.Context, save target.Save, evidence target.PlacementEvidence) error {
		if adapters == nil {
			return nil
		}
		adapter, exists := adapters.Adapter(discovered.Adapter)
		if !exists {
			return fmt.Errorf("placement adapter unavailable")
		}
		finisher, finishes := adapter.(target.PlacementFinisher)
		if !finishes {
			return nil
		}
		placement, err := finisher.FinishPlacement(ctx, discovered, game, save, evidence)
		if err != nil {
			report.StoreRegistrationFailed(title, err)
			return err
		}
		if placement.Skipped != "" {
			report.StoreRegistrationSkipped(title, placement.Skipped)
			return errors.New("store reconciliation is pending")
		}
		report.StoreRegistered(title, len(placement.Registered))
		report.StoreDeleted(title, len(placement.Deleted))
		report.StoreRegistrationIncomplete(title, len(placement.Unregistered)+placement.Outside)
		report.StoreExtras(title, len(placement.Extras))
		if len(placement.Failed) > 0 || placement.Outside > 0 || len(placement.Extras) > 0 {
			err := errors.New("store reconciliation is incomplete; restore remains pending")
			report.StoreRegistrationFailed(title, err)
			return err
		}
		return nil
	}
}

// appliedSave is save as a successful ApplyCurrent of current left it: the
// same identity holding the revision's files at their native paths. The
// mapping just carried the apply, so a failure here is unreachable in
// practice; the discovery-time files are the honest fallback if it happens.
func appliedSave(save target.Save, current omnisave.Revision) target.Save {
	files, err := binding.AppliedFiles(save, current)
	if err != nil {
		return save
	}
	applied := save
	applied.Files = files
	return applied
}

// removedPaths reports files removed by a successful ApplyCurrent. Those files
// were preserved in a revision before placement; unrelated cloud entries are
// not deletion candidates. Mapping failures leave the store untouched.
func removedPaths(save target.Save, current omnisave.Revision) []string {
	removed, err := binding.RemovedFiles(save, current)
	if err != nil {
		return nil
	}
	return removed
}

// syncToDevice offers a game's server saves to a Device with no local save
// for it, and places the one a person picks at the game's only compatible
// destination (FDR-003). A pass without the prompt reports the offer and
// leaves the game untouched; it never writes into a game unasked.
func (r *reconciliation) syncToDevice(ctx context.Context, empty emptyCandidate) error {
	discovered := empty.discovered
	title := discovered.Game.Identity.DisplayTitle(discovered.Game.ID)
	// Resume a journaled first placement even if no files landed before exit.
	for _, pending := range r.state.PendingPlacements {
		if pending.Save.GameID == discovered.Game.ID && pending.Save.TargetID == empty.scan.Target.ID {
			save := pending.Save
			save.Files = nil
			c := candidate{local: LocalSaveFrom(empty.scan, discovered, save), save: save,
				discovered: empty.scan.Target, game: discovered.Game, serverGameID: empty.serverGameID,
				finish: finishPlacement(r.Adapters, empty.scan.Target, discovered.Game, title, r.Report)}
			r.retryPlacement(ctx, c, pending)
			return nil
		}
	}
	if len(r.lineages.byGame[empty.serverGameID]) == 0 {
		// A game with nothing local and nothing on the server is one line
		// in the report and no work at all.
		r.Report.NoSave(title)
		return nil
	}
	working(ctx, r.Report, title)
	if len(discovered.Destinations) == 0 {
		r.Report.SaveLocationUnavailable(title)
		return nil
	}
	type availableSave struct {
		save        omnisave.Omnisave
		current     omnisave.Revision
		destination target.SaveDestination
	}
	gameSaves := r.lineages.gameSaves(empty.serverGameID)
	options := make([]SyncToDeviceOption, 0, len(gameSaves))
	available := make(map[string]availableSave, len(gameSaves))
	for _, save := range gameSaves {
		if save.CurrentRevisionID == nil {
			continue
		}
		if save.PathFormatVersion != omnisave.PathFormatNative {
			// Only a persisted native version admits placement. With no
			// local save there is no manifest to prove a mapping, so a
			// retired — or unreported — version only waits here.
			r.Report.MigrationHeld(title, omnisaveDisplayName(save), HoldNoNativeSave)
			r.outcome.Held++
			continue
		}
		history, err := r.lineages.history(ctx, save.ID)
		if err != nil {
			r.failed(title, err)
			return nil
		}
		current, exists := revisionByID(history, save.CurrentRevisionID)
		if !exists {
			r.failed(title, errors.New("Omnisave has no readable current revision"))
			return nil
		}
		var compatible []target.SaveDestination
		for _, destination := range discovered.Destinations {
			if binding.CanMaterialize(destination, current) == nil {
				compatible = append(compatible, destination)
			}
		}
		if len(compatible) != 1 {
			continue
		}
		available[save.ID] = availableSave{save: save, current: current, destination: compatible[0]}
		options = append(options, SyncToDeviceOption{OmnisaveID: save.ID, Name: omnisaveDisplayName(save)})
	}
	if len(options) == 0 {
		r.Report.SaveLocationUnavailable(title)
		return nil
	}
	if r.Prompts.SyncToDevice == nil {
		r.Report.SaveAvailable(title)
		return nil
	}
	choice, err := r.Prompts.SyncToDevice(title, options)
	if err != nil {
		return err
	}
	if choice.OmnisaveID == "" {
		r.Report.SaveAvailable(title)
		return nil
	}
	selected, exists := available[choice.OmnisaveID]
	if !exists {
		return fmt.Errorf("unknown sync-to-device choice %q", choice.OmnisaveID)
	}
	materialized, err := binding.PlannedMaterialization(selected.destination, selected.current)
	if err != nil {
		r.failed(title, err)
		return nil
	}
	local := LocalSaveFrom(empty.scan, discovered, materialized)
	pending := tracking.PendingPlacement{OmnisaveID: selected.save.ID, Save: materialized, Current: selected.current, Destination: &selected.destination}
	r.state.RecordPlacement(local, pending)
	emptySave := materialized
	emptySave.Files = nil
	c := candidate{local: local, save: emptySave, discovered: empty.scan.Target, game: discovered.Game,
		finish: finishPlacement(r.Adapters, empty.scan.Target, discovered.Game, title, r.Report)}
	if err := r.completePlacement(ctx, c, pending); err != nil {
		r.failed(title, err)
		return nil
	}
	r.outcome.Pulled++
	r.Report.SyncedWith(title, omnisaveDisplayName(selected.save), time.Now())
	return nil
}
