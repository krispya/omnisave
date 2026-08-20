# ADR-014: Require Durable Proof Before Forgetting Data

**Date:** 2026-08-17

## Context

Omnisave keeps live metadata in SQLite and a tool-independent recovery copy in the portable store ([ADR-012](ADR-012-portable-save-store.md)). Neither can be updated atomically with the other or with filesystem objects. Treating absence in one place as evidence that content is dead creates unsafe races: a reference can arrive after a liveness check, an unreadable manifest can look absent, and a deletion marker written for a transaction that later fails can look final.

The safe failure is always to retain bytes or pause destructive work. Leaking content temporarily is repairable; deleting content whose liveness is unknown is not.

## Decision

**Destruction requires positive, durable proof.** Absence is never proof that data is safe to forget. The persistence boundary carries three kinds of proof.

**SQLite proves online artifact availability and reference safety.** The artifact registry is part of the relational model rather than a size cache. Revision files and game media reference it with foreign keys, and references can be created only for an artifact recorded as available. Publishing or removing an object and changing its availability share the same short SQLite write transaction; streaming and verification happen beforehand. Removal uses an atomic quarantine rename while that transaction is open. Startup restores every quarantine, so a crash before commit retains the object and a crash after commit merely makes the next complete sweep repeat the removal. SQLite serialization therefore decides commit-versus-reclamation races, and a foreign-key restriction prevents reclaiming an artifact a surviving record uses.

**A transactional outbox is the only path from SQLite to the portable store.** Every transaction affecting portable state records a self-contained snapshot or deletion fact in the same commit. Replay never has to query rows a later transaction may have changed or removed. The outbox writes manifests, mutable records, and deletion markers in commit order. A drainer acquires SQLite's writer position before selecting the oldest action and holds it through the durable store write and queue removal. This makes projection order database-wide. Two processes cannot select the same action or let an older mutable record overwrite a newer one. A mutation that creates or changes durable state is successful only after its required store work is durable. A failed store write remains queued rather than becoming an untracked, best-effort side effect. The committed result is discoverable, but it was not acknowledged. The drainer's wait for the writer position is bounded: contention that outlasts its patience surfaces as a projection failure instead of holding the mutation path indefinitely.

**Deletion is an immutable committed fact.** A deleting transaction removes the logical rows, records their identifiers in SQLite's Deletion Ledger, and enqueues immutable deletion markers for the affected game, save, and revision identifiers. Schema triggers consult the ledger, so another process cannot reuse an identifier while its marker is still queued. The markers are written only after SQLite commits; if that commit fails, no marker exists. Recovery honors markers and preserves data without one. Manifests and objects are reclaimed only after their markers are durable. Delete requests are idempotent with respect to a committed ledger row or an existing marker.

**A deletion is acknowledged at its SQLite commit.** Unlike a creating mutation, a delete's durable local proof — the ledger row and its queued markers — is complete when the transaction commits, so the request does not wait for the projection or for physical reclamation. A background task, serialized with other mutations, projects the markers and then drops manifests and reclaims objects. A projection failure there stops later durable mutations exactly as an inline failure would, surfacing on the next mutation rather than on the delete; the next open replays the queue. A crash before the deferred cleanup runs leaves the same state as one during it, which recovery already repairs.

**Only complete recovery may infer garbage.** Recovery reports whether every record needed to establish reachability was read successfully. Only that complete result can authorize a sweep. Any unreadable record, manifest, or deletion marker leaves recovery useful but degraded: readable data is served, while reclamation is skipped. Unknown always means retain.

Degradation must be escapable by non-destructive repair. Reconcile runs even against an incomplete inventory. It snapshots the database into the same ordered outbox used by live mutations and never deletes portable data. The inventory is retaken afterward, so a gap the database can rewrite heals in the same open. Only damage that this repair cannot replace keeps mutations disabled, and that state asks for repair, not for a restart.

Process-local locks may serialize implementation work, but they are not a correctness boundary. Schema constraints, durable outbox rows, immutable markers, and the recovery-completeness proof carry correctness.

## Consequences

Easier:

- A reference to unavailable content is rejected at the persistence boundary, regardless of what an earlier service-layer check observed.
- Failed database transactions cannot leave authoritative deletion markers.
- Portable-store writes are ordered, replayable, and impossible for a mutation path to omit accidentally.
- Corruption disables destructive inference instead of expanding damage.
- Save, revision, and game deletion share one protocol.
- A delete answers in the time of its own transaction, independent of how much history and content it reclaims.

More difficult:

- Artifact publication and reclamation require schema constraints and short database transactions spanning local filesystem renames or removals.
- Outbox projection holds SQLite's writer position across a local store write and directory sync. Other mutations wait so portable records cannot be reordered across processes.
- Store format migrations must convert legacy tombstones into immutable markers.
- A store that cannot accept queued work makes creating mutations unavailable rather than allowing the portable recovery copy to lag silently; after an acknowledged deletion the same failure surfaces one mutation later than it happened.
- Damage can retain garbage indefinitely until the damaged records are repaired or deliberately removed.
