# Glossary

The canonical vocabulary for Omnisave: UI surfaces, product concepts, authorization terms, and backend infrastructure. One line per entry (occasionally one short paragraph) is just enough to recognize the word and know where to read more.

This document is **also a naming surface**: when we need a name for a thing we're building, we add it here first. That's how vocabulary stays consistent across code, UI, docs, and conversation.

This is **not** a tutorial, design doc, or API reference. If a concept needs more than a paragraph, link to the owning package, [`AGENTS.md`](../AGENTS.md), or a future FDR/ADR rather than inlining.

Entries within each section are ordered by **conceptual flow**, with foundational terms first and derivatives after, not alphabetically. See [`.agents/skills/glossary/SKILL.md`](../.agents/skills/glossary/SKILL.md) for the maintenance workflow.

## UI

Names for visible surfaces. When a name here disagrees with a file or component name in the codebase, the glossary wins. The file is the one that should rename.

**Dash**. The web app (`apps/dash`) for managing the server: the Library, server settings, and debug features. Where omnisaves are created and browsed, where a Device's Pairing request is approved, and where Credentials are listed and revoked.

**Client CLI**. The terminal surface of the client binary: the `connect`, `scan`, `track`, `sync`, `watch`, and `bind` commands and the prompts they render (`internal/client/tui`). `connect` finds a server, pairs with it, and persists the Credential the Owner approved, so the other commands need no token or URL. `sync` and `watch` are headless and never prompt.

## Product

User-facing concepts. If a user might say the word, it goes here.

**Library**. The user's collection of Games on the server. It includes every Game resolved in, with or without saves. Dash's main surface (the poster wall) shows it. UI copy says "library", never "catalog" (see _Catalog_, Backend).

**Game**. One server-owned canonical record in the Library, with its own UUID. Identifiers and fingerprints accumulate on a Game over time as clients and providers contribute evidence (see _Evidence_, Backend).

**Omnisave**. One independently named and synchronized game save on the server. It owns a revision tree and one global _Current Revision_. Restoring moves that pointer. Forking creates another omnisave when progress must synchronize independently. In everyday use it is just "save." An adapter-native save on disk is always a _Local Save_. An omnisave always carries a display name. The server assigns one when creation omits it ("Save N," while forks inherit the source name plus " (fork)"), and numbers a requested name the game's saves already carry ("Save 1 (Steam Deck) 2"). A name can be changed but never cleared (see [FDR-003](fdr/FDR-003-automatic-save-binding.md) and [FDR-005](fdr/FDR-005-save-sync.md)).

**Revision**. A content-immutable save snapshot with at most one parent. A revision may have a mutable display name shared by every fork that shares the node. Naming changes no content or history. Revisions form unnamed branches naturally when one node has several children, and Omnisave does not support merges.

**Current Revision**. The one revision an omnisave presently represents and every bound Device synchronizes toward. Restoring another node moves this global pointer without changing either revision. The next commit becomes a child of the selected node. A **tip** is any revision without children and need not be current.

**Labeler**. A per-game script that names a revision from its save content at commit running on the server so every device sees the same name. A labeler with nothing to say leaves the revision unnamed, and a name a person chooses by hand always outranks the labeler's. See [FDR-007](fdr/FDR-007-revision-labeling.md).

**Achievement Mark**. A game achievement placed on the first revision committed at or after it was unlocked, so a history shows where in a playthrough it happened: at that revision it was earned, and before it, not yet. Marks belong to an omnisave rather than to the snapshot, and cover only unlocks a Device watched happen. See [FDR-008](fdr/FDR-008-achievement-marks.md).

**Restore**. Make any revision in an omnisave's tree current without creating or changing a revision. Moving to an ancestor is a **rewind**, moving to a descendant is a **fast-forward**, and moving between sibling branches is a **jump**.

**Fork**. A new omnisave started at an existing revision. It shares that node, its name, and its ancestor path, then synchronizes independently. Later revisions belong only to the omnisave that creates them. Forks, rather than branches, are named and bound to Devices.

