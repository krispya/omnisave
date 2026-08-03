# ADR-010: Take Ownership on First Contact, and Carry It with a PIN

**Date:** 2026-07-26

## Context

[ADR-007](ADR-007-per-device-credentials.md) took its model from Plex and
Jellyfin: one owner, a credential per client, and a code approved from a session
that is already trusted. It copied half of it. The half it copied — per-Device
credentials approved from a trusted session — is right. The half it skipped is
how a session becomes trusted in the first place, and that became
`OMNISAVE_TOKEN`: a secret that has to exist before anyone can do anything.

Everything downstream of that gap went wrong in the same direction. A fresh
server refused to start without a 64-character secret the operator had to
invent, so `docker compose up -d` failed before the server ran, on a variable
whose purpose nobody had encountered yet. Generating the token for them removed
the inventing and left the friction, because the shape of the problem was never
the generating: a fresh server was handing back a login screen for a password it
had written into a log file. And a server one browser could get into was still a
server the _second_ browser could not, which sent that browser back to the log.

No comparable product works this way. Jellyfin's setup wizard asks the first
browser to reach it to create the owner, and every browser after that logs in.
Home Assistant's onboarding does the same. A Plex server is openly reachable on
the local network until it is claimed, which is how you reach the screen that
claims it. All three establish ownership at first contact on a trusted network,
and all three then give the owner something they can carry to the next browser.
Omnisave asked for a token because of a decision recorded here, not because the
problem demands one.

## Decision

Ownership is taken at first contact and carried with a PIN. Three paths lead to
the same place, and the place is the one ADR-007 already describes: a revocable
per-client credential, listed beside the owner's Devices.

**A server that has issued no credential is unclaimed, and the first browser to
reach it from the local network claims it.** Claiming mints the first credential
and hands it to that browser. The moment it exists the server is claimed, and
claiming is refused from then on, permanently — there is no window to reopen and
no way back except deleting every credential. Two conditions guard it and both
must hold: the server must have issued nothing, and the request must arrive from
a loopback or private address, so a server already exposed to the internet is not
taken by whoever finds it first.

Deliberately not a condition: elapsed time. A limit protects the server that sits
exposed and unclaimed for hours, and breaks the person who installs on Friday and
sets it up on Sunday — sending them back to the log for a token, which is the
friction this decision exists to remove. The category standard is to stay
claimable until claimed.

**Claiming is where the owner sets a four-digit PIN, and every browser after that
signs in with it.** Signing in is not a session; it mints that browser its own
credential. Four digits is ten thousand possibilities, which is not a secret and
is not treated as one. **What protects the PIN is refusal, not entropy.**
Failures are counted per source address and across the server as a whole, and
sign-in locks for escalating periods once they accumulate — the bargain a phone
or a cash machine makes, where a short code is safe because the thing checking it
will not be hurried. The stored hash is PBKDF2 with a per-server salt, which
slows an attacker holding the database without pretending a four-digit space is
unguessable. Anyone who takes the database has the saves anyway.

**The owner token remains, generated rather than demanded.** A deployment that
sets `OMNISAVE_TOKEN` or `OMNISAVE_TOKEN_FILE` keeps exactly the behavior it has
today, and keeps winning. In their absence the server generates one on first
start and writes it beside the database, with owner-only permissions, and reads
it back on every later start. That is not configuration the server invented for
itself: it is the one credential that cannot be handed to a server nobody has
reached, so the server mints it rather than refusing to start. Everything else in
[ADR-003](ADR-003-environment-server-configuration.md) stands — no configuration
file, no precedence rules, no setting readable from two places.

The token is printed once, on the start that generated it, beside the path it was
written to; later starts print the path alone. It is now the exception rather
than the entrance: it claims a server whose network the address check cannot
vouch for, it is the way back in when the PIN is forgotten or its lockout is
being held down by someone else, and it is what automation uses. It is never
throttled and never locked. The PIN can be changed by the owner from a browser
that already holds a credential, which is what makes forgetting it survivable
without involving the token at all.

Devices are untouched. They pair, they show a code, the owner approves it, and
none of that involves claiming or the PIN.

Still out of scope, as ADR-007 said: accounts, roles, and multi-user access.
There is one owner. What is withdrawn is only ADR-007's assumption that the owner
needs no credential they can carry.

## Consequences

Easier:

- A fresh install works the way every comparable product works: open it, claim
  it, choose a PIN, use it. Nothing is read out of a log, and the operator never
  meets the token at all unless something has gone wrong.
- The second browser, and the tenth, have an answer that involves neither a
  second machine nor a log file. This is the case that has been unserved since
  ADR-007 and the reason claiming alone felt like half a feature.
- Every deployment behaves the same way — development, Compose, and Synology all
  end up with a token nobody had to invent.
- Every entry path ends in the same revocable per-client credential, so the list
  in the Dash stays the complete answer to "what can reach this server".

More difficult:

- **The throttle is load-bearing in a way a password's would not be.** A
  four-digit PIN is guessable in ten thousand tries. If the throttle is ever
  weakened, removed, or bypassed by a path that forgets to consult it, the PIN
  stops being a credential at all. It must be tested as the security control it
  is, not assumed.
- The global failure counter means a stranger can lock the owner out by failing
  sign-in repeatedly. The owner token is deliberately exempt so that denial is an
  annoyance rather than a lockout, but the annoyance is real.
- A PIN is short enough to be read over a shoulder and shared casually, and it
  will be reused from someone's phone. That is the cost of being memorable, and
  the answer is the same as everywhere else: it admits a browser, and the owner
  can revoke that browser.
- An unclaimed server on a shared network can be claimed by anyone on it. That is
  the trade Plex, Jellyfin, and Home Assistant all make, and it is real: a server
  installed on an office LAN and left unclaimed belongs to whoever opens it
  first. The server says so in its log on every start until it is claimed.
- The address check is best-effort and weakest exactly where it matters most. A
  deployment behind a reverse proxy sees the proxy's address on every request, so
  an internet-exposed server whose proxy sits on a private network looks local
  and is claimable from anywhere until claimed. Those deployments should set
  `OMNISAVE_TOKEN` and claim with it, which is why that path stays.
- **The server now writes one file it owns, which ADR-003 said it would not do.**
  The boundary is narrow — a single generated secret, never re-read as
  configuration, never merged with the environment — but it is a boundary that
  has to be defended each time something else wants to be written there.
- The token sits beside the database rather than in the save store
  ([ADR-012](ADR-012-portable-save-store.md)), so the backup that holds every
  save does not carry the way back into the server — but neither does backing up
  the saves back up ownership. Losing the file loses ownership; recovery is
  deleting it for a fresh token, which is survivable because issued credentials
  live in the database, not in this file. Restoring a store onto a fresh server
  produces an unclaimed one, and the owner claims it again.
- A secret generated by the server appears in its startup log once. Synology
  generates its token before the server starts and stores it without displaying
  it, because normal setup claims the server locally. Deployments that ship logs
  off the host may still ship a server-generated token, and the answer for
  anyone who minds is to set the variable, which is the path that was always
  there.
- Ownership is now something a server acquires on its own rather than something
  configuration confers, so "who owns this server" is answered by its database
  instead of by its environment.
