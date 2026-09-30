package savesync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/activity"
	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// Options tune one reconciliation.
type Options struct {
	// Prompts answer the questions the pass meets; the zero value leaves
	// every question waiting.
	Prompts Prompts
	// Gate defers pulls for running games; nil defers nothing.
	Gate *PullGate
	// PushFloor spaces commits of one save: a change within it of the last
	// sync waits for a later pass. Zero commits every change.
	PushFloor time.Duration
}

// Reconcile works every save of the confirmed games in scans. It binds
// unbound saves — seeding, rebinding by content, or asking — syncs bound
// saves three ways against their baseline, offers server saves to games with
// no local save, and then reports achievements for every bound save. It adds
// what it did to outcome and records bindings, baselines, and remembered
// verdicts in state.
//
// Per-save failures are counted and reported, never returned: one save's
// trouble does not stop the rest. A failure to list the server's saves is
// reported as a binding failure and ends the pass quietly. The only errors
// returned come from Prompts — a prompt's own error, unchanged, so a caller
// can tell a person calling off the run apart from a failure, or an answer
// that is not one of the question's choices.
func Reconcile(
	ctx context.Context,
	ports Ports,
	state *tracking.State,
	scans []client.TargetScan,
	confirmed map[string]bool,
	outcome *Outcome,
	options Options,
) error {
	activity.Report(ctx, "checking saves")
	// Saves are worked one game at a time; however this pass leaves, it
	// leaves no game marked as being worked on.
	defer ports.Report.Idle()
	candidates, empties := saveCandidates(state, scans, confirmed)
	if len(candidates) == 0 && len(empties) == 0 {
		return nil
	}
	remoteSaves, err := ports.Server.ListOmnisaves(ctx)
	if err != nil {
		outcome.Failed++
		ports.Report.BindingFailed(err)
		return nil
	}
	r := &reconciliation{
		Ports:    ports,
		Options:  options,
		state:    state,
		outcome:  outcome,
		lineages: newLineagePass(ports.Server, remoteSaves),
	}
	for _, c := range candidates {
		if _, tracked := state.Games[c.local.GameID]; !tracked {
			// An earlier candidate's server-side deletion untracked this
			// game mid-pass; its remaining saves have nothing to bind to.
			continue
		}
		if err := r.reconcileSave(ctx, c); err != nil {
			return err
		}
	}
	// Achievements are reported once every save this pass touched has settled,
	// so a save seeded or committed a moment ago is already a revision the
	// server can place an unlock on.
	for _, c := range candidates {
		if bound, isBound := state.BindingFor(c.local); isBound {
			reportAchievements(ctx, ports, state, c.local, c.save, c.discovered, c.game, bound.OmnisaveID)
		}
	}
	for _, empty := range empties {
		if _, tracked := state.Games[empty.discovered.Game.ID]; !tracked {
			continue
		}
		if err := r.syncToDevice(ctx, empty); err != nil {
			return err
		}
	}
	return nil
}

// reconciliation is one Reconcile call's shared context: the ports every
// decision works through, the state and outcome it records into, and the
// pass's single view of the server's lineages.
type reconciliation struct {
	Ports
	Options
	state    *tracking.State
	outcome  *Outcome
	lineages *lineagePass
}

// candidate is one local save with content, and what working it needs.
type candidate struct {
	local tracking.LocalSave
	save  target.Save
	// discovered and game name where the save came from, which is what an
	// adapter needs to answer anything about it beyond its files.
	discovered   target.Target
	game         target.InstalledGame
	serverGameID string

	// Set when the save is worked. readManifest reads and hashes the save
	// at most once; loadHistory reads a lineage's history, migrating or
	// holding a retired path format first; finish settles the store after a
	// placement.
	readManifest func() ([]omnisave.RevisionFile, error)
	loadHistory  func(omnisaveID string) ([]omnisave.Revision, error)
	finish       placementFinisher
}

