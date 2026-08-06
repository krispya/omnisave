# FDR-003: Automatic Save Binding

**Status:** Experimental
**Last reviewed:** 2026-08-03

## Overview

How a Device's Local Saves get connected to Omnisaves without a separate
bind step. Tracking ends with a binding pass: games whose saves the server
has never seen are seeded from the local save, games the server already
knows are re-attached by content match, and only genuine ambiguity asks the
user. Manual `bind` remains for corrections. This is the first slice of the
synchronization flow — seeding builds the client's write path and
establishes the baseline that sync will later diff against.

## Behavior

- After tracking completes, every tracked game that has a local save but no
  binding on this Device goes through the binding pass. Games without a
  local save are skipped untouched, and the result says that no save is
  available — unless the server has Omnisaves for them, in which
  case syncing one down is offered
  ([FDR-004](FDR-004-sync-to-device.md)).
- If the server has no Omnisaves for the game, one is created and seeded:
  the local save's content becomes its initial revision, and the binding
  records that revision as the sync baseline. The server names the new
  Omnisave ("Save N"), the result reads "Save N · synced just now," and it
  appears in the Dash under that name with no further action.
- If the server already has Omnisaves for the game, the local save is
  compared by content against their full revision histories.
- Matching the Current Revision of exactly one Omnisave rebinds automatically with that
  revision as the baseline — this Device is simply up to date, and the result
  reads "Save N · synced just now."
- Matching an older revision of exactly one Omnisave means the save went
  stale: it was tracked at some point and play continued on another
  Device. The user chooses between fast-forwarding — the Current Revision's content
  replaces the local save and the binding starts there — or forking
  at the matched revision, keeping the lineage and continuing this
  playthrough independently.
- If the local save matches nothing, or matches more than one Omnisave, the
  user chooses: bind to one of the existing Omnisaves (matches are marked),
  create a new one seeded from the local save, or decide later — the save
  stays unbound and the result points at `omnisave bind`.
- Choosing an existing Omnisave that does not match the local save records
  the binding with no baseline; nothing is uploaded or overwritten.
  Reconciling that divergence is synchronization's job.
- Each Local Save binds independently; a game with several local saves can
  seed several Omnisaves.
- Untracking and re-tracking a game does not duplicate its saves: the
  Omnisaves survived untracking, so the pass finds them again and rebinds
  by content match.
- The pass only binds against games whose server records were confirmed in
  the same run; a game whose tracking failed is never seeded.
- A binding whose Omnisave no longer exists on the server is discarded
  during the pass. If other Omnisaves for the game survive, the local save
  falls back to content matching; if none remain, the deletion syncs back —
  the game is untracked on this Device rather than reseeded, so a deletion
  in the Dash is never resurrected
  ([FDR-002](FDR-002-game-lifecycle.md), decision 10).

## Design Decisions

### 1. Binding is automatic; prompting is the exception

**Decision:** Tracking ends with a binding pass that involves the user only
when content matching cannot decide.
**Why:** Binding is the step that actually protects a save and the easiest
one to forget. Tracking already expresses the intent — "Omnisave should
care about this game" — so requiring a second command to make that
protection real is ceremony, not consent.
**Tradeoff:** Server writes now happen as a side effect of tracking; a user
who only meant to browse selections still creates Omnisaves. Untracking and
deleting remain available to undo it.

### 2. Zero-save games are seeded without asking

**Decision:** A tracked game with a local save and no Omnisaves gets one
created automatically, seeded from the local content, baseline set to the
seed revision.
**Why:** With nothing on the server there is nothing to diverge from — the
case is conflict-free by construction, so asking would be asking permission
to do the only sensible thing.
**Tradeoff:** Two Devices tracking the same game near-simultaneously can
each seed, leaving two Omnisaves. That is legal in the model (separate
playthroughs), loses nothing, and is visible in the Dash; a server-side
create-if-empty operation could close the race if it proves annoying.

### 3. The server decides whether a game has saves

**Decision:** The pass asks the server for the game's Omnisaves rather than
consulting anything local.
**Why:** The server is the only authority
([ADR-001](../adr/ADR-001-server-authority.md)), and local state may be
gone precisely when this flow matters (fresh install, wiped Device). It
also makes re-tracking correct for free: Omnisaves survive untracking
([FDR-002](FDR-002-game-lifecycle.md)), so a re-tracked game reports
existing saves and takes the match path instead of seeding a duplicate.
**Tradeoff:** Binding requires the server to be reachable — acceptable,
since tracking already does.

