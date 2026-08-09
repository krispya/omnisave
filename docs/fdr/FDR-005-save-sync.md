# FDR-005: Save Sync

**Status:** Experimental
**Last reviewed:** 2026-08-09

## Overview

How a bound save and its omnisave stay the same thing over time. Sync is a
pass over this Device's bindings: local progress commits to the server as
new revisions, server progress applies to the local files, and the sync
baseline arbitrates which direction is safe. It runs one-shot as
`omnisave sync` and continuously as `omnisave watch`, which
commits a quiet moment after the game writes its save — and continuous is
where a bare `omnisave` run ends up, because running Omnisave is
running the watcher. This completes what
[FDR-003](FDR-003-automatic-save-binding.md) and
[FDR-004](FDR-004-sync-to-device.md) started: seeding built the write
path, syncing-down built the read path; sync makes both routine.

## Behavior

- Sync decides per bound save by comparing three states: the local
  content, the binding's sync baseline, and the Omnisave's Current Revision.
  - All three equal — the save is in sync; nothing happens, and the game's
    line reads its standing state: "Save 1 · synced 2m ago".
  - Local moved, current still at the baseline — the local changes commit as
    a new revision and the baseline advances with the Current Revision.
  - Current moved, local still at the baseline — the Current Revision syncs down and the
    baseline advances. Nothing is lost: the replaced content is exactly
    the baseline revision, which the server keeps.
  - Both moved — the save is diverged. Sync never resolves divergence on
    its own.
- A binding without a baseline (a manual bind to non-matching content,
  [FDR-003](FDR-003-automatic-save-binding.md) decision 9) is diverged
  from the start and resolves the same way.
- `omnisave sync` runs one pass over every binding, reports each
  outcome in the track report voice, and exits. It never prompts.
- `omnisave watch` stays running: it watches bound save locations —
  the known files and their directories, so a file appearing in a save
  also triggers — and commits shortly after the game finishes a burst of
  writes, so a crash or a Deck going to sleep loses at most the burst in
  progress. It checks for server-side movement when it starts, when the
  server's event stream announces movement — a Dash restore reaches a
  watching Device in seconds — and periodically as the fallback while the
  stream is down. It never prompts.
- Diverged saves are reported — "save diverged from …; run
  omnisave track to resolve" — and skipped until an interactive
  track run asks: fork here, and this Device's progress continues as a new
  lineage, or jump to latest, which first preserves the unsynced local
  progress as a fork and then applies the Current Revision. Neither choice destroys
  content.
- A sync pass also completes the binding decisions that need no question:
  a tracked game's first save seeds, and a save matching an Omnisave's
  Current Revision rebinds. Anything that would prompt — a stale match, an ambiguous
  match, a fresh Device's offer ([FDR-004](FDR-004-sync-to-device.md)), a
  divergence — is reported and waits for track.
- Syncing down uses the staging policy of
  [FDR-004](FDR-004-sync-to-device.md): download, verify, place
  all-or-nothing. Immediately before placing, the local content is
  re-checked against the baseline; any change — the game just wrote —
  aborts untouched and the next pass reconsiders.
- Syncing down also waits for the game: a pull whose game is being played
  is deferred — "Save 1 · waiting for game to close" — and watch
  applies it within about a poll interval of the game exiting
  (decision 13).
- Commits are gated: nothing commits when the bytes did not change,
  an empty local save never commits over good server content, and each
  save has a spacing floor so continuously flushing saves (emulator SRAM)
  do not flood their history.
- Transfers are incremental even though revisions are complete: a commit
  is attempted first, and only the content the server reports missing is
  uploaded — an unchanged file, or content another Device already
  uploaded, never travels twice.
- The Dash shows an unnamed revision by its short identifier. Clicking that
  label turns it into an inline name field; Enter or leaving the field saves,
  and Escape cancels. A custom name replaces the identifier in flat and tree
  history views and is used in that revision's download filename.
- Naming a revision changes only its label. Its identifier, content, place in
  history, and current status do not change.
- Every Omnisave has one global current revision, which is the snapshot every
  bound Device synchronizes toward. In the Dash, any revision in that
  Omnisave's tree can be restored as current. Restoring creates no revision,
  changes no revision timestamp or label, and discards no history.
- The next commit after a restore is a child of the restored revision. If that
  revision already had a child, the history branches inside the same
  Omnisave. Branches are unnamed paths; users name important revisions instead.
- Rewinding means restoring an ancestor, fast-forwarding means restoring a
  descendant, and jumping covers a sibling branch. When several descendants
  are available the user chooses the exact node or tip; the server never
  chooses a path.
