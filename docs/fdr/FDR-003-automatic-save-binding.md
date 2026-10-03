# FDR-003: Automatic Save Binding

**Status:** Experimental **Last reviewed:** 2026-10-03

## Overview

Binding connects a Device's saves to omnisaves during tracking and every sync pass. A Local Save seeds a new omnisave, rejoins one by exact content match, or waits for a person when more than one safe future remains. An empty native destination, a game with no Local Save or an empty save slot, can have an existing omnisave placed on the Device, by explicit choice. The result is a sync baseline later passes use without guessing.

## Behavior

### Choosing what to protect

- Whole-save tracking captures every regular file in the discovered boundary.
- A game whose Game Save Adapter finds save slots on this Device tracks every slot, without asking, before matching or seeding: each occupied slot binds on its own and each empty slot is a destination. Slots keep the game's own names, such as STS2's profiles. A game the adapter cannot divide here, on an unsupported launcher or before any slot exists, keeps its whole save. A discovery that cannot finish holds the game's saves, reported, until it succeeds; it never falls back to the whole save.
- The first adapter supports STS2's three vanilla profiles through Steam as save slots. Each profile includes its complete directory, including runs, recovery copies, preferences, and history. Shared selection and device settings remain outside slot histories and are not backed up unless the game is switched to its whole save. Known native account directories are required.
- The slot default is recorded the first time a pass applies it, and survives moving the save folder: a slot is identified by the adapter's anchor and key, not its absolute path. Slots found later are tracked too. Unavailable adapters or bound slots are reported and never broaden into whole-save capture.
- Existing whole-save bindings keep their meaning. The whole save is an explicit per-game choice: `omnisave bind --choose-scope` switches between save slots and the whole save, retiring incompatible local mappings while retaining their server histories. Pending restores must finish before scope can change.
- A new slot history is named after its slot, such as "Profile 1", and the server adds a number if that name is taken; a whole save keeps the default "Save N". The name stays editable and never decides where a history is placed.
- Dash distinguishes save slot histories from whole-save histories independently of their editable names; TUI also identifies the bound local slot. A slot number is local destination context, not a server history identity.

### A Local Save without a binding

- Every unbound Local Save of a tracked game goes through binding. A pass binds only games whose server records it confirmed, so a tracking failure cannot seed an omnisave.
- A Device binds the folder the game itself reads and writes, never a store's cloud mirror of it (decision 10).
- If the game has no omnisaves, the local content seeds one and that revision becomes the sync baseline.
- Otherwise the content is matched exactly against the full history of every compatible-scope omnisave for the game that no other save of the installation owns, across the per-OS spellings of its save location (decisions 11 and 13).
- One match at a Current Revision rebinds automatically. One match at any other revision asks whether to sync with that omnisave, choosing which save becomes current, or fork from the match.
- A save matching no omnisave, or several, asks whether to bind a match, sync with another omnisave, or create a new omnisave. Syncing with an unmatched omnisave asks which save becomes current: keeping its Current Revision first keeps the local content as a branch of it, and using the local save commits it as the new Current Revision. Omnisaves whose Current Revision cannot land in this save's layout are not offered. When nothing is left to offer, a new omnisave is created without asking, attended or not, unless one of the game's omnisaves is held for migration ([FDR-005](FDR-005-save-sync.md), decision 14): splitting its history waits for a person.
- Unattended passes never ask; the save waits, reported, for an interactive run. There is no ignore answer, since tracking is the intent to synchronize, and leaving a question changes nothing.
- Each Local Save binds independently, so one game may seed or bind several omnisaves.
- Manual rebinding offers only compatible-scope omnisaves of the save's own Game that no other save of the installation owns, and records no baseline.
- If a bound omnisave was deleted, the binding is dropped and surviving omnisaves are matched by content. When none survive, the deletion wins and the game is untracked on that Device ([FDR-002](FDR-002-game-lifecycle.md), decision 10).

### Empty native destinations

