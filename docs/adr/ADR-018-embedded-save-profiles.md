# ADR-018: Compile Save-Location Knowledge into the Client

**Date:** 2026-08-13

## Context

The Steam adapter's only native save source was Steam Cloud, so a game that opts
out of Steam Cloud — Undertale is the canonical case — scanned as having no save
at all. Save Profiles close that gap with save-location rules from the community
Ludusavi manifest, and
[FDR-004](../fdr/FDR-004-sync-to-device.md) already describes Steam locations
as "save-location rules expanded against the environment", but the distributed
client did not yet include or fetch that knowledge.

The community manifest covers far more than the client needs. Omnisave consumes
only entries associated with Steam games and their save paths. The question is
how that data reaches a Device. Scanning is
deliberately client-local and offline — detection is cheap and indiscriminate
([FDR-002](../fdr/FDR-002-game-lifecycle.md)), and `omnisave scan` works with
no server and no configuration — which any answer has to preserve.

## Decision

Ship the manifest inside the client binary, pruned to what the client reads.

A maintained build step downloads the community manifest, applies the same
interpretation used by the client, and keeps only the Steam save rules the
client consumes. The result is checked into the repository and embedded in the
client, so builds and scans are offline and reproducible. Deterministic output
keeps upstream refreshes reviewable, and the data is loaded only when a profile
is first needed.

Reviewed patches in `internal/client/saveprofile/ludusavi/patches` cover
narrowly scoped paths when an upstream entry is known to be wrong and cannot be
corrected promptly. Patches are additive for now: each file identifies one
existing upstream title and Steam id, documents the reason and upstream source,
and adds save rules without removing or replacing community rules. The build
applies patches before pruning and fails when their title or Steam identity
drifts or when a patch adds no effective rule. Tests also verify that every
checked-in patch reached the generated manifest, so the directory remains a
visible current inventory rather than an untracked fork. A patch is removed
once the equivalent correction arrives upstream.

The distributed client archives include `THIRD_PARTY_NOTICES`, which preserves
the Ludusavi Manifest's MIT copyright and permission notice alongside the
derived data embedded in the binary.

A handful of interpretation rules make community data safe to consume.
Duplicate Steam ids from renamed pages and split editions merge every
rule-bearing title into one profile; the first such title in sorted order names
it deterministically, so a stub page never shadows an entry that locates saves.
Exact rules repeated across pages land once, while distinct platform and store
constraints on the same path survive. Rules over Steam's `userdata`
directories are dropped however the manifest spells them: those are Steam
Cloud's, already discovered and attributed to an account by the steam adapter,
and a profile rule over them would report every Cloud save twice. For the same
reason a game's profile saves stand aside when the adapter's own save already
holds the same save family — files by the names the rules locate — so a Cloud
game's native folder is not rediscovered as a competing layout, while a
mirror holding only auxiliary content suppresses nothing
([FDR-003](../fdr/FDR-003-automatic-save-binding.md), decision 10). Only absolute expanded
paths are searched or offered as destinations, which discards the manifest's
occasional prose and relative entries. Path matching — literal rules
included — is case-insensitive, as Ludusavi's is, because community casing
is unreliable. When several on-disk spellings fold to one literal rule and
none is exact, the path is ambiguous and resolves nothing rather than choosing
a save or restore destination arbitrarily. Metacharacters that arrive through
real filesystem values (a library named with brackets) stay literal rather
than becoming pattern syntax.

A rule's identity — the location id recorded in revision file paths on the
server — derives from its template text, not its position in the entry, so
canonical paths keep meaning across manifest refreshes that add, remove, or
reorder a game's other rules. An entry's rules are also each other's
aliases: a resolved save carries every spelling its entry gives the
location, which is what lets a revision minted under another OS's template
be recognized as the same place
([FDR-003](../fdr/FDR-003-automatic-save-binding.md), decision 11).
Filesystem trouble while resolving one rule narrows what that rule finds;
it never fails the scan.

Freshness rides releases. A new game's save location becomes known to devices
through the next client release and `omnisave update`
([ADR-015](ADR-015-client-binary-distribution.md)), not through a runtime
download. Downloading at runtime (Ludusavi's own approach) would add network,
cache state, and staleness handling on the least-willing hosts Omnisave
targets, and serving profiles from the server would couple scanning to a
connection it deliberately does not need. Either remains open later without
changing the profile contract, since providers are an interface behind the
scanner.

## Consequences

Easier:

- Steam games without Steam Cloud are discovered out of the box, offline, on
  every platform, including Windows-only games under Proton.
- Builds and tests need no network: the manifest is repository data with one
  command to refresh it.
- Known upstream mistakes can be corrected for a release without hiding edits
  inside the generated manifest.
- Devices hold no cache and no download state; what a binary knows is exactly
  what its release knew.

More difficult:

- Save-location knowledge ages with the release; a newly catalogued game waits
  for the next client release to be discoverable.
- The client binary carries embedded profile data whether or not it ever scans.
- Refreshing the manifest is a step someone must run; nothing fails when it is
  forgotten, the knowledge just stays stale.
- Unpatched community data quality is inherited, and every local correction
  adds a small review and removal obligation.
- A leftover Proton prefix misclassifies a game that switched back to its
  native build: profile rules then search the stale prefix and skip the
  native ones. Reading Steam's compat-tool selection would resolve it.
- `<storeUserId>` matches any account directory, so a machine with several
  Steam accounts aggregates them into one profile save, and a template
  containing both a glob and a bracketed real path is still misread.
- The generated asset may change when its build tooling changes even if the
  upstream rules do not.
