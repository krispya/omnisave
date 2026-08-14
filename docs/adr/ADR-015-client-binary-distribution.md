# ADR-015: Install the Client from Prebuilt Release Archives

**Date:** 2026-08-13

## Context

The server has two distribution paths ([ADR-004](ADR-004-oci-image-distribution.md),
[ADR-005](ADR-005-synology-package-distribution.md)); the client had none. It
was buildable only from source, which asks every player for a Go toolchain.

The client runs where the games are, and those machines are the least willing
hosts Omnisave targets. A Steam Deck runs SteamOS with an immutable root
filesystem: nothing may be written to `/usr`, and an OS update discards what
was. It has no compiler. Windows and macOS have no toolchain either, and both
interpose on unsigned downloaded executables — Gatekeeper through the quarantine
attribute, SmartScreen through the mark of the web — which a normal browser
download would trip.

The client is one statically linked Go binary with no runtime dependencies, and
it cross-compiles for every target platform without a C toolchain. What it
needs is somewhere to be published, a way to land in a directory the player owns
and an OS update will not reclaim, and a way to prove it arrived intact.

## Decision

Publish prebuilt client archives per platform on tagged releases, and install
them with a script the player pipes into a shell.

A `v`-prefixed tag is the only thing that publishes, and the tag is the version:
the `VERSION` variable in the Makefile remains the development default. Each
release carries one archive per supported platform — Linux, macOS, and Windows
on `amd64`, plus Linux and macOS on `arm64` — holding the client binary and the
license, beside a checksum file covering all of them. Archives are named for their
version and platform alone; the build number stays inside the binary, so one
release names its assets one way. Releases also carry build provenance
attestation.

Two installers consume that release: a POSIX shell script for macOS, Linux, and
SteamOS, and a PowerShell script for Windows. Both are run by piping them into
an interpreter, so neither reads standard input — the script is what standard
input already holds, and a prompt would consume the rest of it. Everything a
player might set is an environment variable instead. Both resolve the newest
release, verify the download against the release's checksum file, refuse to
install on a mismatch, and are safe to re-run.

An installed client upgrades itself with `omnisave upgrade`, reading that same
release layout and applying the same verification, so the archives are the
contract rather than the installers. It replaces the running binary in place and
only ever moves forward unless asked for a version by name. Re-running an
installer stays equivalent, which is what a player reaches for when a client is
too old to know the command.

Installation is per-user and needs no administrator: `~/.local/bin` on POSIX
systems, `%LOCALAPPDATA%\omnisave` on Windows. This is what makes the Steam Deck
work — `/home` survives a SteamOS update and `/usr` is not writable in the first
place. Because neither directory is reliably on `PATH`, the installers add it to
the player's shell startup file or user environment, report the file they
changed, and take an opt-out.

Fetching with `curl` or `Invoke-WebRequest` rather than a browser means the
downloaded binary carries neither the macOS quarantine attribute nor the Windows
mark of the web, so the client runs unsigned. Code signing, notarization, and a
browser-downloadable installer are therefore out of scope, as are package
manager channels such as Homebrew, AUR, and winget: each adds a distribution
surface to maintain before there is a user asking for it.

The installers take the release location as an override rather than hard-coding
it, which leaves room for an Omnisave server to serve the matching client to the
devices pairing with it, without changing how installation works.

## Consequences

Easier:

- Players install and upgrade the client on SteamOS, Windows, and macOS with one
  command and no toolchain, no administrator, and no package manager.
- Installation survives a SteamOS update, because nothing is written outside the
  player's home directory.
- A tampered or truncated download cannot install, and a release states which
  workflow built it.
- Cutting a release is tagging a commit; the archives, checksums, and notes
  follow from it.

More difficult:

- Five platform archives must stay building and verified alongside the client,
  and Windows carries a second installer written in a different language.
- The client is unsigned, so a player who downloads an archive through a browser
  instead of the installer meets a Gatekeeper or SmartScreen warning.
- Resolving the newest release depends on GitHub's API, which rate-limits by
  address; a pinned version is the way around it.
- The installers edit a shell startup file by default, which is a change to a
  file the player owns.
- Archives are named by version alone, so republishing a version would change
  what an already-recorded checksum refers to.
