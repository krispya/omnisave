# FDR-001: Game Identity Resolution

**Status:** Active
**Last reviewed:** 2026-07-18

## Overview

Game Identity Resolution gives clients and catalog providers a shared way to
refer to the same game without requiring one universal external identifier.
The server resolves available identity evidence into a server-owned Game,
which Omnisaves can then reference consistently across platforms, stores, and
emulators.

## Behavior

- A client can resolve a Game using one or more scoped identifiers, optional
  content fingerprints, and descriptive title or platform hints.
- Resolving known evidence returns the existing canonical Game. New evidence
  supplied alongside known evidence becomes part of that Game's identity.
- Catalog providers may add identifiers, fingerprints, descriptive metadata,
  and media when they can substantiate a match.
- If no existing Game or provider match is available, the server can create a
  provisional Game when the client provides an identity and usable title.
- Resolving the same game through different known identifiers returns the same
  server-owned Game.
- Evidence already assigned to different Games produces an Identity Conflict.
  The server does not silently merge the Games or partially apply the new
  evidence.
- A user-selected catalog match enriches the chosen Game while preserving its
  existing identity evidence.
- Resolution does not choose an Omnisave or start synchronization. It only
  establishes which Game the discovered installation represents.

## Design Decisions

### 1. The server owns the canonical Game identity

**Decision:** Every Game receives a server-local UUID. External identifiers
are evidence attached to the Game rather than its primary identity.

**Why:** No external catalog covers every commercial game, ROM, emulator, and
regional release. A local identity remains stable when providers are missing,
changed, or supplemented later. Downstream of
[ADR-001](../adr/ADR-001-server-authority.md): the server is the
authority Omnisaves reference, so it must own the identity they reference by.

**Tradeoff:** Two independent Omnisave servers can assign different UUIDs to
the same game and need evidence, rather than UUID equality, to reconcile
catalogs in the future.

### 2. Identity is accumulated evidence

**Decision:** A Game may carry many scoped identifiers and many content
fingerprints. Descriptive title and platform values are hints, not unique
identities.

**Why:** Steam games commonly have a strong store identifier but no content
fingerprint, while ROMs commonly have strong hashes but no store identifier.
The same model must represent both without inventing missing data.

**Tradeoff:** Resolution must normalize and compare a collection of evidence,
which is more complex than looking up one provider ID.

### 3. Identifier namespaces are explicit

**Decision:** External IDs are qualified by a namespace such as `steam.app`,
`igdb.game`, or `hasheous.game`.

**Why:** An unqualified numeric or textual ID has no reliable meaning. Scoped
identifiers allow unrelated providers to use overlapping values safely and
make the source and kind of an identity clear.

**Tradeoff:** Adapters and providers must agree on stable namespace names.

### 4. Fingerprints are optional and platform-scoped

**Decision:** A fingerprint records a platform, hash algorithm, and value, but
Games are not required to have one.

**Why:** Exact hashes are valuable for identifying ROM content and regional
variants. They are unavailable or unsuitable for many installed PC games, so
making them mandatory would exclude Steam and similar targets.

**Tradeoff:** Games without fingerprints depend on store identifiers or later
catalog matching and cannot receive exact-content guarantees.

### 5. Providers make claims rather than own Games

**Decision:** A catalog provider can enrich resolution with additional
evidence and metadata, but the provider's record is not the canonical Game.

**Why:** This keeps the catalog provider replaceable and permits claims from
Hasheous, IGDB, Steam, Ludusavi, or future sources to coexist on one Game.

**Tradeoff:** Conflicting provider claims require explicit detection and future
repair tools instead of being resolved by trusting one provider globally.

### 6. Known evidence is resolved locally first

**Decision:** The server reuses a locally known Game before consulting an
external provider.

**Why:** Repeated scans should be deterministic, fast, and available when an
external catalog is offline. Provider calls are only useful when local evidence
does not already establish identity.

**Tradeoff:** A known provisional Game is not automatically refreshed by every
resolution request; metadata refresh needs its own policy.

### 7. Ambiguity fails instead of merging automatically

**Decision:** Each identifier or fingerprint can identify only one Game. If a
request connects evidence already owned by different Games, resolution fails
atomically with an Identity Conflict.

**Why:** Automatically joining catalog records can attach saves to the wrong
game, which is more damaging than requiring a user or future repair workflow
to resolve the ambiguity.

**Tradeoff:** Incorrect historical matches can block resolution until catalog
identity-management tools exist.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — the
  server-as-authority premise behind server-owned canonical identity.
- **FDRs:** [FDR-002](FDR-002-game-lifecycle.md) — when resolution happens in
  a game's lifecycle (at track time).

