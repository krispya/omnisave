# ADR-013: The Server Is the Only Clock for Presence

**Date:** 2026-08-08

## Context

Devices report which games they are playing, and the reports are credible only
briefly: a device that crashes or loses its network must stop reading as
"playing" on its own. The first implementation aged reports out passively —
reads ignored anything older than a three-minute window — and served
`playing_reported_at` so the Dash could judge staleness itself. That spread one
window across three codebases as mirrored constants (server Go, client Go,
Dash TypeScript) coupled only by comments, forced the Dash to run its own
expiry timers, and made every badge depend on the browser's clock agreeing
with the server's. A machine a few minutes off showed sessions forever, or
never. The server is the only authority under
[ADR-001](ADR-001-server-authority.md); leaving readers to compute staleness
made them a second authority over what "now" means.

## Decision

Presence expiry is a state change the server announces, not a judgment readers
make. The presence store arms a timer for the earliest report's aging-out
moment; when it fires, the server sweeps stale reports and publishes
`devices.changed` exactly as it would for a device's own report — expiry rides
the invalidation channel of [ADR-002](ADR-002-sse-view-invalidation.md), and
`devices.changed` invalidates the lightweight presence view (`GET
/api/v1/presence`) rather than the whole Library.

Readers render what the server last served and do no time math. The Dash
treats a served playing flag as credible until an event or refetch says
otherwise; it holds no copy of the window and schedules nothing. The
credibility window and the client's re-affirmation cadence remain related
constants, but both live in Go in this repository, and the relationship is
server-internal.

The safety refreshes the Dash already performs (interval, reconnect, returning
to a visible tab) remain the backstop for a missed event; they refresh
everything, so a dropped `devices.changed` costs at most a few minutes of a
stale badge, never a wrong one after reconvergence.

## Consequences

Easier:

- Clock skew between server and viewer cannot show a wrong badge; only the
  server's clock decides.
- One code path: session start, stop, crash, and expiry all arrive as
  `devices.changed` followed by a light presence fetch.
- The window is one constant in one process; tuning it needs no coordinated
  change in the Dash.
- The Dash deleted its TTL constant, staleness predicate, and expiry timers.

More difficult:

- The server holds a timer and does active work on an otherwise idle process;
  expiry correctness now depends on the sweep, not just on reads.
- A viewer that misses the expiry event shows a stale badge until the next
  safety refresh, where reader-side aging hid it on time.
- The timer is process-local like the event broker; a multi-server deployment
  would need to place expiry beside whatever replaces the broker.
