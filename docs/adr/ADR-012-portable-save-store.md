# ADR-012: Keep Saves in a Portable Store

**Date:** 2026-07-27

## Context

[ADR-001](ADR-001-server-authority.md) makes the server the durable authority for saves. Owners need to be able to copy, inspect, and recover those saves without a running Omnisave server or specialized repository or database software. A person who has only the store and ordinary text and gzip tools must be able to get the original save files out.

Save content is content-addressed: a file's bytes, named by their hash. The store also needs the context that makes those bytes a save: the game and Omnisave they belong to, their revision and path, and the order of their history.

## Decision

**The store is a tool-independent recovery artifact.** Records are plain JSON, content is gzip, and recovery instructions live in the project documentation ([docs/RECOVERY.md](../RECOVERY.md)) rather than inside every store: the format is simple enough to explain once, and the instructions were the one file an otherwise immutable directory kept rewriting. Getting a save out requires no Omnisave binary, Git or SVN client, database software, or network connection — the `VERSION` marker names the format so a reader knows what to look up.

**One directory is the portable save store.** It holds everything needed to recover every save in it.

```
VERSION            the format marker
objects/ab/<sha256>.gz    file content, sharded by the hash's first byte
revisions/ab/<id>.json    one manifest per snapshot
omnisaves/ab/<id>.json    lineage records
games/ab/<id>.json        catalog identity
deletions/<kind>/ab/<id>.json committed save, revision, and game deletions
```

**Manifests carry identity rather than referencing it.** Each names the game and the lineage it belongs to, so one manifest read alone is enough to place its files even if every other record were lost.

**Current revisions are recorded.** Each Omnisave record carries its movable Current Revision pointer. It cannot be derived from timestamps or graph tips: restoring may select any ancestor, descendant, or sibling branch. Updating the pointer replaces only the small Omnisave record atomically; revision manifests remain immutable.

**Server state lives outside the store.** Credentials, pairing state, and the owner's PIN ([ADR-007](ADR-007-per-device-credentials.md), [ADR-010](ADR-010-taking-ownership.md)) are not portable save data. A directory meant to be copied and handed to someone must not carry the way into the server that produced it.

**The database is an index, and the store regrows it.** Opening imports whatever the store holds that the database does not — every game, lineage, and revision — before reconciling the other direction. Losing the database therefore costs credentials and pairings, never a save: a server pointed at a surviving store lists everything in it again, under the same revision identifiers a device's sync baseline already names.

**Nothing is rewritten in place.** Objects, manifests, and deletion markers are immutable. The Omnisave record that carries a save's name, Current Revision, fork origin, and revision labels is replaced atomically.

**Deletion is recorded, not merely performed.** Deleting a save writes an immutable deletion marker. A revision manifest is dropped only when no surviving Omnisave retains that node through shared fork ancestry, so deleting a fork source cannot erase history still active through another save.

Deleting a single revision out of a live lineage is recorded the same way: rebuild refuses to import a manifest named by a committed deletion marker, and the marker stays forever, so a store restored from a backup cannot resurrect the deleted snapshot. Only a node the graph no longer needs may go — nothing builds on it, no save holds it as current, no fork begins there — because a child's manifest names its parent, and manifests are never rewritten.

**SQLite commits before the store records the result.** The same transaction that accepts a change enqueues its portable-store projection. The ordered outbox then writes manifests, mutable records, or immutable deletion markers. Each projection holds SQLite's writer position from selecting the oldest action through its durable store write and queue removal, so separate processes cannot apply mutable records out of order. A durable-save request is not successful until that work rests in the store. A database failure therefore creates no deletion marker, while a store failure leaves replayable work rather than an untracked gap. This is the proof protocol defined by [ADR-014](ADR-014-durable-proof-before-forgetting.md).

**Content is reclaimed only with proof.** Relational references can name only available artifacts and prevent their registry rows from being removed. Filesystem removal and the availability change share a short SQLite write transaction and a reversible quarantine rename, so a concurrent reference either lands first and prevents the removal or lands later and observes the artifact as unavailable. Startup restores an interrupted quarantine; what a crash strands is swept only after recovery read every reachability record successfully; damage disables sweeping rather than turning unknown content into garbage ([ADR-014](ADR-014-durable-proof-before-forgetting.md)).

Revision identifiers stay opaque rather than becoming content hashes. Hashing manifests would make the history a verifiable Merkle DAG, but it changes the API and every client's sync baseline, and it is separable from this decision.

## Consequences

Easier:

- Recovering a save needs no Omnisave installation or specialized software: a person can read its manifest and decompress its files with commonplace tools.
- Backing up is copying a directory. Every object is named by the hash of its content, so the copy verifies with `shasum` and no reference to the original.
- Damage is localized. A partial copy loses the snapshots referencing what is missing, not the history.
- Handing saves to someone leaks no credential.

More difficult:

- **Repair is load-bearing, and runs both ways.** Opening first replays the transactional outbox, then inventories the portable store. Valid deletion markers remove stale rows from an older database; complete portable records restore acknowledged mutable state and missing history; reconcile snapshots the resulting database through the ordered outbox without deleting portable data. The sweep that follows is the one pass that infers garbage, and its API requires the completeness proof produced by that inventory. These passes have to be tested as the correctness mechanism they are.
- Rebuild trusts identity wherever it finds it. A lineage whose record was lost is reconstructed from the identity its manifests carry — with the caveat that a rename after the last commit lived only in the lost record. Immutable deletion markers are the boundary it will not cross, so a restore does not resurrect what its owner threw away.
- A failed store write remains in SQLite's transactional outbox and fails the request until the portable projection is durable. Retrying the outbox is safe because every operation is ordered and idempotent. SQLite serializes drainers across processes, including the filesystem write, because idempotence alone would not prevent an older mutable record from landing last. An open that cannot replay the queue serves reads and defers recovery rather than running rebuild against a store the queue is still ahead of. Durable mutations wait for an open that lands the queued work.
- An unreadable record leaves reads available from the database and disables imports, durable mutations, and reclamation until the inventory is complete. Reconcile still runs because it never deletes portable data, so a gap the database can rewrite, such as a lost manifest for a revision it still holds, is repaired and the inventory retaken in the same open. Only damage the database cannot rewrite keeps the server read-only. Unknown state is retained rather than deleted.
- Portable records are the acknowledged mutable state: a record that disagrees with a row the database already holds overwrites it, and says so in the log. The two agree at rest — the outbox is the only writer — so a disagreement means a legacy store that lagged, or an older store backup restored beside a newer database, and the rollback is deliberate and loud rather than silent.
- A store is many small files — one manifest per revision on top of one object per distinct file — and the inode cost is real where they are metered.
- Denormalized identity ages: a renamed game leaves older manifests carrying the old title. They are historical records, and the lineage and game records hold current truth.
- Restoring only the store gives an unclaimed server. The saves return; browsers and Devices re-establish themselves. Game media does not: cover art rests in the store as objects no record references, so a rebuilt index cannot name it, and it is refetched from providers instead.
- The on-disk format is a compatibility surface with its own version. An older binary refuses a newer store rather than guessing, which is why `VERSION` exists.
