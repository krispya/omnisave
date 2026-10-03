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

var errPlacementDeferred = errors.New("placement waits for the game to close")

func (r *reconciliation) checkpoint() error {
	if r.Checkpoint == nil {
		return nil
	}
	if err := r.Checkpoint(*r.state); err != nil {
		return fmt.Errorf("persist pending restore: %w", err)
	}
	return nil
}

// place journals both sides before touching disk. The journal remains until
// the store has accepted the restore; a local content match cannot bypass it.
func (r *reconciliation) place(ctx context.Context, c candidate, before, current omnisave.Revision, omnisaveID string) error {
	p := tracking.PendingPlacement{OmnisaveID: omnisaveID, Save: c.save, Before: before, Current: current}
	if bound, ok := r.state.BindingFor(c.local); ok {
		p.BindingID = bound.OmnisaveID
	}
	r.state.RecordPlacement(c.local, p)
	return r.completePlacement(ctx, c, p)
}

// completePlacement can resume before or after local placement. Any unrelated
// change holds the restore rather than writing stale content over new progress.
func (r *reconciliation) completePlacement(ctx context.Context, c candidate, p tracking.PendingPlacement) error {
	if c.save.ID != p.Save.ID || c.save.Scope != p.Save.Scope {
		return errors.New("pending restore held: scope changed")
	}
	if r.Gate.holdPull(c.local.GameID) {
		r.outcome.Deferred++
		r.Report.PullDeferred(c.local.DisplayTitle(), "pending restore")
		return errPlacementDeferred
	}
	bound, isBound := r.state.BindingFor(c.local)
	bindingID := ""
	if isBound {
		bindingID = bound.OmnisaveID
	}
	// A crash after the successful bind but before clearing the journal is
	// also resumable: the bound revision then identifies the same placement.
	completedBinding := isBound && bindingID == p.OmnisaveID && bound.LastSyncedRevisionID != nil && *bound.LastSyncedRevisionID == p.Current.ID
	if bindingID != p.BindingID && !completedBinding {
		return errors.New("pending restore held: local binding changed")
	}
	saves, err := r.Server.ListOmnisaves(ctx)
	if err != nil {
		return err
	}
	currentStillSelected := false
	for _, save := range saves {
		if save.ID == p.OmnisaveID && save.Scope == c.save.Scope && save.CurrentRevisionID != nil && *save.CurrentRevisionID == p.Current.ID {
			currentStillSelected = true
		}
	}
	if !currentStillSelected {
		return errors.New("pending restore held: the server selected a different revision")
	}
	// Retry persistence too: an earlier checkpoint may have failed while
	// leaving the pending restore in this process's memory.
	if err := r.checkpoint(); err != nil {
		return err
	}
	if len(c.save.Files) == 0 && p.Destination != nil {
		placed, err := binding.Materialize(ctx, r.Server, *p.Destination, p.Current)
		if err != nil {
			return err
		}
		c.save = placed
	}
	manifest, err := binding.ManifestContext(ctx, c.save)
	if err != nil {
		return err
	}
	if !binding.MatchesManifest(manifest, c.save.LocationAliases, p.Current) {
		if p.Before.ID == "" || !binding.MatchesManifest(manifest, c.save.LocationAliases, p.Before) {
			return errors.New("pending restore held: local files changed")
		}
		if err := binding.ApplyCurrent(ctx, r.Server, c.save, p.Before, p.Current); err != nil {
			// An unbound adoption that never replaced local progress remains
			// an open answer: its branch holds the content, so the next pass
			// asks again as a stale match (FDR-003, decision 9). Once files
			// land, store failures must keep the restore journal.
			if p.BindingID == "" {
				unchanged, checkErr := binding.ManifestContext(ctx, c.save)
				if checkErr == nil && binding.MatchesManifest(unchanged, c.save.LocationAliases, p.Before) {
					r.state.ClearPlacement(c.local)
					if persistErr := r.checkpoint(); persistErr != nil {
						r.state.RecordPlacement(c.local, p)
						return errors.Join(err, persistErr)
					}
				}
			}
			return err
		}
		c.save = appliedSave(p.Save, p.Current)
	}
	evidence := target.PlacementEvidence{Before: map[string]string{}, Removed: removedPaths(p.Save, p.Current)}
	if p.Before.ID != "" {
		files, err := binding.AppliedFiles(p.Save, p.Before)
		if err != nil {
			return err
		}
		for index, file := range files {
			evidence.Before[file.Path] = p.Before.Files[index].Artifact.SHA256
		}
	}
	if err := c.finish(ctx, c.save, evidence); err != nil {
		return err
	}
	// An ordinary pull keeps its binding and achievement watermark. Only
	// adoption of another lineage starts a fresh binding.
	if !isBound || bound.OmnisaveID != p.OmnisaveID {
		if err := r.state.Bind(c.local, p.OmnisaveID); err != nil {
			return err
		}
	}
	if err := r.state.RecordSynced(c.local, p.OmnisaveID, p.Current.ID); err != nil {
		return err
	}
	r.state.ClearPlacement(c.local)
	if err := r.checkpoint(); err != nil {
		r.state.RecordPlacement(c.local, p)
		return err
	}
	return nil
}

func (r *reconciliation) retryPlacement(ctx context.Context, c candidate, p tracking.PendingPlacement) {
	if _, exists := r.lineages.save(p.OmnisaveID); !exists && len(r.lineages.byGame[c.serverGameID]) == 0 {
		r.untrackDeleted(ctx, c)
		return
	}
	working(ctx, r.Report, c.local.DisplayTitle())
	if err := r.completePlacement(ctx, c, p); err != nil {
		r.failed(c.local.DisplayTitle(), err)
		return
	}
	r.outcome.Pulled++
	remoteSave, _ := r.lineages.save(p.OmnisaveID)
	r.Report.SyncedWith(c.local.DisplayTitle(), omnisaveDisplayName(remoteSave), time.Now())
}
