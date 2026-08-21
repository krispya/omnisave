# ADR-016: Publish Every Distribution from One Version Tag

**Date:** 2026-08-13

## Context

Omnisave ships a client release archive ([ADR-015](ADR-015-client-binary-distribution.md)), a multi-architecture OCI image ([ADR-004](ADR-004-oci-image-distribution.md)), and two Synology SPKs ([ADR-005](ADR-005-synology-package-distribution.md)). Building those paths separately allows their public versions and source commits to drift even though they belong to one product release.

DSM also requires a monotonically increasing numeric build component in addition to the product version. The client already embeds that build number, while OCI registries identify an image by its tag and immutable digest.

## Decision

A pushed `vMAJOR.MINOR.PATCH` tag is the only release signal and supplies the version for every distribution. One workflow derives the version and the repository commit count once, then uses those values for every build from the tagged commit. The workflow refuses a tag whose commit is not reachable from `main`, so releases come only from the tested, monotonically advancing mainline.

Before creating the GitHub release, the workflow builds and verifies the client archives, both Synology architectures, and the `linux/amd64` and `linux/arm64` OCI image. Client archives and SPKs are attached to the GitHub release, while the image is published to GHCR with both the version and `latest` tags. The downloadable artifacts receive build-provenance attestations.

The semantic version appears unchanged in client archive names and OCI tags. The SPKs use `MAJOR.MINOR.PATCH-BUILD`, where `BUILD` is the commit count shared with the client binary. Outside the release workflow, builds default to the nearest reachable version tag and the current commit count, so post-release artifacts retain that release's semantic version while their build number advances. Explicit `VERSION` and `BUILD` overrides remain available for development builds without publishing anything.

## Consequences

Easier:

- One tag identifies matching client, OCI, and Synology distributions.
- A tag on an unmerged branch cannot publish a release.
- A release cannot be created by the workflow after an earlier build or publish step fails.
- The SPK build number remains numeric and monotonically increasing while its semantic version stays aligned with the other formats.
- Local post-release SPKs can upgrade the tagged release they follow instead of falling behind it under a stale development version.
- Pull requests exercise every release build before a tag points at the commit.

More difficult:

- Cutting a release now depends on the JavaScript, Go, Synology packaging, and multi-platform container toolchains all succeeding in one workflow.
- GHCR publication is an external write and cannot be transactional with GitHub release creation; a failure after the image push may require rerunning the workflow before the release appears.
- Moving `latest` is part of every successful versioned image publication.