// emptyCandidate is a tracked game with no local save content, which a server
// save may be placed onto.
type emptyCandidate struct {
	scan         client.TargetScan
	discovered   client.GameScan
	serverGameID string
}

// saveCandidates splits the confirmed games' scan results into saves to
// reconcile and games that have none.
func saveCandidates(state *tracking.State, scans []client.TargetScan, confirmed map[string]bool) ([]candidate, []emptyCandidate) {
	var candidates []candidate
	var empties []emptyCandidate
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			game := state.Games[discovered.Game.ID]
			if !confirmed[discovered.Game.ID] || game.ServerGameID == "" {
				continue
			}
			hasSave := false
			for _, save := range discovered.Saves {
				if len(save.Files) == 0 {
					continue
				}
				hasSave = true
				candidates = append(candidates, candidate{
					local:        LocalSaveFrom(scan, discovered, save),
					save:         save,
					discovered:   scan.Target,
					game:         discovered.Game,
					serverGameID: game.ServerGameID,
				})
			}
			if !hasSave {
				empties = append(empties, emptyCandidate{
					scan: scan, discovered: discovered, serverGameID: game.ServerGameID,
				})
			}
		}
	}
	return candidates, empties
}

// failed counts and reports one save's failure; the pass moves on.
func (r *reconciliation) failed(title string, err error) {
	r.outcome.Failed++
	r.Report.SaveFailed(title, err)
}

// working hands a game to the live view and says what is about to happen to
// it. It is called where work begins, never where a pass merely considers a
// game: a row that spins for something the pass decided not to do says a
// sync is happening when none is, and at the speed those decisions are made
// it says it as a flicker.
func working(ctx context.Context, report Reporter, title string) {
	// Marked before the phase is reported, so the phase lands on the row
	// rather than in the header the pass had just been speaking from.
	report.Working(title)
	activity.Report(ctx, "checking "+title)
}

// bindSynced binds a Local Save and records the revision it is known to
// equal as the binding's sync baseline.
func (r *reconciliation) bindSynced(local tracking.LocalSave, omnisaveID, revisionID string) error {
	if err := r.state.Bind(local, omnisaveID); err != nil {
		return err
	}
	return r.state.RecordSynced(local, omnisaveID, revisionID)
}

// reconcileSave syncs a bound save, or binds an unbound one.
func (r *reconciliation) reconcileSave(ctx context.Context, c candidate) error {
	c.readManifest = memoManifest(ctx, c.save)
	c.loadHistory = r.historyLoader(ctx, c)
	c.finish = finishPlacement(r.Adapters, c.discovered, c.game, c.local.GameTitle, r.Report)
	if bound, isBound := r.state.BindingFor(c.local); isBound {
		if remoteSave, exists := r.lineages.save(bound.OmnisaveID); exists {
			return r.syncBound(ctx, c, bound, remoteSave)
		}
		if len(r.lineages.byGame[c.serverGameID]) == 0 {
			r.untrackDeleted(ctx, c)
			return nil
		}
		// Drop a dead mapping when another server lineage still survives.
		r.state.Unbind(c.local)
	}
	return r.bindUnbound(ctx, c)
}

// untrackDeleted handles a bound save whose Omnisave was deleted on the
// authoritative server with no other lineage left for the game. The deletion
// syncs back as untracking — reseeding here would resurrect the deleted
// content. Re-tracking starts fresh.
func (r *reconciliation) untrackDeleted(ctx context.Context, c candidate) {
	title := c.local.GameTitle
	working(ctx, r.Report, title)
	r.state.Untrack(c.local.GameID)
	if err := r.Server.UntrackGame(ctx, c.serverGameID, r.state.Device.ID); err != nil && !errors.Is(err, catalog.ErrNotFound) {
		r.failed(title, err)
	}
	r.outcome.Untracked++
	r.outcome.Tracked--
	r.Report.SaveDeleted(title)
	r.Report.Removed(title)
}

