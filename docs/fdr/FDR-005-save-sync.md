# FDR-005: Save Sync

**Status:** Experimental
**Last reviewed:** 2026-07-25

## Overview

How a bound save and its omnisave stay the same thing over time. Sync is a
pass over this Device's bindings: local progress commits to the server as
new revisions, server progress applies to the local files, and the sync
baseline arbitrates which direction is safe. It runs one-shot as
`omnisave-client sync` and continuously as `omnisave-client watch`, which
commits a quiet moment after the game writes its save — and continuous is
where a bare `omnisave-client` run ends up, because running Omnisave is
running the watcher. This completes what
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
- `omnisave-client` with no command is the whole app, and it skips whatever
  this Device already did: no saved connection asks for one, no tracked
  games asks which to track, and then every run reconciles once — the pass
  that may ask — and watches until it is quit. `track`, `bind`, `sync`,
  `watch`, `scan`, and `connect` stay as the explicit commands.
- `omnisave-client track` is that run's selection step on its own: it always
  asks which games this Device protects, so a game installed since the last
  run can join, then reconciles once, reports what happened, and exits.
  Protecting those saves from then on is the commandless run's job.
- A run that does not ask keeps the standing selection: the scan refreshes
  the tracked games it found and leaves the ones it missed tracked and
  untouched.
- The run that asked is the run that answers: a run that put a question on
  screen prints its report, and a run that asked nothing prints nothing.
  For the commandless run that means the report — when there is one —
  becomes ordinary scrollback above the live view, which opens on what the
  pass found with a footer already proving the run reached the server.
  Quitting the view ends the run.
- In a terminal, watch presents a live view: one line per tracked game
  carrying only what is true now — the save's standing state, or its
  unresolved condition — a footer proving liveness (files watched, last
  sync, server), and two keys, sync now and quit. The block never grows.
- What happens is written once, above the live view, into the terminal's
  scrollback: one line per event, carrying the clock time that makes it
  readable later. A condition announces when it appears and again only if
  it clears and comes back, so an idle watcher stays silent. When output
  is not a terminal — piped, or under a service manager — those same event
  lines are the log.
- A diverged save simply shows its conflict status; resolution still
  belongs to an interactive track run.
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

### 6. The bare command is the app, and it skips what is already done

**Decision:** `omnisave-client` with no command is a state machine over this
Device's own state — connect if there is no saved connection, choose games
if none are tracked, then always one reconcile pass and the watch loop. It
is the only run that keeps watching; the named commands each do one of those
steps deliberately and end.
**Why:** Every step the bare command owns is answered once and then true
forever, so a run that re-asks them is charging rent on a decision already
made; the second run has work to do — sync and watch — and should get to it.
One entry point that always ends in the running app is also the shortest
honest answer to "how do I use this": you run it. Keeping the continuous
behavior in that one place is what lets every other command stay a command:
you can change a selection, bind a save, or take one pass without signing up
for a process that never returns.
**Tradeoff:** A game installed after the first run is invisible to the bare
command until an explicit `track` run adds it — the state machine cannot
distinguish "not chosen" from "not there yet", and re-asking every run is
the thing this decision refuses. The run that protects saves also never
terminates, so anything scripted uses `sync`.

### 7. The live view holds standing state; the terminal keeps the history

**Decision:** The live view holds only what is true now, and each event is
printed once above it rather than held in the view. A run prints a report
only when it asked something; a run that asked nothing is the view alone.
**Why:** Keeping a pass's events inside a live view means either stale text
sitting there for the fifteen minutes until the next pass, or history that
vanishes; printing them into scrollback gives both a readable record and an
idle view that stays clean, because the terminal does the remembering. A
report above the view when nothing was asked is that same text twice — the
view already says what is true, so the scrollback copy is noise the user
scrolls past forever.
**Tradeoff:** Events are announced when a condition appears and stay quiet
while it holds, so a long-standing problem is easy to scroll past — the
live view's line is what keeps it visible. And the pass a silent run does
at startup leaves no record of itself; only what happens next is written
down.

### 8. Watch is a foreground process; the OS owns its lifetime

**Decision:** Watch runs in the foreground until stopped. Restarts,
boot-time start, and logging belong to the service manager
(systemd --user, launchd); a shipped service unit is deployment work.
**Why:** Daemonization rituals are obsolete; service managers supervise
better than any hand-rolled fork-and-pidfile dance, and a foreground
process is trivially debuggable.
**Tradeoff:** Until packaging lands, "install the watcher" is a second
manual step after installing the binary.

### 9. Artifacts are compressed at rest and in transit

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
