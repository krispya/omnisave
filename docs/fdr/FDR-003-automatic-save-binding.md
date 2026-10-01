# FDR-003: Automatic Save Binding

**Status:** Experimental **Last reviewed:** 2026-09-30

## Overview

Binding connects a Device's saves to omnisaves during tracking and every sync pass. A Local Save seeds a new omnisave, rejoins one by exact content match, or waits for a person when more than one safe future remains. A game with no Local Save can have an existing omnisave placed on the Device, by explicit choice. The result is a sync baseline later passes use without guessing.

## Behavior

### A Local Save without a binding

- Every unbound Local Save of a tracked game goes through binding. A pass binds only games whose server records it confirmed, so a tracking failure cannot seed an omnisave.
- A Device binds the folder the game itself reads and writes, never a store's cloud mirror of it (decision 10).
- If the game has no omnisaves, the local content seeds one and that revision becomes the sync baseline.
- Otherwise the content is matched exactly against the full history of every omnisave for the game, across the per-OS spellings of its save location (decision 11).
- One match at a Current Revision rebinds automatically. One match at any other revision asks whether to adopt the Current Revision or fork from the match.
- A save matching no omnisave, or several, asks whether to bind a match, adopt another omnisave after the local content is preserved, or create a new omnisave. Omnisaves whose Current Revision cannot land in this save's layout are not offered. When nothing is left to offer, a new omnisave is created without asking, attended or not, unless one of the game's omnisaves is held for migration ([FDR-005](FDR-005-save-sync.md), decision 14): splitting its history waits for a person.
- Unattended passes never ask; the save waits, reported, for an interactive run. There is no ignore answer, since tracking is the intent to synchronize, and leaving a question changes nothing.
- Each Local Save binds independently, so one game may seed or bind several omnisaves.
- Manual rebinding offers only omnisaves of the save's own Game and records no baseline.
- If a bound omnisave was deleted, the binding is dropped and surviving omnisaves are matched by content. When none survive, the deletion wins and the game is untracked on that Device ([FDR-002](FDR-002-game-lifecycle.md), decision 10).

### A game with no Local Save

- A tracked game with no local content is offered its existing omnisaves, even when there is only one. Choosing one places its Current Revision in the game's own save folder and records it as the baseline. Declining, or an unattended pass, places nothing; the offer returns on the next interactive run.
- An omnisave is offered only when the Device's save-location knowledge names exactly one destination its Current Revision fits, and never while it is held for migration.
- A Windows game run through Wine, under Proton or GameHub, keeps its save folders inside its prefix. Until the launcher has created that prefix, the game has no destination to offer.
- Placement is verified before it lands, never overwrites content that appeared after discovery, leaves nothing partial on failure, and finishes in the store's registry where the game needs that ([FDR-005](FDR-005-save-sync.md), decision 13).

## Design Decisions

### 1. Binding is automatic; asking is the exception

**Decision:** Tracking includes binding, which asks only when content matching leaves more than one safe outcome. When exactly one remains, attended and unattended passes both take it. **Why:** Tracking is the user's intent to protect a game. A second routine step would leave saves unprotected through omission rather than choice. **Tradeoff:** Tracking may create server history as part of completing that intent.

### 2. A game with no omnisaves seeds automatically

**Decision:** Local content seeds the first omnisave without confirmation. **Why:** With no server-side playthrough there is nothing to conflict with. **Tradeoff:** Two Devices seeding simultaneously may create two independent omnisaves; both remain valid and visible.

### 3. Placing onto a Device with no Local Save is explicit and starts at current

**Decision:** An omnisave is placed on a Device with no Local Save only when a person chooses it, even as the only candidate, and placement starts from its Current Revision. **Why:** This writes into the folder the game will load, which merits consent that seeding a server copy does not. Starting anywhere but current would silently begin a different history. **Tradeoff:** A fresh Device needs one interaction before play continues, and older revisions are reached by restoring ([FDR-005](FDR-005-save-sync.md)).

### 4. Matching means exact content equality across full history

**Decision:** A Local Save matches only a revision with the same file set and content, and historical revisions count as well as the Current Revision. **Why:** Exact matching recovers a binding after a Device was offline or lost its local state without risking a false attachment. **Tradeoff:** Near-matches require a decision, and matching work grows with history.