**Device**. One self-identified machine running the Client, e.g. a Steam Deck or a desktop. A device mints a stable ID and a human-readable name on first run, kept in local tracking state and reported to the server. Identity belongs to the client installation, so wiping local state makes a new Device. A Device hosts Targets (see _Target_, Backend), but it is not one itself. See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Provenance**. A Game's append-only record of the Devices that have tracked it: per device, when it was first tracked, when it was last seen, whether the install is still present, and whether it has since been untracked. Provenance survives untracking, uninstalls, and deletion of every omnisave. Only deleting the Game removes it. It is distinct from a save's fork lineage (see _Fork_). See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Scan**. A read-only discovery pass across adapters, reporting each target, its installed games, and their current saves without modifying anything. Run with `omnisave scan`.

**Tracking**. A Device's selection of which discovered games to synchronize, chosen in the `track` prompt and persisted in local tracking state (`client.json` in the user config directory). Tracking is what makes Omnisave aware of a game. At track time the game is resolved from the Catalog into the Library before any save is bound. Games that disappear from a scan stay tracked until explicitly unselected, or until a track run learns the game was deleted on the server (see [FDR-002](fdr/FDR-002-game-lifecycle.md)).

**Local Save**. One adapter-native save set discovered on this machine. It may contain multiple files.

**Binding**. A machine-local mapping from one Local Save to one omnisave. Tracking creates bindings automatically when it seeds a new omnisave or finds one lineage with matching current content. `omnisave bind` remains available for corrections. A binding records the revision whose content the Local Save is known to equal, called the **sync baseline**. A manual binding to non-matching content has no baseline and starts life diverged (see _Sync_).

**Sync**. The pass keeping a bound save and its omnisave equal: local progress commits up as revisions, server progress applies down to disk, and the sync baseline arbitrates which direction is safe. New progress on both sides is **divergence** only when the Current Revision has advanced beyond the baseline; sync never resolves that on its own, and a person answers — from a track run or from the watch view — both answers keeping everything: **fork** continues the local progress as a new omnisave named for the Device, while **jump to current** keeps unsynced progress as a Device-named branch inside the same omnisave and adopts the Current Revision — content the history already holds needs no copy either way. Local progress beside a Current Revision that was restored backwards or sideways is committed as a branch instead, with nothing to ask. Runs once via `omnisave sync` and continuously via `omnisave watch`. See [FDR-005](fdr/FDR-005-save-sync.md).

## Authorization

Access vocabulary. Deliberately small. There are no accounts, roles, or users. There is one Owner, and access is expressed as credentials rather than identities (see [ADR-007](adr/ADR-007-per-device-credentials.md)).

**Owner**. The single person a server belongs to. Not an account and not a login: ownership is held as credentials, and a server acquires an owner the first time someone claims it.

**Credential**. One issued, revocable bearer token held by exactly one client, whether a Device, a browser, or a script. The server stores it hashed and can never recover it, records when it was last used, and can withdraw it without disturbing any other. Every way into a server ends at one of these, which is why the list in the Dash is the complete answer to what can reach it. See [ADR-007](adr/ADR-007-per-device-credentials.md).

**Claiming**. The first browser to reach a server that has issued nothing takes ownership of it, from the local network and without a secret, and chooses the Owner PIN while doing so. Permanent and one-shot: a claimed server refuses every later claim. See [ADR-010](adr/ADR-010-taking-ownership.md).

**Owner PIN**. Four digits, one per server, chosen when the server is claimed. Every browser after the first proves it to be issued a Credential of its own. Short by design and safe by refusal rather than by length: wrong answers are counted per source address and across the server, and sign-in locks for escalating periods. See [ADR-010](adr/ADR-010-taking-ownership.md).

**Pairing**. How a Device with no Credential gets one: it asks, displays a short **code**, and waits while holding a long **handle** that is the only thing able to collect the credential. The Owner approves the request whose code matches the screen in front of them. A request's name and address are supplied by whoever sent it, so the code is the only part that ties it to the Device that sent it. See [FDR-006](fdr/FDR-006-connecting-a-device.md).

**Owner Token**. The deployment-level credential, set with `OMNISAVE_TOKEN` (or `OMNISAVE_TOKEN_FILE`) and generated beside the database when neither is. It is the way in that never depends on the Dash: claiming from off the network, recovering a forgotten PIN, and automation. Never throttled, never locked, and never carried by a Device. See [ADR-010](adr/ADR-010-taking-ownership.md).

## Backend

Infrastructure jargon. If only contributors say the word, it goes here.

**Server**. The self-hosted Go service (NAS-first) that owns the Library, omnisaves, and artifacts, exposed as an HTTP API under `/api/v1`. The server is the source of truth. Clients hold only machine-local state.