// bindUnbound decides which lineage an unbound save belongs to: a new one
// when the game has none, the one whose Current Revision it already equals,
// or — when content alone cannot decide — the one a person chooses.
func (r *reconciliation) bindUnbound(ctx context.Context, c candidate) error {
	title := c.local.GameTitle
	gameSaves := r.lineages.gameSaves(c.serverGameID)
	// Matching content against every lineage reads the save in full.
	working(ctx, r.Report, title)
	if len(gameSaves) == 0 {
		r.seed(ctx, c)
		return nil
	}
	matchable := make([]binding.Lineage, 0, len(gameSaves))
	held := 0
	for _, listed := range gameSaves {
		history, err := c.loadHistory(listed.ID)
		if errors.Is(err, errMigrationHeld) {
			held++
			continue
		}
		if err != nil {
			r.failed(title, err)
			return nil
		}
		// Re-read after loading: the loader may just have migrated it.
		remoteSave, _ := r.lineages.save(listed.ID)
		matchable = append(matchable, binding.Lineage{Omnisave: remoteSave, Revisions: history})
	}
	manifest, err := c.readManifest()
	if err != nil {
		r.failed(title, err)
		return nil
	}
	matches := binding.FindManifestMatches(manifest, c.save, matchable)
	// A failed adoption left this save's progress preserved in an Omnisave it
	// recorded, and the answer was to adopt another. Content equality must not
	// quietly settle that answer the other way, so the question stays open and
	// a repeated answer reuses the preservation (FDR-005, decision 4).
	if pendingID, pending := r.state.PendingPreservationFor(c.local); pending &&
		len(matches) == 1 && matches[0].Omnisave.ID == pendingID {
		return r.chooseLineage(ctx, c, matches, matchable, held)
	}
	if len(matches) == 1 && matches[0].MatchesCurrent() {
		matched := matches[0].Omnisave
		if err := r.bindSynced(c.local, matched.ID, *matched.CurrentRevisionID); err != nil {
			r.failed(title, err)
			return nil
		}
		r.outcome.Rebound++
		r.Report.SyncedWith(title, omnisaveDisplayName(matched), time.Now())
		return nil
	}
	if len(matches) == 1 {
		return r.resolveStale(ctx, c, matches[0])
	}
	return r.chooseLineage(ctx, c, matches, matchable, held)
}

// resolveStale handles an unbound save whose content matches exactly one
// lineage at a revision that is not current. A headless pass leaves it
// waiting; a person chooses between jumping to the Current Revision and
// forking at the matched revision. The fork answer names the save it would
// create, and creates exactly that one.
func (r *reconciliation) resolveStale(ctx context.Context, c candidate, match binding.ContentMatch) error {
	title, name := c.local.GameTitle, omnisaveDisplayName(match.Omnisave)
	current, currentFound := revisionByID(r.lineages.histories[match.Omnisave.ID], match.Omnisave.CurrentRevisionID)
	if !currentFound {
		r.failed(title, errors.New("matching Omnisave has no readable current revision"))
		return nil
	}
	matchedRevision := match.Revisions[len(match.Revisions)-1]
	if r.Prompts.Stale == nil {
		// Headless: the stale question waits for an interactive run.
		r.outcome.Unbound++
		r.Report.Stale(title, name)
		return nil
	}
	forkName := deconflictName(match.Omnisave, deviceDisplayName(r.state))
	choice, err := r.Prompts.Stale(StaleQuestion{GameTitle: title, OmnisaveName: name, ForkName: forkName})
	if err != nil {
		return err
	}
	switch choice {
	case StaleJump:
		if err := binding.ApplyCurrent(ctx, r.Server, c.save, matchedRevision, current); err != nil {
			r.failed(title, err)
			return nil
		}
		c.finish(ctx, appliedSave(c.save, current), removedPaths(c.save, current))
		if err := r.bindSynced(c.local, match.Omnisave.ID, current.ID); err != nil {
			r.failed(title, err)
			return nil
		}
		r.outcome.Jumped++
		r.Report.SyncedWith(title, name, time.Now())
		return nil
	case StaleFork:
		fork, err := r.Server.ForkOmnisave(ctx, match.Omnisave.ID, omnisave.ForkOmnisave{
			RevisionID:  matchedRevision.ID,
			DisplayName: forkName,
		})
		if err != nil {
			r.failed(title, err)
			return nil
		}
		if err := r.bindSynced(c.local, fork.Omnisave.ID, fork.Revision.ID); err != nil {
			r.failed(title, err)
			return nil
		}
		r.outcome.Forked++
		r.Report.SyncedWith(title, omnisaveDisplayName(fork.Omnisave), time.Now())
		return nil
	default:
		return fmt.Errorf("unknown stale binding choice %q", choice)
	}
}