### 4. Matching is exact content equality against full history

**Decision:** A Local Save matches a revision when its file set and content
are identical. Any revision in an Omnisave's history counts, not just the
Current Revision.
**Why:** Artifacts are content-addressed, so equality is trustworthy.
Matching history rather than only heads means a Device that sat offline —
or was wiped and re-minted its identity, the acknowledged cost of
self-identification in [FDR-002](FDR-002-game-lifecycle.md) — still finds
its lineage instead of falling into the manual prompt. Content matching is
what makes losing client state recoverable.
**Tradeoff:** Matching cost grows with history length; fine at self-hosted
scale. Near-matches (one file differs) get no special treatment and fall
through to the prompt.

### 5. A stale match asks: fast-forward or fork

**Decision:** When the local save equals an out-of-date revision, the pass
does not silently rebind. The user picks fast-forward — adopt the Current Revision —
or fork at the matched revision.
**Why:** The match proves what happened: this save was tracked once, went
stale, and play continued on another Device. The two futures are
incompatible, and both are safe to offer — fast-forwarding discards
nothing because the local content already exists as the matched revision,
and playing on from the stale point on the same lineage would only
guarantee a current revision conflict at the first commit. Asking at bind time, when
"this device is behind" is visible and explainable, converts that
inevitable conflict into an informed choice. Forking keeps the lineage:
the new Omnisave records its origin, so the Dash shows where the
playthrough split.
**Tradeoff:** Fast-forward pulls download-and-apply machinery into this
feature ahead of synchronization proper. The client must download and verify
the complete Current Revision before moving any local file, then restore the matched local
snapshot if applying it fails. The replaced content also remains
recoverable as the older server revision that triggered the choice.

### 6. Ambiguity prompts; the pass never guesses

**Decision:** Automatic rebinding requires exactly one matching Omnisave.
Zero or several matches ask the user.
**Why:** Binding decides which lineage future revisions extend. A silent
wrong guess writes new history onto the wrong playthrough — strictly worse
than one prompt in an otherwise automatic flow.
**Tradeoff:** Forks whose content has not yet diverged always prompt, since
the same save matches both lineages.

### 7. No local save, no Omnisave

**Decision:** Tracked games without a local save are skipped — no empty
Omnisave, no binding — and reported as having no save available.
**Why:** There is nothing to protect yet, and an Omnisave with no revisions
is indistinguishable from lost data. A later pass picks the game up once
it has been played.
**Tradeoff:** A tracked-but-unplayed game stays unprotected until the next
pass after its first save exists — the same exposure window
[FDR-002](FDR-002-game-lifecycle.md) accepts for untracked games.

### 8. Every Omnisave carries a persisted display name

**Decision:** The server guarantees a display name at creation: an unnamed
create gets "Save N", numbered past the game's highest surviving "Save N";
an unnamed fork inherits its source's name with a " (fork)" suffix; renaming
to an empty name is rejected. Records that predate the rule were backfilled
in creation order.
**Why:** Names are how saves are told apart everywhere they surface — the
Dash poster wall, the track report's "seeded as" line, the bind prompt.
Client-side fallbacks invented a name per surface and could disagree;
assigning it once, at the source of truth
([ADR-001](../adr/ADR-001-server-authority.md)), makes every surface show
the same identity.
**Tradeoff:** Default names are generic — "Save 2" says nothing about the
playthrough — and a deleted save's number can be reissued later, so a name
is not a stable identifier; the ID remains the only permanent handle.

### 9. Mismatched choices defer to synchronization

**Decision:** Binding to a non-matching existing Omnisave records the
mapping with an empty baseline and moves no content in either direction.
**Why:** What to do when both sides have content is exactly the divergence
question synchronization owns. Answering it at bind time would smuggle a
conflict policy into a mapping decision.
**Tradeoff:** That local save stays unprotected until synchronization
exists and runs.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — the server
  judges creation and match claims; clients originate content but never
  arbitrate it.
- **FDRs:** [FDR-001](FDR-001-game-identity-resolution.md) — track-time
  resolution that puts the game in the Library before any binding;
  [FDR-002](FDR-002-game-lifecycle.md) — the detect → track → bind
  lifecycle this automates, Device self-identification, and Omnisave
  survival across untracking; [FDR-004](FDR-004-sync-to-device.md) — the
  read counterpart: offering server saves to a Device that has none;
  [FDR-005](FDR-005-save-sync.md) — ongoing sync, which re-runs this
  pass's automatic half and gives baseline-less bindings their
  resolution.
