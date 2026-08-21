# FDR-005: Save Sync

**Status:** Experimental **Last reviewed:** 2026-08-20

## Overview

Save Sync keeps a bound Local Save and its Omnisave aligned over time. Each pass compares local content, the binding's sync baseline, and the Omnisave's Current Revision to decide whether to push, pull, do nothing, or require a lineage decision. The same reconciliation runs once on demand or continuously while a Device watches its saves.

## Behavior

- When local content, baseline, and Current Revision all agree, the save is in sync and nothing changes.
- When only local content moved, it commits as a new revision and becomes current. When only Current Revision moved, it is applied locally and becomes the new baseline. A commit onto a lineage whose location is spelled by another OS's rule keeps the lineage's spelling, so shared history stays in one vocabulary ([FDR-003](FDR-003-automatic-save-binding.md), decision 11).
- When both moved, ancestry determines whether the histories truly diverged. A current revision descending from the baseline represents progress this Device has not seen and requires a decision. A current revision at an ancestor or sibling does not contain unseen progress, so local work continues from its actual baseline as a branch.
- A binding without a baseline is treated as unresolved unless its content can be proven equal to the Current Revision.
- Divergence never resolves automatically. Interactive users can either keep local progress as an independent Omnisave or rejoin the existing Omnisave at its Current Revision. Rejoining preserves unsynced local progress inside the existing lineage before applying current content. Neither outcome destroys progress.
- Before preserving divergent content, the server's full revision history is checked. Content already present is reused rather than duplicated.
- Noninteractive passes report unresolved work and continue with other saves; they never block waiting for an answer. An interactive watch can accept a user-initiated resolution without changing the reconciliation rules.
- Sync also completes automatic binding work that needs no decision: a newly created local save can seed its first Omnisave, and content matching one Current Revision can rebind.
- Pulls are verified and applied atomically. The local save is checked again before placement, and concurrent changes abort the pull without modifying it.
- An automatic pull waits while the game is known to be running, so in-memory game state cannot immediately overwrite restored content. Detection is best-effort; pre-placement verification remains the safety boundary.
- Where a game keeps its saves behind its store's cloud API, placing files also registers them with the store, because such a game trusts the store's file registry — not its folder — for whether live state exists (decision 13). A registration that cannot run or cannot finish is reported alongside the completed placement rather than failing it, since the restore may otherwise be discarded silently at the game's next launch.
- Commits require changed, non-empty content and are spaced so repeated writes do not flood history. Continuous sync coalesces a burst of save writes into a stable snapshot.
- Transfer failures leave both sides valid: an interrupted upload does not move Current Revision, and an interrupted download does not alter the local save.
- Continuous sync notices local save changes and server-side movement, retries transient failures, and periodically reconciles as a fallback.
- Revisions may have mutable display names without changing their identity, content, topology, or current status.
- Each Omnisave has one server-owned Current Revision. Restoring selects any revision in its history without creating or deleting revisions; clean Devices adopt that selection on their next sync.
- Revision history may branch within one Omnisave. A fork is different: it creates another named Omnisave with its own Current Revision while sharing ancestry at the fork point.
- The Dash can download the Current Revision, any individual historical revision, or the save's complete revision history. A complete-history archive includes every branch and shared ancestor visible through that Omnisave, with each revision materialized as a separate full snapshot.
- A revision may be deleted only when no child, Current Revision, or fork origin depends on it. Deletion never reparents history or moves Current Revision.
- Revisions record when save content was written when that evidence is available, separately from when it reached the server. Older revisions fall back to their server creation time.

## Design Decisions

### 1. The baseline arbitrates direction

**Decision:** Synchronization uses a three-way comparison between local content, the binding baseline, and Current Revision. It never chooses by timestamp or a generic newest-wins rule. **Why:** The baseline is proof of the last content both sides shared. Comparing from that fact makes safe pushes and pulls distinguishable from divergence even when clocks disagree. **Tradeoff:** A missing or incorrect baseline cannot be repaired by guessing and may require an interactive decision.