// chooseLineage settles a save matching zero or several lineages, which
// needs an explicit choice. held counts the game's lineages held for
// migration, which were never matchable.
func (r *reconciliation) chooseLineage(
	ctx context.Context,
	c candidate,
	matches []binding.ContentMatch,
	matchable []binding.Lineage,
	held int,
) error {
	title := c.local.GameTitle
	matchedRevisions := make(map[string]string, len(matches))
	for _, match := range matches {
		matchedRevisions[match.Omnisave.ID] = match.Revisions[len(match.Revisions)-1].ID
	}
	// Offered from the matchable lineages, never from every listed one: a
	// held lineage has no adoptable future until it migrates, and binding to
	// one would only earn refusals on the next commit. Held lineages never
	// entered matchable, so the rule holds by construction rather than by
	// whether their retired paths happen to fail the layout check below.
	offered := make([]AmbiguousOption, 0, len(matchable))
	for _, lineage := range matchable {
		remoteSave := lineage.Omnisave
		if matchedRevisions[remoteSave.ID] == "" {
			// Adopting an unmatched lineage ends by applying its Current
			// Revision to this save's files, so a lineage whose current
			// cannot land in this save's layout has no adoptable future here
			// and is not offered.
			current, exists := revisionByID(lineage.Revisions, remoteSave.CurrentRevisionID)
			if !exists || binding.CanApply(c.save, current) != nil {
				continue
			}
		}
		offered = append(offered, AmbiguousOption{
			OmnisaveID:        remoteSave.ID,
			Name:              omnisaveDisplayName(remoteSave),
			MatchedRevisionID: matchedRevisions[remoteSave.ID],
		})
	}
	if len(offered) == 0 {
		if held == 0 {
			// Nothing matched and nothing is adoptable from this save's
			// layout, so creating a new Omnisave is the one safe outcome left
			// and the pass takes it without a question (FDR-003, decision 1).
			r.seed(ctx, c)
			return nil
		}
		// A held lineage may be this very save's history waiting on its
		// migration. A new lineage could never rejoin it, so splitting the
		// game's history is the user's explicit call — an unanswered
		// question waits, exactly like every other question here.
		create := false
		if r.Prompts.HeldSeed != nil {
			var err error
			create, err = r.Prompts.HeldSeed(title)
			if err != nil {
				return err
			}
		}
		if !create {
			r.outcome.Unbound++
			r.Report.Unbound(title)
			return nil
		}
		r.seed(ctx, c)
		return nil
	}
	if r.Prompts.Ambiguous == nil {
		r.outcome.Unbound++
		r.Report.Unbound(title)
		return nil
	}
	choice, err := r.Prompts.Ambiguous(title, offered)
	if err != nil {
		return err
	}
	switch {
	case choice.Create:
		r.seed(ctx, c)
	case choice.OmnisaveID != "" && matchedRevisions[choice.OmnisaveID] != "":
		if err := r.bindSynced(c.local, choice.OmnisaveID, matchedRevisions[choice.OmnisaveID]); err != nil {
			r.failed(title, err)
			return nil
		}
		r.outcome.Bound++
		chosen, _ := r.lineages.save(choice.OmnisaveID)
		r.Report.SyncedWith(title, omnisaveDisplayName(chosen), time.Now())
	case choice.OmnisaveID != "":
		selected, exists := r.lineages.save(choice.OmnisaveID)
		if !exists || selected.GameID != c.serverGameID {
			r.failed(title, errors.New("chosen save is no longer available"))
			return nil
		}
		current, currentFound := revisionByID(r.lineages.histories[selected.ID], selected.CurrentRevisionID)
		if !currentFound {
			r.failed(title, errors.New("chosen save has no readable current revision"))
			return nil
		}
		r.syncUnmatched(ctx, c, selected, current)
	default:
		return errors.New("ambiguous binding prompt returned no choice")
	}
	return nil
}

