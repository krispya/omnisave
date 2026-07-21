# FDR-005: Save Sync

**Status:** Experimental
**Last reviewed:** 2026-07-19

## Overview

How a bound save and its omnisave stay the same thing over time. Sync is a
pass over this Device's bindings: local progress commits to the server as
new revisions, server progress applies to the local files, and the sync
baseline arbitrates which direction is safe. It runs one-shot as
`omnisave-client sync` and continuously as `omnisave-client watch`, which
commits a quiet moment after the game writes its save. This completes what
[FDR-003](FDR-003-automatic-save-binding.md) and
[FDR-004](FDR-004-sync-to-device.md) started: seeding built the write
path, syncing-down built the read path; sync makes both routine.

## Behavior

- Sync decides per bound save by comparing three states: the local
  content, the binding's sync baseline, and the Omnisave's head.
  - All three equal — the save is in sync; nothing happens, and the game's
    line reads its standing state: "Save 1 · synced 2m ago".
  - Local moved, head still at the baseline — the local changes commit as
    a new revision and the baseline advances with the head.
  - Head moved, local still at the baseline — the head syncs down and the
    baseline advances. Nothing is lost: the replaced content is exactly
    the baseline revision, which the server keeps.
  - Both moved — the save is diverged. Sync never resolves divergence on
    its own.
- A binding without a baseline (a manual bind to non-matching content,
  [FDR-003](FDR-003-automatic-save-binding.md) decision 9) is diverged
  from the start and resolves the same way.
- `omnisave-client sync` runs one pass over every binding, reports each
  outcome in the track report voice, and exits. It never prompts.
- `omnisave-client watch` stays running: it watches bound save locations —
  the known files and their directories, so a file appearing in a save
  also triggers — and commits shortly after the game finishes a burst of
  writes, so a crash or a Deck going to sleep loses at most the burst in
  progress. It checks for server-side movement when it starts and
  periodically after. It never prompts.
- Diverged saves are reported — "save diverged from …; run
  omnisave-client track to resolve" — and skipped until an interactive
  track run asks: fork here, and this Device's progress continues as a new
  lineage, or jump to latest, which first preserves the unsynced local
  progress as a fork and then applies the head. Neither choice destroys
  content.
- A sync pass also completes the binding decisions that need no question:
  a tracked game's first save seeds, and a save matching an Omnisave's
  head rebinds. Anything that would prompt — a stale match, an ambiguous
  match, a fresh Device's offer ([FDR-004](FDR-004-sync-to-device.md)), a
  divergence — is reported and waits for track.
- Syncing down uses the staging policy of
  [FDR-004](FDR-004-sync-to-device.md): download, verify, place
  all-or-nothing. Immediately before placing, the local content is
  re-checked against the baseline; any change — the game just wrote —
  aborts untouched and the next pass reconsiders.
- Commits are gated: nothing commits when the bytes did not change,
  an empty local save never commits over good server content, and each
  save has a spacing floor so continuously flushing saves (emulator SRAM)
  do not flood their history.
- Transfers are incremental even though revisions are complete: a commit
  is attempted first, and only the content the server reports missing is
  uploaded — an unchanged file, or content another Device already
  uploaded, never travels twice.
- Failures leave both sides valid: an interrupted upload leaves the head
  where it was; an interrupted download leaves the local save untouched.
- In a terminal, watch presents a live view: every tracked game's line
  updating as passes run, a footer proving liveness (files watched, last
  sync, server), and two keys — sync now and quit. A diverged save simply
  shows its conflict status; resolution still belongs to an interactive
  track run. When output is not a terminal — piped, or under a service
  manager — watch logs plainly instead, printing only passes that changed
  something.
- Watch is a plain foreground process. Keeping it alive across reboots
  belongs to the OS service manager; the service unit is deployment work,
  not part of this feature.

## Design Decisions

### 1. The baseline arbitrates; sync never guesses

**Decision:** Direction is decided purely by comparing local content and
the head against the binding's baseline — never timestamps, never "newest
wins".
**Why:** The baseline is the revision the local content is known to equal,
so a three-way comparison makes push and pull provably lossless and makes
divergence detectable rather than silently absorbed. Clocks disagree
across machines; content-addressed equality does not.
**Tradeoff:** Sync is only as good as its baselines — a binding without
one starts life diverged and needs the one-time fork-or-jump answer
before it flows.

### 2. Change-driven, not session-driven