- An empty selected destination is offered compatible existing omnisaves that no other save of the installation owns, including when another selected slot is occupied, even when there is only one. Choosing one places its Current Revision in the game's own save folder and records it as the baseline. Declining, or an unattended pass, places nothing; the offer returns on the next interactive run. Empty slots beside an occupied one say nothing when there is nothing to offer.
- Whole-save placement requires exactly one compatible destination. Save slots can offer several compatible empty destinations; a person chooses both the history and its destination. Empty slots never seed empty histories. A history held for migration is not offered.
- A Windows game run through Wine, under Proton or GameHub, keeps its save folders inside its prefix. Until the launcher has created that prefix, the game has no destination to offer.
- Placement is verified before it lands, never overwrites content that appeared after discovery, leaves nothing partial on failure, and finishes in the store's registry where the game needs that ([FDR-005](FDR-005-save-sync.md), decision 13).

## Design Decisions

### 1. Binding is automatic; asking is the exception

**Decision:** Tracking includes binding. A game whose adapter offers save slots tracks every slot by default rather than asking for a boundary; content matching asks only when more than one safe outcome remains. When exactly one remains, attended and unattended passes both take it. **Why:** Tracking is the user's intent to protect a game. A second routine step would leave saves unprotected through omission rather than choice. **Tradeoff:** Tracking may create server history as part of completing that intent.

### 2. A game with no omnisaves seeds automatically

**Decision:** Local content seeds the first omnisave without confirmation. **Why:** With no server-side playthrough there is nothing to conflict with. **Tradeoff:** Two Devices seeding simultaneously may create two independent omnisaves; both remain valid and visible.

### 3. Placing onto a Device with no Local Save is explicit and starts at current

**Decision:** An omnisave is placed on a Device with no Local Save only when a person chooses it, even as the only candidate, and placement starts from its Current Revision. **Why:** This writes into the folder the game will load, which merits consent that seeding a server copy does not. Starting anywhere but current would silently begin a different history. **Tradeoff:** A fresh Device needs one interaction before play continues, and older revisions are reached by restoring ([FDR-005](FDR-005-save-sync.md)).

### 4. Matching means exact content equality across full history

**Decision:** A Local Save matches only a revision with the same file set and content, and historical revisions count as well as the Current Revision. **Why:** Exact matching recovers a binding after a Device was offline or lost its local state without risking a false attachment. **Tradeoff:** Near-matches require a decision, and matching work grows with history.

### 5. A match at a non-current revision requires choosing current or forking

**Decision:** A save matching a revision other than the Current Revision, behind it or ahead of it after a restore, is never moved silently. The user syncs, keeping the Current Revision or making the local save current on top of it, or forks from the matched revision. **Why:** Continuing both on one omnisave would immediately conflict. Every outcome is lossless: the matched revision already exists on the server, and a local save made current stacks on the revision it replaces. **Tradeoff:** A safe but meaningful choice interrupts an otherwise automatic flow.

### 6. Unmatched and ambiguous saves are never guessed

**Decision:** Automatic binding requires exactly one proven omnisave. Otherwise a person binds a match, syncs with another omnisave, or creates a new one, unless creating one is the only safe outcome left (decision 1). **Why:** A wrong guess would extend the wrong playthrough, and an ignore state would contradict tracking. **Tradeoff:** Some saves need interaction before synchronization can begin.

### 7. No local content means no seed; any local content means matching

**Decision:** A game without local content never seeds an empty omnisave, and placement (decision 3) is available only while the Device has no Local Save. **Why:** An empty omnisave cannot tell an unplayed game from missing data, and only without local content can placement not discard progress. **Tradeoff:** Protection begins after the game writes a save, and even unwanted local content rules out placement until it binds normally.

### 8. Every omnisave has a server-owned display name

**Decision:** The server assigns a non-empty, game-unique display name when an omnisave is created or forked. Fork names retain enough source and Device context to distinguish independent playthroughs. **Why:** A name assigned once by the authority remains consistent everywhere the save appears. **Tradeoff:** Generated names are descriptive labels, not stable identifiers, and may be reused after deletion.

### 9. Syncing with an unmatched omnisave settles current in the same run

