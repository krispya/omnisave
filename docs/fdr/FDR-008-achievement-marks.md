# FDR-008: Achievement Marks

**Status:** Experimental **Last reviewed:** 2026-10-03

## Overview

Achievement marks place observed unlocks on the save history surrounding them, helping someone identify which revision first belongs to the achieved state. A mark is an orientation aid for restoring progress, not a complete account-wide trophy history.

## Behavior

- Account-wide store achievements are not assigned to save slot histories without evidence identifying which slot earned them. The initial save slot implementation therefore leaves those histories unmarked.
- A Device observing an unlock reports the store's unlock time. The server places it on the earliest revision committed at or after that time; if none exists yet, the next commit on that omnisave claims it.
- Only unlocks observed after Omnisave begins watching a binding are marked. Earlier account history is not backfilled.
- Repeated reports never move a mark, whatever their order or Device.
- Deleting a marked revision leaves the achievement waiting for the next commit rather than deleting the observation.
- Marks belong to one omnisave, so a fork starts with none, even on the ancestors it shares.
- Revision history shows which revisions carry marks and names their achievements.
- Unsupported or unreadable store data produces no marks and never blocks save synchronization.

## Design Decisions

### 1. Marks are placed by time

**Decision:** The server places each unlock on the first revision committed at or after the store's unlock time, leaving newer unlocks unplaced until a commit exists. **Why:** Achievement notification and save writing are independent events and may occur in either order. Time-based placement remains stable regardless of polling or report order. **Tradeoff:** Store timestamps and revision timestamps have limited precision, so events close together can only be ordered as precisely as their sources allow.

### 2. Marks belong to the omnisave, not to revision content

**Decision:** Achievement placement is mutable state of one omnisave's history rather than part of an immutable revision snapshot. **Why:** Achievements belong to a player's store account, not to save bytes, and must be placed again when their revision is deleted. **Tradeoff:** Recovering the full history view needs both immutable snapshots and mutable omnisave records ([ADR-012](../adr/ADR-012-portable-save-store.md)), and forks do not inherit marks.

### 3. Achievement detection belongs to the Device adapter

**Decision:** Each adapter may report the unlocks its local store records; the server owns placement. Steam is the first source, read from its local cache with no account connection or network request. **Why:** Store records live on the Device and differ by platform, while placement must stay consistent across Devices. **Tradeoff:** Omnisave inherits the availability and quality limits of local, sometimes undocumented store data.

### 4. Observation starts at a watermark, not with history

**Decision:** The first look at a binding records how far its store history reaches and reports nothing. Later passes report only newer unlocks, ties at the watermark included, and advance it only after the server accepts a report. **Why:** Placing years of account history on the oldest revision Omnisave holds would claim more than that snapshot supports. A watermark survives ties and failed reports without storing the account's history. **Tradeoff:** A library adopted mid-playthrough has no marks for earlier achievements, rebinding starts observation again, and unlocks that surface locally with earlier times are never marked.

## Related

- **ADRs:** [ADR-021](../adr/ADR-021-independent-save-boundaries.md) — save slot ownership excludes unsupported account-wide claims; [ADR-012](../adr/ADR-012-portable-save-store.md) — the portable omnisave records that carry marks; [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md) — deleting a marked revision.
- **FDRs:** [FDR-005](FDR-005-save-sync.md) — synchronization commits the history marks attach to; [FDR-007](FDR-007-revision-labeling.md) — labels derive from content while achievement marks deliberately do not.

## Open Questions

- Whether store-specific artwork is worth third-party fetching or durable storage.
- Whether descendants, including forks, should also display marks from an earlier revision.
- Which other stores can provide trustworthy local unlock times.