// recordedAdoption is the preservation a failed adoption of this save
// recorded, while it still holds the save's content: a repeated answer
// continues it instead of preserving again. Only the recorded identity is
// trusted, never content equality alone (FDR-005, decision 4).
func (r *reconciliation) recordedAdoption(c candidate) (*omnisave.Omnisave, *omnisave.Revision) {
	pendingID, pending := r.state.PendingPreservationFor(c.local)
	if !pending {
		return nil, nil
	}
	recorded, listed := r.lineages.save(pendingID)
	if !listed {
		return nil, nil
	}
	history, err := c.loadHistory(pendingID)
	if err != nil {
		return nil, nil
	}
	current, exists := revisionByID(history, recorded.CurrentRevisionID)
	manifest, err := c.readManifest()
	if !exists || err != nil || !binding.MatchesManifest(manifest, c.save.LocationAliases, current) {
		return nil, nil
	}
	return &recorded, &current
}

// seed creates a new Omnisave from one local save and records the seed
// revision as the binding's sync baseline.
func (r *reconciliation) seed(ctx context.Context, c candidate) {
	title := c.local.GameTitle
	created, revision, err := binding.Seed(ctx, r.Server, c.serverGameID, c.save, "")
	if err != nil {
		r.failed(title, err)
		return
	}
	if err := r.bindSynced(c.local, created.ID, revision.ID); err != nil {
		r.failed(title, err)
		return
	}
	r.outcome.Seeded++
	r.Report.SyncedWith(title, omnisaveDisplayName(*created), time.Now())
}

// syncUnmatched preserves unmatched local progress as a new save before
// adopting the chosen save. The two saves have no common revision, so the
// preservation is a seed rather than a branch or fork.
func (r *reconciliation) syncUnmatched(ctx context.Context, c candidate, selected omnisave.Omnisave, current omnisave.Revision) {
	title := c.local.GameTitle
	// Adoption ends by applying the chosen save's Current Revision to this
	// save's files, so prove the layout can take it before local progress is
	// preserved toward it.
	if err := binding.CanApply(c.save, current); err != nil {
		r.failed(title, err)
		return
	}
	preserved, preservedRevision := r.recordedAdoption(c)
	if preserved == nil {
		created, revision, err := binding.Seed(ctx, r.Server, selected.GameID, c.save,
			deconflictName(selected, deviceDisplayName(r.state)))
		if err != nil {
			r.failed(title, err)
			return
		}
		preserved, preservedRevision = created, revision
		r.outcome.Seeded++
		r.Report.PreservedAs(title, omnisaveDisplayName(*preserved))
	}

	// Past here the preservation exists; a failure records it so a later
	// pass recognizes it as this save's own rather than starting over.
	if err := binding.ApplyCurrent(ctx, r.Server, c.save, *preservedRevision, current); err != nil {
		r.state.RecordPendingPreservation(c.local, preserved.ID)
		r.failed(title, err)
		return
	}
	c.finish(ctx, appliedSave(c.save, current), removedPaths(c.save, current))
	if err := r.bindSynced(c.local, selected.ID, current.ID); err != nil {
		r.state.RecordPendingPreservation(c.local, preserved.ID)
		r.failed(title, err)
		return
	}
	r.outcome.Pulled++
	r.Report.SyncedWith(title, omnisaveDisplayName(selected), time.Now())
}
