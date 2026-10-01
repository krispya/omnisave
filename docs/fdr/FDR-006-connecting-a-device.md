# FDR-006: Connecting a Device

**Status:** Experimental **Last reviewed:** 2026-09-30

## Overview

Connecting gives a Device its own revocable credential without putting the owner token on it. The Device asks to pair and shows a short code, and the owner approves the matching request from a signed-in browser. A browser gets its own credential by claiming the server or by signing in with the PIN. Local discovery can find a server's address but never grants access.

## Behavior

- A Device either finds a server announced on its local network or is given an address. Both lead to the same pairing flow ([ADR-009](../adr/ADR-009-mdns-server-discovery.md)).
- While it waits, the Device shows a short code. A request expires within minutes, works once, and is rate limited.
- The owner approves the request whose code matches the one on the Device's screen, or denies it. Only approval grants anything, and only a browser or the owner token can approve. A Device cannot approve another ([ADR-007](../adr/ADR-007-per-device-credentials.md)).
- The Device keeps the credential it is issued and uses it from then on. A Device given the owner token trades it for a credential of its own and keeps only that credential.
- A Device credential can sync saves and use the Library, nothing more. Approving requests, changing the PIN, managing credentials, and changing owner settings all need a browser.
- Every credential is listed with its holder and when it was last used. Each one can be revoked without affecting the others. A Device that pairs again gets another visible credential instead of replacing its old one.
- A browser gets its own credential in one of three ways: claiming an unclaimed server from its local network (which is when the owner chooses the four-digit PIN), signing in with the PIN, or trading the owner token. Repeated wrong PINs lock sign-in for longer and longer periods ([ADR-010](../adr/ADR-010-taking-ownership.md)).
- Any signed-in browser can set or change the PIN.
- The server announces itself on the local network by default. The owner can turn announcing off, and it stops immediately, unless the deployment has pinned the setting ([ADR-003](../adr/ADR-003-environment-server-configuration.md)).

## Related

- **ADRs:** [ADR-007](../adr/ADR-007-per-device-credentials.md) covers credentials and pairing; [ADR-010](../adr/ADR-010-taking-ownership.md) covers claiming, PIN sign-in, and the owner token; [ADR-009](../adr/ADR-009-mdns-server-discovery.md) covers local discovery; [ADR-003](../adr/ADR-003-environment-server-configuration.md) covers owner settings; [ADR-002](../adr/ADR-002-sse-view-invalidation.md) covers live updates to pending requests.
- **FDRs:** [FDR-002](FDR-002-game-lifecycle.md) covers Device identity and provenance; [FDR-003](FDR-003-automatic-save-binding.md) covers binding saves once a Device is connected.

## Open Questions

- Which reverse proxies may supply a trusted source address for claiming and rate limiting.
- Whether a Device that pairs again should replace its own credential instead of receiving another.
