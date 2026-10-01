# ADR-009: Announce the Server over mDNS

**Date:** 2026-07-25

## Context

A Device on the same local network as its server should be able to connect without being given an address. A custom UDP broadcast, as nearby media servers ship, means owning a protocol and a port. SSDP carries UPnP's history, which is why networks filter it. mDNS with DNS-SD is what home networks already carry for printers, AirPlay, Chromecast, and Home Assistant.

## Decision

Announce the server over mDNS with DNS-SD, and let discovery learn an address and nothing else.

The announcement carries the address and a name a person can recognize. It carries no credential, no token, and nothing about who may connect. A discovered server is not a trusted one: the Device still pairs and waits for the Owner to approve its code ([ADR-007](ADR-007-per-device-credentials.md)). Where multicast cannot reach, such as a bridged container, a routed or segmented network, or anything across the internet, the address is given with `--server` and every later step is identical. Discovery is an optional first leg of one connection flow.

Announcing is on by default. It is an owner setting that the deployment may pin ([ADR-003](ADR-003-environment-server-configuration.md)).

## Consequences

Easier:

- A server on the host network, such as the Synology package ([ADR-005](ADR-005-synology-package-distribution.md)), answers `connect` with no arguments.
- The announcement carries no authority, so the security surface stays exactly the pairing flow.

More difficult:

- The Compose deployment ([ADR-004](ADR-004-oci-image-distribution.md)) cannot announce as shipped: bridged networking does not pass multicast to the LAN. Its operators pass `--server` or run the container on the host network, and the documentation has to say so plainly.
- The server and client embed an mDNS library rather than relying on the operating system's responder, so the server runs its own responder beside any the host already has.
- Two servers on one network must be told apart by their announced names, so the name is something a person reads and is worth choosing.
- Anything can claim to be an Omnisave server. A Device that pairs with an impostor gets a code that is never approved, so the cost is confusion rather than access, but a discovery list is attacker-influenced and proves nothing.
- The announcement tells everything on the network that the server exists, which is why the setting exists.
