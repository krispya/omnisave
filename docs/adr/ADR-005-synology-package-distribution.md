# ADR-005: Ship a Native Synology Package

**Date:** 2026-07-25

## Context

Synology NAS owners can run the OCI image ([ADR-004](ADR-004-oci-image-distribution.md)) through Container Manager, but some expect Package Center to own installation, service lifecycle, upgrades, and access to the Dash. The server needs nothing on the NAS beyond its own binary, so a native package is only a matter of respecting DSM's lifecycle and persistent-directory conventions.

## Decision

Ship native DSM 7 SPKs containing the server and Dash for the `x86_64` and `armv8` package families.

The package runs the server as DSM's unprivileged package user, registers its port with Package Center, and adds the Dash to DSM's Main Menu. The menu entry is visible to every DSM user; Omnisave still applies its own access model after launch.

The lifecycle script exports a package-owned environment file in the package `etc` directory before starting the server ([ADR-003](ADR-003-environment-server-configuration.md)); the database and artifacts live in the package `var` directory. DSM keeps `etc` and `var` across upgrades and uninstalls, so configuration and save history survive both, and a reinstall reuses the surviving environment file unchanged. Installation writes a generated owner token into that file with owner-only permissions and does not display it: ordinary setup claims the server from the Dash ([ADR-010](ADR-010-taking-ownership.md)).

The package requires DSM 7.0-40314, the release that introduced the persistent `var` directory, and can be moved between volumes.

## Consequences

Easier:

- Synology users get native installation, start, stop, upgrade, port registration, and direct Main Menu access to the Dash.
- Configuration and durable state survive payload upgrades and reinstalls.
- The pure-Go SQLite implementation cross-compiles without a Synology C toolchain.

More difficult:

- Uninstalling does not reclaim the space the save history occupies; operators who want it gone must remove the package `var` directory themselves.
- Port 8080 is fixed in the package metadata, so changing the listen address in the environment file breaks the Main Menu link and port registration.
- SPKs require installation testing on representative DSM hardware even when their archive structure validates in CI.
