# FDR-003: Automatic Save Binding

**Status:** Experimental
**Last reviewed:** 2026-08-18

## Overview

Automatic Save Binding connects a Device's Local Saves to Omnisaves as part of
tracking. It seeds games the server has never seen, reattaches saves by exact
content match, and asks only when more than one safe future remains. The result
is a synchronization baseline that later passes can use without guessing.

## Behavior

- Every tracked game with local save content but no binding goes through the
  binding pass. Games without local content are left untouched unless an
  existing Omnisave can be synchronized to the Device
  ([FDR-004](FDR-004-sync-to-device.md)).
- A game has one save representation per Device: a save its adapter itself
  discovered wins, and community save-location rules stand in only for games
  whose adapter found no save content. One game therefore never tracks two
  Local Saves holding the same progress in different layouts (decision 10).
- If the game has no Omnisaves, the local content becomes the initial revision
  of a new Omnisave and that revision becomes the binding baseline.
- Otherwise, local content is matched exactly against the full revision history
  of every Omnisave for the game.
- One match at the Current Revision rebinds automatically. One match at an older
  revision requires a choice between adopting current progress and continuing
  the older progress as a separate Omnisave.
- A Local Save matching no revision is an Unmatched Local Save. The user can
  synchronize it with an existing Omnisave or create a new Omnisave from it.
  Synchronizing first preserves the unmatched local content independently, then
  applies and binds the selected Omnisave's Current Revision. Only Omnisaves
  whose Current Revision can be applied into this save's layout are offered;
  when none qualify and nothing matches, creating a new Omnisave is the one
  safe outcome left and is taken without a question.
- A Local Save matching several Omnisaves requires the user to choose one of the
  matches or preserve the content as another independent Omnisave.
- Interactive binding offers no ignore outcome: tracking expresses an intent to
  synchronize. Leaving a question aborts without changing the unresolved save.
- Each Local Save binds independently, so a game with several local saves may
  seed or bind several Omnisaves.
- Untracking and later re-tracking does not duplicate content already held by
  the server; content matching restores the binding.
- The pass binds only games whose server records were confirmed during that
  run. A tracking failure cannot seed an Omnisave.
- If a bound Omnisave was deleted, the stale binding is discarded. Surviving
  Omnisaves are considered by content match; when none survive, the server
  deletion wins and the game is untracked on that Device
  ([FDR-002](FDR-002-game-lifecycle.md), decision 10).

## Design Decisions

### 1. Binding is automatic; prompting is the exception

**Decision:** Tracking includes binding and asks only when content matching
cannot identify one safe outcome.
**Why:** Tracking is the user's intent to protect a game. Requiring a second
routine step would leave saves unprotected through omission rather than choice.
**Tradeoff:** Tracking may create server history as part of completing that
intent.

### 2. A game with no Omnisaves seeds automatically

**Decision:** Local content seeds the first Omnisave without confirmation.
**Why:** With no server-side playthrough there is nothing to conflict with.
**Tradeoff:** Two Devices seeding simultaneously may create two independent
Omnisaves; both remain valid and visible.

### 3. The server determines the available Omnisaves

**Decision:** Binding uses the server's current set of Omnisaves rather than a
local memory of them.
**Why:** The server is authoritative and local state is disposable
([ADR-001](../adr/ADR-001-server-authority.md)).
**Tradeoff:** Binding requires the server to be reachable.

### 4. Matching means exact content equality across full history

**Decision:** A Local Save matches only a revision with the same file set and
content, and historical revisions count as well as the Current Revision.
**Why:** Exact matching can recover lineage after a Device was offline or lost
its local state without risking a false attachment.
**Tradeoff:** Near-matches require a decision, and matching work grows with
history.

### 5. An older match requires adopting current or separating the playthrough

**Decision:** A save matching an older revision is never advanced silently. The
user either adopts the Omnisave's Current Revision or forks from the matched
revision.
**Why:** Continuing both histories on one lineage would immediately create a
conflict. Both offered outcomes preserve the older content because the matched
revision already exists on the server.
**Tradeoff:** A safe but meaningful choice interrupts an otherwise automatic
flow.

### 6. Unmatched and ambiguous saves are never guessed

**Decision:** Automatic binding requires exactly one proven lineage. Unmatched
content may adopt an existing Omnisave only after being preserved, while
ambiguous content requires choosing a match or creating another Omnisave.
**Why:** A wrong guess would extend the wrong playthrough. Preserving first
keeps synchronization lossless without adding an ignore state that contradicts
tracking.
**Tradeoff:** Some saves require interaction before synchronization can begin.

### 7. A game without local content does not seed an empty Omnisave

**Decision:** No local content means no new Omnisave and no binding.
**Why:** There is nothing to protect, and an empty Omnisave cannot distinguish
an unplayed game from missing data.
**Tradeoff:** Protection begins on a later pass after the game creates a save.

### 8. Every Omnisave has a server-owned display name

**Decision:** The server assigns a non-empty, game-unique display name when an
Omnisave is created or forked. Fork names retain enough source and Device
context to distinguish independent playthroughs.
**Why:** A name assigned once by the authority remains consistent everywhere
the save appears.
**Tradeoff:** Generated names are descriptive labels, not stable identifiers,
and may be reused after deletion.

### 9. Adopting an existing Omnisave resolves the known conflict immediately

**Decision:** When unmatched local content adopts an existing Omnisave, the
local content is preserved first and the selected Current Revision is applied
in the same run. An adoption that cannot finish — the selected Current
Revision does not fit the save's layout — is refused before anything is
preserved.
**Why:** The conflict is already known, so deferring it to a second divergence
question would repeat the decision. Checking applicability first means a
failed answer leaves no half-taken state behind, and content that was
preserved before an interruption is found by matching on a later pass rather
than preserved again.
**Tradeoff:** Preservation creates another Omnisave that the user may later
choose to delete.

### 10. One save representation per game per Device

**Decision:** A Device tracks one representation of a game's save. A save the
game's adapter discovered — Steam Cloud's mirror — is that representation,
and community save-location rules stand in only for games whose adapter found
no save content.
**Why:** A Steam Cloud game holds the same progress twice on one machine: the
game's native folder and the mirror Steam replicates between Devices. The two
spell their content in different layouts, so a lineage built from one can
never be adopted by the other, and discovering both invites binding them into
exactly that impossibility while storing every save twice. The mirror wins
because its layout is identical on every Device and OS, which is what lets a
Steam Deck and a desktop share one lineage.
**Tradeoff:** The native folder's extras — backup twins, replays — go
unprotected for Cloud games, and a Device where Steam Cloud is disabled for a
game keeps synchronizing a mirror that no longer moves. Election is per
Device and evidence-based, so a game changes representation only when what
the adapter can discover changes.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — the server judges
  creation and matching claims while clients originate content.
- **FDRs:** [FDR-001](FDR-001-game-identity-resolution.md) — game resolution
  precedes binding; [FDR-002](FDR-002-game-lifecycle.md) — the lifecycle binding
  completes; [FDR-004](FDR-004-sync-to-device.md) — placing an existing save on
  a Device with no local content; [FDR-005](FDR-005-save-sync.md) — ongoing
  synchronization after a baseline exists.
