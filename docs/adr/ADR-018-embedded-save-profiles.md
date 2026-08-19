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

The distributed client archives include `THIRD_PARTY_NOTICES`, which preserves
the Ludusavi Manifest's MIT copyright and permission notice alongside the
derived data embedded in the binary.

A handful of interpretation rules make community data safe to consume.
Duplicate Steam ids from renamed pages and split editions resolve
deterministically to the first title in
sorted order that actually carries save rules, so a stub page never shadows
the entry that locates saves. Rules over Steam's `userdata` directories are
dropped however the manifest spells them: those are Steam Cloud's, already
discovered and attributed to an account by the steam adapter, and a profile
rule over them would report every Cloud save twice. Only absolute expanded
paths are searched or offered as destinations, which discards the manifest's
occasional prose and relative entries. Glob matching is case-insensitive,
as Ludusavi's is, because community casing is unreliable; and metacharacters
that arrive through real filesystem values (a library named with brackets)
stay literal rather than becoming pattern syntax.

A rule's identity — the location id recorded in revision file paths on the
server — derives from its template text, not its position in the entry, so
canonical paths keep meaning across manifest refreshes that add, remove, or
reorder a game's other rules. Filesystem trouble while resolving one rule
narrows what that rule finds; it never fails the scan.

Freshness rides releases. A new game's save location becomes known to devices
through the next client release and `omnisave upgrade`
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
- Devices hold no cache and no download state; what a binary knows is exactly
  what its release knew.

More difficult:

- Save-location knowledge ages with the release; a newly catalogued game waits
  for the next client release to be discoverable.
- The client binary carries embedded profile data whether or not it ever scans.
- Refreshing the manifest is a step someone must run; nothing fails when it is
  forgotten, the knowledge just stays stale.
- Community data quality is inherited — a wrong path in the manifest ships
  until a release after someone corrects it upstream.
- A leftover Proton prefix misclassifies a game that switched back to its
  native build: profile rules then search the stale prefix and skip the
  native ones. Reading Steam's compat-tool selection would resolve it.
- `<storeUserId>` matches any account directory, so a machine with several
  Steam accounts aggregates them into one profile save, and a template
  containing both a glob and a bracketed real path is still misread.
- The generated asset may change when its build tooling changes even if the
  upstream rules do not.
