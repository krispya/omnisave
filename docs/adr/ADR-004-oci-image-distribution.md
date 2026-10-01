# ADR-004: Ship the Server as an OCI Image

**Date:** 2026-07-21

## Context

The server is the only durable authority for save history, so running and backing it up must be approachable for a home-server owner. It is one statically linked Go process serving the Dash, with SQLite metadata and content-addressed artifacts on local storage: it needs no cluster, external database, or language runtime on the host. A generic self-hosting package should make storage and upgrades explicit without making Docker the product's runtime contract.

## Decision

Ship a multi-architecture OCI image as the primary generic self-hosting artifact. Docker Compose is the documented single-host installer, while any OCI-compatible runtime may run the image.

The image runs one server instance as a fixed unprivileged user and contains no mutable state. All durable state lives under one persistent data root, which Compose mounts as a named volume. The image supplies its container paths as environment defaults ([ADR-003](ADR-003-environment-server-configuration.md)), so Compose sets only what an operator owns. The server shuts down gracefully on SIGTERM.

Kubernetes packaging, an external database, and multiple server replicas are out of scope. They add operational surface without serving the single-owner, single-host deployment model.

## Consequences

Easier:

- Generic hosts get a copy-and-edit Compose installation and image-based upgrades.
- Docker, Podman, Container Manager, and other OCI runtimes share one image.
- One persistent root gives operators a clear backup boundary; the Portable Store inside it is what recovers saves ([ADR-012](ADR-012-portable-save-store.md)).

More difficult:

- Bind-mounted data must have permissions compatible with the image's unprivileged user; the default named volume avoids that setup burden.
- SQLite keeps the server intentionally single-instance; horizontal replicas would require a different storage decision.