**Decision:** Watch commits after a debounced quiet window following save
writes. There is no game-process detection, and session end is not the
trigger.
**Why:** Games write saves as bursts — temp files renamed over originals,
long multi-file flushes — and a quiet window coalesces a burst into one
revision while doubling as "you stopped playing or reached a checkpoint".
Session-end detection would add per-adapter process knowledge and miss
crashes and sleep, exactly the moments a save service earns its keep.
Field experience (Hoard's save watcher) converged on this same shape and
informs the guards here.
**Tradeoff:** Long sessions produce mid-session revisions; the spacing
floor caps how many. The quiet window is a heuristic — set too low it
risks committing a torn write, which the gate absorbs on the next pass.

### 3. Pull is automatic only when it cannot lose anything

**Decision:** Heads apply automatically only while the local save still
equals its baseline, re-verified immediately before placement. Every
other download is prompted where it belongs — joining a lineage
([FDR-004](FDR-004-sync-to-device.md)) or resolving divergence.
**Why:** The binding is the standing consent to follow that lineage;
asking again to continue it is ceremony
([FDR-003](FDR-003-automatic-save-binding.md), decision 1). The
losslessness is structural — the replaced bytes are the baseline
revision, which the server keeps — and the pre-placement re-check turns
"the game started writing mid-download" into a clean abort.
**Tradeoff:** A game already running during a pull can later overwrite
the pulled files from memory and push that state; history keeps every
revision, but the head follows the running game.

### 4. Divergence prompts, and both answers keep everything

**Decision:** Headless sync and watch report divergence and skip.
Interactive track asks: fork here, or jump to latest — and jump first
preserves the unsynced local progress as a fork before applying the head.
**Why:** A binding decides which lineage future revisions extend, and a
silent guess writes history onto the wrong playthrough
([FDR-003](FDR-003-automatic-save-binding.md), decision 6). Making jump
preserve-first means the prompt has no destructive option: nothing is
ever lost to sync, only ever kept on a lineage you may later delete.
**Tradeoff:** Jumps leave a fork to clean up in the Dash, and unresolved
divergences accumulate lineages.

### 5. Sync completes the automatic half of binding

**Decision:** Every sync pass runs the binding decisions FDR-003 makes
without asking — first-save seeding and head rebinds — so a game first
played after tracking is protected at the next sync, not the next track.
**Why:** Watch makes "played it, so it's protected" true without ritual,
which was the promise tracking made. Prompting cases still wait for
track, where a human is present.
**Tradeoff:** Prompt-shaped work queues up invisibly until an interactive
run; the report is the only nudge.

### 6. Watch is a foreground process; the OS owns its lifetime

**Decision:** Watch runs in the foreground until stopped. Restarts,
boot-time start, and logging belong to the service manager
(systemd --user, launchd); a shipped service unit is deployment work.
**Why:** Daemonization rituals are obsolete; service managers supervise
better than any hand-rolled fork-and-pidfile dance, and a foreground
process is trivially debuggable.
**Tradeoff:** Until packaging lands, "install the watcher" is a second
manual step after installing the binary.

### 7. Artifacts are compressed at rest and in transit

**Decision:** Artifact content is gzip-compressed on the server's disk and
on the wire in both directions. Identity never changes: an artifact is
named and verified by the SHA-256 and size of its uncompressed content.
Artifacts from before compression stay on disk as-is and remain readable;
nothing rewrites them.
**Why:** Save content is highly compressible, and both ends of the
self-hosted deployment are constrained — NAS disk and handheld Wi-Fi.
Keying identity to uncompressed content keeps dedup, content matching, and
client verification independent of the encoding, and lets the encoding
evolve per file later (zstd) without a data migration. gzip over stronger
codecs because it is in the standard library: zero new dependencies.
**Tradeoff:** CPU spent on every transfer, weaker ratios than zstd, and
logical sizes need their own metadata since disk size no longer states
them.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — heads move
  only by commit against expected state, which is what makes the baseline
  comparison trustworthy; [ADR-002](../adr/ADR-002-sse-view-invalidation.md)
  — the server event stream that could someday wake watch instead of
  polling.
- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — the binding
  pass whose automatic half sync re-runs and whose lineage philosophy
  divergence inherits; [FDR-004](FDR-004-sync-to-device.md) — joining a
  lineage on a Device with no local save, and the staging policy this feature's pull
  reuses; [FDR-002](FDR-002-game-lifecycle.md) — Device identity and
  liveness, which sync events update.
