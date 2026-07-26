# FDR-006: Connecting a Device

**Status:** Experimental
**Last reviewed:** 2026-07-26

## Overview

How a Device gains access to a server, and how the owner takes it away. A
client with no credential asks the server to pair, shows a short code, and
waits; the owner finds that code in the Dash and approves it, and the Device
receives a credential of its own. On a local network the client finds the
server over mDNS, so connecting is one command with no arguments; anywhere else
it is the same flow with an address typed in. The owner's token is never copied
onto a Device.

This record was written before the code so the security-carrying parts were
argued once, in one place. The feature is now implemented and behaves as
described; it is experimental until it has been lived with on more than one
network.

## Behavior

- `omnisave connect` with no arguments looks for servers announcing
  themselves over mDNS on the local network
  ([ADR-009](../adr/ADR-009-mdns-server-discovery.md)). One match is used;
  several are listed to choose from; none reports that nothing was found and
  asks for an address.
- `omnisave connect --server <url>`, or the same URL given positionally
  as `connect` accepts it today, skips discovery. It is the only shape that
  works where the server cannot announce — a bridged container, a routed or
  segmented network, anything across the internet — and everything after the
  address is identical.
- The client asks the server to pair, sending the Device identity it already
  self-identifies with. The server answers with a short code, which the client
  displays, and the client waits.
- The code expires in minutes, works once, and is refused after too many
  attempts from one address. An expired request leaves nothing behind; the
  client says so and stops.
- The Dash lists pending requests, each showing its code beside the Device's
  name and the source address. The owner approves the request whose code
  matches the one on the Device's screen, or denies it. Requests appear and
  expire without a refresh ([ADR-002](../adr/ADR-002-sse-view-invalidation.md))
  and are visible only while they live.
- Approving mints a credential for that Device, hands it to the waiting client,
  and retires the code. Denying ends the request; the client reports the
  refusal and stops. Nothing is minted by any other means.
- A Device that pairs again is issued another credential rather than replacing
  the one it holds. Both are listed, and either can be revoked on its own.
- Once connected, the Device keeps its credential beside the rest of its local
  tracking state and uses it for everything afterward; later commands need no
  token and no address.
- The Dash lists issued credentials with the Device they belong to and when
  each was last used, and revokes any one of them. The next request from a
  revoked Device fails; connecting again means pairing again.
- A server nobody owns yet says so, and the first browser to open it from the
  local network claims it — no token, no setup wizard. Claiming asks for a
  four-digit PIN, mints that browser's credential, and the server refuses every
  later claim permanently ([ADR-010](../adr/ADR-010-taking-ownership.md)).
- Every browser after that signs in with the PIN and gets a credential of its
  own. Wrong PINs are counted per address and across the server, and sign-in
  locks for escalating periods once they add up — four digits are safe because
  the server will not be hurried, not because they are hard to guess
  ([ADR-010](../adr/ADR-010-taking-ownership.md)).
- The PIN is changed from any browser already signed in, which is how an owner
  who forgot it recovers without touching the owner token.
- The Dash otherwise holds an issued credential the same way: the owner enters
  the owner token once, and the Dash exchanges it for a credential of its own.
  That is the path for a server already claimed, and for one reached from
  outside its network, where claiming is refused.
- A deployment that configures no owner token has one generated on first start,
  printed once to the server log and kept beside the database
  ([ADR-010](../adr/ADR-010-taking-ownership.md)). Setting
  `OMNISAVE_TOKEN` supplies one instead and nothing is generated.
- The owner token keeps working as the way in when nothing else does:
  `omnisave connect --server <url> --token <owner token>` skips pairing
  and stores it. Devices connected before pairing existed hold that token and
  keep working unchanged; running `connect` again is what trades it for a
  credential of their own.
- The server announces itself on the local network by default. Turning the
  announcement off in the Dash stops it immediately, with no restart. A
  deployment that pins the setting with `OMNISAVE_DISCOVERY` gets a Dash that
  shows the value and says where it came from instead of offering to change it
  ([ADR-008](../adr/ADR-008-owner-settings-beside-environment.md)).
- The announcement switch, the pending requests, and the issued credentials all
  live in one server settings area of the Dash — what the owner opens for
  anything about the server rather than about the Library.

## Design Decisions

### 1. The Device shows a code; the owner approves it

The Device that wants access is often the one that cannot be trusted to hold a
secret yet, and sometimes the one that can barely type. It asks, displays a
short code, and waits for someone who is already trusted to say yes.

**Why:** it puts the decision where the trust already is, and it never requires
the owner's secret to travel to the Device asking for access. A television, a
handheld, and a headless box all connect the same way as a laptop.

**Tradeoff:** connecting takes two devices and the owner's attention. That is
the point, and it is the cost.

