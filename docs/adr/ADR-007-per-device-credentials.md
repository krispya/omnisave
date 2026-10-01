# ADR-007: Issue a Credential per Holder, and Pair Devices with a Code

**Date:** 2026-07-25

## Context

One shared bearer token cannot show what holds it or withdraw one holder's access, and a leaked copy can only be fixed by rotating it everywhere. Connecting a Device with it means copying the owner's secret onto every machine.

Plex and Jellyfin both give every client its own token. A Device that cannot type shows a short code, and the owner approves it from a session that is already trusted. Their web interface holds an issued token like anything else.

## Decision

Every Device and every browser holds its own issued credential. The server stores only a hash of each, plus when it was issued, when it was last used, and whether it has been revoked. It authenticates a request by looking up its token. The owner token ([ADR-010](ADR-010-taking-ownership.md)) is not an issued credential and is never stored on a Device.

Credentials come in two kinds. A browser credential carries the owner's authority, and ADR-010 covers how a browser gets one. A Device credential syncs saves and uses the Library, and nothing more. Administering the server needs a browser credential or the owner token, and refuses a Device credential:

- listing, approving, and denying pairing requests;
- setting the PIN;
- listing and revoking credentials;
- reading and writing owner settings ([ADR-003](ADR-003-environment-server-configuration.md)).

A Device with no credential asks to pair, naming the Device identity it already reports. The server answers with two values: a short code for a person to read, and a long handle that collects the result. Only the handle can collect. The Device shows the code and polls with the handle. A request expires in minutes, works once, and is rate limited per source address.

**Only the owner approves, by matching the code.** A request's name and identity are self-asserted, and its address is only as trustworthy as the network path it crossed. The code on the Device's screen is the one thing the owner can check. Approval hands the credential to the poller once. The only other way a Device gets a credential is by trading the owner token (ADR-010). Being on the local network, knowing a code, or waiting mints nothing. Because a Device credential cannot approve, one compromised Device cannot let another in.

Every credential is listed and revoked individually, and revoking one leaves the rest working. A Device that pairs again gets another credential instead of silently replacing its old one, so the owner sees the duplicate and can revoke it.

## Consequences

Easier:

- Connecting a Device never hands out the owner's secret, and a lost Device is revoked alone instead of forcing a rotation everywhere.
- The owner can see what holds a credential and when it was last used.
- Pairing works the same on a local network and across the internet.

More difficult:

- Authentication now depends on the database, which is a store to migrate where one comparison used to be enough.
- The pairing endpoints answer without a credential, so their expiry, single use, rate limit, and approval-only minting are load-bearing and tested as such.
- Approval is only as good as the owner's attention. Nothing in a request can be verified, so the Dash has to make matching the code the obvious path. A list that invites a reflexive click gives away what this decision protects.
- Rate limiting and the address shown for approval assume the server sees the real peer. The server does not trust forwarded headers, so behind a reverse proxy every request appears to come from the proxy.
- Letting a new Device in needs a browser or the owner token, because no paired Device can approve another.
- The minted token is stored unhashed on its request between approval and collection. Every other secret stored here is a hash.
