# ADR-010: Take Ownership on First Contact, and Carry It with a PIN

**Date:** 2026-07-26

## Context

A server that demands a token the operator had to invent fails on `docker compose up`, and it sends every new browser back to the server log for a secret. Jellyfin and Home Assistant ask the first browser that reaches them to create the owner. Plex can be claimed from its local network until someone claims it. All three establish ownership at first contact, then give the owner something short to carry to the next browser.

## Decision

A browser carries owner authority through its own credential ([ADR-007](ADR-007-per-device-credentials.md)). It gets that credential in one of three ways: claiming the server, signing in with the PIN, or trading the owner token.

**The first browser to reach a server that has issued nothing claims it, from the local network only.** Claiming mints that browser's credential and sets the owner's four-digit PIN. Two conditions must both hold: nothing has been issued, and the request comes from a loopback, private, or link-local address. The address check stops a stranger from taking a server that is already exposed to the internet. Once anything has been issued, claiming is refused for good. Revoked credentials still count, so only a fresh database can be claimed again. There is no time limit on claiming. A limit would protect a server left exposed and unclaimed, but it would break the owner who installs on Friday and sets up on Sunday.

**Every later browser signs in with the PIN.** Signing in mints that browser its own credential rather than a session. Four digits allow only ten thousand values, so **what protects the PIN is refusal, not entropy**. Failures are counted per source address and across the whole server, and once they build up, sign-in locks for longer and longer periods. A cash machine makes the same bargain. The PIN is stored as a slow salted hash. Any browser that holds a credential can set or change the PIN. That includes a server claimed with the owner token, which has no PIN until then.

**The server generates the owner token instead of demanding one.** A value from `OMNISAVE_TOKEN` or `OMNISAVE_TOKEN_FILE` wins. Without one, the server generates a token beside its database on first start and prints it once ([ADR-003](ADR-003-environment-server-configuration.md)). The token authenticates without being an issued credential, and it is never throttled or locked. It is the exception rather than the way in, with three jobs:

- claiming a server whose network the address check cannot vouch for;
- recovering a forgotten or locked-out PIN when no signed-in browser is left;
- automation.

A browser or Device given the token trades it once for a credential of its own and keeps only that credential. A Device's credential is tied to its own identity. The token itself is never stored on a Device.

## Consequences

Easier:

- A fresh install works the same on every deployment: open it, claim it, choose a PIN. The operator never meets the token unless something has gone wrong.
- A second browser needs only the PIN, not a second machine or the server log.
- Every browser and Device holds a listed, revocable credential. Only requests made with the owner token itself never appear in that list.

More difficult:

- **The throttle is load-bearing in a way a password's is not.** If it is weakened, removed, or bypassed by a path that forgets to check it, the PIN stops being a credential. It is tested as the security control it is.
- One counter covers the whole server, so a stranger can lock the owner out by failing sign-in repeatedly. The owner token is exempt, which makes this an annoyance rather than a lockout. Throttle state lives in memory, so a restart clears every lockout, the owner's and an attacker's alike.
- A PIN is short enough to be read over a shoulder. It only admits a browser, and the owner can revoke that browser.
- An unclaimed server on a shared network belongs to whoever opens it first. Plex, Jellyfin, and Home Assistant all make this trade. The server warns about it in its log on every start until it is claimed.
- The address check is weakest exactly where it matters most. Behind a reverse proxy, every request comes from the proxy's address. An internet-exposed server whose proxy sits on a private network can therefore be claimed from anywhere until it is claimed. Those deployments set `OMNISAVE_TOKEN`, claim with it, and then set the PIN.
- Ownership lives in the database, not in the portable store ([ADR-012](ADR-012-portable-save-store.md)). A store restored onto a fresh server is unclaimed and gets claimed again. Deleting the token file produces a new token on the next start, and every issued credential keeps working.
- A generated token appears once in the startup log. Deployments that ship logs off the host set the variable instead.
