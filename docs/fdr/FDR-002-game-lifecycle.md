# FDR-002: Game Lifecycle

**Status:** Active **Last reviewed:** 2026-09-30

## Overview

How a game enters the Library, what the server remembers about where it has lived, and what it takes for a game to leave. The lifecycle runs detect → track → bind: a Device's adapters discover installed games, the user tracks the ones to protect, and binding connects their Local Saves to omnisaves.

## Behavior

- Scanning discovers installed games without changing anything. Detection alone never adds a game to the Library.
- Tracking a game adds it to the Library: the game is resolved into a Game ([FDR-001](FDR-001-game-identity-resolution.md)) before any save is bound.
- Binding attaches a Local Save to an omnisave of a game already in the Library. The server refuses to create an omnisave for a game outside the Library, so no save can outlive its game.
- Each client installation is a Device: a stable ID minted on first run and a human-readable name, registered with the server.
- A Device also reports which tracked games it is playing. That presence is short-lived, and only the server ages it out ([ADR-013](../adr/ADR-013-server-announced-presence-expiry.md)).
- The server keeps each Game's Provenance: which Devices tracked it, when each first tracked it and was last seen, whether it is still installed there, and whether it has since been untracked. Untracking and uninstalling annotate the record; neither removes it or the game.
- A Device reports an uninstall at its next sync by clearing its installed flag. Saves and provenance are unaffected.
- Open Library views refresh when games, saves, revisions, or provenance change ([ADR-002](../adr/ADR-002-sse-view-invalidation.md)).
- A game or save deleted on the server is untracked on each Device at its next sync (decision 7).
- Deleting all of a game's omnisaves leaves the game and its provenance in the Library.
- Deleting the game removes its saves, revision history, unshared artifacts, and provenance, and records immutable deletion markers so restoring an older portable copy cannot undo it. This is the lifecycle's only act of forgetting.

## Design Decisions

### 1. Tracking, not detection, is library entry

**Decision:** Games enter the Library only when a user tracks them. **Why:** Detection is cheap and indiscriminate — one RetroArch playlist can surface hundreds of games the user never intends to back up. Tracking is the deliberate opt-in that makes membership mean something. **Tradeoff:** Nothing is protected automatically; a game the user forgot to track has no saves on the server.

### 2. Library membership never depends on current installation

**Decision:** Installation is per-Device data, not a membership criterion. **Why:** The server is the durable side of the system ([ADR-001](../adr/ADR-001-server-authority.md)); uninstalling to free space is precisely when the server copy matters most. Requiring an install would orphan saves the moment they became valuable. **Tradeoff:** Libraries accumulate games installed nowhere; cleanup is a deliberate delete, never automatic.

### 3. Provenance is append-only and dies only with the game

**Decision:** Untracking and uninstalling annotate provenance; nothing removes it except deleting the game itself. **Why:** The history of where a game lived is the value that justifies keeping a game with no saves. An empty game record with provenance still answers "what happened here". **Tradeoff:** Records accumulate without pruning; acceptable at self-hosted scale.

### 4. Devices self-identify

**Decision:** A Device mints its own ID on first run and reports its own name. Identity belongs to the client installation, not the hardware. **Why:** There are no accounts to assign identities from. Pairing issues each Device its own credential, bound to the identity it already has ([ADR-007](../adr/ADR-007-per-device-credentials.md)), so identity has to exist before access does. **Tradeoff:** Identity is self-asserted and resets if local client state is wiped.

### 5. Explicit liveness, not per-request heartbeat

**Decision:** A Device's last-seen time refreshes when it registers at the start of each sync. Its provenance records refresh when what it tracks changes, and otherwise about hourly. **Why:** No middleware, no write amplification, no implicit registration; sync cadence is fresh enough for "last seen 3 days ago". **Tradeoff:** A provenance record's last-seen time can lag by up to an hour.

### 6. Provenance is part of the Game view

**Decision:** A Game is presented with its provenance, not as a separate feature or destination. **Why:** Where a game has lived is part of understanding it, especially once it is installed nowhere. **Tradeoff:** Views that do not need provenance still receive it.

### 7. Server deletions sync back as untracking

**Decision:** When the server no longer has a tracked game — or no longer has any omnisave for a game whose save this Device had bound — the Device untracks the game, drops its bindings, and reports why, instead of re-resolving or reseeding it. **Why:** Deleting in the Dash is the user's explicit "forget this", and the server is the authority ([ADR-001](../adr/ADR-001-server-authority.md)). Re-resolving from scan evidence or reseeding from local content would mean a deletion could never win while any Device still remembered the game. Re-tracking is deliberate and starts fresh. **Tradeoff:** A full server data reset untracks every game on every Device, so users re-track after a reset. A save deleted to reseed it also needs a re-track first.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — server truth behind membership, provenance, and deletion winning; [ADR-002](../adr/ADR-002-sse-view-invalidation.md) — view refresh; [ADR-007](../adr/ADR-007-per-device-credentials.md) — per-Device credentials; [ADR-012](../adr/ADR-012-portable-save-store.md) and [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md) — durable deletion markers; [ADR-013](../adr/ADR-013-server-announced-presence-expiry.md) — presence expiry; [ADR-020](../adr/ADR-020-domains-own-their-contracts.md) — where the game-and-save rule is enforced.
- **FDRs:** [FDR-001](FDR-001-game-identity-resolution.md) — track-time resolution; [FDR-003](FDR-003-automatic-save-binding.md) — the binding pass that applies deletion-wins to bound saves; [FDR-006](FDR-006-connecting-a-device.md) — how a Device gets its credential.
