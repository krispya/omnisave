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
be there to start it. Each platform uses its native per-user manager: a systemd
user unit on Linux, a LaunchAgent on macOS, and a logon-triggered Scheduled Task
on Windows. Their readable definitions live under the player's home directory,
and none requires administrator access.

The portable baseline is starting when the player signs in. Linux additionally
asks logind to keep the user's manager alive without a session, which lets the
service start at boot and carries a Steam Deck across the switch from Desktop
Mode to Gaming Mode. A refused linger request leaves a working login-started
service. LaunchAgents and interactive Scheduled Tasks intentionally wait for a
login because they run with that user's session and access.

The client exposes only the service lifecycle needed for setup, removal, and
inspection. Ongoing lifecycle control remains with the platform's native tool
rather than being duplicated imperfectly in Omnisave.

**Installing requires a connected device.** The unit runs `watch` with no
flags and no environment, so the only credential it can ever use is the one
`connect` persisted. A service installed before that starts, fails to find a
connection, and retries forever into a log nobody reads. That device is
confidently doing nothing, and from the outside it is indistinguishable from
one that works.

**Restarting is on purpose, and only what is running.** `omnisave update`
replaces the binary by renaming the new one over the path, which leaves a
running client executing the file it already opened — deliberate, so nothing
is interrupted mid-pass, but it means a service keeps running the old client
until something restarts it. On the devices that most need a service there is
nobody to notice. So an update restarts a running service and says it did. A
service that is stopped stays stopped: replacing a binary is no reason to
start something the player turned off.

**The service takes over rather than joining.** Handing continuous sync to the
managed service ends the foreground watcher, because two watchers on one Device
would run competing passes over the same tracking state. Once that handoff has
been decided, later runs preserve it rather than repeatedly asking.

## Consequences

Easier:

- A Steam Deck is set up once in Desktop Mode and keeps syncing in Gaming
  Mode, across mode switches and reboots, with no terminal.
- The service survives a SteamOS update, because it is defined in `$HOME`
  alongside the binary.
- macOS and Windows players get the same service lifecycle and update behavior
  through their native per-user managers.
- A player can ask a device with no display whether it is actually running,
  and get an answer that separates stopped from never installed.
- Updating a headless device leaves it running the client it just installed.

More difficult:

- Three native managers implement the same lifecycle, and their definitions,
  status models, failure messages, and restart rules must remain aligned.
- The client now writes a file outside its own state directory, and a player
  who edits that native definition will have it overwritten by the next
  install.
- A device running the service and a player running `omnisave` by hand are two
  clients over one tracking state. The first-run offer ends its own run to
  avoid it, but nothing prevents it afterward.
- Everything the client cannot do without a person — syncing a save down to a
  device that has none, resolving a stale or ambiguous binding, resolving a
  divergence — now goes permanently unanswered on a device running headless
  rather than waiting for the next interactive run. The service makes that
  condition normal instead of occasional, and nothing yet reports it anywhere
  the player will look.