**Client**. The portable `omnisave` binary that runs where games live (Steam Deck first). It scans, tracks, binds, and syncs, talking to one server through the authenticated `remote` HTTP client (`internal/client/remote`). One self-identified installation of it is a _Device_ (Product).

**Repository**. The server's persistence boundary: Omnisave records, Game records, and artifact storage behind one interface (`internal/storage`), implemented on SQLite.

**Portable Store**. The tool-independent directory of gzip objects, JSON manifests and records, and immutable Deletion Markers from which save history can regrow without the server database. See [ADR-012](adr/ADR-012-portable-save-store.md).

**Store Outbox**. SQLite's ordered queue of self-contained Portable Store projections, committed in the same transaction as the state they describe and replayed before recovery. See [ADR-014](adr/ADR-014-durable-proof-before-forgetting.md).

**Deletion Marker**. An immutable Portable Store record proving that deletion of one Game, omnisave, or revision committed. Recovery and reclamation never infer that fact from absence. See [ADR-014](adr/ADR-014-durable-proof-before-forgetting.md).

**Deletion Ledger**. SQLite's online record of deleted identifiers, committed with the logical delete so concurrent processes cannot reuse an identifier before its Deletion Marker reaches the Portable Store. See [ADR-014](adr/ADR-014-durable-proof-before-forgetting.md).

**Artifact**. Content-addressed immutable bytes keyed by SHA-256, with a format and size. Revisions reference artifacts through **revision files**, which map a canonical save path to an artifact, so identical content is stored once and never rewritten.

**Artifact Registry**. SQLite's record of Artifacts whose bytes have been verified and may safely be referenced. A direct check of the Portable Store cannot prevent bytes from being reclaimed between the check and the database commit. The registry lets SQLite serialize reference creation with reclamation and close that race. See [ADR-014](adr/ADR-014-durable-proof-before-forgetting.md).

**Current Revision Conflict**. Rejection of a commit or restore whose expected Current Revision is no longer the omnisave's actual Current Revision. It provides optimistic concurrency for save history. The error carries the actual revision so a client can reconcile without a stale Device silently reactivating an old branch.

**Adapter**. Client component that discovers application targets, their installed games, and native saves. Two exist today: `retroarch`, which maps emulated platforms to playlist names and save extensions through per-platform profiles (SNES first), and `steam`.

**Target**. One application installation resolved on this machine (e.g. a RetroArch install). Targets are found by **locators**, one per install type: Steam library, application bundle, or standalone installer.

**Installed Game**. One game exposed through a target, carrying identity evidence (see _Evidence_), an install root, and the environment it runs in.

**Environment**. Where an installed game runs: host OS, runtime (native or Proton), home, store root, and prefix root. Save-location rules expand against the environment. Under Proton, Windows paths resolve inside the prefix (`drive_c/…`).

**Save Profile**. Provider-neutral save-location knowledge for one game, expressed as **rules**. Each rule is a templated path plus the OS/store where it applies (Windows rules also apply under Proton). Currently sourced from the community Ludusavi manifest, keyed by Steam ID (`internal/client/saveprofile`), pruned and compiled into the client binary (see [ADR-018](adr/ADR-018-embedded-save-profiles.md)).

**Catalog**. The provider-hosted universe of known games: what exists and can be matched against, not what the user has (that's the _Library_, Product). `internal/catalog` currently names the server's local cache of resolved identity and metadata. This includes Games, exact ROM signatures (**GameROM**), and provider media with attribution. The code is pending rename.

**Catalog Provider**. External service hosting the catalog, consulted to learn what games exist: resolves evidence into identity and metadata claims, searches by title, and serves media. The providers include Hasheous for ROMs and IGDB for everything else.

**Evidence**. What a client submits to resolve a Game: **identifiers** (external IDs scoped to a namespace such as `steam.app`, `igdb.game`, or `hasheous.game`), **fingerprints** (exact-content hashes `{platform, algorithm, value}` for ROM matching), and weak descriptive hints (title, platform).

**Resolution**. The reuse-or-create outcome of resolving evidence into a canonical Game, reported as `existing` or `created`. Resolution fails with an **identity conflict** when supplied evidence is already assigned to different Games.

**Selection Token**. Provider-owned token attached to each search candidate. Redeeming it via match applies that provider's claim to a game without requiring a local fingerprint.