- Restoring is global server state. A clean bound Device adopts the restored
  content on its next sync. A Device with unsynced changes diverges and gets
  the usual fork-or-jump choice (decision 4): either answer first preserves
  the unsynced progress as a fork, and an unwanted fork is deleted in the
  Dash rather than discarded by sync.
- When a Device reports the game as being played, the Dash's restore dialog
  says so — "Being played on Steam Deck" — and its confirm reads "Rewind
  anyway". Either way the restore only lands on that Device after the game
  closes (decision 13); the dialog makes the wait visible instead of
  surprising.
- Forking at a revision creates a separately named and synchronized Omnisave
  whose current revision is that same immutable node. The fork shares the
  node's label and ancestor path, creates no copied root revision, and owns
  only the revisions later committed through it.
- A revision nothing needs can be deleted in the Dash: a tip that is not
  current, has no later revisions building on it, and is not where a fork
  began. Anything else is refused with the reason. Deleting removes the
  snapshot and any content only it references; labels, history order, and
  the Current Revision do not move (decision 14).
- A Device whose sync baseline named a deleted revision is not broken:
  content matching the Current Revision rebinds silently on the next pass,
  and anything else resolves as a divergence with the usual fork-or-jump
  answer (decision 4). The server does not know Device baselines, so
  deletion cannot warn about them.
- Failures leave both sides valid: an interrupted upload leaves the Current Revision
  where it was; an interrupted download leaves the local save untouched.
- `omnisave` with no command is the whole app, and it skips whatever
  this Device already did: no saved connection asks for one, no tracked
  games asks which to track, and then every run reconciles once — the pass
  that may ask — and watches until it is quit. `track`, `bind`, `sync`,
  `watch`, `scan`, and `connect` stay as the explicit commands.
- `omnisave track` is that run's selection step on its own: it always
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
the Current Revision against the binding's baseline — never timestamps, never "newest
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
revision, but the Current Revision follows the running game. Decision 13
closes most of that window by holding pulls while the game runs.

### 4. Divergence prompts, and both answers keep everything

**Decision:** Headless sync and watch report divergence and skip.
Interactive track asks: fork here, or jump to latest — and jump first
preserves the unsynced local progress as a fork before applying the Current Revision.
**Why:** A binding decides which lineage future revisions extend, and a
silent guess writes history onto the wrong playthrough
([FDR-003](FDR-003-automatic-save-binding.md), decision 6). Making jump
preserve-first means the prompt has no destructive option: nothing is
ever lost to sync, only ever kept on a lineage you may later delete.
**Tradeoff:** Jumps leave a fork to clean up in the Dash, and unresolved
divergences accumulate lineages.

### 5. Sync completes the automatic half of binding

**Decision:** Every sync pass runs the binding decisions FDR-003 makes
without asking — first-save seeding and Current Revision rebinds — so a game first
played after tracking is protected at the next sync, not the next track.
**Why:** Watch makes "played it, so it's protected" true without ritual,
which was the promise tracking made. Prompting cases still wait for
track, where a human is present.
**Tradeoff:** Prompt-shaped work queues up invisibly until an interactive
run; the report is the only nudge.

### 6. The bare command is the app, and it skips what is already done

**Decision:** `omnisave` with no command is a state machine over this
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

### 10. Revision names are mutable labels, not revision content

**Decision:** Revisions may have an owner-supplied display name. An unnamed
revision falls back to its short identifier, and renaming is accepted
last-write-wins without creating a revision or moving the Current Revision.
**Why:** A memorable checkpoint such as "before the final boss" is more useful
than a hash, but presentation metadata does not deserve the concurrency rules
that protect irreplaceable save history. This follows ADR-001's proportional
authority for labels.
**Tradeoff:** Concurrent renames do not conflict; the last accepted name wins.
The short identifier remains the only stable identity for integrations and
diagnostics.

### 11. Current revision is a movable global pointer

**Decision:** An Omnisave has one server-owned current revision. Restoring
moves that pointer to any node in the Omnisave's tree without creating a
revision; commits extend whichever node is current.
**Why:** Revision content stays immutable and recoverable while users can
rewind, fast-forward, and explore alternate progress without multiplying save
slots. One global pointer preserves the server authority and the existing
binding model: every Device follows the same answer unless it forks.
**Tradeoff:** Restoring on one surface affects every bound Device, and a Device
with unsynced work becomes diverged rather than silently reactivating its old
branch.

### 12. Branches are topology; forks are independent saves

