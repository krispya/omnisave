# ADR-015: Install the Client from Prebuilt Release Archives

**Date:** 2026-08-13

## Context

The Client runs where the games are, and those machines are the least willing hosts Omnisave targets. A Steam Deck runs SteamOS with an immutable root filesystem: `/usr` is not writable, an OS update discards anything written outside the home directory, and there is no compiler. Windows and macOS have no toolchain either.

The Client is one statically linked Go binary that cross-compiles for every target platform without a C toolchain. It needs somewhere to be published, a directory the player owns that an OS update will not reclaim, and proof that it arrived intact.

## Decision

Publish prebuilt Client archives on every release ([ADR-016](ADR-016-synchronized-release-distributions.md)), and install them with a script the player pipes into a shell.

Each release carries one archive per supported operating system and architecture, named by version and platform alone, beside a checksum file covering all of them.

A POSIX shell installer covers macOS, Linux, and SteamOS; a PowerShell installer covers Windows. Because they are piped into an interpreter, they take settings only from environment variables. Both resolve the newest or a pinned release, verify it against the checksum file, refuse a mismatch, and are safe to re-run.

`omnisave update` reads the same release layout with the same verification, so the archives are the contract rather than the installers. It only moves forward unless asked for a version by name. Re-running an installer is equivalent, for a Client too old to know the command.

Installation is per-user and needs no administrator: `~/.local/bin` on POSIX systems, `%LOCALAPPDATA%\omnisave` on Windows. That survives a SteamOS update. The installers add the directory to `PATH` in the player's shell startup file or user environment, with an opt-out.

Fetching with `curl` or `Invoke-WebRequest` rather than a browser leaves no macOS quarantine attribute or Windows mark of the web, so the Client ships unsigned. Code signing, notarization, browser-downloadable installers, and package managers such as Homebrew, AUR, and winget are out of scope until someone asks for them.

## Consequences

Easier:

- Players install and update the Client with one command and no toolchain, administrator, or package manager.
- Installation survives a SteamOS update, because nothing is written outside the player's home directory.
- A tampered or truncated download cannot install.

More difficult:

- Windows carries a second installer written in a different language.
- A player who downloads an archive through a browser instead meets a Gatekeeper or SmartScreen warning.
- Resolving the newest release depends on GitHub's API, which rate-limits by address; pinning a version avoids it.
- The installers edit a shell startup file by default, which is a file the player owns.
- Archives are named by version alone, so republishing a version would change what an already-recorded checksum refers to.
