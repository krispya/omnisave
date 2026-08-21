# FDR-003: Automatic Save Binding

**Status:** Experimental **Last reviewed:** 2026-08-18

## Overview

Automatic Save Binding connects a Device's Local Saves to Omnisaves as part of tracking. It seeds games the server has never seen, reattaches saves by exact content match, and asks only when more than one safe future remains. The result is a synchronization baseline that later passes can use without guessing.

## Behavior

- Every tracked game with local save content but no binding goes through the binding pass. Games without local content are left untouched unless an existing Omnisave can be synchronized to the Device ([FDR-004](FDR-004-sync-to-device.md)).
- A Device binds the folder the game itself reads and writes, located by save-location rules; a store's cloud mirror of that folder is a transport and is never bound, placed into, or snapshotted, whichever source names it. A game no rule can place here has no save on this Device and says so (decision 10).
- If the game has no Omnisaves, the local content becomes the initial revision of a new Omnisave and that revision becomes the binding baseline.
- Otherwise, local content is matched exactly against the full revision history of every Omnisave for the game. Matching, adoption, and placement translate between the per-OS spellings a game's save-location rules give one logical location, so a lineage begun on another OS is recognized, joined, and followed rather than duplicated (decision 11).
- One match at the Current Revision rebinds automatically. One match at an older revision requires a choice between adopting current progress and continuing the older progress as a separate Omnisave.
- A Local Save matching no revision is an Unmatched Local Save. The user can synchronize it with an existing Omnisave or create a new Omnisave from it. Synchronizing first preserves the unmatched local content independently, then applies and binds the selected Omnisave's Current Revision. Only Omnisaves whose Current Revision can be applied into this save's layout are offered; when none qualify and nothing matches, creating a new Omnisave is the one safe outcome left and is taken without a question.
- A Local Save matching several Omnisaves requires the user to choose one of the matches or preserve the content as another independent Omnisave.
- Interactive binding offers no ignore outcome: tracking expresses an intent to synchronize. Leaving a question aborts without changing the unresolved save.
- Each Local Save binds independently, so a game with several local saves may seed or bind several Omnisaves.
- Untracking and later re-tracking does not duplicate content already held by the server; content matching restores the binding.
- The pass binds only games whose server records were confirmed during that run. A tracking failure cannot seed an Omnisave.
- If a bound Omnisave was deleted, the stale binding is discarded. Surviving Omnisaves are considered by content match; when none survive, the server deletion wins and the game is untracked on that Device ([FDR-002](FDR-002-game-lifecycle.md), decision 10).

## Design Decisions

### 1. Binding is automatic; prompting is the exception

**Decision:** Tracking includes binding and asks only when content matching cannot identify one safe outcome. **Why:** Tracking is the user's intent to protect a game. Requiring a second routine step would leave saves unprotected through omission rather than choice. **Tradeoff:** Tracking may create server history as part of completing that intent.

### 2. A game with no Omnisaves seeds automatically

**Decision:** Local content seeds the first Omnisave without confirmation. **Why:** With no server-side playthrough there is nothing to conflict with. **Tradeoff:** Two Devices seeding simultaneously may create two independent Omnisaves; both remain valid and visible.

### 3. The server determines the available Omnisaves

**Decision:** Binding uses the server's current set of Omnisaves rather than a local memory of them. **Why:** The server is authoritative and local state is disposable ([ADR-001](../adr/ADR-001-server-authority.md)). **Tradeoff:** Binding requires the server to be reachable.

### 4. Matching means exact content equality across full history

**Decision:** A Local Save matches only a revision with the same file set and content, and historical revisions count as well as the Current Revision. **Why:** Exact matching can recover lineage after a Device was offline or lost its local state without risking a false attachment. **Tradeoff:** Near-matches require a decision, and matching work grows with history.

### 5. An older match requires adopting current or separating the playthrough

**Decision:** A save matching an older revision is never advanced silently. The user either adopts the Omnisave's Current Revision or forks from the matched revision. **Why:** Continuing both histories on one lineage would immediately create a conflict. Both offered outcomes preserve the older content because the matched revision already exists on the server. **Tradeoff:** A safe but meaningful choice interrupts an otherwise automatic flow.

### 6. Unmatched and ambiguous saves are never guessed

**Decision:** Automatic binding requires exactly one proven lineage. Unmatched content may adopt an existing Omnisave only after being preserved, while ambiguous content requires choosing a match or creating another Omnisave. **Why:** A wrong guess would extend the wrong playthrough. Preserving first keeps synchronization lossless without adding an ignore state that contradicts tracking. **Tradeoff:** Some saves require interaction before synchronization can begin.

### 7. A game without local content does not seed an empty Omnisave

**Decision:** No local content means no new Omnisave and no binding. **Why:** There is nothing to protect, and an empty Omnisave cannot distinguish an unplayed game from missing data. **Tradeoff:** Protection begins on a later pass after the game creates a save.

