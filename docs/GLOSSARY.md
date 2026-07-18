# Glossary

The canonical vocabulary for Omnisave: UI surfaces, product concepts, authorization terms, and backend infrastructure. One line per entry (occasionally one short paragraph) — just enough to recognize the word and know where to read more.

This document is **also a naming surface**: when we need a name for a thing we're building, we add it here first. That's how vocabulary stays consistent across code, UI, docs, and conversation.

This is **not** a tutorial, design doc, or API reference. If a concept needs more than a paragraph, link to the owning package, [`AGENTS.md`](../AGENTS.md), or a future FDR/ADR rather than inlining.

Entries within each section are ordered by **conceptual flow** — foundational terms first, derivatives after — not alphabetically. See [`.agents/skills/glossary/SKILL.md`](../.agents/skills/glossary/SKILL.md) for the maintenance workflow.

## UI

Names for visible surfaces. When a name here disagrees with a file or component name in the codebase, the glossary wins — the file is the one that should rename.

**Dash** — The web app (`apps/dash`) for managing the server: connection, the Library, and debug features. Where Omnisaves are created and browsed.

**Client CLI** — The terminal surface of the client binary: the `connect`, `scan`, `track`, and `bind` commands and the prompts they render (`internal/client/tui`). `connect` verifies and persists the server connection so the other commands need no token or URL.

## Product

User-facing concepts. If a user might say the word, it goes here.

**Library** — The user's collection of Games on the server — every Game resolved in, with or without saves. Dash's main surface (the poster wall) shows it. UI copy says "library", never "catalog" (see _Catalog_, Backend).

**Game** — One server-owned canonical record in the Library, with its own UUID. Identifiers and fingerprints accumulate on a Game over time as clients and providers contribute evidence (see _Evidence_, Backend).

**Omnisave** — One independently versioned game save on the server; the unit users create, name, fork, and bind to. Several Omnisaves can exist for the same Game — separate playthroughs or forked lineages.

**Revision** — An immutable state in an Omnisave's linear history, committed as file upserts/deletes against an expected head. The newest revision is the **head**; a commit naming a stale head is rejected (see _Head Conflict_, Backend).

**Fork** — A new Omnisave started from an existing revision's snapshot. The fork records its origin (source Omnisave and revision) and then versions independently.

**Device** — One self-identified machine running the Client, e.g. a Steam Deck or a desktop. A device mints a stable ID and a human-readable name on first run, kept in local tracking state and reported to the server; identity belongs to the client installation, so wiping local state makes a new Device. A Device hosts Targets (see _Target_, Backend); it is not one itself. See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Provenance** — A Game's append-only record of the Devices that have tracked it: per device, when it was first tracked, when it was last seen, whether the install is still present, and whether it has since been untracked. Provenance survives untracking, uninstalls, and deletion of every Omnisave; only deleting the Game removes it. Distinct from a save's fork lineage (see _Fork_). See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Scan** — A read-only discovery pass across adapters, reporting each target, its installed games, and their current saves without modifying anything. Run with `omnisave-client scan`.

**Tracking** — A Device's selection of which discovered games to synchronize, chosen in the `track` prompt and persisted in local tracking state (`client.json` in the user config directory). Tracking is what makes Omnisave aware of a game: at track time the game is resolved from the Catalog into the Library, before any save is bound. Games that disappear from a scan stay tracked until explicitly unselected.

**Local Save** — One adapter-native save set discovered on this machine; may contain multiple files.

**Binding** — A machine-local mapping from one Local Save to one Omnisave, created with `omnisave-client bind`. A binding records the last successfully synced revision — the **sync baseline** — which stays empty until a synchronization completes.

## Authorization

Access vocabulary. Deliberately small — there are no user accounts or roles yet, so this section grows with them.

**API Token** — The single bearer token protecting the server HTTP API (`internal/omnisave/httpapi`), compared in constant time; clients supply it via `OMNISAVE_API_TOKEN`.

## Backend

Infrastructure jargon. If only contributors say the word, it goes here.

**Server** — The self-hosted Go service (NAS-first) that owns the Library, Omnisaves, and artifacts, exposed as an HTTP API under `/api/v1`. The server is the source of truth; clients hold only machine-local state.

**Client** — The portable `omnisave-client` binary that runs where games live (Steam Deck first). It scans, tracks, binds, and syncs, talking to one server through the authenticated `remote` HTTP client (`internal/client/remote`). One self-identified installation of it is a _Device_ (Product).

**Repository** — The server's persistence boundary: Omnisave records, Game records, and artifact storage behind one interface (`internal/storage`), implemented on SQLite.

**Artifact** — Content-addressed immutable bytes keyed by SHA-256, with a format and size. Revisions reference artifacts through **revision files** — canonical save path → artifact — so identical content is stored once and never rewritten.

**Head Conflict** — Rejection of a revision commit whose expected head is no longer the Omnisave's actual head; optimistic concurrency for save history. The error carries the actual head so a client can reconcile.

**Adapter** — Client component that discovers application targets, their installed games, and native saves. Two exist today: `retroarch`, which maps emulated platforms to playlist names and save extensions through per-platform profiles (SNES first), and `steam`.

**Target** — One application installation resolved on this machine (e.g. a RetroArch install). Targets are found by **locators**, one per install type: Steam library, application bundle, or standalone installer.

**Installed Game** — One game exposed through a target, carrying identity evidence (see _Evidence_), an install root, and the environment it runs in.

**Environment** — Where an installed game runs: host OS, runtime (native or Proton), home, store root, and prefix root. Save-location rules expand against the environment — under Proton, Windows paths resolve inside the prefix (`drive_c/…`).

**Save Profile** — Provider-neutral save-location knowledge for one game, expressed as **rules** — each a templated path plus the OS/store where it applies (Windows rules also apply under Proton). Currently sourced from the community Ludusavi manifest, keyed by Steam ID (`internal/client/saveprofile`).

**Catalog** — The provider-hosted universe of known games: what exists and can be matched against, not what the user has (that's the _Library_, Product). `internal/catalog` currently names the server's local cache of resolved identity and metadata — Games, exact ROM signatures (**GameROM**), provider media with attribution — pending rename.

**Catalog Provider** — External service hosting the catalog, consulted to learn what games exist: resolves evidence into identity and metadata claims, searches by title, and serves media. The providers include Hasheous for ROMs and IGDB for everything else.

**Evidence** — What a client submits to resolve a Game: **identifiers** (external IDs scoped to a namespace — `steam.app`, `igdb.game`, `hasheous.game`), **fingerprints** (exact-content hashes `{platform, algorithm, value}` for ROM matching), and weak descriptive hints (title, platform).

**Resolution** — The reuse-or-create outcome of resolving evidence into a canonical Game, reported as `existing` or `created`. Resolution fails with an **identity conflict** when supplied evidence is already assigned to different Games.

**Selection Token** — Provider-owned token attached to each search candidate; redeeming it via match applies that provider's claim to a game without requiring a local fingerprint.
