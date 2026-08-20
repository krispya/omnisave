# ADR-004: Ship the Server as an OCI Image

**Date:** 2026-07-21

## Context

Omnisave's server is the only durable authority for save history, so running it reliably and backing it up must be approachable for a home-server owner. The server is one Go process serving one Dash, with SQLite metadata and content-addressed artifacts on local storage. It does not need a cluster, an external database, or a language runtime on the host.

The generic self-hosting package must keep the server's storage, configuration, and upgrade behavior explicit. It also must not make Docker the product's runtime contract when OCI-compatible alternatives can run the same image.

## Decision

Ship a multi-architecture OCI image as the primary generic self-hosting artifact. Docker Compose is the documented single-host installer, while any OCI-compatible runtime may run the image.

The image runs one server instance, listens on port 8080 by default, exposes unauthenticated liveness and readiness endpoints, and keeps the SQLite database and artifacts together in one persistent data root. The image supplies its own container paths as environment defaults, following the environment configuration contract in [ADR-003](ADR-003-environment-server-configuration.md); Compose overrides only the settings an operator owns. The server handles SIGTERM with a graceful HTTP shutdown before the container is stopped.

The image runs as a fixed unprivileged user and contains no mutable state. Compose mounts the data root; the server generates its own owner token when none is supplied.

Kubernetes packaging, an external database, and multiple server replicas are out of scope. They add operational surface without serving the single-owner, single-host deployment model.

## Consequences

Easier:

- Generic hosts get a copy-and-edit Compose installation and image-based upgrades.
- Docker, Podman, Container Manager, and other OCI runtimes share one image.
- One persistent root gives operators a clear backup boundary.

More difficult:

- Image metadata and architecture matrices must stay tested alongside the server.
- Bind-mounted data must have permissions compatible with the image's unprivileged user; the default named volume avoids that setup burden.
- SQLite keeps the server intentionally single-instance; horizontal replicas would require a different storage decision.
