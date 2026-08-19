# ADR-013: The Server Is the Only Clock for Presence

**Date:** 2026-08-08

## Context

Devices report which games they are playing, and the reports are credible only
briefly: a device that crashes or loses its network must stop reading as
"playing" on its own. The first implementation aged reports out only when they
were read and exposed their report time so every viewer could judge staleness
independently. That duplicated the expiry rule across clients and made presence
depend on each viewer's clock agreeing with the server. A machine a few minutes
off could show sessions forever, or never. The server is the only authority under
[ADR-001](ADR-001-server-authority.md); leaving readers to compute staleness
made them a second authority over what "now" means.

## Decision

Presence expiry is a state change the server announces, not a judgment readers
make. The server schedules the next expiry, removes stale reports when it is
reached, and invalidates the presence view through
[ADR-002](ADR-002-sse-view-invalidation.md), just as it does when a Device
reports a presence change.

Readers render what the server last served and do no expiry math. A playing
state remains credible until invalidation or a later read says otherwise. The
relationship between expiry and Device reaffirmation remains internal to the
system rather than part of the viewer contract.

Periodic and reconnection refreshes remain the backstop for a missed event, so a
dropped invalidation causes only a temporarily stale playing state and
converges on the next complete read.

## Consequences

Easier:

- Clock skew between server and viewer cannot show a wrong playing state; only the
  server's clock decides.
- Session start, stop, crash, and expiry all converge through one server-owned
  presence view.
- Tuning the credibility window needs no coordinated client change.

More difficult:

- The server holds a timer and does active work on an otherwise idle process;
  expiry correctness now depends on the sweep, not just on reads.
- A viewer that misses the expiry event shows stale presence until the next
  safety refresh, where reader-side aging hid it on time.
- The timer is process-local like the event broker; a multi-server deployment
  would need to place expiry beside whatever replaces the broker.
