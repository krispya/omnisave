# ADR-001: The Server Is the Only Authority

**Date:** 2026-07-18

## Context

Omnisave spans machines. Games and their native saves live on Devices — a Steam Deck, a desktop — while the product's value is that save history outlives any one of them. The same save therefore exists in several places at once: adapter-native files on disk, the client's local state, and the server's records. Many features need one answer to "which copy wins": synchronization needs a baseline, concurrent commits need an arbiter, uninstalling a game must not destroy history, and the Dash needs one place that can show everything. Without a single authority, every feature would negotiate conflicts pairwise between machines.

## Decision

Clients make requests; the server is the only authority. Every write reaches the server as a request it judges — accept, reject, or enrich — and its answer is final. Clients remain the sole originators of save content (the server never invents a revision; it only judges what devices bring it), so authority is centralized while authorship is not.

That authority cashes out four ways:

**The server owns everything durable.** The Library, Omnisaves with their full revision history, Provenance, and content-addressed artifacts live on the server. Clients keep only machine-local selections — which games this Device tracks, bindings from Local Saves to Omnisaves, and the last-synced revision (the sync baseline). The litmus test: everything a client stores can be rebuilt from a fresh scan plus the server; nothing the server stores can be rebuilt from clients.

**Writes are arbitrated, and claims are verified.** A revision commit or restore names the Current Revision it expects; if another Device moved that pointer first, the server rejects the stale request (Current Revision Conflict) rather than merging, and forking preserves both sides of a divergence. Resolution weighs identity evidence and refuses to connect evidence already owned by different Games (Identity Conflict). An artifact upload claims a content hash the server recomputes from the payload before accepting. A catalog match redeems a server-issued selection token rather than writing metadata directly. Nothing a client asserts is taken on faith.

**Machine-local facts are reports, not commands.** Installed, tracked, and bound are data about a Device, recorded (as Provenance) without defining server truth. A game uninstalled everywhere remains in the Library with its history intact.

**Forgetting is explicit.** Deleting saves or games is a deliberate request for destruction that the server executes and nothing else implies — never a side effect of client state changing or disappearing.

Authority is applied in proportion to what is irreplaceable: history commits are arbitrated with expected Current Revisions, while presentation metadata (a save's display name) is accepted last-write-wins. Guarding a label like a ledger would add ceremony without protecting anything that cannot be retyped.

## Consequences

Easier:

- Synchronization is always device ↔ server, never peer-to-peer reconciliation between machines.
- Devices are disposable. Wiping a client loses only local selections; the history the user cares about is unaffected.
- Every feature reads from one authority: the Dash, provenance, deletion semantics, and future features can assume the server's view is complete.
- Concurrency stays simple: expected-current checks and forks, no merge algorithms.
- New API surface has a design test: every write must be phrased as a request the server can refuse — with the state it judged against named in the request where staleness matters.

More difficult:

- The server is a single point of durability. Users must run it reliably and back it up — the NAS-first deployment assumption exists because of this.
- A Device that plays offline holds saves newer than the server until it reconnects; the sync baseline only advances on successful synchronization.
- There is no multi-server or federation story: one client installation talks to one server.
