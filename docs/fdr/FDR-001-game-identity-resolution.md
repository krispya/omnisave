# FDR-001: Game Identity Resolution

**Status:** Active **Last reviewed:** 2026-09-30

## Overview

Resolution turns the Evidence a Device reports for an installed game into one server-owned Game, which every omnisave of that game references across stores, platforms, and emulators. No single external identifier has to cover every game.

## Behavior

- A Device resolves a game from scoped identifiers and content fingerprints, plus optional title and platform hints. At least one identifier or fingerprint is required.
- Evidence the Library already holds resolves to that Game, whichever piece is sent; new evidence sent with it joins that Game.
- Unknown evidence is offered to the Catalog. A provider's claim counts only if it repeats evidence it was asked about, and then adds its identifiers, fingerprints, description, and media. Providers are asked in turn, each seeing what earlier claims added, and one that is unavailable is skipped.
- When no provider knows the game, the server creates a provisional Game named by the Device's title hint. Without a title, resolution fails.
- Evidence already held by different Games is an identity conflict: resolution fails without merging the Games or applying any of the new evidence.
- Choosing a catalog match for a Game replaces how it is described (title, metadata, media) with that claim's and adds the claim's evidence. The match is refused if that evidence belongs to another Game.

## Design Decisions

### 1. The server owns the Game identity

**Decision:** Every Game gets a server-local ID. External identifiers are evidence attached to it, never its identity.

**Why:** No external catalog covers every commercial game, ROM, emulator, and regional release, and a local identity stays stable when providers are missing, change, or are added later. The server is the authority omnisaves reference ([ADR-001](../adr/ADR-001-server-authority.md)), so it owns the identity they reference by.

**Tradeoff:** Two servers can give the same game different IDs; only evidence can reconcile them.

### 2. Identity is scoped, accumulated evidence

**Decision:** A Game holds any number of namespaced identifiers (`steam.app`, `igdb.game`, `hasheous.game`) and platform-scoped fingerprints. Neither kind is required, and titles and platforms are only hints.

**Why:** Steam games have a store ID and no content hash; ROMs have exact hashes and no store ID. One model holds both without inventing missing data, and an unqualified ID is meaningless once two providers can use the same value.

**Tradeoff:** Adapters and providers must keep namespace names stable, and a Game without fingerprints gets no exact-content guarantee.

### 3. Providers make claims; they never own Games

**Decision:** A provider adds evidence, metadata, and media to a Game, but its record is not the Game.

**Why:** Providers stay replaceable, and claims from Hasheous, IGDB, or future providers can coexist on one Game.

**Tradeoff:** Conflicting claims have to be detected rather than settled by trusting one provider everywhere.

### 4. Known evidence is resolved locally first

**Decision:** The server reuses a Game it already knows before asking any provider.

**Why:** Repeated scans should be deterministic, fast, and work while a provider is offline or unconfigured. Providers only help when local evidence does not already settle identity.

**Tradeoff:** A provisional Game stays provisional until someone chooses a match; resolving known evidence never asks providers again.

### 5. Ambiguity fails instead of merging

**Decision:** Each identifier or fingerprint belongs to at most one Game. A request that connects evidence held by different Games fails atomically with an identity conflict.

**Why:** Joining catalog records automatically can attach saves to the wrong game, which does more damage than asking a person to resolve the ambiguity.

**Tradeoff:** A wrong historical match blocks resolution until a person deletes or rematches a Game.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — the server-as-authority premise behind server-owned identity; [ADR-003](../adr/ADR-003-environment-server-configuration.md) — how provider credentials are configured, and why a provider may be unavailable.
- **FDRs:** [FDR-002](FDR-002-game-lifecycle.md) — resolution happens when a game is tracked.
