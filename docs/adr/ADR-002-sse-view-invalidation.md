# ADR-002: Use SSE to Invalidate Server-Authoritative Views

**Date:** 2026-07-18

## Context

The Dash reads the Library from the server, but it cannot know when another
client changes a game, save, revision, or provenance record. A manual refresh
button leaves an open Dash stale, while frequent polling creates continuous
traffic and still delays updates. The server remains the only authority under
[ADR-001](ADR-001-server-authority.md), so a push channel must not become a
second representation of Library state.

## Decision

The server publishes authenticated Server-Sent Events after successful
mutations that affect server-authoritative views. Events invalidate a named
view; they do not contain replacement entities. The Dash responds by reading
that view again through its ordinary HTTP API.

Two scopes exist. `library.changed` covers games, saves, revisions, and game
provenance, because those values travel together in Library reads.
`access.changed` covers pending pairing requests and issued credentials, which
appear and expire while someone is looking at them. Delivery is at least once
per scope, consumers coalesce repeated invalidations, and refreshes are
idempotent. A consumer subscribes to the scopes it reads; one connection
carries all of them, so watching a second scope costs no second stream.

The event broker is in memory and belongs to the single server process. Each
event has a monotonically increasing process-local ID, subscriber delivery is
non-blocking, and the stream sends heartbeats. A new or reconnected subscriber
receives an invalidation checkpoint and performs a complete resync, so event
history does not need durable storage.

The Dash consumes the stream through an authenticated request rather than the
browser's credential-limited native event source, allowing the credential to
remain in the ordinary authorization header.

## Consequences

Easier:

- An open Dash converges automatically after changes from any client.
- The server remains the only source of Library and provenance data.
- One-way SSE matches the problem without a bidirectional WebSocket protocol.
- Bursts can collapse into one read without delaying the writes that caused
  them.
- Server restarts and network interruptions recover through a full resync.

More difficult:

- The server maintains one long-lived HTTP response per open Dash.
- The Dash needs stream parsing, retry, visibility, and online recovery logic.
- Process-local IDs are not globally meaningful; a multi-server deployment
  would require a shared broker or a different invalidation strategy.
- Reconnection deliberately permits redundant reads in exchange for never
  trusting a possibly stale view.
