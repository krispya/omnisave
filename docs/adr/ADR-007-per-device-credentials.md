# ADR-007: Issue a Credential per Client, and Pair Devices with a Code

**Date:** 2026-07-25

## Context

The server authenticates every request against one static bearer token read from the environment ([ADR-003](ADR-003-environment-server-configuration.md)). Connecting a Device therefore means copying the owner's secret onto it: the operator generates a token, pastes it into the deployment, and then pastes the same string into every client and every browser that opens the Dash.

That single secret is the connection experience and the security model at once, and both suffer. There is no way to see what holds it, no way to withdraw one Device's access, and no way to recover from a leaked copy except rotating the token and reconnecting everything. Meanwhile a Device is already a first-class thing the server knows: clients self-identify, register under `/api/v1/devices/{id}`, and accumulate per-Device provenance ([FDR-002](../fdr/FDR-002-game-lifecycle.md)). Access is the one thing about a Device the server cannot describe.

Comparable systems converge on the same answer. Plex binds a server to an account and issues every client its own token — a television that cannot type shows a short code that the owner approves from somewhere already trusted. Jellyfin reaches the same place without a broker: a local owner, per-device tokens, and a code approved from a signed-in session. In both, the web interface is not privileged; it holds an issued credential like everything else.

## Decision

Issue one credential per client, and keep the owner token as the bootstrap that mints the first one.

The server stores credentials it has issued — hashed, never recoverable — each bound to a Device identity and carrying when it was created, when it was last used, and whether it has been revoked. Authentication becomes a lookup against that store instead of a comparison against one configured string.

The Dash is not privileged. It holds an issued credential like everything else and is revoked like anything else; how the owner comes to hold the first one is [ADR-010](ADR-010-taking-ownership.md)'s subject, not this one's. What matters here is that every way in ends at the same object: a credential this server minted, can describe, and can withdraw on its own.

A client with no credential asks to pair, naming the Device identity it already self-identifies with. The server records the request with two distinct secrets: a short code, because a person reads it, and a long poll handle, because it is what collects the credential. The client displays the code and polls with the handle. The request expires in minutes, is single use, and is rate limited per source address — best effort, since a reverse proxy collapses every requester into one address unless the deployment is configured to pass the original through.

**Only the owner approves, and the code is what they approve.** A pending request shows its code beside the Device's name and source address, and the owner approves the one whose code matches the screen in front of them. Approving is refused to a Device's own credential, however valid: a credential that pairing minted cannot mint another, so one compromised Device admits nothing. A request's name and identity are both self-asserted, so neither separates a Device from something imitating it, and the address is worth no more than the network path it crossed. The code is the only part of a request the owner can check against the Device that sent it. Nothing is minted without that act — not by being on the local network, not by knowing a code, not by waiting. Approval mints the credential, hands it to the poller exactly once, and retires the code.

Credentials are listed and individually revoked in the Dash. Revoking one Device leaves every other credential working. Nothing binds a Device to a single credential: one that pairs again is issued another, and an identity that appears twice is something the owner can see and revoke rather than something the server silently reconciles.

Accounts, roles, and multi-user access are out of scope. There is one owner, who holds a credential like every other client rather than an identity the server reasons about.

## Consequences

Easier:

- Connecting a Device stops distributing the owner's secret; a leaked or lost Device is revoked instead of forcing a rotation across everything.
- Access becomes describable — the Dash can show what holds a credential and when it was last used, alongside the provenance it already shows.
- The pairing flow works the same on a local network and across the internet, so there is one authentication path to review rather than two.

More difficult:

- Authentication gains a database dependency and a store to migrate, where it used to be a constant-time comparison with no state at all.
- The pairing endpoints are the first unauthenticated surface that changes anything — the health checks answer everyone the same thing — so their expiry, single use, rate limiting, and refusal to mint without approval are load-bearing and must be tested as such.
- Approval is only as good as the attention behind it. Nothing in a request is verifiable, so the Dash has to make matching the code the obvious path and an unexpected request hard to wave through; a list that invites a reflexive click gives away the thing this decision exists to protect.
- Rate limiting and the address shown for approval both assume the server sees the real peer, which any deployment behind a reverse proxy has to arrange deliberately rather than inherit.
- An owner with no browser cannot approve a Device. Headless approval from an already-paired client is deliberately not offered — it is what would let one stolen Device credential admit the next — so the owner needs something with a screen to let anything new in.
- Two secrets exist per pairing request and one of them, the minted token, rests unhashed on that request between approval and the single poll that collects it. Everything else this decision stores is a hash.