### 5. A match at a non-current revision requires adopting current or forking

**Decision:** A save matching a revision other than the Current Revision, behind it or ahead of it after a restore, is never moved silently. The user adopts the Current Revision or forks from the matched revision. **Why:** Continuing both on one omnisave would immediately conflict. Both outcomes are lossless because the matched revision already exists on the server. **Tradeoff:** A safe but meaningful choice interrupts an otherwise automatic flow.

### 6. Unmatched and ambiguous saves are never guessed

**Decision:** Automatic binding requires exactly one proven omnisave. Otherwise a person binds a match, adopts another omnisave, or creates a new one, unless creating one is the only safe outcome left (decision 1). **Why:** A wrong guess would extend the wrong playthrough, and an ignore state would contradict tracking. **Tradeoff:** Some saves need interaction before synchronization can begin.

### 7. No local content means no seed; any local content means matching

**Decision:** A game without local content never seeds an empty omnisave, and placement (decision 3) is available only while the Device has no Local Save. **Why:** An empty omnisave cannot tell an unplayed game from missing data, and only without local content can placement not discard progress. **Tradeoff:** Protection begins after the game writes a save, and even unwanted local content rules out placement until it binds normally.

### 8. Every omnisave has a server-owned display name

**Decision:** The server assigns a non-empty, game-unique display name when an omnisave is created or forked. Fork names retain enough source and Device context to distinguish independent playthroughs. **Why:** A name assigned once by the authority remains consistent everywhere the save appears. **Tradeoff:** Generated names are descriptive labels, not stable identifiers, and may be reused after deletion.

### 9. Adopting an omnisave preserves first and finishes in the same run

**Decision:** Unmatched local content adopting an existing omnisave is first preserved as its own omnisave; the selected Current Revision is then applied and bound in the same run. An adoption whose Current Revision cannot land in the save's layout is refused before anything is preserved, and one that fails afterwards follows [FDR-005](FDR-005-save-sync.md), decision 4. **Why:** The conflict is already known, so deferring it to a divergence question would ask twice. **Tradeoff:** Preservation leaves another omnisave the user may later delete.

### 10. A store's cloud mirror is a transport, never a save

**Decision:** A Device binds, places into, and snapshots only the folder the game itself reads and writes, located by save-location rules ([ADR-018](../adr/ADR-018-embedded-save-profiles.md)), never a store's cloud mirror of it. A game no rule can place has no save on the Device and is reported that way. **Why:** A mirror fills file by file, so its snapshot can be a state the game never held, and a file placed there reaches neither the game nor the store registry an API-synced game trusts ([FDR-005](FDR-005-save-sync.md), decision 13). Neither failure shows in the mirror's own files, so only the categorical rule is safe; reporting no save location beats appearing to protect a game. **Tradeoff:** An API-synced game no rule can place goes visibly unprotected. Devices lose the mirror's identical layout everywhere and depend on per-OS rules (decision 11). The game's folder also holds backups, other profiles, and logs, so a Device tracks more files and commits more often.

### 11. An omnisave speaks one location vocabulary; Devices translate

**Decision:** A save resolved from save-location rules carries the identity of every spelling its rules give its location across OSes. A save and a revision that each spell exactly one location the save knows are compared, adopted, and placed as the same place, and a commit keeps the omnisave's original spelling. Anything else stays strict. **Why:** The games Omnisave most exists for are ones Steam does not sync, and their rules spell one save folder differently per OS; without translation, one playthrough could never follow the player between a desktop and a Steam Deck. Requiring a known identity keeps translation inside one game's rules, and keeping the spelling means a history never mixes vocabularies. **Tradeoff:** Rules that name several locations, or anchor at different depths per OS, split per OS. An ambiguous single-file placement is refused rather than guessed. A manifest refresh that rewords a template strands older revisions until content is committed under the new wording.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — the server judges creation and matching claims while clients originate content; [ADR-018](../adr/ADR-018-embedded-save-profiles.md) — the save-location rules binding and placement rely on.
- **FDRs:** [FDR-001](FDR-001-game-identity-resolution.md) — game resolution precedes binding; [FDR-002](FDR-002-game-lifecycle.md) — the lifecycle binding completes; [FDR-005](FDR-005-save-sync.md) — synchronization once a baseline exists, and the migration hold.