### 2. Only the Dash approves, only a person, and the code is what they match

The pairing endpoints cannot be authenticated — a Device with no credential is
exactly who calls them. What stands between a stranger on the network and a
credential is a person matching the code in the Dash against the code on the
Device in front of them.

**Why:** every automatic rule considered here — trust the local network, trust
whoever knows the code, trust the first request after a restart — grants access
to whoever arranges to be in the right place at the right moment. And the rest
of a request proves nothing: the Device name and identity are minted by the
client, so a stranger can arrive wearing the name of the owner's own laptop,
and the source address means only as much as the network path it crossed. The
code is the one part of a request that ties it to the Device that sent it, so
it is what the owner reads and what the Dash shows.

**Tradeoff:** an owner with no browser cannot connect a Device. Approval from
an already-paired client would fix that and is deliberately not offered,
because it turns one reviewed path into two (see
[ADR-007](../adr/ADR-007-per-device-credentials.md)). And listing requests
means an unexpected one is visible — useful as a signal that something is
probing, but only if approving stays a deliberate match rather than a habit.

### 3. The code and the collecting handle are different secrets

The pairing request carries two: a short code the owner reads, and a long
handle the requesting client keeps to itself. Only the handle can collect the
minted credential.

**Why:** the code is short because a person retypes it, which makes it weak by
construction and visible to anyone in the room. If the code also collected the
credential, watching the screen would be enough to take the Device's place. Its
job is to identify a request, not to protect one, and splitting the two secrets
is what lets the Dash display it.

### 4. The Dash pairs like a Device

The browser is not privileged. It takes the owner token once and trades it for
its own credential.

**Why:** it keeps the owner's secret out of every browser it was ever pasted
into, and makes revoking one laptop possible without rotating what every other
Device holds.

**Tradeoff:** the first connection to a new Dash still asks for the owner token,
so the token remains something the owner must have on hand.

### 5. Discovery finds addresses; it never grants access

Being on the local network locates a server and nothing more. A discovered
server still requires pairing, with the same approval by the same person
([ADR-009](../adr/ADR-009-mdns-server-discovery.md)).

**Why:** a local network is not a trust boundary — it holds guests, phones, and
whatever the last visitor plugged in. Keeping discovery free of authority means
there is one way to gain access, and it is the reviewed one. It also means a
server that turns up in a discovery list is a claim rather than a credential;
pairing with an impostor costs a code that nobody approves.

**Tradeoff:** connecting on the local network is not one step; discovery saves
an argument, not an approval.

### 6. Announcing is on by default and switchable in the Dash

The server announces itself unless the owner turns it off, and the switch is a
Dash setting rather than a variable and a restart
([ADR-008](../adr/ADR-008-owner-settings-beside-environment.md)).

**Why:** discovery only helps if it works before anyone reads about it, and the
NAS-first install is exactly where it works — a native package on the host
network. A setting nobody discovers helps nobody.

**Tradeoff:** the announcement tells everything on the network that the server
exists, which is unwanted on a shared or public network. That is why it is a
setting the owner can reach in one place, and why it changes behavior the
moment it is set. The default also does nothing for the Compose deployment,
which is bridged and cannot announce to the LAN at all, so the deployment most
people try first is the one that still types an address.

## Related

- **ADRs:** [ADR-007](../adr/ADR-007-per-device-credentials.md) — the credential
  model this feature is the front door to;
  [ADR-009](../adr/ADR-009-mdns-server-discovery.md) — how a server is found
  and why finding one grants nothing;
  [ADR-008](../adr/ADR-008-owner-settings-beside-environment.md) — why the
  discovery switch is a Dash setting rather than a variable;
  [ADR-002](../adr/ADR-002-sse-view-invalidation.md) — how a pending request
  reaches an open Dash and leaves it when it expires;
  [ADR-001](../adr/ADR-001-server-authority.md) — the server decides what access
  exists, as it decides everything else.
- **FDRs:** [FDR-002](FDR-002-game-lifecycle.md) — the Device identity a pairing
  request carries and the provenance a credential sits beside;
  [FDR-003](FDR-003-automatic-save-binding.md) — the first thing a newly
  connected Device does.

## Open Questions

- Whether the server trusts a forwarded client address, and from which proxies.
  Until it does, both the address shown on a pending request and the per-address
  rate limit are weaker behind a reverse proxy than they look — every requester
  arrives as the proxy. It is the reason the code, not the address, is what the
  owner is asked to match.
- Whether a Device should be able to replace its own credential rather than
  collect a second one. Pairing twice issues two, which is honest about what
  the server can verify — nothing about a request proves it came from the same
  machine — but it leaves the owner tidying a list.