**Decision:** Unmatched local content syncing with an existing omnisave settles which save is current in the same run. Keeping the Current Revision first keeps the local content as a branch of it, named for the Device, then applies and binds it. Using the local save commits it on top of the Current Revision and binds there, leaving local files alone. Either is refused before anything is committed when the Current Revision cannot land in the save's layout. A placement that fails after the branch leaves the content in the omnisave's history, so the next pass asks it as a match at a non-current revision (decision 5), whose jump finishes the answer. **Why:** The conflict is already known, so deferring it to a divergence question would ask twice. A branch keeps the playthrough in the omnisave the person chose; a separate preservation omnisave hid that choice behind a copy of their own progress on the next run. **Tradeoff:** Branches remain until pruned, and using the local save moves current for every bound Device ([FDR-005](FDR-005-save-sync.md), decision 4).

### 10. A store's cloud mirror is a transport, never a save

**Decision:** A Device binds, places into, and snapshots only the folder the game itself reads and writes, located by save-location rules ([ADR-018](../adr/ADR-018-embedded-save-profiles.md)), never a store's cloud mirror of it. A game no rule can place has no save on the Device and is reported that way. **Why:** A mirror fills file by file, so its snapshot can be a state the game never held, and a file placed there reaches neither the game nor the store registry an API-synced game trusts ([FDR-005](FDR-005-save-sync.md), decision 13). Neither failure shows in the mirror's own files, so only the categorical rule is safe; reporting no save location beats appearing to protect a game. **Tradeoff:** An API-synced game no rule can place goes visibly unprotected. Devices lose the mirror's identical layout everywhere and depend on per-OS rules (decision 11). The game's folder also holds backups, other profiles, and logs, so a Device tracks more files and commits more often.

### 11. An omnisave speaks one location vocabulary; Devices translate

**Decision:** A save resolved from save-location rules carries the identity of every spelling its rules give its location across OSes. A save and a revision that each spell exactly one location the save knows are compared, adopted, and placed as the same place, and a commit keeps the omnisave's original spelling. For first placement, rules naming the same known directory root count as one location for translation; their original identities remain valid for revisions that name them directly. Anything else stays strict. **Why:** The games Omnisave most exists for are ones Steam does not sync, and their rules spell one save folder differently per OS; without translation, one playthrough could never follow the player between a desktop and a Steam Deck. Overlapping directory rules leave one unambiguous placement root even before local files exist. Requiring a known identity keeps translation inside one game's rules, and keeping the spelling means a history never mixes vocabularies. **Tradeoff:** Rules that name distinct roots, or anchor at different depths per OS, split per OS. An ambiguous single-file placement is refused rather than guessed. A manifest refresh that rewords a template strands older revisions until content is committed under the new wording.

### 12. Scope compatibility precedes content matching

**Decision:** A history has an immutable portable scope; a Device separately owns its selection of native slots. Whole-save and slot histories cannot match or adopt each other, and overlapping native bindings are refused. **Why:** Equal bytes do not prove equal ownership, and a scope change must not reinterpret existing recovery history ([ADR-021](../adr/ADR-021-independent-save-boundaries.md)). **Tradeoff:** Changing scope starts or adopts compatible histories rather than silently splitting aggregate history.

### 13. A history has one owner per game installation

**Decision:** Content matching, empty-destination placement, and manual binding skip any history already bound to, or being restored into, another discovered save of the same game installation. A sibling slot holding equal bytes starts its own history. A binding whose save is no longer discovered does not count, so a save whose identity changed still rebinds to its history. **Why:** Two slots sharing one history would each pull the other's progress: playing one would overwrite the other ([ADR-021](../adr/ADR-021-independent-save-boundaries.md)). **Tradeoff:** Copying a playthrough into a second slot needs a fork first; the same history cannot be placed twice on one installation.

## Related

- **ADRs:** [ADR-021](../adr/ADR-021-independent-save-boundaries.md) — save boundaries and game adapters; [ADR-020](../adr/ADR-020-domains-own-their-contracts.md) — domain-owned contracts; [ADR-001](../adr/ADR-001-server-authority.md) — the server judges creation and matching claims while clients originate content; [ADR-018](../adr/ADR-018-embedded-save-profiles.md) — the save-location rules binding and placement rely on.
- **FDRs:** [FDR-001](FDR-001-game-identity-resolution.md) — game resolution precedes binding; [FDR-002](FDR-002-game-lifecycle.md) — the lifecycle binding completes; [FDR-005](FDR-005-save-sync.md) — synchronization once a baseline exists, and the migration hold.
