# FDR-008: Achievement Marks

**Status:** Experimental
**Last reviewed:** 2026-08-18

## Overview

Achievement marks place observed unlocks on the save history surrounding them,
helping someone identify which revision first belongs to the achieved state. A
mark is an orientation aid for restoring progress, not a complete account-wide
trophy history.

## Behavior

- A Device observing an achievement reports its store-provided unlock time. The
  server places it on the earliest revision committed at or after that time.
- If no qualifying revision exists yet, the achievement waits for the next
  commit on that Omnisave.
- Only achievements observed after Omnisave begins watching are marked. Existing
  account history establishes a starting point and is not backfilled.
- Repeated reports do not move a mark. Reports may arrive in any order or from
  several Devices without changing the result.
- Deleting the marked revision leaves the achievement waiting to be placed on a
  later commit rather than deleting the achievement observation.
- Revision history indicates that one or more achievements were first observed
  around a marked snapshot and makes their names available on demand.
- Steam is the first supported source and is read locally without requiring an
  account connection or network request. Unsupported, unavailable, or unstable
  store data produces no marks and never blocks save synchronization.

## Design Decisions

### 1. Marks are placed by time

**Decision:** The server places each unlock on the first revision committed at
or after the store's unlock time, leaving newer unlocks unplaced until a commit
exists.
**Why:** Achievement notification and save writing are independent events and
may occur in either order. Time-based placement remains stable regardless of
polling or report order.
**Tradeoff:** Store timestamps and revision timestamps may have limited
precision, so events close together can only be ordered as precisely as their
sources allow.

### 2. Marks belong to lineage state, not revision content

**Decision:** Achievement placement is mutable state associated with an
Omnisave's history rather than part of an immutable revision snapshot.
**Why:** Achievements belong to a player's store account, not to save bytes, and
may need to become unplaced and placed again after revision deletion.
**Tradeoff:** Recovering complete revision presentation requires both immutable
snapshot history and mutable lineage metadata.

### 3. Achievement detection belongs to the Device adapter

**Decision:** Each adapter may report the achievements and unlock times its local
store records; the server owns placement after receiving them.
**Why:** Store records live on the Device and differ by platform, while placement
must remain consistent across all Devices.
**Tradeoff:** Omnisave inherits the availability and quality limits of local,
sometimes undocumented store data.

### 4. Existing achievement history is not backfilled

**Decision:** The first observation establishes a watermark and reports no prior
unlocks.
**Why:** Placing years of account history onto the oldest revision Omnisave
happens to hold would make claims that snapshot cannot support.
**Tradeoff:** A library adopted mid-playthrough has no marks for earlier
achievements, and rebinding begins observation again.

### 5. Observation state is bounded and retryable

**Decision:** A binding retains only enough progress to report new unlocks
without skipping ties, and advances that progress only after successful
reporting.
**Why:** Detection must tolerate several achievements sharing a timestamp and
failed reports without storing an account's complete achievement history.
**Tradeoff:** Unlocks that predate observation but appear locally later remain
outside the feature by design.

### 6. Marks stay compact while names remain available

**Decision:** Revision history represents the presence and count of marks
compactly, with achievement names available through further inspection.
**Why:** Marks should orient someone scanning history without competing with
revision names and save status.
**Tradeoff:** Individual achievement names are not always visible at a glance.

## Related

- **FDRs:** [FDR-005](FDR-005-save-sync.md) — synchronization observes and
  commits the history marks attach to; [FDR-007](FDR-007-revision-labeling.md) —
  labels derive from content while achievement marks deliberately do not.
- **ADRs:** [ADR-012](../adr/ADR-012-portable-save-store.md) — portable lineage
  metadata; [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md) — safe
  placement and deletion.

## Open Questions

- Whether store-specific artwork is worth third-party fetching or durable
  storage.
- Whether descendants should also display marks inherited from an earlier
  revision.
- Which other stores can provide trustworthy local unlock times.
