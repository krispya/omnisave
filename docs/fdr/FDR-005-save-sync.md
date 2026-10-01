# FDR-005: Save Sync

**Status:** Experimental **Last reviewed:** 2026-09-30

## Overview

Save Sync keeps a bound Local Save and its omnisave aligned. Each pass compares local content, the binding's sync baseline, and the Current Revision to push, pull, do nothing, or wait on a person, once on demand or continuously while a Device watches its saves. This record also covers what people can do with the history sync builds: restore, fork, delete, and download revisions.

## Behavior

- When local content, baseline, and Current Revision all agree, nothing changes.
- When only local content moved, it commits as a new revision and becomes current. When only the Current Revision moved, it is applied locally and becomes the new baseline.
- When both moved, a Current Revision descending from the baseline holds progress this Device has not seen, which is divergence. One at an ancestor or sibling of the baseline holds none, so local progress commits as a branch of the baseline (decision 10).
- A binding without a baseline is diverged unless its content equals the Current Revision.
- Divergence never resolves automatically. A person either keeps local progress as a new omnisave or rejoins at the Current Revision after local progress is preserved in the history. Neither loses progress or duplicates content the history already holds.
- Commits require changed, non-empty content and are spaced so repeated writes do not flood history. Continuous sync coalesces a burst of writes into one stable snapshot.
- Pulls are verified before they land. The local save is checked again before placement, and a concurrent change aborts the pull without modifying it.
- Transfer failures leave both sides valid: an interrupted upload does not move the Current Revision, and an interrupted download does not alter the local save.
- An automatic pull waits while the game is known to be running and lands once it closes.
- Placing files for a game that syncs through its store's API also registers them with the store; a registration that cannot finish is reported, not fatal (decision 13).
- An omnisave in a retired path format accepts no commits, restores, forks, or placements until a Device proves its migration (decision 14).
- Continuous sync reacts to local changes and server-side movement, retries transient failures, and reconciles periodically as a fallback.
- Restoring makes any revision current without creating or deleting revisions; clean Devices adopt it on their next sync.
- The Dash downloads the Current Revision, any single revision, or the complete history visible through the omnisave.

## Design Decisions

### 1. The baseline arbitrates direction

**Decision:** Synchronization uses a three-way comparison between local content, the binding baseline, and the Current Revision. It never chooses by timestamp or a generic newest-wins rule. **Why:** The baseline is proof of the last content both sides shared. Comparing from that fact makes safe pushes and pulls distinguishable from divergence even when clocks disagree. **Tradeoff:** A missing or incorrect baseline cannot be repaired by guessing and may require an interactive decision.

### 2. Continuous sync follows changes, not game sessions

**Decision:** Save writes trigger reconciliation after a quiet window rather than waiting for a game session to end. **Why:** Games write in bursts and may crash, or the Device may sleep, before a clean session end. A quiet window captures useful checkpoints without every adapter understanding game processes. **Tradeoff:** Long sessions may produce several revisions, and the quiet window is a heuristic for when a burst is complete.

### 3. Pulls are automatic only when losslessness is proven

**Decision:** The Current Revision applies automatically only while local content still equals the baseline. Placement rechecks that condition. **Why:** The binding is standing consent to follow one omnisave, and baseline content is already recoverable from the server. Any other local content may be progress and must not be overwritten silently. **Tradeoff:** Concurrent local writes abort a safe-looking pull and defer it to a later pass.

### 4. Divergence offers two lossless outcomes

**Decision:** A divergence either continues local progress as a new omnisave or rejoins at the Current Revision, first preserving local progress as a branch of the baseline, or as its own omnisave when there is no baseline. Rejoining is refused before anything is preserved when the Current Revision cannot land in the save's layout. An answer that fails after preserving, here or when adopting during binding ([FDR-003](FDR-003-automatic-save-binding.md), decision 9), records the preservation: later passes never rebind by content but ask again, unattended ones wait, and the repeated answer continues that preservation instead of creating another. **Why:** Both sides hold real progress; the choice is whether they stay independently synchronized, not which to discard. A failed answer must cost nothing and a repeated one must converge. Only the record proves ownership, since another omnisave can briefly hold the same bytes. **Tradeoff:** Preserved branches and forks remain until pruned, and independently progressing Devices may diverge again. A save whose omnisave lives in another layout can only fork.

### 5. Unattended passes never ask

**Decision:** A sync pass completes binding work that needs no decision ([FDR-003](FDR-003-automatic-save-binding.md)) and reports the rest, such as divergence, without asking. A person may open a resolution from an interactive watch; a fresh pass checks the answer before it applies. **Why:** The same watcher must run unattended under a service manager ([ADR-017](../adr/ADR-017-client-user-service.md)), while a present user should resolve a blocked save without stopping it. **Tradeoff:** A headless Device can report unresolved saves indefinitely, work waits while a resolution is open, and a stale answer is discarded rather than forced.

### 6. The Current Revision is a movable pointer

**Decision:** Every omnisave has one Current Revision that may move to any revision without rewriting history, and bound Devices sync toward it. **Why:** Users can restore any earlier or alternate progress while immutable snapshots remain recoverable. **Tradeoff:** A restore affects every bound Device, and a Device with unsynced progress may move current again when it commits (decision 10).

### 7. Branches are history; forks are independent omnisaves

