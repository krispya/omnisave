# ADR-020: Domains Own Their Contracts

**Date:** 2026-09-30

## Context

The server's domains each had a clear service interface, but what they needed from persistence was defined elsewhere. One storage package declared every domain's repository interface and re-declared its errors, so each service depended on a package that depended on all the others. Rules that must hold atomically with a write were implemented twice — by the SQLite adapter and by an in-memory fake most domain tests used — with nothing keeping the two in step. The error codes clients act on were string literals in the server, the Go client, and the Dash, and the client's Save Sync lived in the CLI's main package.

A domain that cannot be read or tested without its neighbours has no boundary another domain can rely on.

## Decision

Each domain owns its contract in its own package: its model, its `Service`, its errors, and the `Repository` it needs from persistence. The server's domains are `omnisave` (save history), `catalog` (Games, their media, and Provenance), `device` (registration and playing presence), `access`, and `settings`. `artifact` is the shared vocabulary for content-addressed bytes that save history and game media both reference.

The `Service` validates and shapes input. The `Repository` enforces every invariant that must hold atomically with the write that could break it — the Current Revision check, the graph's refusal to lose a node something still needs, whole-history path migrations, a save's Game existing — and reports each refusal with the domain's own errors. Its contract documents those invariants.

Adapters implement or adapt those contracts and never define them. `httpapi` adapts every service to HTTP. The wire contract — change-feed event names and the one mapping between domain errors and error responses — lives in `httpapi/contract`, which the server and the Go client both compile against. The Dash mirrors it by hand.

Domain rules have one implementation: domain tests run against the real SQLite adapter in a temporary directory, never a fake.

On the client, Save Sync is its own package (`client/savesync`) behind ports it defines: the server surface it calls, the adapters it consults, a reporter for what happened, and prompts for decisions only a person can make. The CLI supplies the implementations and presents the results.

Save history owns its immutable portable scope contract. Client tracking owns
native save slot selections and local bindings; those account and slot identities
are not server history identity. The client game-save domain owns the extension
contract and validates native ownership; per-game extensions supply membership
rules and fixtures. Launcher adapters consume the resulting placement contracts
and perform store operations; they do not interpret game schemas. The initial
extension capability is read-only directory discovery, not arbitrary restoration
code or a shared runtime with server revision labelers.
See [ADR-021](ADR-021-independent-save-boundaries.md).

## Consequences

Easier:

- A domain can be read from its package alone: what it offers, what it needs, and how it refuses.
- The persistence rules the server ships are the rules every domain test exercises.
- A renamed event or a new refusal is a compile-time change for the server and the Go client, and one round-trip test covers the whole error mapping.
- Save Sync's decisions can be followed and tested without the CLI's flag parsing or terminal rendering.

More difficult:

- Every domain test pays for a SQLite database and a portable store on disk, and must seed the Games its saves belong to.
- Behavior the SQLite adapter performs after answering, such as reclaiming deleted artifacts, is visible to tests only after they wait for it.
- Request and response bodies are the domain types themselves, so renaming a domain field is a wire change, and the Dash's hand-kept copy will not catch it.
- A domain that needs another domain's data must say so in its own contract rather than reach through a shared persistence interface.
