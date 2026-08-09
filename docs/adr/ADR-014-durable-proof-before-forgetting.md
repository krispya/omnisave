# ADR-014: Require Durable Proof Before Forgetting Data

**Date:** 2026-08-09

## Context

Omnisave keeps live metadata in SQLite and a tool-independent recovery copy in
the portable store ([ADR-012](ADR-012-portable-save-store.md)). Neither can be
updated atomically with the other or with filesystem objects. Treating absence
in one place as evidence that content is dead creates unsafe races: a reference
can arrive after a liveness check, an unreadable manifest can look absent, and
a deletion marker written for a transaction that later fails can look final.

The safe failure is always to retain bytes or pause destructive work. Leaking
content temporarily is repairable; deleting content whose liveness is unknown
is not.

## Decision

**Destruction requires positive, durable proof.** Absence is never proof that
data is safe to forget. The persistence boundary carries three kinds of proof.

**SQLite proves online artifact availability and reference safety.** The
artifact registry is part of the relational model rather than a size cache.
Revision files and game media reference it with foreign keys, and references
can be created only for an artifact recorded as available. Publishing or
removing an object and changing its availability share the same short SQLite
write transaction; streaming and verification happen beforehand. Removal uses
an atomic quarantine rename while that transaction is open. Startup restores
every quarantine, so a crash before commit retains the object and a crash after
commit merely makes the next complete sweep repeat the removal. SQLite
serialization therefore decides commit-versus-reclamation races, and a
foreign-key restriction prevents reclaiming an artifact a surviving record
uses.

**A transactional outbox is the only path from SQLite to the portable store.**
Every transaction affecting portable state records a self-contained snapshot
or deletion fact in the same commit. Replay never has to query rows a later
transaction may have changed or removed. The outbox writes manifests, mutable
records, and deletion markers in commit order. A request affecting durable save
state is successful only after its required store work is durable. A failed
store write remains queued rather than becoming an untracked, best-effort side
effect; the committed result is discoverable, but it was not acknowledged.

**Deletion is an immutable committed fact.** A deleting transaction removes
the logical rows, records their identifiers in SQLite's Deletion Ledger, and
enqueues immutable deletion markers for the affected game, save, and revision
identifiers. Schema triggers consult the ledger, so another process cannot
reuse an identifier while its marker is still queued. The markers are written
only after SQLite commits; if that commit fails, no marker exists. Recovery
honors markers and preserves data without one. Manifests and objects are
reclaimed only after their markers are durable. Delete requests are idempotent
with respect to an existing marker.

**Only complete recovery may infer garbage.** Recovery reports whether every
record needed to establish reachability was read successfully. Only that
complete result can authorize a sweep. Any unreadable record, manifest, or
deletion marker leaves recovery useful but degraded: readable data is served,
while reclamation is skipped. Unknown always means retain.

Degradation must be escapable by the passes that only add. Reconcile runs even
against an incomplete inventory, because writing what the database holds and
the store lacks can only repair, and the inventory is retaken after it — a gap
the database can rewrite heals in the same open. Only damage that no additive
pass can rewrite keeps mutations disabled, and that state asks for repair, not
for a restart.

Process-local locks may serialize implementation work, but they are not a
correctness boundary. Schema constraints, durable outbox rows, immutable
markers, and the recovery-completeness proof carry correctness.

## Consequences

Easier:

- A reference to unavailable content is rejected at the persistence boundary,
  regardless of what an earlier service-layer check observed.
- Failed database transactions cannot leave authoritative deletion markers.
- Portable-store writes are ordered, replayable, and impossible for a mutation
  path to omit accidentally.
- Corruption disables destructive inference instead of expanding damage.
- Save, revision, and game deletion share one protocol.

More difficult:

- Artifact publication and reclamation require schema constraints and short
  database transactions spanning local filesystem renames or removals.
- Store format migrations must convert legacy tombstones into immutable
  markers.
- A store that cannot accept queued work makes the corresponding request
  unavailable rather than allowing the portable recovery copy to lag silently.
- Damage can retain garbage indefinitely until the damaged records are repaired
  or deliberately removed.
