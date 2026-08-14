# ADR-018: Compile Save-Location Knowledge into the Client

**Date:** 2026-08-13

## Context

The steam adapter's only native save source is Steam Cloud's `userdata`
directories, and a game that opts out of Steam Cloud — Undertale is the
canonical case — scanned as having no save at all. The Save Profile pipeline
(`internal/client/saveprofile`) was built to close that gap with save-location
rules from the community Ludusavi manifest, and
[FDR-004](../fdr/FDR-004-sync-to-device.md) already describes Steam locations
as "save-location rules expanded against the environment", but no data ever
reached it: the shipping client constructed its scanner without a profile
provider, and nothing shipped or fetched a manifest.

The manifest itself is ~17 MB of YAML covering the PCGamingWiki catalog. The
client consumes a fraction of it: entries carrying a Steam id, and only their
save-tagged paths. The question is how that data reaches a device. Scanning is
deliberately client-local and offline — detection is cheap and indiscriminate
([FDR-002](../fdr/FDR-002-game-lifecycle.md)), and `omnisave scan` works with
no server and no configuration — which any answer has to preserve.

## Decision

Ship the manifest inside the client binary, pruned to what the client reads.

`make refresh-save-profiles` downloads the community manifest and rewrites it
through the same rule interpretation the parser applies at runtime
(`ludusavi.Prune`), so the pruned copy parses into exactly the profiles the
full manifest would produce. Pruning keeps entries with a Steam id and only
their save rules, which reduces 17 MB to 4.3 MB, and 0.6 MB gzip-compressed.
The result is checked into the repository and embedded with `go:embed`, so
builds are offline and reproducible, and the tool's output is byte-stable to
keep refresh diffs reviewable. The client parses the manifest lazily on first
profile lookup; commands that never scan pay nothing.

Two interpretation rules make community data safe to consume. Duplicate Steam
ids — the manifest holds dozens, from renamed wiki pages and split editions —
resolve deterministically to the first title in sorted order, so a base title
outranks its longer variants. Rules under `<root>/userdata` are dropped: those
directories are Steam Cloud's, already discovered and attributed to an account
by the steam adapter, and a profile rule over them would report every Cloud
save twice.

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
- The client binary carries 0.6 MB of manifest whether or not it ever scans.
- Refreshing the manifest is a step someone must run; nothing fails when it is
  forgotten, the knowledge just stays stale.
- Community data quality is inherited — a wrong path in the manifest ships
  until a release after someone corrects it upstream.
