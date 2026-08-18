# FDR-005: Save Sync

**Status:** Experimental
**Last reviewed:** 2026-08-13

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
  - Both moved — what happens depends on where the Current Revision went.
    A Current Revision descending from the baseline means another Device
    built on this same line, so both sides hold progress the other lacks:
    the save is diverged, and sync never resolves divergence on its own. A
    Current Revision the baseline descends from — a restore rewound it — or
    one sitting on a sibling branch adds nothing this Device is missing, so
    the local progress commits as a branch off the baseline and the Current
    Revision follows it (decision 15).
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
- `omnisave service install` keeps watch running through the platform's native
  per-user background manager. It requires a connected device, starts the
  service immediately, and arranges for it to return at boot or login.
  `service status` distinguishes running, stopped, and not installed;
  `service uninstall` stops it and removes its definition. Linux, macOS, and
  Windows expose the same commands.
- Diverged saves are reported — "save diverged from …; run
  omnisave track to resolve" — and skipped until someone answers: fork
  here, and this Device's progress continues as a new lineage, or take
  current, which first preserves the unsynced local progress as a fork
  and then applies the Current Revision. Neither choice destroys content.
- The answer can be given from the live watch view without stopping it:
  the row says which Omnisave is waiting, `r` raises the question as a
  modal over the pinned block, and the choice is applied by the pass that
  follows (decision 16). Escape decides nothing. A watch with no terminal
  keeps reporting and skipping, and an interactive track run still asks
  the same question the same way.
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
- A pass reads a bound save only when something could have happened to it.
  A save whose files stand exactly as they did when a pass last proved them
  equal to the revision they are synced to, under an Omnisave whose current
  revision has not moved since, is settled without being opened and without
  asking the server for its history (decision 19). A pass also restates what
  this Device tracks only when that has changed, or hourly, rather than once
  per game per pass. A pass over a library where nothing has happened does
  no work, says nothing, and moves no row.
- Transfers are incremental in both directions even though revisions are
  complete. Committing attempts first and uploads only the content the
  server reports missing, so an unchanged file, or content another Device
  already uploaded, never travels twice. Syncing down transfers only the
  content this Device does not already hold: a rewind or a fast-forward
  inside one lineage usually moves a few files and leaves the rest
  identical, and those are staged from the local save rather than fetched
  again (decision 18).
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
- The next commit from a Device that adopted a restore is a child of the
  restored revision. A Device that has unsynced progress instead commits onto
  the revision its own content continues, wherever that sits. Either way, a
  node that already had a child gains a second one and the history branches
  inside the same Omnisave. Branches are unnamed paths; users name important
  revisions instead.
- Rewinding means restoring an ancestor, fast-forwarding means restoring a
  descendant, and jumping covers a sibling branch. When several descendants
  are available the user chooses the exact node or tip; the server never
  chooses a path.
- Restoring is global server state. A clean bound Device adopts the restored
  content on its next sync. A Device with unsynced changes commits them as a
  branch and becomes current again (decision 15) rather than stopping to ask:
  its progress is never held behind a prompt, and the restored revision stays
  in the tree to be restored again once that Device is quiet.
- When a Device reports the game as being played, the Dash's restore dialog
  says so — "Being played on Steam Deck" — and its confirm reads "Rewind
  anyway". The restore lands on that Device once the game closes
  (decision 13). If the game writes a save before then, that progress
  branches and takes the Current Revision back, so a rewind issued
  mid-session may need issuing again after the game closes. Nothing is lost
  either way: both lines stay in the tree.
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
- A diverged save shows its condition alone — "Save 1 · diverged" — and
  the footer offers `r resolve` only while something is actually waiting.
- A game's glyph carries what is most current about it: the game the
  running pass has in hand spins, a game being played reads as ▶, and
  otherwise the state the last pass settled stands. The spinning row says
  what is being done to it — "Save 1 · downloading (1/3)" — and returns to
  its settled status when the pass lets go (decision 17).
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

**Decision:** Divergence is new progress on *both* sides of one line — a
Current Revision that descends from this Device's baseline while the local
content moved too (decision 15 covers every other shape). Headless sync and
watch report it and skip. Interactive track asks: fork here, or jump to
latest — and jump first preserves the unsynced local progress as a fork
before applying the Current Revision.
**Why:** Only a Current Revision built on top of the baseline represents
work this Device has never seen, so only then does continuing locally mean
choosing between two real playthroughs — and a binding decides which lineage
future revisions extend, so a silent guess writes history onto the wrong one
([FDR-003](FDR-003-automatic-save-binding.md), decision 6). Making jump
preserve-first means the prompt has no destructive option: nothing is
ever lost to sync, only ever kept on a lineage you may later delete.
**Tradeoff:** Jumps leave a fork to clean up in the Dash, and unresolved
divergences accumulate lineages. Two Devices that both keep playing still
reach this prompt on every pass until one of them stops.

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