### 2. Continuous sync follows changes, not game sessions

**Decision:** Save writes trigger reconciliation after a quiet window rather than waiting for a game session to end. **Why:** Games write in bursts and may crash or the Device may sleep before a clean session end. A quiet window captures useful checkpoints without requiring every adapter to understand game processes. **Tradeoff:** Long sessions may produce several revisions, and the quiet window remains a heuristic for when a write burst is complete.

### 3. Pulls are automatic only when losslessness is proven

**Decision:** Current Revision applies automatically only while local content still equals the baseline. Placement rechecks that condition. **Why:** The binding is standing consent to follow one lineage, and baseline content is already recoverable from the server. Any other local content may be progress and must not be overwritten silently. **Tradeoff:** Concurrent local writes abort a safe-looking pull and defer it to a later pass.

### 4. Divergence offers two lossless lineage outcomes

**Decision:** A true divergence may continue local progress as another Omnisave or rejoin the existing Omnisave at current. Rejoining first preserves local progress as a branch of the baseline; a baseline-less save is preserved independently because it has no node to branch from. Rejoining is refused before anything is preserved when the Current Revision cannot be applied into the Local Save's layout. An answer that fails after preserving records the preservation it created, and the next answer continues that exact Omnisave — binding it, verifying against it, or completing its unfinished push — instead of minting another. **Why:** Both histories contain real progress. The choice is about whether they remain independently synchronized, not which content should be discarded. An answer that cannot finish must cost nothing, and repeating an answer that failed partway must converge on one preservation instead of stacking one per attempt. The record is the only proof of ownership honored: content equality cannot tell this Device's own preservation from an independent lineage that briefly holds the same bytes, and adopting such a twin would cross two playthroughs. **Tradeoff:** Preserved branches and forks remain until deliberately pruned, and two Devices that keep progressing independently may diverge again. A binding whose lineage lives in another layout can only fork; binding one representation per game — the folder the game itself reads ([FDR-003](FDR-003-automatic-save-binding.md), decision 10) — is what keeps such bindings from arising.

### 5. Sync completes only decision-free binding work

**Decision:** A sync pass may seed a first save or rebind an exact Current Revision match, but cases with several valid futures remain unresolved. **Why:** Continuous protection should begin without ritual when the evidence is conclusive, while lineage choices still require a person. **Tradeoff:** A headless Device can report unresolved saves indefinitely until someone uses an interactive surface.

### 6. Current Revision is a movable global pointer

**Decision:** Every Omnisave has one Current Revision that may be moved to any node without rewriting history. Bound Devices synchronize toward that pointer. **Why:** Users can rewind, fast-forward, and revisit alternate progress while immutable snapshots remain recoverable. **Tradeoff:** A restore affects every bound Device, and unsynced Devices may move current again when their progress commits.

### 7. Branches are topology; forks are independent saves

**Decision:** Alternate paths can live inside one Omnisave, while a fork creates another named and independently current Omnisave sharing its ancestry. **Why:** Independent playthroughs need separate names and bindings; historical alternatives do not. Shared ancestry avoids copied snapshots and preserves the true relationship between them. **Tradeoff:** Retention crosses fork boundaries, and dead branches must be removed from their tips inward.

### 8. A pull waits for a running game

**Decision:** Automatic pulls are deferred while the game is known to be playing. **Why:** A running game may hold save state in memory and overwrite restored files on its next write, making a successful restore appear to undo itself. **Tradeoff:** Restored content may not reach the Device until the game closes, and games without activity detection rely on the ordinary verification guard.

### 9. Revision deletion only prunes unneeded tips

**Decision:** A revision is deletable only when it has no children and is not current or the origin retained by a fork. **Why:** Deletion should remove unused history, never rewrite graph topology or move the state Devices follow. Durable deletion follows [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md). **Tradeoff:** Removing a branch takes one deletion per revision, starting at the tip, and a Device whose baseline was deleted may need to re-establish lineage.

### 10. Watch never blocks on unsolicited questions

