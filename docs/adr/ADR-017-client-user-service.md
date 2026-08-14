# ADR-017: Run the Client as a Service the Player Owns

**Date:** 2026-08-13

## Context

The client protects saves by watching them. `omnisave watch` is a complete
loop with nobody in front of it: it needs no terminal, degrades to a plain
event log when it has none, and reconciles exactly what a commandless run
reconciles. What it does not do is start itself.

Every device Omnisave has targeted so far had someone at a keyboard, so
"start it and leave it running" was a terminal window and a habit. A Steam
Deck has neither. Gaming Mode has no terminal at all, and switching to it
ends the Desktop Mode session — which kills the terminal, and with it any
process started from one, because logind reclaims a user's processes when
their last session ends. A client that has to be launched by hand is a client
that never runs on the device whose entire purpose is playing games.

The same gap is not unique to SteamOS. [ADR-015](ADR-015-client-binary-distribution.md)
already established that the client is installed per-user, into a directory
the player owns, with no administrator and nothing written outside `$HOME`,
because that is what survives an immutable OS. Running it needs the same
property for the same reason: a background process that requires root, or
that writes to `/etc`, would be reclaimed by the next SteamOS update even
though the binary beside it survived.

## Decision

Ship a background service that runs `omnisave watch`, defined per-user,
managed by `omnisave service`.

The service runs as the player, from the player's own session manager, using
the same binary [ADR-015](ADR-015-client-binary-distribution.md) installed.
It grants the client nothing: the same UID, the same home directory, the same
access a terminal would have given it. What it changes is that no one has to
be there to start it. On Linux that is a systemd user unit at
`~/.config/systemd/user/omnisave.service`; macOS and Windows have the same
shape available — a launch agent and a scheduled task, both per-user and
session-started — and are not implemented yet.

Three actions, no more: `install` defines the service and starts it,
`uninstall` stops and removes it, `status` says whether it is running. There
is deliberately no `start`, `stop`, or `restart` — those would be a worse copy
of the platform's own tool, which is right there and better at it. `status`
earns its place because it is the only way to answer "is this actually
running?" on a device with no terminal to have watched it from.

**Installing requires a connected device.** The unit runs `watch` with no
flags and no environment, so the only credential it can ever use is the one
`connect` persisted. A service installed before that starts, fails to find a
connection, and retries forever into a log nobody reads. That device is
confidently doing nothing, and from the outside it is indistinguishable from
one that works.

**Restarting is on purpose, and only what is running.** `omnisave upgrade`
replaces the binary by renaming the new one over the path, which leaves a
running client executing the file it already opened — deliberate, so nothing
is interrupted mid-pass, but it means a service keeps running the old client
until something restarts it. On the devices that most need a service there is
nobody to notice. So an upgrade restarts a running service and says it did. A
service that is stopped stays stopped: replacing a binary is no reason to
start something the player turned off.

**The service takes over rather than joining.** The run that sets tracking up
asks once, at the end, whether this device should keep syncing after the
terminal closes — that run already has the player's attention, and on a
handheld it is the last moment before there is none. Accepting ends the run
instead of proceeding to watch, because two watchers on one device would be
two passes writing the same tracking state. Later runs do not ask again; they
mention the command in one dim line and get on with the work.

**Linger is an improvement, not a requirement.** `loginctl enable-linger`
starts the user's manager at boot and keeps it alive with no session, which is
what carries the service across the gap while a Steam Deck switches out of
Desktop Mode. It can be refused by a policy with nobody there to authorize it,
so a refusal does not fail the install — the service still starts with the
session, and the status says which of the two the device has.

## Consequences

Easier:

- A Steam Deck is set up once in Desktop Mode and keeps syncing in Gaming
  Mode, across mode switches and reboots, with no terminal.
- The service survives a SteamOS update, because it is defined in `$HOME`
  alongside the binary.
- A player can ask a device with no display whether it is actually running,
  and get an answer that separates stopped from never installed.
- Upgrading a headless device leaves it running the client it just installed.

More difficult:

- Three platforms will need three implementations of the same shape, and only
  one exists; macOS and Windows say so plainly rather than pretending.
- The client now writes a file outside its own state directory, and a player
  who edits that unit will have it overwritten by the next install.
- A device running the service and a player running `omnisave` by hand are two
  clients over one tracking state. The first-run offer ends its own run to
  avoid it, but nothing prevents it afterward.
- Everything the client cannot do without a person — syncing a save down to a
  device that has none, resolving a stale or ambiguous binding, resolving a
  divergence — now goes permanently unanswered on a device running headless
  rather than waiting for the next interactive run. The service makes that
  condition normal instead of occasional, and nothing yet reports it anywhere
  the player will look.
