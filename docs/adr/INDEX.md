# Architecture Decision Records

Architecture Decision Records capture Omnisave's cross-cutting technical
decisions and the context that produced them. Feature-specific decisions live
in [FDRs](../fdr/INDEX.md), which cite ADRs; citations flow FDR → ADR only.

| #                                                      | Decision                                                 | Date       |
| ------------------------------------------------------ | -------------------------------------------------------- | ---------- |
| [ADR-001](ADR-001-server-authority.md)                 | The server is the only authority                         | 2026-07-18 |
| [ADR-002](ADR-002-sse-view-invalidation.md)            | Use SSE to invalidate server-authoritative views         | 2026-07-18 |
| [ADR-003](ADR-003-environment-server-configuration.md) | Configure the server through the environment             | 2026-07-22 |
| [ADR-004](ADR-004-oci-image-distribution.md)           | Ship the server as an OCI image                          | 2026-07-21 |
| [ADR-005](ADR-005-synology-package-distribution.md)    | Ship a native Synology package                           | 2026-07-25 |
| [ADR-006](ADR-006-dash-path-routes.md)                 | Serve the Dash from an index fallback, addressed by path | 2026-07-25 |
