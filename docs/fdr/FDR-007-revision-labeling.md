# FDR-007: Revision Labeling

**Status:** Experimental
**Last reviewed:** 2026-08-10

## Overview

Revision labeling names each committed revision from the save content it
carries, so a history reads as moments in a game — "Necro A5, Hive flr 18,
53/66 HP" — rather than as timestamps. A labeler is a small script
bound to one game; Omnisave ships built-in labelers for supported games, and
the same mechanism is designed to accept user-provided labelers later.

## Behavior

- When a revision is committed for a game that has a labeler, the server runs
  the labeler against the revision's file set and stores the answer as the
  revision's display name. Every client — CLI, TUI, Dash — sees the name
  wherever revision names already appear.
- A game without a labeler, a labeler with nothing to say, and a labeler that
  fails all produce the same result: the revision commits unnamed, exactly as
  every commit did before this feature. Labeling never fails or delays a
  commit beyond its execution limits.
- Renaming a revision by hand takes the name over: automation never replaces
  a manually chosen name. Labeler-written names remain automation's to
  replace (a future relabel/backfill), and that distinction survives database
  rebuilds from the store.
- Labels are derived only from the revision's content, so the same snapshot
  labels identically no matter which device committed it, and existing
  revisions can be labeled retroactively once a backfill exists.
- Slay the Spire II is the first supported game: mid-run snapshots name the
  character, ascension, act, floor, and health; finished runs name the
  outcome — a win with floors climbed and duration, a death with its killer
  and where, an abandonment. Long character names use compact aliases in the
  revision rail (`Necrobinder` becomes `Necro`). Floors match the game's counter
  by counting every visited map point, including an act's opening Ancient. A
  fresh profile stays unnamed.

## Design Decisions

### 1. Labelers are Starlark scripts, not Go code and not an LLM

**Decision:** A labeler is a Starlark script implementing `label(snapshot)`,
executed in a sandboxed interpreter embedded in the server.
**Why:** The end state is user-provided labelers edited in the Dash, which
rules out compiled-in Go as the contract. An LLM at commit time was rejected
because labels must be deterministic across devices (a revision's name is
shared presentation metadata), free of network and key dependencies, and
private by default; generating the *script* is a fine job for an LLM at
authoring time instead. Starlark specifically: deterministic, hermetic
(no I/O, no imports), terminating by construction in the dialect used, and
close enough to Python that authors need no new language.
**Tradeoff:** A real interpreter dependency and its unfamiliar corners (no
f-strings, no width flags in `%` formatting) versus plain Go's zero overhead.

### 2. The server executes labelers, not clients

**Decision:** Labeling happens inside the commit operation on the server.
**Why:** The server already holds every revision's content as artifacts, so it
can label any commit — and later, any historical revision — without
distributing scripts to devices or managing version skew between them. One
sandbox to harden; every client benefits identically.
**Tradeoff:** The server runs script code on every commit and must bound it
(execution steps, wall clock, read budget). Commit latency absorbs a labeler
run, which the limits keep small.

### 3. Built-ins are embedded scripts on the same runtime as future user scripts

**Decision:** Shipped labelers are `.star` files compiled into the server
binary, each self-registering for the game identities it labels. There is no
storage, editing, or upload surface yet.
**Why:** Committing to the script contract now means the risky half of user
extension — sandboxed execution, contract expressiveness, failure semantics —
ships and gets proven by the built-ins long before the first user script
runs. Extension later is storage plus an editor, with built-ins becoming the
fallback a user script shadows.
**Tradeoff:** Adding or fixing a built-in labeler requires a server release.

### 4. Names carry their source, and manual always wins

**Decision:** Every revision name records whether the labeler or a person set
it. Renaming by hand stamps the name manual; automation may only ever write
over its own names or empty ones. The source survives in the durable store
alongside the names, and store records that predate sources are treated as
manual.
**Why:** Without provenance, the first relabel or backfill would overwrite
names people chose, and the feature's trust with it. Recording it at write
time is the only non-lossy option.
**Tradeoff:** A little provenance plumbing through the database, the store,
and rebuilds, carried before any backfill exists to consume it.

### 5. Scripts see one revision through a two-tier snapshot

**Decision:** A labeler receives only a read-only snapshot of the revision's
file set, addressed by canonical save paths. Path and size questions are
answered from the revision manifest for free; reading file content opens
artifacts lazily under per-file and per-run byte budgets. Missing, oversized,
and malformed files all read as absent.
**Why:** Canonical paths are identical on every device, which is what lets
one script serve all of them. The two tiers keep multi-file saves cheap: the
Spire labeler navigates a 226-file revision by paths alone and reads at most
two files. Absent-not-error keeps a labeler's failure mode "shorter name or
none" as save formats drift across game updates.
**Tradeoff:** Scripts cannot diff against the parent revision yet; games
whose filenames encode nothing may want that to find "what just changed".

## Related

- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) (binding seeds the
  commits that get labeled), [FDR-005](FDR-005-save-sync.md) (sync is where
  commits, and therefore labels, come from)

## Open Questions

- User-provided labelers: storage keyed by game, a Dash editor with dry-run
  preview against real revisions, and a backfill action. The contract here is
  written to survive that unchanged; the trust story for sharing labelers
  between owners is deliberately undecided.
- A `snapshot.changed()` view of the delta against the parent revision, for
  games whose file layout doesn't reveal recency.
- Whether commit-time labeling should ever move off the request path if a
  game's labeler proves slow in practice.
