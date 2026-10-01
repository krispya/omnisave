# ADR-018: Compile Save-Location Knowledge into the Client

**Date:** 2026-08-13

## Context

A save profile needs to know where a game keeps its saves, and the client has no such knowledge of its own. The community Ludusavi manifest has it for most Steam games, as save-location rules per operating system and store. The manifest covers far more than the client needs; Omnisave consumes only entries for Steam games and their save paths.

The question is how that knowledge reaches a Device. Scanning is deliberately local and offline: detection is cheap and indiscriminate, and `omnisave scan` works with no server and no configuration. Any answer has to preserve that.

## Decision

Ship the manifest inside the client binary, pruned to what the client reads.

A maintained build step downloads the community manifest, applies the same interpretation the client uses, and keeps only the Steam save rules the client consumes. The result is checked into the repository and embedded in the client, so builds and scans are offline and reproducible, and deterministic output keeps upstream refreshes reviewable. The client archives carry `THIRD_PARTY_NOTICES` with the Ludusavi Manifest's MIT notice for the derived data.

Reviewed, additive patches correct known upstream mistakes until upstream fixes them. The build refuses a patch whose upstream entry has drifted or that no longer adds anything, so the patch directory stays a current inventory rather than a silent fork; its README holds the rules.

Community data is interpreted defensively. Rules over Steam's `userdata` directories are dropped however they are spelled: that tree is Steam Cloud's mirror, a transport rather than the folder the game reads, so a rule over it would restore into a place the game may never look. Matching is case-insensitive, as Ludusavi's is, and a literal rule with several on-disk spellings and no exact one resolves nothing rather than choosing a save or restore destination arbitrarily.

A rule's identity — the location id recorded in revision file paths on the server — derives from its template text, not its position in the entry, so stored paths keep their meaning across refreshes that add, remove, or reorder a game's other rules. An entry's rules are also each other's aliases, which lets a revision recorded under one operating system's spelling be recognized as the same place on another.

Steam's own cloud configuration is a second source behind the same interface, read from the Device's local Steam files at scan time and consulted only where the manifest cannot place a game. The order never reverses, so a location identity minted under the manifest's spelling is never renamed. A declared folder is believed only when Steam's own sync records do not show the game writing through the API; a game that does is reported as one this source cannot place rather than guessed at.

Freshness rides releases. A new game's save location reaches Devices through the next client release and `omnisave update` ([ADR-015](ADR-015-client-binary-distribution.md)), not a runtime download. Downloading at runtime, as Ludusavi does, would add network, cache state, and staleness handling on the least-willing hosts Omnisave targets, and serving profiles from the server would couple scanning to a connection it deliberately does not need. Either stays open later without changing the profile contract.

## Consequences

Easier:

- Steam games' own save folders are known out of the box, offline, on every platform, including Windows games under Proton.
- Builds and tests need no network: the manifest is repository data with one command to refresh it.
- Known upstream mistakes are corrected for a release without hiding edits inside the generated manifest.
- Devices hold no cache and no download state; what a binary knows is exactly what its release knew.

More difficult:

- Save-location knowledge ages with the release; a newly catalogued game waits for the next client release.
- The client binary carries the embedded data whether or not it ever scans.
- Refreshing the manifest is a step someone must run; nothing fails when it is forgotten, the knowledge just stays stale.
- Community data quality is inherited, and every local correction adds a review and removal obligation.
- Following Ludusavi, `<storeUserId>` matches any account directory, so several Steam accounts on one machine share one save.
