package savesync

import (
	"context"
	"errors"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/binding"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// syncBound compares a bound save's local content, its baseline, and the
// Omnisave's Current Revision (FDR-005, decision 1): local moved alone
// pushes, current moved alone pulls, and both moving is a branch when current
// did not descend from the baseline and a divergence when it did.
func (r *reconciliation) syncBound(ctx context.Context, c candidate, bound tracking.Binding, remoteSave omnisave.Omnisave) error {
	title, name := c.local.DisplayTitle(), omnisaveDisplayName(remoteSave)
	// Summarized before the save is read, so a write landing mid-pass leaves
	// a summary that no longer describes the content this pass verified;
	// the next pass then reads the save rather than trusting the summary.
	signature := saveSignature(c.save)
	if remoteSave.PathFormatVersion == omnisave.PathFormatNative &&
		settledSince(bound, remoteSave, signature) {
		// Neither side has moved since a pass proved this save equal to the
		// revision it is synced to, so there is nothing to commit and nothing
		// to apply. Reading the save and its history would spend the whole
		// save, and a round trip, to prove what is already known. Only a
		// native lineage can be settled: any other version state must reach
		// the loader below and be migrated or held.
		r.Report.SyncedWith(title, name, lastSyncedAt(bound))
		return nil
	}
	// Past here the save is read and its history fetched, so the row is the
	// pass's from now until it settles.
	working(ctx, r.Report, title)
	history, err := c.loadHistory(remoteSave.ID)
	if errors.Is(err, errMigrationHeld) {
		return nil
	}
	if err != nil {
		r.failed(title, err)
		return nil
	}
	manifest, err := c.readManifest()
	if err != nil {
		r.failed(title, err)
		return nil
	}
	current, currentOK := revisionByID(history, remoteSave.CurrentRevisionID)
	if !currentOK {
		r.failed(title, errors.New("bound Omnisave has no readable current revision"))
		return nil
	}
	d := divergence{remoteSave: remoteSave, current: current, history: history, manifest: manifest}
	baseline, baselineOK := revisionByID(history, bound.LastSyncedRevisionID)
	if !baselineOK {
		// A binding without a baseline (a manual bind to non-matching
		// content) is diverged from the start (FDR-005, decision 1).
		return r.resolveDivergence(ctx, c, d)
	}
	d.baseline = &baseline

	if current.ID == baseline.ID {
		if binding.MatchesManifest(manifest, c.save.LocationAliases, baseline) {
			// Proved equal the expensive way; remember how the files stood so
			// the next pass can reach the same answer by looking at them.
			r.state.RecordVerified(c.local, signature)
			r.Report.SyncedWith(title, name, lastSyncedAt(bound))
			return nil
		}
		if r.PushFloor > 0 && bound.LastSyncedAt != nil && time.Since(*bound.LastSyncedAt) < r.PushFloor {
			// Spacing floor: too soon after the last commit.
			r.Report.SyncedWith(title, name, lastSyncedAt(bound))
			return nil
		}
		revision, err := binding.Push(ctx, r.Server, remoteSave.ID, c.save, baseline.ID, current.Files)
		if err != nil {
			r.commitFailed(title, name, err)
			return nil
		}
		if err := r.state.RecordSynced(c.local, remoteSave.ID, revision.ID); err != nil {
			r.failed(title, err)
			return nil
		}
		r.outcome.Pushed++
		r.Report.SyncedWith(title, name, time.Now())
		return nil
	}

	// The Current Revision moved away from the baseline.
	if binding.MatchesManifest(manifest, c.save.LocationAliases, current) {
		// Local already carries the Current Revision's content; only the baseline lags.
		if err := r.state.RecordSynced(c.local, remoteSave.ID, current.ID); err != nil {
			r.failed(title, err)
			return nil
		}
		r.outcome.Rebound++
		r.Report.SyncedWith(title, name, time.Now())
		return nil
	}
	if binding.MatchesManifest(manifest, c.save.LocationAliases, baseline) {
		if r.Gate.holdPull(c.local.GameID) {
			// Defer pulls that a running game could overwrite from memory.
			r.outcome.Deferred++
			r.Report.PullDeferred(title, name)
			return nil
		}
		// Pull. Lossless: the replaced content is the baseline revision,
		// which the server keeps; placement re-verifies local is unchanged.
		if err := r.place(ctx, c, baseline, current, remoteSave.ID); err != nil {
			r.failed(title, err)
			return nil
		}
		r.outcome.Pulled++
		r.Report.SyncedWith(title, name, time.Now())
		return nil
	}
	// Diverge only when Current descends from the baseline; rewinds and sibling
	// branches can accept local progress as a new branch.
	descends, resolved := descendsFrom(history, current, baseline)
	if resolved && !descends {
		revision, err := binding.PushBranch(ctx, r.Server, remoteSave.ID, c.save, current.ID, baseline)
		if err != nil {
			r.commitFailed(title, name, err)
			return nil
		}
		if err := r.state.RecordSynced(c.local, remoteSave.ID, revision.ID); err != nil {
			r.failed(title, err)
			return nil
		}
		r.outcome.Branched++
		r.Report.Branched(title, name)
		r.Report.SyncedWith(title, name, time.Now())
		return nil
	}
	return r.resolveDivergence(ctx, c, d)
}

// commitFailed reports a refused commit. A Current Revision that moved during
// the commit defers reconciliation to the next pass, which reads the moved
// pointer; retrying now would guess. Anything else fails the save.
func (r *reconciliation) commitFailed(title, omnisaveName string, err error) {
	if errors.Is(err, omnisave.ErrConflict) {
		r.outcome.Conflicted++
		r.Report.CurrentMoved(title, omnisaveName)
		return
	}
	r.failed(title, err)
}

// divergence is a bound save whose content, baseline, and Current Revision
// disagree in a way only a person can settle — or that a closer look at the
// history shows needs no settling at all.
type divergence struct {
	remoteSave omnisave.Omnisave
	current    omnisave.Revision
	// baseline is nil for a binding that never recorded one.
	baseline *omnisave.Revision
	history  []omnisave.Revision
	manifest []omnisave.RevisionFile

	// Worked out once the divergence is answered. matched is the newest
	// revision holding the local content, when contentKnown. earlier and
	// resumable are preservations a failed earlier answer recorded (see
	// recordedPreservation). deviceName is the Device's name as the server
	// accepts it.
	matched      omnisave.Revision
	contentKnown bool
	earlier      *preservedProgress
	resumable    *preservedProgress
	deviceName   string
}

// resolveDivergence keeps both sides recoverable, asking only when a prompt
// can answer. Content the history already holds needs no preserving — the
// matched revision is the proof the server has it — so only genuinely
// unsynced progress is committed anywhere, and only "fork" creates a new
// Omnisave (FDR-005, decision 4).
func (r *reconciliation) resolveDivergence(ctx context.Context, c candidate, d divergence) error {
	title, name := c.local.DisplayTitle(), omnisaveDisplayName(d.remoteSave)
	d.matched, d.contentKnown = matchHistory(d.manifest, c.save.LocationAliases, d.history)
	if d.contentKnown && d.matched.ID == d.current.ID {
		// Only reachable without a baseline: the local content is the Current
		// Revision, so nothing has diverged — the binding just never recorded
		// where it stands.
		if err := r.state.RecordSynced(c.local, d.remoteSave.ID, d.current.ID); err != nil {
			r.failed(title, err)
			return nil
		}
		r.outcome.Rebound++
		r.Report.SyncedWith(title, name, time.Now())
		return nil
	}
	// The question names the save forking would create, so it is worked out
	// before anything is asked or reported. The Device's name is trimmed to
	// what the server will accept, since it also names a branch revision on
	// its own.
	d.deviceName = deviceDisplayName(r.state)
	question := DivergedQuestion{
		GameTitle:    title,
		OmnisaveName: name,
		ForkName:     deconflictName(d.remoteSave, d.deviceName),
	}
	waiting := func() error {
		r.outcome.Diverged++
		r.Report.Diverged(title, name, question.ForkName)
		return nil
	}
	if r.Prompts.Diverged == nil {
		return waiting()
	}
	choice, err := r.Prompts.Diverged(question)
	// A replayed answer belongs to one save. Every other divergence the pass
	// meets is one nobody has answered, so it waits exactly as it would have
	// under a headless pass.
	if errors.Is(err, ErrUnanswered) {
		return waiting()
	}
	if err != nil {
		return err
	}
	// A fork answer that failed partway recorded the Omnisave it created.
	// Resolving that record now lets a repeated answer continue it instead
	// of minting another. Using this Device's save needs the record only to
	// clean up after itself, so a failed lookup must not block that answer.
	if !d.contentKnown {
		d.earlier, d.resumable, err = r.recordedPreservation(c, d)
		if err != nil && choice != DivergedUseLocal {
			r.failed(title, err)
			return nil
		}
	}
	switch choice {
	case DivergedFork:
		r.forkDiverged(ctx, c, d)
	case DivergedUseLocal:
		r.useLocalDiverged(ctx, c, d)
	default:
		r.jumpDiverged(ctx, c, d)
	}
	return nil
}

// forkDiverged continues this Device's progress as a new lineage named
// after the Device. Content the history already holds forks at the matched
// revision and pushes nothing; unsynced content forks at the baseline and
// commits on the fork — or seeds a new Omnisave when no baseline exists to
// fork from. A preservation a failed earlier answer recorded is continued —
// bound when it already holds the content, pushed onto when its push never
// landed — instead of created again.
func (r *reconciliation) forkDiverged(ctx context.Context, c candidate, d divergence) {
	title := c.local.DisplayTitle()
	forkName := deconflictName(d.remoteSave, d.deviceName)
	var preserved *omnisave.Omnisave
	var preservedRevision *omnisave.Revision
	switch {
	case d.contentKnown:
		fork, err := r.Server.ForkOmnisave(ctx, d.remoteSave.ID, omnisave.ForkOmnisave{
			RevisionID:  d.matched.ID,
			DisplayName: forkName,
		})
		if err != nil {
			r.failed(title, err)
			return
		}
		preserved, preservedRevision = &fork.Omnisave, &fork.Revision
		r.outcome.Forked++
	case d.earlier != nil:
		// The preservation this answer would create already stands, and the
		// record proves it is this Device's own; the fork is a rebind to it.
		preserved, preservedRevision = &d.earlier.omnisave, &d.earlier.revision
		r.outcome.Rebound++
	case d.baseline == nil:
		created, revision, err := binding.Seed(ctx, r.Server, d.remoteSave.GameID, c.save, forkName)
		if err != nil {
			r.failed(title, err)
			return
		}
		preserved, preservedRevision = created, revision
		r.outcome.Forked++
	default:
		created, revision, err := r.forkLocalProgress(ctx, c, d, forkName)
		if err != nil {
			r.failed(title, err)
			return
		}
		preserved, preservedRevision = created, revision
		r.outcome.Forked++
	}
	if err := r.bindSynced(c.local, preserved.ID, preservedRevision.ID); err != nil {
		r.state.RecordPendingPreservation(c.local, preserved.ID)
		r.failed(title, err)
		return
	}
	r.Report.SyncedWith(title, omnisaveDisplayName(*preserved), time.Now())
}

// forkLocalProgress continues unsynced content as a fork of the baseline: the
// fork is created — or resumed, when a failed answer recorded one whose push
// never landed — and the local content committed onto it. A push that fails
// removes the empty fork so retries cannot stack them; when the same outage
// blocks the removal too, the fork is recorded and the next answer resumes it.
func (r *reconciliation) forkLocalProgress(ctx context.Context, c candidate, d divergence, name string) (*omnisave.Omnisave, *omnisave.Revision, error) {
	fork := d.resumable
	if fork == nil {
		created, err := r.Server.ForkOmnisave(ctx, d.remoteSave.ID, omnisave.ForkOmnisave{
			RevisionID:  d.baseline.ID,
			DisplayName: name,
		})
		if err != nil {
			return nil, nil, err
		}
		fork = &preservedProgress{omnisave: created.Omnisave, revision: created.Revision}
	}
	revision, err := binding.Push(ctx, r.Server, fork.omnisave.ID, c.save, fork.revision.ID, fork.revision.Files)
	if err != nil {
		// A fork holding none of the progress it was made to preserve reads
		// as that progress saved when it is not; remove it so a retry starts
		// clean instead of stacking another.
		if deleteErr := r.Server.DeleteOmnisave(context.WithoutCancel(ctx), fork.omnisave.ID); deleteErr != nil {
			r.state.RecordPendingPreservation(c.local, fork.omnisave.ID)
		} else {
			r.state.ClearPendingPreservation(c.local)
		}
		return nil, nil, err
	}
	return &fork.omnisave, revision, nil
}

// useLocalDiverged makes this Device's content the Current Revision. A fork
// a failed earlier answer recorded never received its push, so the answer
// cleans it up once the local content is current.
func (r *reconciliation) useLocalDiverged(ctx context.Context, c candidate, d divergence) {
	if !r.makeLocalCurrent(ctx, c, d.remoteSave, d.current) {
		return
	}
	if d.resumable != nil {
		_ = r.Server.DeleteOmnisave(context.WithoutCancel(ctx), d.resumable.omnisave.ID)
	}
}

// makeLocalCurrent commits this save's content on top of current and binds
// the save there, making it the Omnisave's Current Revision without touching
// local files; a running game is no obstacle. It stacks on current rather
// than any baseline, so the replaced revision stays in history as its parent
// and other Devices adopt it as an ordinary pull; one holding unsynced
// progress of its own diverges and is asked, instead of branching past it
// (FDR-005, decisions 4 and 10). It settles any preservation a failed
// earlier answer recorded, and reports false when it failed and said so.
func (r *reconciliation) makeLocalCurrent(ctx context.Context, c candidate, remoteSave omnisave.Omnisave, current omnisave.Revision) bool {
	title, name := c.local.DisplayTitle(), omnisaveDisplayName(remoteSave)
	// A save that cannot take this lineage's current cannot speak its
	// layout either, and its commit would mix two vocabularies in one tree.
	if err := binding.CanApply(c.save, current); err != nil {
		r.failed(title, err)
		return false
	}
	revision, err := binding.Push(ctx, r.Server, remoteSave.ID, c.save, current.ID, current.Files)
	if err != nil {
		r.commitFailed(title, name, err)
		return false
	}
	// A save already bound here keeps its binding, and with it the
	// achievement watermark; only a save new to this lineage binds afresh.
	if bound, ok := r.state.BindingFor(c.local); ok && bound.OmnisaveID == remoteSave.ID {
		err = r.state.RecordSynced(c.local, remoteSave.ID, revision.ID)
		r.state.ClearPendingPreservation(c.local)
	} else {
		err = r.bindSynced(c.local, remoteSave.ID, revision.ID)
	}
	if err != nil {
		r.failed(title, err)
		return false
	}
	r.outcome.Pushed++
	r.Report.SyncedWith(title, name, time.Now())
	return true
}

// jumpDiverged adopts the Current Revision after making sure the local
// progress survives. Content the history already holds needs nothing.
// Unsynced content is kept as a branch named for the Device and left behind
// the current pointer, so the answer never creates an Omnisave. The branch
// grows from the baseline, or from current when the binding never recorded
// one and no shared node exists (FDR-005, decision 4). A preservation a
// failed earlier answer recorded stands in for the branch, so a repeated
// answer creates nothing new.
func (r *reconciliation) jumpDiverged(ctx context.Context, c candidate, d divergence) {
	title, name := c.local.DisplayTitle(), omnisaveDisplayName(d.remoteSave)
	// The jump ends by applying the Current Revision over this save's files,
	// so prove the layout can take it before the answer preserves or commits
	// anything toward an adoption that cannot happen.
	if err := binding.CanApply(c.save, d.current); err != nil {
		r.failed(title, err)
		return
	}
	// The revision proved equal to the local content, so the staged placement
	// can verify against it (and stage unchanged files locally). A branch
	// lives in this Omnisave's history, so a failed placement needs no
	// record: the retry finds the local content there and preserves nothing
	// twice.
	verifyAgainst := d.matched
	switch {
	case d.contentKnown:
	case d.earlier != nil:
		// The recorded preservation already holds this exact content, so
		// nothing new is created and the placement verifies against it.
		verifyAgainst = d.earlier.revision
	default:
		parent := d.current
		if d.baseline != nil {
			parent = *d.baseline
		}
		branch, err := binding.PushBranchAside(ctx, r.Server, d.remoteSave.ID, c.save, d.current.ID, parent, d.deviceName)
		if err != nil {
			r.commitFailed(title, name, err)
			return
		}
		r.outcome.Branched++
		r.Report.BranchKept(title, name)
		verifyAgainst = *branch
	}
	if err := r.place(ctx, c, verifyAgainst, d.current, d.remoteSave.ID); err != nil {
		r.failed(title, err)
		return
	}
	if d.resumable != nil {
		// The recorded fork never received its push and this answer kept the
		// progress elsewhere, so the empty shell has nothing left to say;
		// removing it is best-effort cleanup, not part of the answer.
		_ = r.Server.DeleteOmnisave(context.WithoutCancel(ctx), d.resumable.omnisave.ID)
	}
	r.outcome.Pulled++
	r.Report.SyncedWith(title, name, time.Now())
}

// preservedProgress is a preservation a failed earlier answer recorded, with
// the revision it currently stands at.
type preservedProgress struct {
	omnisave omnisave.Omnisave
	revision omnisave.Revision
}

// recordedPreservation resolves the preservation a failed earlier answer
// recorded for this save, so a repeated answer continues it instead of
// minting another. Only the recorded identity is trusted — never content
// equality alone, which cannot tell this Device's own preservation from an
// independent lineage that happens to hold the same bytes for a moment.
// earlier is a preservation that holds the local content; resumable is a
// fork of the baseline whose push never landed, still waiting for it. A
// record that no longer describes either — the Omnisave is gone, or the
// local content has moved on since — is cleared and the answer starts fresh.
func (r *reconciliation) recordedPreservation(c candidate, d divergence) (earlier, resumable *preservedProgress, err error) {
	pendingID, pending := r.state.PendingPreservationFor(c.local)
	if !pending {
		return nil, nil, nil
	}
	gameSaves := r.lineages.gameSaves(d.remoteSave.GameID)
	var recorded *omnisave.Omnisave
	for index := range gameSaves {
		if gameSaves[index].ID == pendingID {
			recorded = &gameSaves[index]
			break
		}
	}
	if recorded == nil {
		r.state.ClearPendingPreservation(c.local)
		return nil, nil, nil
	}
	history, err := c.loadHistory(pendingID)
	if errors.Is(err, errMigrationHeld) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	current, exists := revisionByID(history, recorded.CurrentRevisionID)
	if !exists {
		r.state.ClearPendingPreservation(c.local)
		return nil, nil, nil
	}
	if binding.MatchesManifest(d.manifest, c.save.LocationAliases, current) {
		return &preservedProgress{omnisave: *recorded, revision: current}, nil, nil
	}
	if d.baseline != nil && current.ID == d.baseline.ID {
		return nil, &preservedProgress{omnisave: *recorded, revision: current}, nil
	}
	r.state.ClearPendingPreservation(c.local)
	return nil, nil, nil
}

// matchHistory finds the newest revision whose content equals the local
// manifest. Any hit means the server already holds this exact content, so
// nothing needs preserving before this Device moves on.
func matchHistory(manifest []omnisave.RevisionFile, aliases []string, history []omnisave.Revision) (omnisave.Revision, bool) {
	var matched omnisave.Revision
	found := false
	for _, revision := range history {
		if !binding.MatchesManifest(manifest, aliases, revision) {
			continue
		}
		if !found || revision.CreatedAt.After(matched.CreatedAt) {
			matched = revision
			found = true
		}
	}
	return matched, found
}

// descendsFrom reports strict ancestry and whether the available history proves it.
func descendsFrom(history []omnisave.Revision, node, ancestor omnisave.Revision) (descends, resolved bool) {
	byID := make(map[string]omnisave.Revision, len(history))
	for _, revision := range history {
		byID[revision.ID] = revision
	}
	// Bound the walk so a parent cycle resolves as unknown.
	for step := 0; step <= len(history); step++ {
		if node.ParentID == nil {
			return false, true
		}
		if *node.ParentID == ancestor.ID {
			return true, true
		}
		parent, ok := byID[*node.ParentID]
		if !ok {
			return false, false
		}
		node = parent
	}
	return false, false
}

func lastSyncedAt(bound tracking.Binding) time.Time {
	if bound.LastSyncedAt == nil {
		return time.Time{}
	}
	return *bound.LastSyncedAt
}
