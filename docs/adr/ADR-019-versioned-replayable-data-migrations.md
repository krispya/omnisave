# ADR-019: Evolve Durable Data Through Versioned, Replayable Migrations

**Date:** 2026-08-23

## Context

Omnisave has three independent compatibility surfaces: SQLite's relational schema, the Portable Store's serialization format, and the meaning of durable domain data such as revision paths. Advancing one does not imply that either of the others advanced. Changing the meaning of a revision path can require evidence from a Device, and the change must survive a database rebuild from immutable manifests ([ADR-012](ADR-012-portable-save-store.md)).

Inferring a domain format during ordinary operation is unsafe. It cannot tell an old value from a coincidentally similar new one, it loses the fact that a transformation happened, and it makes a partly understood history look usable. A one-off rewrite embedded in startup or request handling has the opposite problem: it solves the first transition without defining how the next one composes with it.

## Decision

**Version each compatibility surface independently.** Ordered SQL migrations version the SQLite schema. The store's `VERSION` marker and per-record versions govern serialization. Data whose meaning can change, such as an omnisave's revision paths, carries its own domain-format version. None of these substitutes for another.

**Domain migrations are immutable adjacent steps.** A released step names one source version, one target version, and the operation that transforms the data. Exactly one step leaves each retired version, and a single orchestrator applies steps in order until the data reaches the current version. Missing, unclassified, and newer versions fail closed instead of being guessed through. Released steps are never edited or reordered; a correction is another step. No step may produce a retired representation, so any history can be classified unambiguously. Tests assert the table's shape, because every dispatch site reads it and none sees all of it.

**Applying a step is atomic and auditable.** One transaction checks the expected source version, transforms the whole history, appends a durable fact recording both versions, the operation, and its input, and advances the version. A retried request cannot apply a step twice, because its expected source version is stale. Ordinary writes are refused until the data is current, so old and new representations never mix.

**Recovery replays facts, then derives the version.** Manifests are never rewritten to reflect a migration. Every open that runs recovery replays the recorded facts over imported manifests, then derives each version from the resulting data and corrects any claim that disagrees. A database and a store restored from different moments therefore converge on what the data actually says. Facts this build cannot replay, or data it cannot classify, are held rather than mutated ([ADR-014](ADR-014-durable-proof-before-forgetting.md)). A version is reached only by applying a step, never by asserting it.

**Evidence stays at the boundary that can know it.** The server owns transition definitions, atomicity, and validation. A client may gather evidence the server cannot have and supply it, but never chooses or overrides the transition.

**Build only the operations that exist.** Today there is one: renaming the retired Steam mirror location to a native save location across a whole history. There is no migration DSL, no generic payload, and no unused strategy. A new transformation gets its own explicit implementation when it is needed.

## Consequences

- A future domain change has a set place for its version, its step, its atomic write, and its replay, without coupling to database or store versions.
- A history can cross several released versions in order, while each step stays small and independently testable.
- Deriving the version at each recovery costs a pass over each history's data at startup, and means a hand-edited version lasts only until the next restart. Both are accepted, because the alternative is a claim that can outlive the data behind it.
- A migration that needs Device evidence stays visibly held until a suitable Device appears; the server cannot manufacture that evidence.
- A new durable fact advances the store format even when the field is additive, because an older binary would drop it when rewriting the record and silently forget an applied migration. Older releases then refuse the store.
- Every released step is permanent compatibility code until the oldest supported format is deliberately retired.