### 8. Every Omnisave has a server-owned display name

**Decision:** The server assigns a non-empty, game-unique display name when an Omnisave is created or forked. Fork names retain enough source and Device context to distinguish independent playthroughs. **Why:** A name assigned once by the authority remains consistent everywhere the save appears. **Tradeoff:** Generated names are descriptive labels, not stable identifiers, and may be reused after deletion.

### 9. Adopting an existing Omnisave resolves the known conflict immediately

**Decision:** When unmatched local content adopts an existing Omnisave, the local content is preserved first and the selected Current Revision is applied in the same run. An adoption that cannot finish — the selected Current Revision does not fit the save's layout — is refused before anything is preserved. **Why:** The conflict is already known, so deferring it to a second divergence question would repeat the decision. Checking applicability first means a failed answer leaves no half-taken state behind; an answer that fails after preserving records what it created, and a later pass recognizes that content as already safe rather than preserving it again. **Tradeoff:** Preservation creates another Omnisave that the user may later choose to delete.

### 10. A store's cloud mirror is a transport, never a save

**Decision:** A Device binds the folder the game itself reads and writes, located by save-location rules, and never a store's cloud mirror of it. Steam's `userdata` tree is therefore neither a save nor a placement destination, whatever it holds and whichever source named it: a location resolved there is refused where a scan's results become saves, so no source — mistaken, outdated, or added later — can reintroduce one. Rules come from the community manifest first and from Steam's own cloud configuration for games the manifest cannot place here; a game neither source can place has no save on this Device and is reported that way. **Why:** A mirror is where a store stages content on its way somewhere else, and treating it as the save breaks the features that matter. A game may reach its cloud through the store's API, whose file list is the store's own metadata rather than the directory's contents, so a restored file the store does not list does not exist as far as the game is concerned — a rewind writes into a mailbox nobody opens. A mirror is also filled file by file as the store gets to it, so a snapshot of one can be a state the game never held: a run in progress beside the record of that same run having ended, which the game discards on sight. Neither failure is visible in the mirror's own files, and no care taken at restore time recovers from either, so the only safe rule is the categorical one. Saying a game has no save location is a worse answer for that game and a better one for the user than appearing to protect it. **Tradeoff:** A game whose saves the store only replicates through its API, and which no rule can place, goes unprotected where it previously appeared protected — visibly, but unprotected. Devices give up the mirror's one virtue, a layout identical on every Device and OS, and follow rules whose per-OS spellings may anchor at different depths, which decision 11 cannot translate; such games seed a lineage per OS until their rules agree. Lineages already shaped like a mirror cannot be adopted by the game's own folder until they migrate by proven rename ([FDR-005](FDR-005-save-sync.md), decision 14); one no device can prove remains as history. The game's folder also holds what a mirror omits — backup twins, other profiles, logs — so a Device tracks more files and commits more often.

### 11. A lineage speaks one location vocabulary; Devices translate

**Decision:** A save resolved from save-location rules carries the identity of every spelling those rules give its location across OSes. When a save spells exactly one location and a revision spells exactly one location the save knows itself under, the two are compared, adopted, and placed as the same place; and a commit onto such a lineage keeps the lineage's original spelling. A save or revision spelling several locations stays strict, and an identity the save has never heard of is refused as before. **Why:** The games Omnisave most exists for are the ones Steam does not sync, and their community rules spell one save folder differently per OS. Without translation each OS would seed its own lineage and one playthrough could never follow the player between a desktop and a Steam Deck. Requiring a known identity keeps the translation inside one game's own rules — a Cloud mirror or another game's location can never be mistaken for the save — and keeping the lineage's spelling on commit means history never mixes vocabularies, so every Device reads it the same way forever ([ADR-018](../adr/ADR-018-embedded-save-profiles.md)). **Tradeoff:** Games whose rules name several distinct locations do not translate and still split per OS. Translation trusts relative paths to coincide beneath the location; rules that anchor at different depths per OS simply never match and fall back to strict. A location shaped like one save file — or one that does not exist yet and cannot prove its shape — accepts a translated single file only under its own name, refusing rather than guessing a placement the game would never read; rules that pair one OS's file with another OS's folder of several files remain undetectable and are the residual risk. And a manifest refresh that rewords a template changes that spelling's identity, which strands translation for revisions minted under the old wording until content is committed under the new one.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — the server judges creation and matching claims while clients originate content.
- **FDRs:** [FDR-001](FDR-001-game-identity-resolution.md) — game resolution precedes binding; [FDR-002](FDR-002-game-lifecycle.md) — the lifecycle binding completes; [FDR-004](FDR-004-sync-to-device.md) — placing an existing save on a Device with no local content; [FDR-005](FDR-005-save-sync.md) — ongoing synchronization after a baseline exists.
