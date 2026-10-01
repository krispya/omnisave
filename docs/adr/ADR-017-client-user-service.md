# ADR-017: Run the Client as a Service the Player Owns

**Date:** 2026-08-13

## Context

The Client protects saves by watching them. `omnisave watch` is a complete loop that needs nobody in front of it, but nothing starts it.

A Steam Deck has no one at a keyboard. Gaming Mode has no terminal, and switching to it ends the Desktop Mode session, after which logind reclaims every process started from that session. A Client that has to be launched by hand never runs on the Device whose whole purpose is playing games.

Like the binary ([ADR-015](ADR-015-client-binary-distribution.md)), anything that runs it must need no administrator and write nothing outside `$HOME`, or the next SteamOS update reclaims it.

## Decision

Ship a background service that runs `omnisave watch`, defined per-user and managed by `omnisave service`.

The service runs as the player, from the player's own session manager, using the binary ADR-015 installed. It has exactly the access a terminal would have given it; what changes is that no one has to be there to start it. Each platform uses its native per-user manager: a systemd user unit on Linux, a LaunchAgent on macOS, and a logon-triggered Scheduled Task on Windows. Their definitions live under the player's home directory, and none requires administrator access.

The portable baseline is starting when the player signs in. Linux also asks logind to keep the user's manager alive without a session, which starts the service at boot and carries a Steam Deck from Desktop Mode into Gaming Mode. If the linger request is refused, the service still works and starts at login. LaunchAgents and interactive Scheduled Tasks wait for a login because they run with that user's session.

The Client exposes only the lifecycle needed for setup, removal, and inspection. Ongoing control stays with the platform's native tool.

**Installing requires a connected Device.** The service runs `watch` with no flags, so it can only use the credential `connect` saved. Installing refuses a Device that is not connected rather than creating a service that silently retries forever.

**Updating restarts only what is running.** A running Client keeps executing the binary it started with, so `omnisave update` restarts a running service and says so, and a headless Device then runs what it just installed. A stopped service stays stopped.

**The service takes over rather than joining.** Two watchers on one Device would run competing passes over the same tracking state, so accepting the service ends the foreground watcher. The offer comes only from a run that set up tracking on a Device without a service.

## Consequences

Easier:

- A Steam Deck is set up once in Desktop Mode and keeps syncing in Gaming Mode, across mode switches, reboots, and SteamOS updates, with no terminal.
- macOS and Windows players get the same lifecycle and update behavior through their native per-user managers.
- A Device with no display can report whether its service is running, stopped, or not installed.

More difficult:

- Three native managers implement the same lifecycle, and their definitions, status models, failure messages, and restart rules must stay aligned.
- The Client writes a file outside its own state directory, and a player who edits that native definition will have it overwritten by the next install.
- A player who later runs `omnisave` by hand beside the service creates two Clients over one tracking state, and nothing prevents it.
- Decisions that need a person wait indefinitely on a headless Device, and nothing yet surfaces them anywhere the player will look.