**Decision:** A revision has at most one parent and may have several children.
Those unnamed paths remain inside one Omnisave. A fork is the only way to
create another named, bound, independently current Omnisave; it shares the
fork revision and ancestor path rather than copying the snapshot.
**Why:** Users need names and bindings for independent playthroughs, not for
every historical alternative. Sharing immutable ancestry makes lineage
literal, keeps revision names consistent across forks, and avoids an invented
duplicate at every fork point.
**Tradeoff:** Revision retention crosses Omnisave deletion boundaries: shared
ancestors must remain until no surviving Omnisave graph needs them. There are
no branch names or merges; a dead branch is removed by deleting its revisions
tip-first (decision 14).

### 13. A pull never lands under a running game

**Decision:** Before the automatic pull applies, the pass checks whether
the bound game is being played, using the same adapter-owned detection
that powers presence — one process sweep serves both. A playing game
defers the pull: the save reports "Save 1 · waiting for game to close",
and watch — whose presence sweeps tighten to the poll interval
while a pull waits — applies it within about a poll interval of the game
exiting, once the detection stop grace confirms the exit is real. The
games whose pulls wait are recorded per game at the deferral itself, and
they hand off from a track run to its watch phase alongside the watched
files. Only a pass that reached the save comparison speaks for the
standing list: a pass that failed, or whose library sync never reached
the server, knows nothing about which pulls are still held, and taking
its silence for an empty list would drop the exit trigger for a pull that
never applied. The exit-triggered pass honors the quiet-interval rule
like every other trigger, and fires once per exit: a pass that fails
leaves the pull to the periodic one, the fallback every other trigger
already falls back to, rather than retrying every poll for as long as it
keeps failing. A game seen playing again re-arms the trigger, so a
relaunch and a second quit get their own pass. Detection is best-effort
and fails open — an unreadable process list gates nothing — the opposite
of the presence report, which fails closed to protect the standing
picture.
**Why:** A running game holds its save in memory. A pull applied
mid-session is overwritten from memory at the next save and pushed back,
silently undoing the restore the user asked for — the exact failure
decision 3 accepted as a tradeoff. Waiting for the game to close is what
the user means by "rewind" anyway: the restored content is what loads
next.
**Tradeoff:** A rewind issued mid-session is not visible in the game
until it closes and reopens, and a game whose adapter offers no activity
detection is never seen as playing — those pulls fall back to decision
3's pre-placement re-check. The interactive pass checks a sweep taken at
its start, so a game launched while a prompt sits open can slip past the
gate; the re-check absorbs that too. The gate is the automatic pull's
alone: the downloads decision 3 carves out as prompted — joining a
lineage, and decision 4's fork-or-jump answers — are answers the user
just gave about this save, so they apply under a running game and leave
the same in-memory overwrite to the re-check.

### 14. Deleting a revision prunes only what nothing needs

**Decision:** A revision can be deleted only when it has no children, is no
save's current revision, and is no fork's origin. The server refuses anything
else and names the reason. There is no reparenting and no pointer moving; the
Dash offers the action only on tips it can see are unneeded.
**Why:** Deletion should clean the graph, not rewrite it. Moving a current
pointer would rewind every bound Device behind the owner's back, and a child
cannot be reparented because its manifest records its parent and manifests
are immutable ([ADR-012](../adr/ADR-012-portable-save-store.md)). Restricting
deletion to unneeded tips makes it impossible to remove anything a surviving
path still passes through, so cleaning up abandoned branches — divergence
forks jumped away from, mid-session commits — can never orphan history. The
deletion is tombstoned in the store before it is performed, so a crash or a
restored backup cannot resurrect it.
**Tradeoff:** Pruning a dead branch takes one deletion per revision, tip
first. A Device that last synced at a deleted revision loses its baseline and
re-answers with a divergence prompt if its content has moved; nothing is lost
either way, but the server cannot warn which baselines a deletion strands.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — heads move
  only by commit against expected state, which is what makes the baseline
  comparison trustworthy; [ADR-002](../adr/ADR-002-sse-view-invalidation.md)
  — the server event stream that could someday wake watch instead of
  polling; [ADR-012](../adr/ADR-012-portable-save-store.md) — where a
  committed revision comes to rest, and why the content a sync-down replaces
  is recoverable from the store alone.
- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — the binding
  pass whose automatic half sync re-runs and whose lineage philosophy
  divergence inherits; [FDR-004](FDR-004-sync-to-device.md) — joining a
  lineage on a Device with no local save, and the staging policy this feature's pull
  reuses; [FDR-002](FDR-002-game-lifecycle.md) — Device identity and
  liveness, which sync events update.
