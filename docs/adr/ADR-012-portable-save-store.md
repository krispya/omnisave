# ADR-012: Keep Saves in a Portable Store

**Date:** 2026-07-27

## Context

[ADR-001](ADR-001-server-authority.md) makes the server the durable authority
for saves. Owners need to be able to copy, inspect, and recover those saves
without a running Omnisave server or specialized repository or database
software. A person who has only the store and ordinary text and gzip tools must
be able to get the original save files out.

Save content is content-addressed: a file's bytes, named by their hash. The
store also needs the context that makes those bytes a save: the game and
Omnisave they belong to, their revision and path, and the order of their
history.

## Decision

**The store is a tool-independent recovery artifact.** Records are plain JSON,
content is gzip, and recovery instructions live in the project documentation
([docs/RECOVERY.md](../RECOVERY.md)) rather than inside every store: the format
is simple enough to explain once, and the instructions were the one file an
otherwise immutable directory kept rewriting. Getting a save out requires no
Omnisave binary, Git or SVN client, database software, or network connection —
the `VERSION` marker names the format so a reader knows what to look up.

**One directory is the portable save store.** It holds everything needed to
recover every save in it.

```
VERSION            the format marker
objects/ab/<sha256>.gz    file content, sharded by the hash's first byte
revisions/ab/<id>.json    one manifest per snapshot
omnisaves/ab/<id>.json    lineage records
games/ab/<id>.json        catalog identity
```

**Manifests carry identity rather than referencing it.** Each names the game and
the lineage it belongs to, so one manifest read alone is enough to place its
files even if every other record were lost.

**Current revisions are recorded.** Each Omnisave record carries its movable
Current Revision pointer. It cannot be derived from timestamps or graph tips:
restoring may select any ancestor, descendant, or sibling branch. Updating the
pointer replaces only the small Omnisave record atomically; revision manifests
remain immutable.

**Server state lives outside the store.** Credentials, pairing state, and the
owner's PIN
([ADR-007](ADR-007-per-device-credentials.md),
[ADR-010](ADR-010-taking-ownership.md)) are not portable save data. A directory
meant to be copied and handed to someone must not carry the way into the server
that produced it.

**The database is an index, and the store regrows it.** Opening imports
whatever the store holds that the database does not — every game, lineage, and
revision — before reconciling the other direction. Losing the database
therefore costs credentials and pairings, never a save: a server pointed at a
surviving store lists everything in it again, under the same revision
identifiers a device's sync baseline already names.

**Nothing is rewritten in place.** Objects and manifests are immutable. The
Omnisave record that carries a save's name, Current Revision, fork origin,
revision labels, and tombstone is replaced atomically.

**Deletion is recorded, not merely performed.** Deleting a save tombstones its
record. A revision manifest is dropped only when no surviving Omnisave retains
that node through shared fork ancestry, so deleting a fork source cannot erase
history still active through another save.

Deleting a single revision out of a live lineage is recorded the same way: the
owner's record lists it under `deleted_revisions`, rebuild refuses to import a
manifest named there, and the entry stays forever, so a store restored from a
backup cannot resurrect the deleted snapshot. Only a node the graph no longer
needs may go — nothing builds on it, no save holds it as current, no fork
begins there — because a child's manifest names its parent, and manifests are
never rewritten. The list lives only in the record; a rewrite carries it
forward, dropping any entry whose revision the database still holds, which is
how a tombstone written for a deletion that never committed self-corrects.

**A commit is recorded after the database accepts it; a deletion before.**
This holds for deleting a save and for deleting a single revision alike. A
tombstone whose deletion then fails to commit is corrected immediately from
the surviving rows, with the next open as the backstop. The
expected-current check is what makes a commit atomic, so for commits the database
decides: a manifest written first could describe a commit that check rejected,
and an invented revision is worse than a missing one. Deletion inverts because
the failure inverts. A deletion recorded late leaves a save the database has
forgotten and the store still offers, and the store is what a restore reads — a
deletion a crash can undo is not a deletion. Both orderings are safe for the
same reason: whatever the database still holds, reconciling on open writes to
the store, so a manifest missed by a crash is rebuilt and a tombstone left by a
failed deletion is cleared.

**Content is reclaimed only against live references.** A deletion decides
inside its transaction what leaves the history, but never what leaves the
disk. Objects are removed afterwards, under the server's one mutation lock,
by re-checking at removal time that nothing references them — a commit or a
media save landing after the transaction keeps its content, because the
reference check and the removal cannot interleave with the write that would
invalidate them. What a crash strands — an object nothing references, a
manifest behind a tombstone, an index row for either — is swept at the next
open, after both repair passes, when the rebuilt database makes deadness
provable.

Revision identifiers stay opaque rather than becoming content hashes. Hashing
manifests would make the history a verifiable Merkle DAG, but it changes the API
and every client's sync baseline, and it is separable from this decision.

## Consequences

Easier:

- Recovering a save needs no Omnisave installation or specialized software: a
  person can read its manifest and decompress its files with commonplace tools.
- Backing up is copying a directory. Every object is named by the hash of its
  content, so the copy verifies with `shasum` and no reference to the original.
- Damage is localized. A partial copy loses the snapshots referencing what is
  missing, not the history.
- Handing saves to someone leaks no credential.

More difficult:

- **Repair is load-bearing, and runs both ways.** Two write paths per change
  means the database and the store can disagree in the window between them.
  Opening repairs both directions: rebuild imports what the store holds and the
  database lacks, then reconcile writes what the database holds and the store
  lacks, so whichever of the two survived is enough. Each direction only adds —
  rebuild never deletes from the database, reconcile never deletes from the
  store — which is what keeps repair safe to run blind, and why every ordering
  decision above exists. The sweep that follows them is the one pass that
  removes, and it may run blind only because they ran first: with the database
  rebuilt, what nothing references and what a tombstone names is provably
  dead. All three passes have to be tested as the correctness mechanism they
  are.
- Rebuild trusts identity wherever it finds it. A lineage whose record was lost
  is reconstructed from the identity its manifests carry — with the caveat that
  a rename after the last commit lived only in the lost record. Tombstones are
  the one thing it will not cross: a tombstoned lineage is never imported, so a
  restore does not resurrect what its owner threw away.
- A failed store write at runtime no longer fails the request it trails — the
  database has already durably accepted the change, and a client told otherwise
  would retry a commit it holds. The store lags instead, loudly in the log,
  until the next open repairs it; a copy of the store taken inside that window
  lacks what could not be written.
- An unreadable record is logged and skipped, by rebuild and reconcile alike —
  the rest of the store still recovers, and refusing to start over one damaged
  file would turn a blemish into an outage.
- A store is many small files — one manifest per revision on top of one object
  per distinct file — and the inode cost is real where they are metered.
- Denormalized identity ages: a renamed game leaves older manifests carrying the
  old title. They are historical records, and the lineage and game records hold
  current truth.
- Restoring only the store gives an unclaimed server. The saves return; browsers
  and Devices re-establish themselves. Game media does not: cover art rests in
  the store as objects no record references, so a rebuilt index cannot name it,
  and it is refetched from providers instead.
- The on-disk format is a compatibility surface with its own version. An older
  binary refuses a newer store rather than guessing, which is why `VERSION`
  exists.