**Decision:** Alternate paths can live inside one omnisave, while a fork creates another named, independently current omnisave sharing its ancestry. **Why:** Independent playthroughs need separate names and bindings; historical alternatives do not. Shared ancestry avoids copied snapshots and records the true relationship. **Tradeoff:** Retention crosses fork boundaries, and a dead branch is removed one childless revision at a time.

### 8. A pull waits for a running game

**Decision:** Automatic pulls are deferred while the game is known to be playing. **Why:** A running game may hold save state in memory and overwrite restored files on its next write, making a successful restore appear to undo itself. **Tradeoff:** Restored content may not reach the Device until the game closes, and games without activity detection rely on the verification guard.

### 9. Revision deletion only prunes childless revisions

**Decision:** A revision is deletable only when it has no children and is neither current nor a fork's origin. **Why:** Deletion should remove unused history, never rewrite the graph or move the state Devices follow. Durable deletion follows [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md). **Tradeoff:** Removing a branch takes one deletion per revision, newest first, and a Device whose baseline was deleted is left without one.

### 10. A Current Revision moved behind local progress takes a branch

**Decision:** When both sides moved and the Current Revision does not descend from the baseline, because a restore moved it to an ancestor or sibling, the pass commits local content as a child of the baseline and current follows, with no prompt and no new omnisave. The commit names its parent separately from the Current Revision it expects, which stays the concurrency check ([ADR-001](../adr/ADR-001-server-authority.md)). **Why:** The server holds no progress this Device lacks, so calling it divergence would stall sync and leave a fork to clean up. Attaching to the baseline records what actually happened; manifests name their parent immutably ([ADR-012](../adr/ADR-012-portable-save-store.md)). **Tradeoff:** A restore is undone by the next commit from a Device that never adopted it, so restoring a save someone is playing may not stick. Devices playing at once take turns holding current. Sync creates branches nobody made deliberately, and deletion (decision 9) is the only cleanup.

### 11. Save time and sync time are distinct

**Decision:** A revision may record when the game wrote its content separately from when the server accepted it. **Why:** An old save synchronized today should not appear to have been played today, and only the Device reading the native files has that evidence. **Tradeoff:** File times depend on Device clocks and copy behavior, so they inform rather than order.

### 12. Complete-history downloads contain full snapshots

**Decision:** A complete-history download materializes every revision, every branch included, rather than deltas or only the path to the Current Revision. **Why:** The archive is useful without Omnisave-specific reconstruction tooling, and alternate branches are part of the recoverable history. **Tradeoff:** Unchanged files repeat, so the ZIP can be much larger than the server's content-addressed store.

### 13. Placement finishes in the store's registry, on the store's evidence

**Decision:** After files land for a game that syncs through its store's API, the client registers them through that API, speaking as the game, and only under names the registry's own entries prove: an existing entry, or a registered directory and extension. Entries the placement has no file for are reported, never deleted. **Why:** Such a game trusts the registry, not its folder, and at its next launch deletes a restored file the registry does not list. Only the API adds to the registry, and it also carries the restore to other Devices. Guessed names would register files the game never used. **Tradeoff:** Registration needs the store running and signed in, and briefly shows the account playing the game. A game whose registry offers no evidence is placed but visibly unregistered, and files the game keeps out of its cloud stay out.

### 14. Retired path formats are held until a Device proves the migration

**Decision:** Each omnisave records its history's path-format version ([ADR-019](../adr/ADR-019-versioned-replayable-data-migrations.md)). One in the retired mirror vocabulary is held until a Device proves the game's native location and the server atomically renames the whole history, leaving revisions, ancestry, bindings, and achievements untouched. A bound save's layout is evidence enough; an unbound save must also match content. Server refusals, such as for fork families or mixed vocabularies, reach the held report with their reason. A server reporting no version holds the omnisave outright. **Why:** A history mixing vocabularies cannot be read the same way by every Device, and only a Device holding the native save knows the destination. **Tradeoff:** An omnisave no Device can prove waits visibly, retried only when the save or history changes, and fork families wait to migrate whole. Rules anchored at different depths per OS migrate into whichever OS proves first ([FDR-003](FDR-003-automatic-save-binding.md), decision 11). Files the game had deregistered stay in history and may resurrect an abandoned run, the safe side against silently pruning live ones.

## Open Questions

- Whether registry entries the applied revision lacks must be removed for a restore to take fully. Today they are reported and left.
- Whether several Devices placing the same restore need a server-granted placement claim, so the store receives one Device's content once.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — server arbitration and the Current Revision check; [ADR-002](../adr/ADR-002-sse-view-invalidation.md) — server-side movement notifications; [ADR-012](../adr/ADR-012-portable-save-store.md) — recoverable revision storage; [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md) — safe deletion; [ADR-017](../adr/ADR-017-client-user-service.md) — unattended continuous sync; [ADR-019](../adr/ADR-019-versioned-replayable-data-migrations.md) — versioned, replayable path-format migrations; [ADR-020](../adr/ADR-020-domains-own-their-contracts.md) — Save Sync as a client domain behind its own ports.
- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — establishing the baseline, and placement onto a Device with no Local Save; [FDR-002](FDR-002-game-lifecycle.md) — Device identity; [FDR-007](FDR-007-revision-labeling.md) — revision names; [FDR-008](FDR-008-achievement-marks.md) — achievement marks on the history sync builds.
