# ADR-001: The Server Is the Only Authority

**Date:** 2026-07-18

## Context

Omnisave's value is that save history outlives any one machine, so the same save exists at once as native files on Devices, client state, and server records. Sync needs a baseline, concurrent commits need an arbiter, uninstalling a game must not destroy history, and the Dash needs one place that shows everything. Without a single authority, every feature would negotiate conflicts pairwise between machines.

## Decision

Clients make requests; the server is the only authority. Every write reaches the server as a request it judges — accept, reject, or enrich — and its answer is final. Clients remain the sole originators of save content (the server never invents a revision; it only judges what Devices bring it), so authority is centralized while authorship is not.

That authority cashes out four ways:

**The server owns everything durable.** The Library, omnisaves and their revision history, Provenance, and artifacts live on the server. Clients keep only machine-local state: their Device identity and credential, the games they track, and their Bindings with each one's sync baseline. Nothing the server stores can be rebuilt from clients.

**Writes are arbitrated, and claims are verified.** A revision commit or restore names the Current Revision it expects; if another Device moved that pointer first, the server rejects the stale request (Current Revision Conflict) rather than merging, and forking preserves both sides of a divergence. Resolution weighs identity evidence and refuses to connect evidence already held by different Games (an identity conflict). An artifact upload claims a content hash the server recomputes from the payload before accepting. A catalog match redeems a server-issued selection token rather than writing metadata directly.

**Machine-local facts are reports, not commands.** Installation and untracking are reports about a Device, recorded as Provenance; neither removes anything from the Library, so a game uninstalled everywhere keeps its history. Bindings never leave the Device.

**Forgetting is explicit.** Deleting saves or games is a deliberate request for destruction that the server executes and nothing else implies — never a side effect of client state changing or disappearing. How the server forgets safely is [ADR-014](ADR-014-durable-proof-before-forgetting.md).

Authority is applied in proportion to what is irreplaceable: history commits are arbitrated with expected Current Revisions, while presentation metadata (a save's display name) is accepted last-write-wins. Guarding a label like a ledger would add ceremony without protecting anything that cannot be retyped.

## Consequences

Easier:

- Synchronization is always Device ↔ server, never peer-to-peer reconciliation between machines.
- Devices are disposable: wiping a client loses no history.
- Concurrency stays simple: expected-current checks and forks, no merge algorithms.
- New API surface has a design test: every write must be phrased as a request the server can refuse — with the state it judged against named in the request where staleness matters.

More difficult:

- The server is a single point of durability. Users must run it reliably and back it up — the NAS-first deployment assumption exists because of this — and the portable store ([ADR-012](ADR-012-portable-save-store.md)) keeps history recoverable without it.
- A Device that plays offline holds saves newer than the server until it reconnects; the sync baseline only advances on successful synchronization.
- There is no multi-server or federation story: one client installation talks to one server.
