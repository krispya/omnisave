# FDR-009: Save Discovery Reporting

**Status:** Experimental
**Last reviewed:** 2026-08-20

## Overview

Save Discovery Reporting explains where a scan looked for a game's saves and
what it found there. Discovery keeps only the files it locates, so a game whose
community save-location rules are missing, excluded, or pointing at the wrong
place all read the same: no save available. `omnisave scan --verbose` names
every location a rule reached and what was there, so a person can check the
path themselves and file a report that says which of those actually happened.

## Behavior

- A verbose scan reports every installed game, whether or not it has saves, and
  names the build and platform that produced the report.
- Each game reports the identity discovery matched it by, where it is installed,
  and — inside a Proton prefix — the prefix its user-relative rules expanded
  into.
- A game the save profile provider does not know says so and names the store
  identity it was looked up by. This is distinct from a game whose rules were
  followed and found nothing.
- Each of a game's save-location rules reports one outcome: files found, a
  location that exists but holds no files, a location that does not exist, a
  location that could not be read, a location skipped as a symlink, a location
  with several case-insensitive spellings and no exact one, a template holding a
  placeholder this environment cannot fill, or a rule excluded by its platform
  or store constraint.
- A rule that reached a location reports the absolute path it searched, written
  against the home directory. Nothing is elided.
- Rules that met the same outcome are counted in one sentence and their
  locations listed beneath it.
- A manifest entry spells one location once per platform it supports. A location
  another rule already searched is not also reported as skipped, and a location
  excluded under several constraints is reported once with all of them.
- Files the rules found are listed under the rule that found them, capped so one
  large save does not bury the games below it.
- Rules whose files were set aside because an adapter's own save already holds
  the same save family report what they found and that they were set aside
  ([FDR-003](FDR-003-automatic-save-binding.md), decision 10).
- A scan configured with no save profile provider says the rules were not
  consulted rather than implying they were and found nothing.

## Design Decisions

### 1. The trace is recorded by the resolve that produced the saves

**Decision:** Resolving a profile returns both its saves and what each rule did,
from one pass. Nothing re-derives the explanation afterwards.
**Why:** An explanation produced by a second walk can disagree with the
discovery it explains — different moment, different filesystem, drifting logic —
and a diagnostic that lies is worse than none.
**Tradeoff:** Every scan records outcomes whether or not anything reads them.

### 2. Discovery keeps its behavior; only its reporting changes

**Decision:** Rules still find what they found. Outcomes name what was already
happening silently — exclusions, unexpandable templates, symlink and ambiguous
casing skips — without changing which files a scan locates.
**Why:** The reported paths are only useful if they are the paths discovery
actually used ([ADR-018](../adr/ADR-018-embedded-save-profiles.md)).
**Tradeoff:** Outcomes describing filesystem trouble are as complete as the
resolve could observe, not an independent audit of the path.

### 3. The report is for debugging and prefers the specific fact

**Decision:** The verbose scan names store identities, manifest entry titles,
rule templates, absolute paths, and file sizes.
**Why:** Its output is meant to be pasted into an issue. A maintainer needs the
template to compare against the manifest and the expanded path to see a
substitution going wrong.
**Tradeoff:** It is denser than the default scan and unsuitable as the ordinary
view.

### 4. A template is shown only when it adds to its expanded path

**Decision:** A rule template is printed beside its path unless substituting the
home directory makes the two identical.
**Why:** Most rules read `<home>/…`, so printing both doubles every line with no
information. A template naming an account directory or a Windows known folder
still earns its place.
**Tradeoff:** The exact template text is absent for the rules where it matched
the path, and has to be read from the manifest if ever needed.

### 5. Paths are written against home and never elided

**Decision:** A path under the home directory is written with `~`. No path is
shortened by removing its middle.
**Why:** It is shorter to read and keeps the account name out of a pasted
report. A path a person is meant to go and check has to stay exact.
**Tradeoff:** Deeply nested paths are long, and long lines wrap.

### 6. Verbose replaces the file tree rather than extending it

**Decision:** `--verbose` renders the discovery report. The previous tree of
targets, saves, and files is gone.
**Why:** The tree could only show what was found, which is the case that needed
no explaining. Two verbose modes would leave a person guessing which one answers
their question.
**Tradeoff:** Output shapes that read the tree have to read the report instead.

## Related

- **FDRs:** [FDR-002](FDR-002-game-lifecycle.md) — scanning detects games
  offline and without configuration;
  [FDR-003](FDR-003-automatic-save-binding.md) — one representation per game,
  which is what a set-aside profile reports.
- **ADRs:** [ADR-018](../adr/ADR-018-embedded-save-profiles.md) — the embedded
  community manifest whose rules the report explains, including the patch
  directory a wrong path eventually becomes an entry in.

## Open Questions

- Whether a game the manifest does not know should name the upstream project a
  correction belongs to, rather than leaving the routing to the reader.
- Whether the report should be filterable to one game, which matters once a
  library is large enough that the whole scan is unwieldy to read or paste.
- Whether unreadable locations should be reported as precisely inside glob
  recursion as they are at a rule's own path.
