# Architecture Decision Records

Architecture Decision Records capture Omnisave's cross-cutting technical
decisions and the context that produced them. Feature-specific decisions live
in [FDRs](../fdr/INDEX.md), which cite ADRs; citations flow FDR → ADR only.

**Each record describes the decision as it stands today.** A decision that
changes is rewritten, and one that stops applying is deleted — not amended with
a note, and not left behind as a tombstone with a supersession chain. Git holds
what a record used to say; this directory holds what is true. A reader who
opens one of these should not have to reconcile it against a later one to learn
what the system does.

| #                                                       | Decision                                                       | Date       |
| ------------------------------------------------------- | -------------------------------------------------------------- | ---------- |
| [ADR-001](ADR-001-server-authority.md)                  | The server is the only authority                               | 2026-07-18 |
| [ADR-002](ADR-002-sse-view-invalidation.md)             | Use SSE to invalidate server-authoritative views               | 2026-07-18 |
| [ADR-003](ADR-003-environment-server-configuration.md)  | Configure the server through the environment                   | 2026-07-22 |
| [ADR-004](ADR-004-oci-image-distribution.md)            | Ship the server as an OCI image                                | 2026-07-21 |
| [ADR-005](ADR-005-synology-package-distribution.md)     | Ship a native Synology package                                 | 2026-07-25 |
| [ADR-006](ADR-006-dash-path-routes.md)                  | Serve the Dash from an index fallback, addressed by path       | 2026-07-25 |
| [ADR-007](ADR-007-per-device-credentials.md)            | Issue a credential per Device, bootstrapped by the owner token | 2026-07-25 |
| [ADR-008](ADR-008-owner-settings-beside-environment.md) | Keep owner settings beside environment configuration           | 2026-07-25 |
| [ADR-009](ADR-009-mdns-server-discovery.md)             | Announce the server over mDNS                                  | 2026-07-25 |
| [ADR-010](ADR-010-taking-ownership.md)                  | Take ownership on first contact, and carry it with a PIN       | 2026-07-26 |
| [ADR-011](ADR-011-owner-provider-credentials.md)        | Let the owner hold provider credentials                        | 2026-07-26 |
| [ADR-012](ADR-012-portable-save-store.md)               | Keep saves in a portable store                                 | 2026-07-27 |
| [ADR-013](ADR-013-server-announced-presence-expiry.md)  | The server is the only clock for presence                      | 2026-08-08 |
| [ADR-014](ADR-014-durable-proof-before-forgetting.md)   | Require durable proof before forgetting data                   | 2026-08-09 |
| [ADR-015](ADR-015-client-binary-distribution.md)        | Install the client from prebuilt release archives              | 2026-08-13 |
