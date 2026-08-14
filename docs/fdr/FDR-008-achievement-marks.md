# FDR-008: Achievement Marks

**Status:** Experimental
**Last reviewed:** 2026-08-13

## Overview

Achievement marks put a game's unlocks on the save history that surrounds
them, so a revision rail reads as a playthrough rather than a column of
timestamps. A mark answers one question: this is the save where the
achievement had been earned, and everything before it is from before. It is
an orientation aid for choosing where to go back to, not a trophy case.

## Behavior

- A Device watching a game notices when its store records an achievement and
  tells the server, which places the unlock on the first revision committed at
  or after it — the earliest snapshot known to carry it. The Dash places a
  compact trophy beside that revision's name. One trophy represents the first
  achievement, `+1`, `+2`, and so on count any others on the same revision,
  and hovering or focusing the badge reveals their names.
- Only unlocks a Device watched happen are marked. The first pass over a
  bound save records where the game's unlock history already stood and reports
  nothing, because achievements earned before Omnisave was watching belong to
  sessions it holds no revisions for.
- An unlock reported before the save is written waits, and the next commit on
  that save claims it. That is the ordinary order — the toast comes before the
  autosave — and it is why placement is decided by time rather than by
  whichever request happens to arrive first.
- Repeating a report never moves a mark: the placement an achievement was
  first given is the one it keeps, however many Devices report it.
- Deleting a marked revision leaves the achievement waiting again rather than
  taking it away; the next commit claims it. Nothing about a mark is lost with
  a node.
- Steam is the first supported store: the client reads the unlock times and
  achievement names out of Steam's own caches on disk, with no API key, no
  account, and no network. A game with no achievements, a store that cannot
  report them, and a cache Steam is midway through rewriting all read the same
  way — no marks, and the save syncs exactly as it always did.

## Design Decisions

### 1. A mark is placed by time, not attached at commit

**Decision:** A Device reports unlocks with the store's own unlock times; the
server places each on the first revision whose commit is at or after it. An
unlock newer than every revision is stored unplaced and claimed by the next
commit.
**Why:** An unlock and a save write are two independent events, and neither
reliably comes first — a game may autosave then fire the achievement, or fire
it and autosave a minute later. Attaching marks to whatever revision the
reporting pass happened to write would make the answer depend on polling
timing. Placing by time makes the mark mean exactly what it claims, and makes
reports idempotent and order-independent: a report may arrive before its
commit, after it, twice, or from a second Device, and the mark lands the same.
**Tradeoff:** Placement is a rule the server has to run, including at commit
time for waiting marks, rather than a field the client fills in. Unlock times
are whole seconds, so an unlock and a commit within the same second resolve in
the commit's favor.

### 2. Marks are lineage state, not part of a revision

**Decision:** Achievements are stored against the Omnisave, in their own table
and in the portable lineage record, alongside revision names — never inside a
revision manifest.
**Why:** [FDR-007](FDR-007-revision-labeling.md) rests on a label being
derivable from a snapshot's content, identical on every Device and
reproducible from the store. An achievement satisfies none of that: it belongs
to an account, not to bytes, and the same snapshot restored under another
account has earned nothing. Manifests are immutable besides, so a mark that
can be claimed, released by a deletion, and claimed again could not live in
one. The lineage record already carries exactly this kind of mutable
per-revision state.
**Tradeoff:** Two places now describe a revision — its manifest and its
lineage record — and a rebuild has to import marks after the nodes they name.

### 3. Detection belongs to the adapter, reading the store's own records

**Decision:** `target.Achievements` is an optional adapter capability
answering, for one discovered save, what that target records as unlocked and
when. Steam implements it by reading `UserGameStats_<account>_<app>.bin` and
the schema beside it out of its install root.
**Why:** Only the adapter knows where a store keeps this, and the same shape
serves any other store later. Reading Steam's local caches specifically was
chosen over Steam's Web API because it needs no owner credential, no public
profile, and no network, and still carries Valve's own unlock timestamps —
which is what makes two Devices agree on when an unlock happened. Detection
has to run on the Device because that is where the store's records are, even
though everything else about a mark is the server's.
**Tradeoff:** A binary format Valve does not document, read defensively: a
schema that no longer names a bit, a cache being rewritten, and a cache that
changed shape all report nothing rather than failing.

### 4. No backfill of achievements earned before Omnisave watched

**Decision:** The first look at a bound save records the newest unlock already
present and reports nothing; later passes report only what is newer.
**Why:** Every achievement a Steam account has ever earned is sitting in that
cache with its unlock time. Reporting them all would pile a decade of unlocks
onto whichever revision happens to be the oldest one Omnisave holds, which is
a claim about that snapshot that is simply untrue. A mark is only worth
trusting if it means what it says.
**Tradeoff:** A library adopted mid-playthrough shows no marks for what came
before, and rebinding a save starts its watch over.

### 5. The client keeps a bounded watermark per binding, not a full history

**Decision:** A binding remembers the newest unlock time it has accounted for
and the achievement identities seen at that exact second. Anything later, or
anything new at the boundary second, is reported.
**Why:** Store times have whole-second precision, so several achievements may
tie and a report-size boundary may split them across passes. Remembering the
identities only at the newest second prevents those ties from being skipped
without growing state with every achievement in a library. A failed report
leaves the watermark where it was, which is what makes the next pass retry.
**Tradeoff:** An achievement earned before the watch began but synced to this
Device afterwards carries its true, older time and is therefore never
reported — correctly, since Omnisave did not watch it happen. The binding may
briefly retain several identities when a game unlocks many achievements in
one second.

### 6. Revision rows show compact marks and reveal names on demand

**Decision:** A marked revision shows a trophy-only badge beside its name,
plus the number of additional achievements when several landed there. Hovering
or focusing the badge opens the achievement-name list.
**Why:** The mark should orient someone scanning history without letting long
or numerous achievement names compete with revision names and status. The
generic trophy remains recognizable at a glance, while the popover keeps the
specific unlocks one interaction away.
**Tradeoff:** Achievement names are not visible until the badge is inspected,
and the generic mark does not carry a store's achievement artwork.

## Related

- **FDRs:** [FDR-005](FDR-005-save-sync.md) (the sync pass is where unlocks are
  noticed and reported), [FDR-007](FDR-007-revision-labeling.md) (names are
  derived from content; marks deliberately are not)
- **ADRs:** [ADR-012](../adr/ADR-012-portable-save-store.md) (marks travel in
  the lineage record and survive a rebuild),
  [ADR-014](../adr/ADR-014-durable-proof-before-forgetting.md) (placement at
  commit rides the same transaction as the commit)

## Open Questions

- Store artwork. Marks use a generic trophy. Steam's schema carries an icon
  hash whose image lives on Valve's CDN; showing the store-specific image
  means either a request from the Dash to a third party or storing it as an
  artifact. Neither has been argued yet.
- Whether a mark should be visible on a revision's descendants rather than
  only on the one that first carried it. The rail's order already says
  "everything below predates this", so it has not been needed.
- Other stores. The capability is shaped for them, but nothing but Steam
  implements it, and stores without a local record of unlock times may need
  the credentialed path this deliberately avoided.
