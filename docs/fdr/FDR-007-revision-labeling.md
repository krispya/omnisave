# FDR-007: Revision Labeling

**Status:** Experimental **Last reviewed:** 2026-09-30

## Overview

Revision labeling derives a display name from the save content in a revision, so history describes game progress rather than only when a snapshot arrived. Omnisave ships labelers for supported games through the same sandboxed contract intended for future owner-provided labelers.

## Behavior

- When a game has a labeler, the server names each committed revision from its content, and that name appears wherever the revision is shown.
- Missing, malformed, unsupported, or unrecognized content leaves the revision unnamed. Labeling never prevents a commit.
- The same snapshot gets the same name whichever Device committed it.
- A name the committing client supplies, such as the Device that kept a divergent branch, wins over the labeler at commit.
- While the server has a labeler for the game, a person may rerun it on any revision. A rerun replaces the current name, even one set by hand, unless the labeler has no answer.

## Design Decisions

### 1. Labelers are deterministic sandboxed scripts with one contract

**Decision:** Labelers run in a restricted scripting runtime with no network, imports, or external state, and shipped labelers use the interface and failure rules planned for owner-provided ones. **Why:** Labels must be reproducible, private by default, and safe for owner-authored scripts. Compiled code would tie extension to server releases, and an online model would add nondeterminism, credentials, and data exposure. **Tradeoff:** The server carries an interpreter, authors work in a smaller language, and built-in fixes need a server release until owner labelers exist.

### 2. The server executes labelers

**Decision:** Labeling runs on the server as part of accepting a revision, and when a person explicitly requests it for existing history. **Why:** The server holds the authoritative revision content and can apply one contract consistently to every Device and to historical revisions. **Tradeoff:** Labeling consumes server resources on commit and on explicit reruns, and therefore needs bounded execution.

### 3. Names retain their source, and explicit reruns replace them

**Decision:** A revision name records whether it was chosen manually or produced by a labeler. A supplied name wins over labeling at commit, while an explicit rerun replaces the current name and its source. **Why:** Normal synchronization preserves a chosen answer, while relabeling is an unambiguous request for the automated one. **Tradeoff:** A deliberate relabel can discard a manually chosen name, and name provenance is durable state that must survive recovery ([ADR-012](../adr/ADR-012-portable-save-store.md)).

### 4. Labelers receive a bounded, read-only revision view

**Decision:** A labeler can inspect canonical paths and read revision content within explicit resource limits; unavailable or unsuitable content behaves as absent. **Why:** One game contract must work across Devices without letting scripts inspect the host or make commits unreliable as save formats evolve. **Tradeoff:** Some games cannot be labeled well without access to the changes from the parent revision.

## Related

- **ADRs:** [ADR-012](../adr/ADR-012-portable-save-store.md) — revision names and their source are recorded with the history.
- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — binding creates the first labeled revision; [FDR-005](FDR-005-save-sync.md) — synchronization creates later revisions and supplies names during commit.

## Open Questions

- How owners create, test, store, and share custom labelers.
- Whether slow labelers should eventually run outside the commit request.
