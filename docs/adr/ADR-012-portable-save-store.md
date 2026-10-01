# ADR-012: Keep Saves in a Portable Store

**Date:** 2026-07-27

## Context

[ADR-001](ADR-001-server-authority.md) makes the server the durable authority for saves. Owners need to copy, inspect, and recover those saves without a running Omnisave server, a database, or version-control software: someone holding only the directory and ordinary text and gzip tools must be able to get the original save files out.

Save content is content-addressed: a file's bytes, named by their hash. The store also needs the context that makes those bytes a save: the game and omnisave they belong to, their revision and path, and the order of their history.

## Decision

**One directory is the Portable Store, and it is a tool-independent recovery artifact.** It holds everything needed to recover every save in it: plain JSON records, gzip content named by its SHA-256, and a `VERSION` marker naming the format. The layout and the hand-recovery steps are written once, in [docs/RECOVERY.md](../RECOVERY.md).

**Manifests carry identity rather than referencing it.** Each revision manifest names its game and omnisave, so one manifest read alone says what its files are even if every other record were lost. Path renames applied after a manifest was written live on the omnisave record instead, because manifests are never rewritten ([ADR-019](ADR-019-versioned-replayable-data-migrations.md)).

**Current revisions are recorded.** Each omnisave record carries its Current Revision. It cannot be derived from timestamps or graph tips, because a restore may select any node in the tree.

**Nothing is rewritten in place.** Objects, manifests, and deletion markers are immutable. An omnisave's mutable state lives in its omnisave record, which is replaced atomically.

**Deletion is recorded, not merely performed.** Deleting a game, save, or revision writes an immutable deletion marker, and rebuild never imports what a marker names, so a store restored from backup cannot resurrect it. Because a child's manifest names its parent, only a node the graph no longer needs can be deleted, and a revision shared with a surviving fork outlives the save that made it.

**Server state lives outside the store.** Credentials, pairings, and the owner's PIN ([ADR-007](ADR-007-per-device-credentials.md), [ADR-010](ADR-010-taking-ownership.md)) are not save data. A directory meant to be copied and handed to someone must not carry the way into the server that produced it.

**The database is an index, and the store regrows it.** Opening honors deletion markers, imports what the store holds and the database lacks (games, omnisaves, revisions), and then re-projects the database into the store. SQLite commits first, and the store is written and its content reclaimed only under [ADR-014](ADR-014-durable-proof-before-forgetting.md)'s proof rules.

## Consequences

Easier:

- Recovering a save needs no Omnisave installation or specialized software.
- Backing up is copying a directory, and a copy verifies with `shasum` alone, because every object is named by its content's hash. Damage is localized: a partial copy loses only the snapshots that reference what is missing.
- Handing saves to someone leaks no credential.

More difficult:

- Recovery runs whenever the server opens and is correctness code, tested as such.
- Store records are the acknowledged mutable state. A record that disagrees with a database row overwrites it, and a changed omnisave says so in the log, so restoring an older store beside a newer database rolls the database back, loudly.
- Rebuild trusts identity wherever it finds it. An omnisave whose record was lost is rebuilt from its manifests. It loses any name change made after its last commit and any path migrations, so it is held until a Device proves its locations again. An omnisave is imported even when its game's record is missing, because recovering a save outranks the rule that every save belongs to a Library game.
- Restoring only the store gives an unclaimed server. Saves, games, and their history return. Credentials, pairings, owner settings, Devices, and Provenance do not. Cover art rests in the store as objects no record names, so it is fetched again from providers.
- Denormalized identity ages: a renamed game leaves older manifests carrying the old title. The omnisave and game records hold current truth.
- A store is many small files, one manifest per revision on top of one object per distinct file, and the inode cost is real where inodes are metered.
- Manifests are not content-addressed, so history is not self-verifying. Changing that would change every Device's sync baseline.
- The on-disk format is a compatibility surface with its own version. An older binary refuses a newer store rather than guessing.
