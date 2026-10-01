# ADR-016: Publish Every Distribution from One Version Tag

**Date:** 2026-08-13

## Context

Omnisave ships Client archives ([ADR-015](ADR-015-client-binary-distribution.md)), a multi-architecture OCI image ([ADR-004](ADR-004-oci-image-distribution.md)), and Synology SPKs ([ADR-005](ADR-005-synology-package-distribution.md)). Building them separately lets their versions and source commits drift, even though they belong to one release. DSM also requires a monotonically increasing numeric build number beside the product version.

## Decision

A pushed `vMAJOR.MINOR.PATCH` tag is the only release signal and supplies the version for every distribution. One workflow derives the version and the repository commit count once and builds everything from the tagged commit. It refuses a tag whose commit is not reachable from `main`.

CI builds every distribution on every pull request and every push to `main`, so a tag lands on a commit whose release builds have already passed.

The release workflow builds the archives and SPKs and publishes the image to GHCR as the version and `latest`. Only after that does it create the GitHub release. The release carries the archives, the SPKs, the Client checksum file, and build-provenance attestations for the downloadable artifacts.

The semantic version appears unchanged in archive names and image tags. SPKs use `MAJOR.MINOR.PATCH-BUILD`, where `BUILD` is the commit count, which the Client binary also embeds. Builds outside the workflow default to the nearest reachable version tag and the current commit count.

## Consequences

Easier:

- One tag identifies matching Client, OCI, and Synology distributions, all built from `main`.
- No release is created after a failed build or publish step.
- A local post-release SPK keeps its release's version with a higher build number, so it upgrades the release it follows.

More difficult:

- Cutting a release depends on the JavaScript, Go, Synology packaging, and multi-platform container toolchains all succeeding in one workflow.
- GHCR publication cannot be transactional with the GitHub release; a failure after the image push may need a rerun before the release appears.
- Compose tracks `latest` by default, so every release reaches the next `docker compose pull`; holding back means pinning a version.
- Releasing only from `main` rules out maintenance releases of an older version.