### 8. Watch is a foreground process; the native manager owns its lifetime

**Decision:** Watch runs in the foreground until stopped. Starting it again,
starting it at boot or login, and logging belong to the platform's native
per-user manager. The client installs the matching systemd user unit,
LaunchAgent, or Scheduled Task ([ADR-017](../adr/ADR-017-client-user-service.md)).
**Why:** Daemonization rituals are obsolete; service managers supervise
better than any hand-rolled fork-and-pidfile dance, and a foreground
process is trivially debuggable.
**Tradeoff:** Installing the service is still its own step, taken once. The
client writes a native definition outside its own state directory and must
keep three platform lifecycles behaviorally aligned.

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
Those unnamed paths remain inside one Omnisave, and a Device reaches them by
committing (decision 15) as readily as the Dash reaches them by restoring. A
fork is the only way to create another named, bound, independently current
Omnisave; it shares the fork revision and ancestor path rather than copying
the snapshot.
**Why:** Users need names and bindings for independent playthroughs, not for
every historical alternative. Sharing immutable ancestry makes lineage
literal, keeps revision names consistent across forks, and avoids an invented
duplicate at every fork point. Keeping branching cheap enough for sync to do
silently is what stops ordinary rewinds from minting Omnisaves nobody asked
for.
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
3's pre-placement re-check. A deferred pull is also not a reservation: if
the game writes a save while the pull waits, that progress branches and
takes the Current Revision back (decision 15), and the rewind has to be
issued again once the game is closed. The alternative — holding the
Device's progress hostage until the deferred pull lands — would make the
gate silently outrank the player at the controller. The interactive pass checks a sweep taken at
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
deletion is recorded by a committed immutable marker before it is acknowledged,
so a crash or a restored backup cannot resurrect it
([ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md)).
**Tradeoff:** Pruning a dead branch takes one deletion per revision, tip
first. A Device that last synced at a deleted revision loses its baseline and
re-answers with a divergence prompt if its content has moved; nothing is lost
either way, but the server cannot warn which baselines a deletion strands.

### 15. A rewound Current Revision is a branch, not a conflict

**Decision:** When both sides have moved, sync classifies by where the
Current Revision sits relative to the baseline. Only a Current Revision
descending from the baseline is a conflict (decision 4). Anything else — a
restore that rewound current to an ancestor, or moved it to a sibling
branch — means the server holds no progress this Device lacks, so the pass
commits the local content as a child of the baseline it actually continues
and lets the Current Revision follow that commit. It happens headlessly,
with no prompt and no new Omnisave. A commit therefore names its parent
separately from the Current Revision it expects: the parent is where the
node attaches and supplies the file set the change applies to, while the
expected Current Revision remains the optimistic-concurrency check
([ADR-001](../adr/ADR-001-server-authority.md)). A binding that never had a
baseline has no node to branch from and stays a divergence.
**Why:** The old rule read every restore under unsynced progress as a
conflict and answered it with a fork, which broke sync until someone ran an
interactive pass and then left a second Omnisave to clean up — heavy
machinery for a case where nothing is actually in contention. Attaching to
the baseline rather than to current is what makes it honest: the content
continues that node, and manifests record their parent immutably
([ADR-012](../adr/ADR-012-portable-save-store.md)), so claiming the restored
node as parent would record a lineage that never happened. Splitting parent
from expected current keeps the guarantee ADR-001 actually cares about — a
Device that has misread the pointer is still refused — while dropping the
incidental requirement that the two be the same node.
**Tradeoff:** A rewind is undone by the next commit from a Device that never
adopted it, so rewinding a save someone is actively playing may not stick
until they stop. The sibling case can also move current off another Device's
branch, and two Devices playing at once will take turns holding it until one
of them goes quiet; every line survives in the tree, but "what is current"
belongs to whoever committed last. Sync creates branches silently, so a
history can grow paths nobody deliberately made — decision 14's tip-first
deletion is the only cleanup.

### 16. Watch hosts the question but never raises it

