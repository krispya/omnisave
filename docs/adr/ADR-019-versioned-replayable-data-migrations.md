# ADR-019: Evolve Durable Data Through Versioned, Replayable Migrations

**Date:** 2026-08-23

## Context

Omnisave has three independent compatibility surfaces: SQLite's relational schema, the portable store's serialization format, and the meaning of durable domain data such as revision paths. Advancing one does not imply that either of the others advanced. In particular, changing the meaning of a revision path can require evidence from a Device and must survive a database rebuild from immutable manifests ([ADR-012](ADR-012-portable-save-store.md)).

Inferring a domain format repeatedly from current data is unsafe. It cannot distinguish an old value from a coincidentally similar new value, it loses the fact that a transformation happened, and it makes a partially understood lineage look usable. A one-off rewrite embedded in startup or request handling has the opposite problem: it solves the first transition without defining how the next transition composes with it.

## Decision

**Use a versioned aggregate, ordered migration chain, durable migration ledger, and rebuildable projection.** This is the domain-data counterpart to ordinary database migrations: explicit versions select small forward-only steps, while durable facts let a disposable projection reproduce their effects.

**Version each compatibility surface independently.** Ordered SQL migrations version the SQLite schema. The portable store marker and record versions govern serialization compatibility. An aggregate owns a separate domain-format version when the meaning of its durable data changes. None of these versions substitutes for another.

**Domain migrations are immutable adjacent steps.** A released step names one source version, one target version, and the operation that transforms the aggregate. Exactly one step may leave a retired version. A single orchestrator repeatedly selects the step for the aggregate's persisted version until it reaches the current version. Missing, unclassified, and newer versions fail closed instead of being guessed through. Released steps are never edited or reordered; a correction is another step. The chain's shape — adjacency, one step per retired version, ending at the current one, and no step transforming data back into a retired representation — is asserted against the table itself, because every dispatch site reads it and none of them can see the whole of it.

**Applying a step is atomic and auditable.** The persistence transaction validates the expected source version, transforms the whole aggregate, appends a durable fact containing the source version, target version, operation, and operation-specific input, and advances the aggregate version. Retrying a completed request cannot apply the same step twice because its expected source version is stale. Ordinary writes are refused while an aggregate is not current, preventing old and new representations from mixing.

**Recovery replays facts before deriving state.** Immutable source records are not rewritten merely to reflect a domain migration. Rebuild imports them, replays the ordered migration facts idempotently, and then derives and validates the aggregate's version from the resulting data. The SQLite projection is disposable; the portable record and its facts are sufficient to regain the acknowledged state. Unknown facts or data that cannot be classified remain unavailable for mutation, following the retain-and-hold rule in [ADR-014](ADR-014-durable-proof-before-forgetting.md).

**The data outranks the version claim, on every open.** Replay and derivation are not reserved for an open that imported something: they run whenever the store opens, so a persisted version that disagrees with the data it describes is corrected before anything reads either. This is what makes skew converge — a database and a store restored from different moments cannot leave a version pointing at a representation the data no longer has — and it is also the limit of the version's authority. The durable ledger is what recovery replays; the version is what recovery concludes, never a claim that survives being wrong. A version can therefore only be advanced by applying a step, never by asserting it, and holding an aggregate whose data cannot be classified is the cost paid for that guarantee.

**Migration evidence stays at the boundary that can know it.** The server owns transition definitions, atomicity, and validation. A client may run the evidence-gathering loop and supply operation-specific evidence that the server cannot possess, but never chooses or overrides the transition itself. For the path-format migration, a Device proves the native location mapping; the server-defined step decides which retired vocabulary and target version that proof may advance.

**Build only the current operation.** The first use of this pattern is the mirror-to-native revision-path migration: path format 1 to 2 by one proven whole-history location rename. Its ordered step table, client loop, atomic repository operation, and durable replay fact are the required extension points. This decision does not introduce a migration DSL, generic payloads, or unused strategies. A different transformation earns its own explicit implementation when it exists.

## Consequences

- Future domain changes have a stable place to record their version, adjacent transition, atomic write, and replay behavior without coupling them to database or JSON schema versions.
- A lineage can cross several released versions in order, while each individual operation stays small and independently testable.
- Backup skew converges because recovery can replay what happened rather than trust a free-standing version claim.
- Deriving the version on every open costs a pass over each aggregate's durable data at startup, and makes a hand-edited version worthless: it lasts until the next restart. Both are accepted, because the alternative is a claim that can outlive the data supporting it.
- Migrations that need Device evidence may remain visibly held until a suitable Device appears; the server cannot manufacture that evidence.
- Adding a version to a durable record advances the serialization format even though the field itself is additive, because an older binary would read the record, ignore the field, and write the aggregate back without the facts recorded on it — silently forgetting an applied migration. The store's format marker moves forward the first time this build opens a store, and prior releases refuse it from then on.
- Every released step becomes permanent compatibility code until the oldest supported durable format is deliberately retired.
