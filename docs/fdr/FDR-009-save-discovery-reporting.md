# FDR-009: Save Discovery Reporting

**Status:** Experimental **Last reviewed:** 2026-09-30

## Overview

Discovery keeps only the files it locates, so a game whose save profile is missing, excluded here, or pointing at the wrong place reads the same way: no save. `omnisave scan --verbose` names every location a rule reached and what was there, so a person can check the path and file a report that says which of those happened.

## Behavior

- A verbose scan reports every installed game, with or without saves, and names the build and platform that produced it.
- Each game reports the identity it was matched by, where it is installed, and, inside a Proton prefix, the prefix its user-relative rules expanded into.
- A game no save profile covers says so and names the store identity it was looked up by, distinct from a game whose rules were followed and found nothing.
- The report names the source whose rules answered, so community knowledge is distinguishable from a store's own configuration ([ADR-018](../adr/ADR-018-embedded-save-profiles.md)).
- Each rule reports one outcome: files found, empty, missing, unreadable, skipped (a symlink, or several case-insensitive spellings with no exact one), holding a placeholder this environment cannot fill, or excluded by its platform or store constraint. A rule that reached a location names the absolute path it searched, and found files are listed under it.
- A game a source knows but cannot place — Steam seeing its cloud saves written through the API — reports that reason rather than reading as a game no rule covers. Its save folder stays unknown, not absent, for better rules to place.
- Locations a source offered inside a store's cloud mirror are counted as refused, since a mirror is never a save ([FDR-003](FDR-003-automatic-save-binding.md)).

## Design Decisions

### 1. The report is recorded by the discovery it explains

**Decision:** Resolving a save profile returns its saves and what each rule did from one pass, and reporting changes nothing that pass finds. **Why:** An explanation from a second walk can disagree with the discovery it explains — a different moment, filesystem, or logic — and a diagnostic that lies is worse than none. The reported paths are only useful if they are the paths discovery actually used. **Tradeoff:** Every scan records outcomes whether or not anything reads them, and filesystem trouble is reported only as completely as the pass observed it.

### 2. The report is for debugging and prefers the specific fact

**Decision:** The verbose scan names store identities, entry titles, rule templates, absolute paths, and file sizes. Paths under home are written with `~` and never shortened by eliding their middle. **Why:** The output is meant to be pasted into an issue. A maintainer needs the template to compare against the manifest and the expanded path to see a substitution going wrong. `~` keeps the account name out of the paste, while a path someone must go and check stays exact. **Tradeoff:** It is denser than the default scan, unsuitable as the ordinary view, and long paths wrap.

## Related

- **FDRs:** [FDR-003](FDR-003-automatic-save-binding.md) — a store's cloud mirror is a transport and never a save, which is why a game may report none.
- **ADRs:** [ADR-018](../adr/ADR-018-embedded-save-profiles.md) — the save profile sources the report explains, why scanning is offline, and the patch directory a wrong path eventually becomes an entry in.

## Open Questions

- Whether a game the manifest does not know should name the upstream project a correction belongs to, rather than leaving the routing to the reader.
- Whether the report should be filterable to one game, once a library is large enough that the whole scan is unwieldy to read or paste.