**Decision:** Continuous sync reports unresolved decisions but asks nothing on its own. An interactive user may deliberately open a resolution, whose answer is validated by a fresh reconciliation pass before it is applied. **Why:** The same watcher must work unattended under a service manager. A user who is present should still be able to resolve a blocked save without stopping the watcher. **Tradeoff:** Work waits while an interactive resolution is open, and stale answers are discarded rather than forced onto changed state.

### 11. Save time and sync time are distinct

**Decision:** A revision may record when the game wrote the content separately from when the server accepted it. **Why:** An old save synchronized today should not appear to have been played today. Only the Device that reads the native files can provide that evidence. **Tradeoff:** File times depend on Device clocks and file-copy behavior, so they are informative rather than authoritative ordering.

### 12. Complete-history downloads contain full snapshots

**Decision:** A complete-history download materializes every revision independently rather than exporting deltas or only the path leading to Current Revision. **Why:** The archive is useful without Omnisave-specific reconstruction tooling, and alternate branches are part of the recoverable history. **Tradeoff:** Unchanged files may appear many times in the ZIP, so the download can be much larger than the content-addressed server representation.

### 13. Placement finishes in the store's registry, on the store's evidence

**Decision:** After files land in the save folder of a game whose cloud moves through its store's API, the store's file registry is reconciled to match them — through the store's own API, speaking as the game via the Steamworks library the game itself ships, in a helper process held open only as long as the writes take. Registration is evidence-bound: the placed folder's position in the store's namespace is proven by the registry's own names or nothing is written, and a new name registers only where the registry shows the game keeps files like it — an existing entry, or a registered directory and extension. Entries the placement carries no file for are never deleted, only reported. **Why:** Measured (2026-08-20, Slay the Spire 2): the game deletes restored live state its store's registry does not list, silently, whatever the folder or the mirror holds, and bytes placed anywhere but the API register nothing — so the API write is the only channel a restore can enter durably, and it also carries the restore to other Devices through the store itself. Guessing where evidence runs out would register files under names the game never used, which is the corruption this feature exists to prevent. **Tradeoff:** Reconciliation needs the store running and signed in, and briefly shows the account as playing the game. A game whose registry offers no evidence — never synced here, or empty — is placed but not registered, visibly. Files the game deliberately keeps out of its cloud stay out, even when a revision carries them.

## Open Questions

- A Device binds the folder the game itself reads ([FDR-003](FDR-003-automatic-save-binding.md), decision 10), but where a game keeps live state behind its store's cloud API, the store's file registry — not any folder — is what the game trusts for whether that state exists. Measured with Slay the Spire 2 (2026-08-20): a rewound run placed in the game's own folder was silently deleted at the next launch because the store's registry no longer listed the run file, even though nothing in the save's own content recorded the run as over; the same launch left registry-unlisted archive files untouched, so the registry governs live state rather than everything. Bytes placed in the store's mirror register nothing and sit unacknowledged indefinitely — decision 10's refusal, measured rather than assumed. What made the restore durable was re-registering the file through the store's own cloud API, the channel the game itself writes: the store then updated its transport and cloud on its own, and the next launch continued the restored run unchanged.
- Placement now finishes in the store's registry (decision 13), but never by deletion: whether registry entries the applied revision lacks must also be removed for a rewind to fully take is still unmeasured, so extras are reported and left. The settled direction for several bound Devices remains a server-granted placement claim, with the claim holder's content entering the store once rather than each Device arguing with the cloud on its own — registration through the store's own API narrows this, since the store propagates one Device's writes itself, but two Devices pulling the same restore still both write.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — server arbitration; [ADR-002](../adr/ADR-002-sse-view-invalidation.md) — server-side movement notifications; [ADR-012](../adr/ADR-012-portable-save-store.md) — recoverable revision storage and lineage; [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md) — safe deletion; [ADR-017](../adr/ADR-017-client-user-service.md) — unattended continuous sync.
- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — establishing the baseline; [FDR-004](FDR-004-sync-to-device.md) — first placement on a Device; [FDR-002](FDR-002-game-lifecycle.md) — Device identity and liveness.
