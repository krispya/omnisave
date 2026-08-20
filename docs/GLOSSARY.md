# Glossary

The canonical vocabulary for Omnisave: UI surfaces, product concepts, authorization terms, and backend infrastructure. One line per entry (occasionally one short paragraph) is just enough to recognize the word and know where to read more.

This document is **also a naming surface**: when we need a name for a thing we're building, we add it here first. That's how vocabulary stays consistent across code, UI, docs, and conversation.

This is **not** a tutorial, design doc, implementation index, or API reference. If a concept needs more than a paragraph, link to the owning FDR or ADR rather than inlining it.

Entries within each section are ordered by **conceptual flow**, with foundational terms first and derivatives after, not alphabetically. See [`.agents/skills/glossary/SKILL.md`](../.agents/skills/glossary/SKILL.md) for the maintenance workflow.

## UI

Names for visible surfaces. When a name here disagrees with a file or component name in the codebase, the glossary wins. The file is the one that should rename.

**Dash**. The browser interface for managing the Library, server settings, pairing, and credentials.

**Client CLI**. The terminal interface for connecting a Device, selecting games, and synchronizing Local Saves.

## Product

User-facing concepts. If a user might say the word, it goes here.

**Library**. The user's collection of Games on the server, including Games with or without saves. Distinct from the provider-hosted _Catalog_ of games that could be added.

**Game**. One server-owned canonical record in the Library, with its own UUID. Identifiers and fingerprints accumulate on a Game over time as clients and providers contribute evidence (see _Evidence_, Backend).

**Omnisave**. One independently named and synchronized game save on the server. It owns a revision tree and one global _Current Revision_. Forking creates another Omnisave when progress must synchronize independently. In everyday use it is simply a “save”; native content on a Device is a _Local Save_. See [FDR-003](fdr/FDR-003-automatic-save-binding.md) and [FDR-005](fdr/FDR-005-save-sync.md).

**Revision**. A content-immutable save snapshot with at most one parent. A revision may have a mutable display name shared by every fork that shares the node. Naming changes no content or history. Revisions form unnamed branches naturally when one node has several children, and Omnisave does not support merges.

**Current Revision**. The one revision an omnisave presently represents and every bound Device synchronizes toward. Restoring another node moves this global pointer without changing either revision. The next commit becomes a child of the selected node. A **tip** is any revision without children and need not be current.

**Labeler**. A per-game server script that names a revision from its save content at commit or when explicitly rerun on existing history, so every device sees the same name. See [FDR-007](fdr/FDR-007-revision-labeling.md).

**Achievement Mark**. A game achievement placed on the first revision committed at or after it was unlocked, so a history shows where in a playthrough it happened: at that revision it was earned, and before it, not yet. Marks belong to an omnisave rather than to the snapshot, and cover only unlocks a Device watched happen. See [FDR-008](fdr/FDR-008-achievement-marks.md).

**Restore**. Make any revision in an omnisave's tree current without creating or changing a revision. Moving to an ancestor is a **rewind**, moving to a descendant is a **fast-forward**, and moving between sibling branches is a **jump**.

**Fork**. A new omnisave started at an existing revision. It shares that node, its name, and its ancestor path, then synchronizes independently. Later revisions belong only to the omnisave that creates them. Forks, rather than branches, are named and bound to Devices.

**Device**. One self-identified machine running the Client, e.g. a Steam Deck or a desktop. A device mints a stable ID and a human-readable name on first run, kept in local tracking state and reported to the server. Identity belongs to the client installation, so wiping local state makes a new Device. A Device hosts Targets (see _Target_, Backend), but it is not one itself. See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Provenance**. A Game's append-only record of the Devices that have tracked it: per device, when it was first tracked, when it was last seen, whether the install is still present, and whether it has since been untracked. Provenance survives untracking, uninstalls, and deletion of every omnisave. Only deleting the Game removes it. It is distinct from a save's fork lineage (see _Fork_). See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Scan**. A read-only discovery pass across adapters that reports Targets, installed Games, and Local Saves without changing them.

**Tracking**. A Device's durable selection of which discovered Games to synchronize. Tracking resolves a Game from the Catalog into the Library before binding its saves. See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Local Save**. One adapter-native save set discovered on this machine. It may contain multiple files.

**Unmatched Local Save**. A Local Save with no Binding whose content matches no Revision in any of the Game's existing saves. See [FDR-003](fdr/FDR-003-automatic-save-binding.md).

**Binding**. A Device-local mapping from one Local Save to one Omnisave. Its **sync baseline** is the revision whose content the Local Save is known to equal. See [FDR-003](fdr/FDR-003-automatic-save-binding.md).

**Sync**. Reconciliation that keeps a bound Local Save and its Omnisave aligned. The sync baseline determines whether to push, pull, do nothing, or report **divergence**—progress on both sides that requires a lineage decision. See [FDR-005](fdr/FDR-005-save-sync.md).

## Authorization

