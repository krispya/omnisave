# FDR-004: Syncing Saves to a Device

**Status:** Experimental
**Last reviewed:** 2026-07-20

## Overview

How server-side saves reach a Device that has none. When a tracked game has
one or more Omnisaves on the server but no local save on this Device — a
fresh install, a new Device, a wiped machine — the binding pass offers to
sync one of them down. Choosing one syncs it right away: the head revision's
files are placed at the game's native save location, and the binding starts
in sync at the head. This is the read counterpart of
[FDR-003](FDR-003-automatic-save-binding.md)'s seeding — the second slice of
synchronization, and the main workflow for putting an existing playthrough
onto a new machine.

## Behavior

- During the binding pass, a tracked and confirmed game with no local save
  whose server has Omnisaves triggers the offer.
- The offer lists the game's Omnisaves by name, plus "decide later". It is
  always asked, even when exactly one Omnisave exists.
- Choosing an Omnisave syncs it immediately: its head revision's files are
  downloaded, verified, and placed at the game's native save location for
  this Device. The binding records the head as the sync baseline, so the
  Device is up to date the moment the pass finishes — launch the game and
  continue the playthrough.
- The result names the selected omnisave and shows it synced just now; the
  closing tally counts it. Nothing changes on the server: syncing down
  reads content and records a machine-local binding.
- Deciding later changes nothing; the offer repeats on the next track run
  while the game still has no local save.
- The game's native save location is determined by its adapter even though
  no save exists there yet. When the adapter cannot determine it, nothing
  downloads and the result says the game is waiting for its first local
  save.
- A server save is offered only when its head maps to exactly one prospective
  native destination. An absent or ambiguous destination is reported honestly
  and nothing downloads.
- Placement never overwrites: the flow only runs when the Device has no
  local save, and files that appeared in the location since the scan abort
  the placement untouched — the offer returns on a later run.
- A failed or interrupted download leaves the Device exactly as it was.
  There is never a partial local save.
- Games that do have a local save are out of scope here; their binding is
  decided by content matching ([FDR-003](FDR-003-automatic-save-binding.md)).
- Leaving the prompt (esc) aborts the run, consistent with the other
  binding prompts.

## Design Decisions

### 1. Syncing down is offered, never automatic

**Decision:** The user is always asked before a save syncs to the Device,
even when only one Omnisave exists.
**Why:** This is the one moment in tracking that writes into the game's own
save location — the directory the game reads on launch. That deserves
explicit consent every time. The asymmetry with seeding
([FDR-003](FDR-003-automatic-save-binding.md), decision 2) is deliberate:
seeding writes to the server, which exists to receive copies; this writes
where the game plays.
**Tradeoff:** One more prompt in the fresh-Device flow that automation
could have removed.

### 2. The head is what syncs

**Decision:** Choosing an Omnisave materializes its head revision, and the
binding's baseline is the head.
**Why:** The head is the lineage's present; a Device joining a playthrough
should join where it left off. Starting anywhere else would silently fork
history. Older revisions stay reachable through forking when someone
actually wants the past.
**Tradeoff:** No revision picker at track time; the prompt stays one
question.

### 3. Adapters answer where a save would live

**Decision:** Each adapter can state a game's native save location before
any save exists there — the prospective location that placement writes
into.
**Why:** Revision files carry canonical paths, but placing them needs a
root on this machine, and discovery only reports files that exist. The
knowledge is already in adapter territory: RetroArch derives locations from
its platform profiles, Steam from save-location rules expanded against the
environment.
**Tradeoff:** New adapter surface that must be trustworthy — it decides
where files are written. A game whose location cannot be derived is
honestly skipped rather than guessed.

### 4. Stage, verify, then place — never a partial save

**Decision:** All content downloads to a staging area and is verified
against its content hashes before any file is placed; placement is
all-or-nothing with rollback, and it refuses to overwrite files it did not
expect. This is the same download-and-apply machinery fast-forward uses,
now owned by one policy.
**Why:** A partial or corrupt local save is worse than none — inside the
game it is indistinguishable from lost progress. FDR-003 flagged that the
overwrite-safety policy should be decided once before shipping twice; this
decision is that single home, and synchronization's pull path inherits it.
**Tradeoff:** Content briefly exists twice on disk while staging.

### 5. Only a Device with no local save syncs down

**Decision:** The offer requires that the Device has no local save for the
game. Any local content routes through content matching instead.
**Why:** With nothing local there is nothing to lose — the case is
conflict-free by construction, mirroring seeding's logic in reverse. The
moment local content exists, choosing sides is the divergence question that
belongs to matching and, eventually, synchronization
([FDR-003](FDR-003-automatic-save-binding.md), decision 9).
**Tradeoff:** A Device with a worthless local save never gets this offer;
fast-forward, forking, and manual bind cover it.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — content flows
  from the authoritative copy; the client places but never arbitrates.
- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — the binding pass
  this extends, whose open question about fresh Devices this resolves, and
  whose fast-forward staging this shares;
  [FDR-002](FDR-002-game-lifecycle.md) — Device identity and the lifecycle
  that makes a fresh Device expected rather than exceptional;
  [FDR-005](FDR-005-save-sync.md) — ongoing sync for saves once they are
  bound, whose pull direction reuses this record's staging policy.
