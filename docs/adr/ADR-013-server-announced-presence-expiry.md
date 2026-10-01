# ADR-013: The Server Is the Only Clock for Presence

**Date:** 2026-08-08

## Context

Devices report which games they are playing, and a report is credible only briefly: a Device that crashes or loses its network must stop reading as playing on its own. If readers aged reports themselves, the expiry rule would be copied into every viewer and would depend on each viewer's clock agreeing with the server's — a machine a few minutes off could show sessions forever, or never. The server is the only authority under [ADR-001](ADR-001-server-authority.md); reader-side aging would make readers a second authority over what "now" means.

## Decision

Presence expiry is a state change the server announces, not a judgment readers make. The server schedules the next expiry, removes stale reports when it is reached, and publishes `devices.changed` ([ADR-002](ADR-002-sse-view-invalidation.md)), just as it does when a Device reports a presence change.

Readers learn only whether a Device is playing, never when it last said so, and do no expiry math. A playing state remains credible until an invalidation or a later read says otherwise. The credibility window is not part of the read contract.

Periodic and reconnection refreshes remain the backstop for a missed event, so a dropped invalidation causes only a temporarily stale playing state that converges on the next complete read.

## Consequences

Easier:

- Clock skew between server and viewer cannot show a wrong playing state; only the server's clock decides.
- Session start, stop, crash, and expiry all converge through one server-owned presence view.
- Tuning the credibility window needs no viewer change, as long as Devices still reaffirm more often than the window.

More difficult:

- The server holds a timer and does active work on an otherwise idle process; expiry correctness depends on the sweep, not just on reads.
- A viewer that misses the expiry event shows stale presence until its next backstop refresh, where reader-side aging would have hidden it on time.
