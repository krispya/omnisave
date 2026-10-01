package savesync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// placementFinisher settles whatever a game's store must be told after
// files land in the game's own save folder. The placement itself has
// already succeeded when this runs, so it reports rather than fails: a
// registry that could not be settled is a warning the user must see, not a
// reason to unwind a completed placement (FDR-005).
type placementFinisher func(ctx context.Context, save target.Save)

// finishPlacement builds the finisher for one game's placements. Adapters
// with nothing to settle produce a finisher that does nothing.
func finishPlacement(
	adapters Adapters,
	discovered target.Target,
	game target.InstalledGame,
	title string,
	report Reporter,
) placementFinisher {
	return func(ctx context.Context, save target.Save) {
		if adapters == nil {
			return
		}
		adapter, exists := adapters.Adapter(discovered.Adapter)
		if !exists {
			return
		}
		finisher, finishes := adapter.(target.PlacementFinisher)
		if !finishes {
			return
		}
		placement, err := finisher.FinishPlacement(ctx, discovered, game, save)
		if err != nil {
			report.StoreRegistrationFailed(title, err)
			return
		}
		if placement.Skipped != "" {
			report.StoreRegistrationSkipped(title, placement.Skipped)
			return
		}
		if len(placement.Failed) > 0 {
			report.StoreRegistrationFailed(title,
				fmt.Errorf("the store refused %d of the placed files", len(placement.Failed)))
		}
		report.StoreRegistered(title, len(placement.Registered))
		report.StoreRegistrationIncomplete(title, len(placement.Unregistered)+placement.Outside)
		report.StoreExtras(title, len(placement.Extras))
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

// syncToDevice offers a game's server saves to a Device with no local save
// for it, and places the one a person picks at the game's only compatible
// destination (FDR-004). A pass without the prompt reports the offer and
// leaves the game untouched; it never writes into a game unasked.
func (r *reconciliation) syncToDevice(ctx context.Context, empty emptyCandidate) error {
	discovered := empty.discovered
	title := discovered.Game.Identity.DisplayTitle(discovered.Game.ID)
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
	materialized, err := binding.Materialize(ctx, r.Server, selected.destination, selected.current)
	if err != nil {
		r.failed(title, err)
		return nil
	}
	finishPlacement(r.Adapters, empty.scan.Target, discovered.Game, title, r.Report)(ctx, materialized)
	local := LocalSaveFrom(empty.scan, discovered, materialized)
	if err := r.bindSynced(local, selected.save.ID, selected.current.ID); err != nil {
		r.failed(title, err)
		return nil
	}
	r.outcome.Pulled++
	r.Report.SyncedWith(title, omnisaveDisplayName(selected.save), time.Now())
	return nil
}
