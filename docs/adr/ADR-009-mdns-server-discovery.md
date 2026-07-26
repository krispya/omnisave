# ADR-009: Announce the Server over mDNS

**Date:** 2026-07-25

## Context

[FDR-006](../fdr/FDR-006-connecting-a-device.md) makes `omnisave
connect` with no arguments the ordinary way onto a local network. That asks the
server to be findable without an address, and nothing on the wire says it
exists today: every client is handed a URL by hand, typed from whatever the
operator remembers the NAS is called.

Three ways to be findable. A custom UDP broadcast is the smallest thing that
works, and it is what the media servers nearby actually ship — Plex answers on
its own GDM ports, Jellyfin on its own broadcast — but it means owning a
protocol, a port, and a listener on every platform to solve a problem the
platforms already solved. SSDP has the reach and arrives with UPnP's history,
which is why networks filter it. mDNS with DNS-SD is what the rest of the house
already speaks: macOS ships a responder, Linux has Avahi, Windows has resolved
`.local` since 10, and printers, Chromecast, AirPlay, and Home Assistant are
all found this way. A Synology is already running a responder for its own
services.

The deployments are not equal here. The Synology package
([ADR-005](ADR-005-synology-package-distribution.md)) runs on the host network,
where multicast works. The Compose deployment
([ADR-004](ADR-004-oci-image-distribution.md)) runs bridged with a published
port, where multicast from inside the container never reaches the LAN.

## Decision

Announce the server over mDNS with DNS-SD, and let discovery learn an address
and nothing else.

The announcement carries what a client needs to reach the server and what a
person needs to recognize it: an instance name, the address and port, and
whether the connection is TLS. It carries no credential, no token, and nothing
about who may connect.

A discovered server is not a trusted one. The client that finds a server still
asks to pair, still shows a code, and still waits for the owner to approve it
in the Dash ([ADR-007](ADR-007-per-device-credentials.md)). Discovery removes
an argument from a command; it never removes an approval.

Where mDNS cannot work — a bridged container, a routed or segmented network,
anything across the internet — the address is given with `--server`, and every
step after it is identical. There is one connection flow, and discovery is an
optional first leg of it.

Announcing is on by default and switched off in the Dash
([ADR-008](ADR-008-owner-settings-beside-environment.md)). It is the first
owner setting, because whether to announce is a fact about tonight's network
rather than about the installation.

## Consequences

Easier:

- The install this product is built for — a native package on a NAS, on the
  host network — answers `connect` with no arguments, which is the difference
  between a command someone runs and a URL they have to go find.
- Nothing new to implement on the wire: the resolvers already exist on every
  platform a client runs on, and the server side is an announcement rather than
  a protocol.
- Because the announcement carries no authority, there is nothing to review in
  it. The security surface stays exactly the pairing flow.

More difficult:

- The Compose deployment cannot announce as shipped: bridged networking does
  not pass multicast to the LAN. Those operators run the server on the host
  network or type an address, and the documentation has to say so plainly
  rather than let it be found out.
- mDNS shares a namespace, so two Omnisave servers on one network have to be
  distinguishable by instance name. The name becomes something a person reads
  and therefore something worth choosing, and conflicts have to resolve to
  something other than two identical entries.
- Anything can claim to be an Omnisave server. A client that discovers an
  impostor sends it a Device identity and gets a code that is never approved,
  so the cost is confusion rather than access — but discovery lists are
  attacker-influenced, and a server offered there is not evidence of anything.
- The announcement tells everything on the network that the server exists,
  including whatever a guest brought. That is why the switch exists, and why
  the default deserves revisiting if Omnisave ever installs somewhere less
  domestic than a house.
