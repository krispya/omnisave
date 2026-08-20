# FDR-006: Connecting a Device

**Status:** Experimental **Last reviewed:** 2026-08-18

## Overview

Connecting gives a Device its own revocable credential without copying the owner's bootstrap secret onto it. A Device requests pairing and shows a short code; an already trusted owner approves the matching request. Local discovery may find the server address, but it never grants access.

## Behavior

- A Device can discover servers announced on its local network or connect to an address supplied by the user. Both paths enter the same pairing flow.
- The Device sends its self-reported identity, receives a short code, and waits while retaining a separate secret handle that can collect the result.
- Pairing requests expire, work once, and are rate limited. Expiration or denial grants nothing.
- The owner can inspect pending requests and approve only the one whose code matches the Device in front of them.
- Approval creates a credential for that Device. The Device stores it locally and uses it for later requests without needing the owner token again.
- Credentials are listed individually, show the Device they belong to, and can be revoked without affecting other clients. Pairing the same Device again creates another visible credential rather than silently replacing one.
- An unclaimed server may be claimed once from its local network. The owner chooses a four-digit PIN, and later browsers exchange that PIN for their own revocable credentials.
- Sign-in failures are limited per source and across the server. The PIN is safe because guessing is throttled, not because four digits have high entropy.
- An already authenticated browser can change the PIN. The owner token remains the recovery and automation path when normal claiming or PIN sign-in is not available.
- The server announces itself on the local network by default. The owner may disable announcement immediately unless the deployment has pinned the setting.

## Design Decisions

### 1. The Device shows a code and an owner approves it

**Decision:** A Device without trust requests access and waits for a person who already has trust to approve a matching code. **Why:** The owner's secret never travels to the requesting Device, and the same flow works for clients with limited input capabilities. **Tradeoff:** Connecting requires the owner's attention on another trusted client.

### 2. Only a trusted owner may approve pairing

**Decision:** A Device credential cannot approve another Device. The owner must match the displayed code deliberately. **Why:** Device names, identities, and source addresses are not proof of who sent a request. Allowing one paired Device to mint another would let one compromise spread. **Tradeoff:** An owner without an authenticated browser cannot approve a new Device.

### 3. The displayed code and collection handle are different secrets

**Decision:** The human-readable code identifies a request; a separate high- entropy handle is required to collect the issued credential. **Why:** A code short enough to read aloud or copy is also easy to observe. It must not be sufficient to steal the Device's credential. **Tradeoff:** Pairing has two secrets and must keep their responsibilities separate.

### 4. Browsers hold ordinary client credentials

**Decision:** The Dash receives and stores revocable credentials under the same model as Devices. **Why:** No browser becomes permanently privileged, and one browser can be revoked without rotating access everywhere. **Tradeoff:** A fresh browser still needs the PIN or owner token before it can receive a credential.

### 5. Discovery finds addresses but grants no authority

**Decision:** Local discovery supplies connection information only; pairing and approval remain mandatory. **Why:** A local network contains untrusted Devices and is not an authorization boundary. **Tradeoff:** Discovery removes address entry but not the approval step.

### 6. Announcement is on by default and controlled by the owner

**Decision:** Servers announce themselves unless the owner or deployment turns discovery off. **Why:** Discovery is useful only when it works before setup instructions are needed, while an immediate setting lets owners hide a server on unsuitable networks. **Tradeoff:** Announcement reveals the server's presence to everyone on the local network, and bridged deployments may not be able to announce at all.

## Related

- **ADRs:** [ADR-007](../adr/ADR-007-per-device-credentials.md) — credential and pairing security; [ADR-009](../adr/ADR-009-mdns-server-discovery.md) — local discovery; [ADR-010](../adr/ADR-010-taking-ownership.md) — claiming and PIN sign-in; [ADR-008](../adr/ADR-008-owner-settings-beside-environment.md) — owner control of discovery; [ADR-002](../adr/ADR-002-sse-view-invalidation.md) — live pending-request updates.
- **FDRs:** [FDR-002](FDR-002-game-lifecycle.md) — Device identity and provenance; [FDR-003](FDR-003-automatic-save-binding.md) — the protection flow available after connecting.

## Open Questions

- Which reverse proxies may supply a trusted client address for claiming and rate limiting.
- Whether a Device should eventually replace its own credential instead of receiving another one when it pairs again.