**Decision:** Watch still never prompts. The user asks: a save waiting on
a divergence offers `r`, which takes over the pinned block with the two
answers and nothing else. What comes back is not a resolution but a
recorded decision, keyed by the game and Omnisave the question named; the
pass that follows replays it through the same reconcile an interactive
track run uses. An answer is spent by the pass it is replayed into, the
key is ignored while a pass is running, and no pass runs while a question
is open. Escape records nothing and runs nothing. A watch with no
terminal has no key and behaves exactly as before.
**Why:** The rule watch is built on is that nothing blocks on a question
nobody may be there to answer, and a prompt the pass raises breaks it —
under a service manager it would wait forever. But the rule was costing a
user who *is* there: a divergence stranded the save with no backup at all
until they quit watch, ran track, and started over. Letting the user ask
keeps the guarantee and removes the cost. Replaying the answer rather than
acting on it directly is what keeps one reconcile path: the answer is
validated against fresh state, so a divergence that resolved itself
between question and pass is simply never reached, and a decision can
never apply to a situation the user did not see.
**Tradeoff:** Passes are held while a question is on screen, so a save
written behind an open modal commits on the first poll after it closes and
server movement noticed meanwhile falls back to the pull ticker. Several
waiting saves are answered one at a time rather than picked from a list.
And an answer given about a table that a concurrent `omnisave bind` has
since changed is discarded rather than reconciled, which is the cautious
half of the same validation.

### 17. A running pass narrates itself on the row it is working

**Decision:** While a pass runs it names the game it currently has in hand,
and that game's row wears the spinner in place of its glyph — over the
playing marker, over its settled state — and says what is being done to it
instead of when it last succeeded: "Save 1 · downloading (1/3)",
"Save 1 · placing". The words are the work's own, the same ones an
interactive command shows on its one-line spinner; a phase that names the
game is trimmed, since the row already does. Work between games — scanning,
registering the device, reaching the server about all of them — has no row
and reads beside the header spinner instead. The table itself is still
replaced only when the pass finishes, so the settled status returns intact
the moment the pass lets the row go, and a finished pass leaves neither a
mark nor a phase. Plain mode has no row to mark and stays silent.
**Why:** A rewind from the Dash arrives here as a pull that can take real
time, and until it lands the view could only offer a header spinner, which
says a pass is running but not what it is doing or to what. A watcher's
whole job is to be believable while nothing appears to happen, and the
difference between a sync in progress, one deferred behind a running game,
and one that never triggered was not visible. A spinner alone says a row is
busy; the phase is what separates working from stuck, since a spinner
turning on "downloading (2/3)" reads nothing like one that has sat on
"checking" for a minute. The phases already existed for the interactive
commands' spinner — watch simply had nowhere to put them.
A pass takes a game in hand only where work begins, never where it merely
considers one: a settled pass marks nothing and the table sits still.
**Tradeoff:** The phase displaces the settled status for as long as the row
is held, so "synced 4m ago" is unavailable exactly while a sync is running;
this is the trade that makes the row worth watching. Work is marked per
game rather than per operation, so a row spins the same way for a save
being read as for one being uploaded, and a game the library phase and the
save phase both do work for is marked twice. Phases are live signals like
presence, not part of the report: one missed costs nothing, because the
next one replaces it.

### 18. A sync down fetches only what this Device does not already hold

**Decision:** Applying a revision stages every file it contains, but a file
whose content this Device already holds is staged from the local save
instead of the server. The revision being left is the index of what is on
disk — the apply has just proved the local save equals it — and content is
addressed by hash, so a hash appearing in both revisions is content already
present, wherever it sits. Content repeated within one revision is
transferred once and reused for every path it appears at. Local copies are
verified exactly as downloads are, and any failure — the file moved, or is
no longer what the revision said — falls through to the download rather
than trusting what it found.
**Why:** A rewind or a fast-forward inside one lineage typically changes one
save file and leaves the others untouched, and the transfer was the whole
revision every time — for the common case, most of a save fetched to
replace a part of it. This is the same property the upload side already
used, applied to the direction that lacked it.
**Tradeoff:** Reused content is still copied into staging rather than left
in place, because the apply's all-or-nothing swap replaces every file it
manages and rolls the whole set back together; leaving files untouched
would need a second, narrower rollback path. A local read is cheap against
a network fetch, so the copy stays. The fallback to downloading cannot be
reached in a test — it exists for a game rewriting a save between the match
check and the copy — so it is defensive code that carries the guarantee
rather than proven behavior.

### 19. A pass proves a save settled by looking, not by reading