Access vocabulary. Deliberately small. There are no accounts, roles, or users. There is one Owner, and access is expressed as credentials rather than identities (see [ADR-007](adr/ADR-007-per-device-credentials.md)).

**Owner**. The single person a server belongs to. Not an account and not a login: ownership is held as credentials, and a server acquires an owner the first time someone claims it.

**Credential**. One issued, revocable bearer token held by a Device, browser, or script. Revoking it does not disturb any other client. See [ADR-007](adr/ADR-007-per-device-credentials.md).

**Claiming**. The first browser to reach a server that has issued nothing takes ownership of it, from the local network and without a secret, and chooses the Owner PIN while doing so. Permanent and one-shot: a claimed server refuses every later claim. See [ADR-010](adr/ADR-010-taking-ownership.md).

**Owner PIN**. Four digits, one per server, chosen when the server is claimed. Every browser after the first proves it to be issued a Credential of its own. Short by design and safe by refusal rather than by length: wrong answers are counted per source address and across the server, and sign-in locks for escalating periods. See [ADR-010](adr/ADR-010-taking-ownership.md).

**Pairing**. How a Device without a Credential requests one and an already trusted Owner approves the matching short code. See [FDR-006](fdr/FDR-006-connecting-a-device.md).

**Owner Token**. The deployment-level recovery and automation credential that exists independently of the Dash and is never carried by a Device. See [ADR-010](adr/ADR-010-taking-ownership.md).

## Backend

Infrastructure jargon. If only contributors say the word, it goes here.

**Server**. The self-hosted authority that owns the Library, Omnisaves, and Artifacts. Clients retain only Device-local state.

**Client**. The portable application that runs where games live, discovers and synchronizes saves, and communicates with one Server. One self-identified installation is a _Device_.

**Repository**. The server's persistence boundary for Game, Omnisave, revision, and Artifact state.

**Portable Store**. The tool-independent directory of gzip objects, JSON manifests and records, and immutable Deletion Markers from which save history can regrow without the server database. See [ADR-012](adr/ADR-012-portable-save-store.md).

**Store Outbox**. SQLite's ordered queue of self-contained Portable Store projections, committed in the same transaction as the state they describe and replayed before recovery. See [ADR-014](adr/ADR-014-durable-proof-before-forgetting.md).

**Deletion Marker**. An immutable Portable Store record proving that deletion of one Game, omnisave, or revision committed. Recovery and reclamation never infer that fact from absence. See [ADR-014](adr/ADR-014-durable-proof-before-forgetting.md).

**Deletion Ledger**. SQLite's online record of deleted identifiers, committed with the logical delete so concurrent processes cannot reuse an identifier before its Deletion Marker reaches the Portable Store. See [ADR-014](adr/ADR-014-durable-proof-before-forgetting.md).

**Artifact**. Content-addressed immutable bytes keyed by SHA-256, with a format and size. Revisions reference artifacts through **revision files**, which map a canonical save path to an artifact, so identical content is stored once and never rewritten.

**Artifact Registry**. SQLite's record of Artifacts whose bytes have been verified and may safely be referenced. A direct check of the Portable Store cannot prevent bytes from being reclaimed between the check and the database commit. The registry lets SQLite serialize reference creation with reclamation and close that race. See [ADR-014](adr/ADR-014-durable-proof-before-forgetting.md).

**Current Revision Conflict**. Rejection of a commit or restore whose expected Current Revision is no longer the omnisave's actual Current Revision. It provides optimistic concurrency for save history. The error carries the actual revision so a client can reconcile without a stale Device silently reactivating an old branch.

**Adapter**. A Client component that discovers application Targets, their installed Games, and native Local Saves.

**Target**. One application installation resolved on this machine (e.g. a RetroArch install). Targets are found by **locators**, one per install type: Steam library, application bundle, or standalone installer.

**Installed Game**. One game exposed through a target, carrying identity evidence (see _Evidence_), an install root, and the environment it runs in.

**Environment**. The host, runtime, and filesystem context in which an Installed Game runs and against which save-location rules are expanded.

**Save Profile**. Provider-neutral save-location knowledge for one Game, expressed as path rules for the environments where they apply. See [ADR-018](adr/ADR-018-embedded-save-profiles.md).

**Catalog**. The provider-hosted universe of Games that can be identified and added, distinct from the user's _Library_.

**Catalog Provider**. External service hosting the catalog, consulted to learn what games exist: resolves evidence into identity and metadata claims, searches by title, and serves media. The providers include Hasheous for ROMs and IGDB for everything else.

**Evidence**. What a client submits to resolve a Game: **identifiers** (external IDs scoped to a namespace such as `steam.app`, `igdb.game`, or `hasheous.game`), **fingerprints** (exact-content hashes `{platform, algorithm, value}` for ROM matching), and weak descriptive hints (title, platform).

**Resolution**. Reusing or creating a canonical Game from identity Evidence. An **identity conflict** occurs when supplied evidence already belongs to different Games.

**Selection Token**. Provider-owned token attached to each search candidate. Redeeming it via match applies that provider's claim to a game without requiring a local fingerprint.
