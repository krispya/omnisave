# Glossary

The canonical vocabulary for Omnisave: UI surfaces, product concepts, authorization terms, and backend infrastructure. One line per entry (occasionally one short paragraph) is just enough to recognize the word and know where to read more.

This document is **also a naming surface**: when we need a name for a thing we're building, we add it here first. That's how vocabulary stays consistent across code, UI, docs, and conversation.

This is **not** a tutorial, design doc, implementation index, or API reference. If a concept needs more than a paragraph, link to the owning FDR or ADR rather than inlining it.

Entries within each section are ordered by **conceptual flow**, with foundational terms first and derivatives after, not alphabetically. See [`.agents/skills/glossary/SKILL.md`](../.agents/skills/glossary/SKILL.md) for the maintenance workflow.

## UI

Names for visible surfaces. When a name here disagrees with a file or component name in the codebase, the glossary wins. The file is the one that should rename.

**Dash**. The browser interface for managing the Library, server settings, pairing, and credentials.

**Client**. The application that runs on each machine where games live. It finds Local Saves and keeps them in sync with the server, from the terminal or as a background service. The Dash is not a client in this sense.

## Product

User-facing concepts. If a user might say the word, it goes here.

**Library**. The Games on the server, whether or not they have saves. Distinct from the _Catalog_ of games that could be added.

**Game**. One server-owned record in the Library. Its identity is the _Evidence_ that clients and providers have attached to it over time. See [FDR-001](fdr/FDR-001-game-identity-resolution.md).

**Omnisave**. One independently named and synchronized game save on the server, with a revision tree and one _Current Revision_. In everyday use it is simply a “save”; native content on a Device is a _Local Save_. See [FDR-005](fdr/FDR-005-save-sync.md).

**Revision**. A content-immutable save snapshot with at most one parent. Its display name can change without changing its content or history. A revision with several children forms an unnamed branch; there are no merges.

**Current Revision**. The one revision an omnisave presently represents, which every bound Device syncs toward; the next commit becomes its child. A write made against a Current Revision that has since moved is refused as a **Current Revision Conflict**, which names the actual one.

**Labeler**. A per-game server script that names a revision from its save content, so every Device sees the same name. See [FDR-007](fdr/FDR-007-revision-labeling.md).

**Restore**. Make any revision in an omnisave's tree current without creating or changing a revision. Restoring onto a sibling branch is a **jump**.

**Fork**. A new omnisave started at an existing revision. It shares that revision and its ancestors, then synchronizes independently. Forks, not branches, are named and bound to Devices.

**Device**. One installation of the Client, such as on a Steam Deck or a desktop. It mints a stable ID and name on first run, so wiping its local state makes a new Device. Its **presence** is its short-lived report of the games it is playing, which only the server ages out ([ADR-013](adr/ADR-013-server-announced-presence-expiry.md)). See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Provenance**. A Game's append-only record of the Devices that have tracked it: when each first tracked it and was last seen, whether it is still installed, and whether it has since untracked it. Only deleting the Game removes it. See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Tracking**. A Device's choice of which discovered games to protect. Tracking a game is what adds it to the Library. See [FDR-002](fdr/FDR-002-game-lifecycle.md).

**Local Save**. One selected native save boundary on a Device, possibly several files; it may be a whole save or a save slot.

**Save Scope**. The content boundary one history protects: a whole save, or one **save slot**, a part of a game's save that can be restored on its own, such as an STS2 profile or a Dark Souls character. The slot number is local destination context, never the history's identity. Distinct from a _Save Profile_, which describes file locations. See [ADR-021](adr/ADR-021-independent-save-boundaries.md).

**Binding**. A Device-local link from one Local Save to one omnisave. Its **sync baseline** is the revision the Local Save is known to equal. See [FDR-003](fdr/FDR-003-automatic-save-binding.md).

**Sync**. Keeping a bound Local Save and its omnisave aligned. Comparing both with the sync baseline decides whether to push, pull, do nothing, or report **divergence**: progress on both sides that needs a person to decide. See [FDR-005](fdr/FDR-005-save-sync.md).

## Authorization

Access vocabulary. Deliberately small. There are no accounts, roles, or users. There is one Owner, and access is expressed as credentials rather than identities (see [ADR-007](adr/ADR-007-per-device-credentials.md)).

**Owner**. The single person a server belongs to. Not an account: ownership is held as credentials. The first browser to reach a server that has issued nothing **claims** it, permanently, and sets the Owner PIN. See [ADR-010](adr/ADR-010-taking-ownership.md).

**Credential**. One issued, revocable bearer token held by a Device, browser, or script. Revoking it disturbs no other. See [ADR-007](adr/ADR-007-per-device-credentials.md).

**Owner PIN**. Four digits every browser after the first proves to be issued a Credential of its own. Short by design, and safe because wrong answers lock sign-in. See [ADR-010](adr/ADR-010-taking-ownership.md).

**Pairing**. How a Device without a Credential asks for one, and the Owner approves it by matching a short code. See [FDR-006](fdr/FDR-006-connecting-a-device.md).

**Owner Token**. The deployment-level recovery and automation credential. It exists apart from the Dash and is never held by a Device. See [ADR-010](adr/ADR-010-taking-ownership.md).

## Backend

Infrastructure jargon. If only contributors say the word, it goes here.

**Artifact**. Content-addressed immutable bytes. A revision maps each save path to an artifact, so identical content is stored once and never rewritten.

**Portable Store**. The tool-independent directory of artifacts, manifests, and records from which save history can be recovered without the server database. A committed deletion is recorded there as an immutable **deletion marker**, so restoring an older copy cannot undo it. See [ADR-012](adr/ADR-012-portable-save-store.md) and [ADR-014](adr/ADR-014-durable-proof-before-forgetting.md).

**Adapter**. The Client's knowledge of one application, such as Steam or RetroArch: it finds that application's installations (**targets**), their games, and their Local Saves. A **Game Save Adapter** is the game-specific counterpart: a sandboxed client extension that finds a game's save slots inside its whole save and names its **ignored files**, which sync never captures, restores, or deletes. See [ADR-021](adr/ADR-021-independent-save-boundaries.md).

**Save Profile**. Where one game keeps its saves, as path rules for each operating system and runtime they apply to. See [ADR-018](adr/ADR-018-embedded-save-profiles.md).

**Catalog**. The games that could be added to the Library, as external providers know them: Hasheous for ROMs, IGDB for everything else. Providers make claims about a game; they never own one.

**Evidence**. What identifies a Game: **identifiers** scoped to a namespace such as `steam.app` or `igdb.game`, content **fingerprints** such as a ROM's hash, and weak hints like a title. Resolving evidence reuses the Game that holds it or creates one; evidence held by two Games is an **identity conflict**, never a merge. See [FDR-001](fdr/FDR-001-game-identity-resolution.md).