**Decision:** When a pass proves a bound save equal to its Omnisave's current
revision, it remembers how the save's files stood — the same size and
modification-time summary the watch loop's poll compares between ticks.
A later pass that finds the same summary, on an Omnisave still pointing at
the revision that summary was proved against, settles the save without
reading it and without fetching its history. Both halves are required:
unchanged files under a moved current revision is a pull, and moved files
under an unchanged current revision is a commit. The summary is only ever
grounds to skip work — it is recorded solely by the pass that proved the
equality, it is taken before the save is read so a write landing mid-pass
invalidates it rather than being recorded as verified, and every sync drops
it, since a sync moves exactly what it described.
**Why:** Every pass read every bound save in full and asked for every
Omnisave's history, and passes run on the periodic pull and on any Device's
commit to anything — so another machine saving one game cost this one a
full read of its whole library. Nothing about that work could change the
answer for a save nothing had touched. Measured on one 32 MB save, the
save phase of a settled pass falls from 13.3 ms to 0.15 ms, and the pass
stops spending a request per Omnisave.
The same holds for the library half of a pass: tracking is a claim this
Device makes to the server — this game, this adapter, installed or not —
and restating an unchanged claim tells it nothing, so the claim is repeated
only when it changes, or once an hour so that an unnoticed loss on the
server's side cannot outlive that. Deletion is not what this catches: a
game gone from the Library is found by the one listing every pass already
makes.
**Tradeoff:** The summary is stat evidence, so content rewritten with its
size and modification time preserved is not noticed until something else
moves. That is not a new assumption — the poll already decides whether to
run a pass at all on exactly this evidence (decision 2) — but it does
extend it from choosing when to look to choosing whether to read. A sync
drops the summary rather than updating it, so the pass after any real sync
reads the save once to re-establish it; that pass had already done far more
work than one read.

### 20. Every way a watcher goes quiet has to end on its own

**Decision:** Watch recovers from its own failures without anything
external prompting it. A failed pass is retried on a backoff that starts
well inside the pull interval and grows to it, rather than waiting for the
next scheduled pull. A failed pass keeps the file list the last successful
pass proved, instead of adopting the empty one it just failed to build. And
the event stream is ended by silence: a stream that says nothing at all for
several of the server's keepalive intervals is redialed rather than read
from forever.
**Why:** Every trigger watch has assumes something still works. The poll
notices local writes, but only among files a pass discovered — so a pass
that failed and cleared that list leaves the poll comparing nothing, and
the device stops noticing its own saves for as long as the server is
unreachable. Server-side movement arrives on the event stream, but a
device that suspends wakes holding a socket whose peer is gone without
having said so: no bytes, no error, and a read that blocks for as long as
the kernel keeps it. Both failures are invisible, both leave the periodic
pull as the only remaining trigger, and the pull is spaced for a server
with nothing to say rather than for a client that has lost track of it.
On the devices this matters most for there is nobody watching a terminal to
notice, so recovering has to be something the loop does rather than
something a person does.
**Tradeoff:** Three more pieces of timing to reason about, and a
handheld that spends a day out of range wakes up to redial and retry on a
schedule it cannot be told to skip. The retry ceiling is the pull interval
precisely so that a device with no server ends up asking exactly as often
as a device with a quiet one.

### 21. A revision records when the save was written, not only when it arrived

**Decision:** Every commit reports the newest modification time among the
save's files, and the revision keeps it beside its creation time as
`saved_at`. It is a client-reported fact the server stores verbatim: the
portable manifest carries it, and an Omnisave surfaces its current
revision's value. Dash leads with it wherever it says what a save is from —
the save card and the history stamp — and keeps the sync time beside it in
tooltips and details. Revisions committed before devices reported the field
have no saved date, and every display falls back to the sync time.
**Why:** A revision's creation time is when content reached the server, and
for a save that has not been played in months the two dates tell different
stories: the sync date says "today" about progress written long ago. The
save's file times are the only witness of when the game wrote it, and the
committing Device is the only party that ever sees them.
**Tradeoff:** File modification times are only as honest as the device's
clock and whatever last touched the files — a copy that rewrites mtimes
reads as freshly saved. That is the same stat evidence the change poll and
the settled proof already stand on (decisions 2, 19), extended from when to
sync to what the synced content says about itself. History stays ordered by
creation time, so a freshly synced old save sits newest in the list while
wearing an old date; the tooltip naming both dates is what makes that read
as intended rather than as disorder.

## Related

- **ADRs:** [ADR-001](../adr/ADR-001-server-authority.md) — heads move
  only by commit against expected state, which is what makes the baseline
  comparison trustworthy; [ADR-002](../adr/ADR-002-sse-view-invalidation.md)
  — the server event stream that could someday wake watch instead of
  polling; [ADR-012](../adr/ADR-012-portable-save-store.md) — where a
  committed revision comes to rest, and why the content a sync-down replaces
  is recoverable from the store alone;
  [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md) — the durable
  proof required before revision content can be forgotten;
  [ADR-017](../adr/ADR-017-client-user-service.md) — running watch with
  nobody in front of it, which is what makes decision 20's recoveries the
  only ones a device gets.
- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — the binding
  pass whose automatic half sync re-runs and whose lineage philosophy
  divergence inherits; [FDR-004](FDR-004-sync-to-device.md) — joining a
  lineage on a Device with no local save, and the staging policy this feature's pull
  reuses; [FDR-002](FDR-002-game-lifecycle.md) — Device identity and
  liveness, which sync events update.
