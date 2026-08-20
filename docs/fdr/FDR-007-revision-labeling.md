# FDR-007: Revision Labeling

**Status:** Experimental **Last reviewed:** 2026-08-19

## Overview

Revision labeling derives a useful display name from the save content in a revision, so history can describe game progress rather than only when a snapshot arrived. Omnisave ships labelers for supported games and uses the same sandboxed contract intended for future owner-provided labelers.

## Behavior

- When a game has a labeler, the server derives a display name from the committed revision and makes that name available wherever revisions are shown.
- Missing, malformed, unsupported, or unrecognized content leaves the revision unnamed. Labeling never prevents the revision from committing.
- A name supplied by a person wins during commit. Explicitly rerunning the labeler later replaces any current name, including a manual one, and records the new name as labeler-derived.
- Labels depend only on revision content, so the same snapshot produces the same result regardless of which Device committed it.
- A person may rerun the current game labeler on any existing revision without changing its content or identity.
- Relabeling is offered only while the server has a labeler matching the game.
- Rerunning a labeler with no usable answer leaves the revision's existing name unchanged.

## Design Decisions

### 1. Labelers are deterministic sandboxed scripts

**Decision:** Labelers use a restricted scripting runtime with no network, imports, or external state. **Why:** Labels must be reproducible, private by default, and safe enough for future owner-authored scripts. Compiled application code would make extension require a server release, while an online model would add nondeterminism, credentials, and data exposure. **Tradeoff:** The server carries an interpreter and script authors work within a smaller language than general application code.

### 2. The server executes labelers

**Decision:** Labeling runs on the server as part of accepting a revision and when a person explicitly requests it for existing history. **Why:** The server holds the authoritative revision content and can apply one contract consistently to every Device and to historical revisions. **Tradeoff:** Labeling consumes server resources on commit and on explicit reruns, and therefore needs bounded execution.

### 3. Built-in and future owner labelers share one contract

**Decision:** Shipped labelers run through the same interface and failure rules planned for owner-provided labelers. **Why:** Built-ins exercise the extensibility and safety model before an editing surface is introduced. **Tradeoff:** Built-in fixes still require a server release until owner-provided labelers exist.

### 4. Names retain their source and explicit reruns replace them

**Decision:** A revision name records whether it was chosen manually or produced automatically. A manual name wins over labeling during commit, while explicitly rerunning the labeler replaces the current name and changes its source. **Why:** Normal synchronization preserves a person's answer, while the relabel action is an unambiguous request to use the current automated answer instead. **Tradeoff:** A deliberate relabel can discard a manually chosen name, and name provenance is durable state that must survive recovery.

### 5. Labelers receive a bounded, read-only revision view

**Decision:** A labeler can inspect canonical paths and read revision content within explicit resource limits; unavailable or unsuitable content behaves as absent. **Why:** One game contract must work across Devices without allowing scripts to inspect the host or make commits unreliable as save formats evolve. **Tradeoff:** Some games cannot be labeled well without future access to changes from the parent revision.

## Related

- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — binding creates the first labeled revision; [FDR-005](FDR-005-save-sync.md) — synchronization creates later revisions and preserves names supplied during commit.

## Open Questions

- How owners create, test, store, and share custom labelers.
- Whether labelers should be able to inspect the delta from a revision's parent.
- Whether slow labelers should eventually run outside the commit request.
