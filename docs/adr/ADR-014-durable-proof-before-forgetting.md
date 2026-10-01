# ADR-014: Require Durable Proof Before Forgetting Data

**Date:** 2026-08-17

## Context

Omnisave keeps live metadata in SQLite and a tool-independent recovery copy in the Portable Store ([ADR-012](ADR-012-portable-save-store.md)). Neither can be updated atomically with the other or with filesystem objects. Treating absence in one place as evidence that content is dead creates unsafe races: a reference can arrive after a liveness check, an unreadable manifest can look absent, and a deletion marker written for a transaction that later fails can look final.

The safe failure is always to retain bytes or pause destructive work. Leaking content temporarily is repairable; deleting content whose liveness is unknown is not.

## Decision

**Destruction requires positive, durable proof.** Absence is never proof that data is safe to forget.

**The artifact registry proves availability.** Revision files and game media reference it with foreign keys, and schema triggers allow a reference only to an artifact marked available. Content is streamed and hash-checked first. Publishing then takes SQLite's write lock, verifies the object again, and marks it available. Reclaiming takes the same lock, confirms nothing references the object, and quarantines it with an atomic rename before committing. Startup restores every quarantined object, so a crash only delays a removal. SQLite's write order decides every race between a new reference and a reclaim.

**The store outbox is the only path from SQLite to the Portable Store.** A transaction that changes portable state enqueues a self-contained record or deletion fact in the same commit. A drainer applies the queue in commit order while holding the write lock, so an older record can never land last, even across processes. A creating mutation succeeds only once its projection is durable. If projection fails, the work stays queued, the request fails although its rows committed, and durable mutations stop until an open replays the queue. The one exception is the upgrade of version-3 stores. It predates the outbox and writes deletion markers and omnisave records directly, under the same write lock.

**Deletion is an immutable committed fact.** A deleting transaction removes the rows, records their identifiers in the deletion ledger, and enqueues deletion markers. The ledger bars reuse of a deleted identifier and makes a repeated delete succeed. Markers are written only after SQLite commits, and manifests and objects are reclaimed only after their markers are durable.

**A deletion is acknowledged at its SQLite commit.** The ledger row and queued markers are already durable proof, so a background task projects the markers and reclaims content afterwards. If that projection fails, durable mutations stop just as after an inline failure.

**Only complete recovery may infer garbage.** Recovery uses whatever records it can read, but only an inventory that read every record needed for reachability may authorize a sweep. An unreadable deletion marker also stops imports. Reconcile runs regardless, because it only adds, and the inventory is then retaken, so damage the database can rewrite heals in the same open. Any other damage keeps durable mutations off until an open finds the inventory complete. That read-only recovery mode appears only in the server log, and clearing it takes a repair and a restart.

Process-local locks are not a correctness boundary. Schema constraints, the store outbox, immutable markers, and the completeness proof are.

## Consequences

Easier:

- A reference to unavailable content is rejected at the persistence boundary, whatever an earlier service check observed.
- A failed database transaction cannot leave an authoritative deletion marker.
- Portable-store writes are ordered and replayable, and no mutation path can skip them.
- Corruption disables destructive inference instead of spreading damage.
- Save, revision, and game deletion share one protocol, and a delete answers in the time of its own transaction.

More difficult:

- The write lock is held across filesystem work: a full re-read of the object when publishing, a rename when reclaiming, and a store write and directory sync when projecting. Other mutations wait.
- A creating mutation can fail after its rows committed and became readable. Durable mutations then stay off until the queue is replayed, rather than letting the recovery copy lag silently. After a delete, the same failure surfaces one mutation later.
- Read-only recovery mode is not reported through the API or the Dash. An owner sees failing mutations and has to read the server log to learn why.
- Damage can retain garbage indefinitely until the damaged records are repaired or deliberately removed.
