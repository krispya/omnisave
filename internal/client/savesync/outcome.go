package savesync

// Outcome tallies what one run did, game by game and save by save. A run's
// entry points add to the same Outcome, so its summary covers the whole run.
type Outcome struct {
	// Tracked is how many games this Device tracks after the run.
	Tracked int
	// Added and Linked count games newly created in, or matched to, the
	// Library. Pending counts tracked games this scan had no evidence for.
	Added   int
	Linked  int
	Pending int
	// Untracked counts games this Device stopped tracking, whether it chose
	// to or the server deleted them.
	Untracked int
	// Seeded counts new Omnisaves created from Local Saves.
	Seeded int
	// Rebound counts saves bound, or re-baselined, because their content
	// already matched a revision.
	Rebound int
	// Jumped counts stale saves that adopted the Current Revision. Forked
	// counts progress continued as, or preserved into, a new Omnisave.
	Jumped int
	Forked int
	// Bound counts saves bound at a revision the user chose among matches.
	Bound int
	// Unbound counts saves left waiting for a binding decision.
	Unbound int
	// Pushed and Pulled count commits and placements of a bound save.
	Pushed int
	Pulled int
	// Diverged counts saves with progress on both sides left waiting.
	Diverged int
	// Branched counts local progress committed as a new branch because a
	// restore moved current off this Device's baseline.
	Branched int
	// Deferred counts pulls held back because the game is being played;
	// the pass after the game closes applies them.
	Deferred int
	// Conflicted counts commits the server refused because the Current
	// Revision moved mid-pass; the next pass reconciles them.
	Conflicted int
	// Held counts lineages this pass could not work because their paths
	// still use a retired format and no migration proved out (FDR-005,
	// decision 14). A held lineage is neither a failure nor a no-op, so it
	// gets its own tally rather than disappearing from the summary.
	Held   int
	Failed int
	// Synced reports that the Device reached the server, so the run's save
	// work could begin at all.
	Synced bool
}

// Changed reports whether the run did anything worth showing.
func (o Outcome) Changed() bool {
	return o.Added+o.Linked+o.Untracked+o.Pending+o.Seeded+o.Rebound+o.Jumped+
		o.Forked+o.Bound+o.Unbound+o.Pushed+o.Pulled+o.Branched+o.Diverged+o.Deferred+
		o.Conflicted+o.Held+o.Failed > 0
}
