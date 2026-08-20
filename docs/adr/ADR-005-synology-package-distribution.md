# ADR-005: Ship a Native Synology Package

**Date:** 2026-07-25

## Context

Synology NAS owners can run the OCI image through Container Manager, but some expect Package Center to own installation, service lifecycle, upgrades, and access to the Dash. Omnisave's server is one statically linked Go process serving one Dash, so a native package does not need a language runtime, an external database, or a separate web server on the NAS.

Native packaging must preserve the same server behavior as other deployments while respecting DSM's package lifecycle and persistent-directory conventions.

## Decision

Ship native DSM 7 SPKs containing the server and Dash for Synology `x86_64` and `armv8` package families.

The package runs the server as DSM's unprivileged package user, registers port 8080 with Package Center, and registers the Dash in DSM's Main Menu. The menu entry is visible to every DSM user; Omnisave still applies its own access model after launch. A package-owned environment file lives under the package `etc` directory, following the environment configuration contract in [ADR-003](ADR-003-environment-server-configuration.md); the lifecycle script exports it before launching the server. The SQLite database, content-addressed artifacts, PID, and service log live under the package's persistent `var` directory. Installation generates the owner token, writes that environment file with owner-only permissions, and shows the token once in Package Center's completion message ([ADR-010](ADR-010-taking-ownership.md)).

DSM removes only `target` and `tmp` when a package is upgraded or uninstalled, so `etc` and `var` carry configuration and save history across both. Because `etc` therefore outlives an uninstall, a reinstall keeps the surviving environment file as it stands — including the owner token, which devices may already have been connected against — and generates one only if that file has none. The package requires DSM 7.0-40314, the release that introduced the persistent `var` directory. The paths written into the environment file address `etc`, `var`, and `target` through their `/var/packages` symlinks rather than the volume-specific paths those resolve to, so the package remains movable between volumes.

The package lifecycle scripts start, stop, and report the status of one server process. Stopping sends SIGTERM and allows up to 15 seconds for graceful HTTP shutdown before forcing termination. Upgrades replace the server and Dash payload without replacing configuration or durable state.

## Consequences

Easier:

- Synology users get native installation, start, stop, upgrade, port registration, and direct Main Menu access to the Dash.
- Configuration and durable state survive payload upgrades.
- The pure-Go SQLite implementation cross-compiles without a Synology C toolchain.

More difficult:

- SPK metadata, lifecycle scripts, and separate architecture packages must stay tested alongside the server.
- Uninstalling does not reclaim the space the save history occupies; operators who want it gone must remove the package `var` directory themselves.
- The package cannot run on DSM releases older than 7.0-40314.
- SPKs require installation testing on representative DSM hardware even when their archive structure validates in CI.
