# FDR-004: Syncing Saves to a Device

**Status:** Experimental **Last reviewed:** 2026-08-18

## Overview

This feature places an existing Omnisave on a tracked Device that has no local save. The user chooses whether to adopt a server-side playthrough, its Current Revision is placed at the game's native save location, and the resulting binding starts in sync.

## Behavior

- A tracked game with no local save is offered its existing Omnisaves during binding. The choice is always explicit, even when only one Omnisave exists.
- Choosing an Omnisave places its Current Revision at the Device's native save location and records that revision as the binding baseline.
- Declining leaves the game tracked without local content, so the choice remains available on a later interactive run.
- The adapter must identify one unambiguous native destination before content is offered. If no safe destination is known, nothing is placed.
- Placement never overwrites unexpected local content. If content appears after discovery, the operation stops and a later pass reconsiders the save through normal binding.
- A failed or interrupted transfer leaves no partial local save.
- Games that already have local content are resolved by content matching rather than this flow ([FDR-003](FDR-003-automatic-save-binding.md)).

## Design Decisions

### 1. Syncing to a Device is explicit

**Decision:** The user chooses before an Omnisave is placed on a Device, even when there is only one candidate. **Why:** This operation writes into the location the game will load. That merits explicit consent, unlike seeding a protective server copy from existing local content. **Tradeoff:** A fresh Device requires one interaction before play can continue.

### 2. A Device adopts the Current Revision

**Decision:** Choosing an Omnisave places its Current Revision and starts the binding there. **Why:** Current represents the playthrough every bound Device follows. Choosing an older revision would silently create a different history. **Tradeoff:** Historical revisions are not selected through this workflow.

### 3. Adapters own prospective save locations

**Decision:** An adapter may identify where a game's save belongs before one exists there. **Why:** Placement needs Device-specific knowledge that canonical revision paths alone cannot provide. **Tradeoff:** A game whose location cannot be determined safely remains without a local save.

### 4. Placement is atomic and non-destructive

**Decision:** Content is verified before placement, applied as one operation, and never overwrites files that appeared unexpectedly. **Why:** A partial or corrupt local save is worse than having no local save. **Tradeoff:** Safe placement requires temporary disk space and may abort when the game or another process writes concurrently.

### 5. Existing local content always goes through matching

**Decision:** This workflow is available only while the Device has no local save. **Why:** With no local content, placement cannot discard progress. Once content exists, lineage must be established through the matching and divergence rules. **Tradeoff:** Even unwanted local content prevents this simpler workflow until it is resolved normally.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — content comes from the authoritative Omnisave; [ADR-012](../adr/ADR-012-portable-save-store.md) — the recoverable content being placed; [ADR-018](../adr/ADR-018-embedded-save-profiles.md) — the save-location knowledge used by adapters.
- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — the binding pass and content-matching alternative; [FDR-005](FDR-005-save-sync.md) — ongoing sync after the binding exists.
