# FDR-002: Game Lifecycle

**Status:** Active **Last reviewed:** 2026-08-20

## Overview

How a game enters the Library, what the server remembers about where it came from, and what it takes for a game to leave. The lifecycle runs detect → track → bind: a Device's adapters discover installed games, the user tracks the ones Omnisave should know about, and binding connects native saves to Omnisaves.

## Behavior

- Scanning a Device discovers installed games without changing anything. Detection alone never adds a game to the Library.
- Tracking a game is the act of library entry — "make Omnisave aware of this game". The game is resolved from the Catalog into the Library at track time, before any save is bound (resolution behavior: [FDR-001](FDR-001-game-identity-resolution.md)).
- A stored Library identity the server no longer recognizes means the game was deleted there, and the deletion syncs back: the Device untracks the game, drops its save bindings, and reports why. Server truth outranks client memory ([ADR-001](../adr/ADR-001-server-authority.md)); re-tracking later is a fresh, deliberate library entry.
- Binding attaches a Device's Local Save to an Omnisave of a game that is already in the Library.
- Each client installation identifies itself as a Device: a stable ID minted on first run plus a human-readable name, registered with the server.
- The server keeps Provenance for each game: which Devices have tracked it, when each first tracked it, when each was last seen, whether the install is still present there, and whether it has since been untracked.
- Untracking marks the provenance record inactive; it removes neither the record nor the game.
- Uninstalling a game from a device flips that device's installed flag at the next scan or sync. Saves and provenance are unaffected.
- Deleting all of a game's Omnisaves leaves the game and its provenance in the Library.
- Deleting the game removes everything — its saves, revision history, unshared artifacts, and provenance — and records immutable game, save, and revision deletion markers so restoring an older portable copy cannot undo it. This is the lifecycle's only act of forgetting.
- Game views include provenance and refresh when games, saves, revisions, or provenance change.
- Ordering the Library by latest activity uses the newest revision received for each game, regardless of which historical revision is currently selected.
- Device liveness (last seen) updates on explicit acts — registration, tracking, sync — not on every request.

## Design Decisions

### 1. Tracking, not detection, is library entry

**Decision:** Games enter the Library only when a user tracks them. **Why:** Detection is cheap and indiscriminate — one RetroArch playlist can surface hundreds of games the user never intends to back up. Tracking is the deliberate opt-in that makes membership mean something. **Tradeoff:** Nothing is protected automatically; a game the user forgot to track has no saves on the server.

### 2. Library membership never depends on current installation

**Decision:** Installation is per-device data, not a membership criterion. **Why:** The server is the durable side of the system ([ADR-001](../adr/ADR-001-server-authority.md)); uninstalling to free space is precisely when the server copy matters most. Requiring an install would orphan saves the moment they became valuable. **Tradeoff:** Libraries accumulate games installed nowhere; cleanup is a deliberate delete, never automatic.

### 3. Provenance is append-only and dies only with the game

**Decision:** Untracking and uninstalling annotate provenance; nothing removes it except deleting the game itself. **Why:** The history of where a game lived is the value that justifies keeping a game with no saves. An empty game record with provenance still answers "what happened here". **Tradeoff:** Records accumulate without pruning; acceptable at self-hosted scale.

### 4. Devices self-identify

**Decision:** A Device mints its own ID on first run and reports its own name. Identity belongs to the client installation, not the hardware. **Why:** The server has a single shared API token and no accounts; there is no authority that could assign device identities. Self-reporting matches the self-hosted trust model. **Tradeoff:** Identity is spoofable and resets if local client state is wiped.

### 5. Explicit liveness, not per-request heartbeat

**Decision:** Last-seen updates only on registration, tracking, and sync. **Why:** No middleware, no write amplification, no implicit registration. Sync cadence is fresh enough for "last seen 3 days ago". **Tradeoff:** Liveness granularity is one sync interval.

### 6. Provenance is part of the Game view

**Decision:** A Game is presented with its provenance rather than making provenance a separate feature or destination. **Why:** Where a game has lived is part of understanding that Library entry, especially after it is no longer installed anywhere. **Tradeoff:** Views that do not need provenance still receive a richer Game representation.

### 7. "Device" is a new term; "Target" is not reused

**Decision:** The self-identified machine is a Device. Target keeps its existing meaning — one application installation on a device. **Why:** A device hosts several targets, and provenance plausibly wants both ("tracked on steam-deck via RetroArch"). Overloading target would recreate the catalog/library ambiguity. **Tradeoff:** One more glossary noun to learn.

### 8. Resolution stays free of device context

**Decision:** Track intent is reported separately from identity resolution. **Why:** Debug resolution and future manual-add flows resolve games with no device involved; keeping resolution pure preserves FDR-001's single responsibility. **Tradeoff:** Track time makes two calls where one might do.

### 9. Server events invalidate; reads replace

**Decision:** Open Library views refresh automatically instead of requiring a manual refresh. A server event invalidates the current view, which is replaced from ordinary API reads as described by [ADR-002](../adr/ADR-002-sse-view-invalidation.md). **Why:** Events announce that server truth changed without creating a second state model for games, saves, revisions, or provenance. **Tradeoff:** Reconnection and burst coalescing add client complexity, and a reconnect may perform a redundant read to guarantee convergence.

### 10. Server deletions sync back as untracking

**Decision:** When the server no longer has a tracked game — or no longer has any Omnisave for a game whose save this Device had bound — the client untracks the game on that device instead of re-resolving or reseeding it. **Why:** Deleting in the Dash is the user's explicit "forget this", and the server is the authority ([ADR-001](../adr/ADR-001-server-authority.md)). The earlier behavior — silently re-creating a deleted game from scan evidence, or reseeding a deleted save from local content — meant a deletion could never win while any device still remembered the game. Untracking honors the deletion and keeps re-protection explicit: re-tracking is deliberate and starts fresh. **Tradeoff:** A full server data reset untracks every game on every device instead of self-healing invisibly, so users re-track after a reset. A save deleted with the intent of reseeding it also requires that re-track first.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — the source-of-truth premise behind installation-independent membership and server-side provenance; [ADR-002](../adr/ADR-002-sse-view-invalidation.md) — how clients learn that server-owned views changed; [ADR-012](../adr/ADR-012-portable-save-store.md) — why deleting a game durably marks the game and its saves rather than only erasing them, so a restore does not undo the deletion; [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md) — why committed markers must exist before deleted content can be reclaimed.
- **FDRs:** [FDR-001](FDR-001-game-identity-resolution.md) — how track-time resolution chooses or creates the canonical Game; [FDR-003](FDR-003-automatic-save-binding.md) — the binding pass that applies deletion-wins to bound saves.
