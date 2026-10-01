# ADR-002: Use SSE to Invalidate Server-Authoritative Views

**Date:** 2026-07-18

## Context

Open views — the Dash, and a watching client deciding whether to sync — cannot know when another client changes server state. A refresh button leaves them stale, while frequent polling creates continuous traffic and still delays updates. The server is the only authority under [ADR-001](ADR-001-server-authority.md), so a push channel must not become a second representation of that state.

## Decision

The server publishes authenticated Server-Sent Events after successful mutations that affect server-authoritative views. Events invalidate a named view; they do not contain replacement entities. A consumer responds by reading that view again through the ordinary HTTP API.

Three scopes exist. `library.changed` covers games, saves, revisions, and provenance, because those values travel together in Library reads. `access.changed` covers pairing requests and issued credentials, which appear and expire while someone is looking at them. `devices.changed` covers playing presence, including expiry the server decides on its own ([ADR-013](ADR-013-server-announced-presence-expiry.md)). One stream carries every scope, and each consumer acts only on the scopes it reads. Delivery is at least once per scope, consumers coalesce repeated invalidations, and refreshes are idempotent.

The event broker is in memory and belongs to the single server process, and the stream sends heartbeats so a consumer can tell a quiet stream from a dead one. A new or reconnected subscriber receives a checkpoint that means "resync everything you show", so event history needs no durable storage.

Consumers read the stream with an ordinary authorized request rather than the browser's native event source, so the credential stays in the authorization header.

## Consequences

Easier:

- Open views converge automatically after changes from any client, without polling.
- One-way SSE matches the problem without a bidirectional WebSocket protocol.
- Bursts can collapse into one read without delaying the writes that caused them.

More difficult:

- The server holds one long-lived response per open Dash or watching client.
- Every consumer carries its own stream parsing, reconnect, and backstop-refresh logic.
- Server restarts and network interruptions recover through a full resync, which deliberately spends redundant reads rather than trust a possibly stale view.
- Event IDs are process-local; a multi-server deployment would need a shared broker or a different invalidation strategy.
