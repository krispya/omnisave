# ADR-003: Configure the Deployment Through the Environment and Owner Settings Through the Dash

**Date:** 2026-07-22

## Context

Omnisave runs the same server binary during local development, in an OCI container, and in a native Synology package. Supporting both a configuration file and environment variables would give those installations two contracts with precedence rules, incomplete field coverage, and separate examples to maintain.

Not every answer belongs to the deployment, though. Whether to announce on the local network is a choice about tonight's network, and IGDB credentials come from the owner's own Twitch account — two people running identical deployments hold different ones, and one person keeps theirs across a reinstall. Putting those behind a variable and a restart turns a runtime choice into a deployment ritual; making them writable without a rule invites two sources of truth that disagree after the next restart.

## Decision

**The deployment is configured through the environment.** Everything the deployment owns — paths, the listen address, the announced name, provider endpoints, timeouts and rate limits, and the owner token — comes from `OMNISAVE_*` variables read once at startup. The server parses no configuration file and accepts no configuration path, and optional values have server-owned defaults. `.env.example` is the complete list. Launchers export the variables — the development script from an ignored `.env`, Compose into the container, the Synology package from its own environment file — and the server never reads `.env` itself. The one value the server may generate is the owner token: with no `OMNISAVE_TOKEN` or `OMNISAVE_TOKEN_FILE`, it mints one beside its database and uses it from then on ([ADR-010](ADR-010-taking-ownership.md)).

**Owner settings sit beside it.** A small set of choices — local network discovery, and the IGDB client ID and secret — is stored in the database, read and changed by the owner in the Dash, and applied immediately: turning discovery off stops the announcement, and saving IGDB credentials swaps the provider in without a restart. A setting qualifies only when the owner is the right person to decide it and the answer can change without the deployment changing. Each is argued individually; everything else stays a variable.

A setting declares its kind. A switch is on or off, and text is shown and edited plainly. **A secret is written and never read back**: the API reports only whether one is stored, and replacing it is the only way to change it. A secret is kept as given rather than hashed like the PIN, because the server must replay it to the provider.

**The environment wins.** A variable pins its setting: the Dash shows the value as set by the deployment and refuses edits.

## Consequences

Easier:

- Every deployment uses one set of names and one contract, and container orchestrators and secret mounts configure the server directly.
- The choices an owner revisits live in the Dash and take effect when made. An owner adds IGDB when they get to it, instead of learning where their deployment keeps its environment.
- Fleet operators keep a single source of truth by pinning variables, and see in the Dash which settings they have taken over.
- The next provider's credentials fit the same tier without a decision of their own.

More difficult:

- Configuration has two homes, so every new knob needs a deliberate answer about which one it belongs in, and the answer is easy to get wrong toward convenience.
- Owner settings are durable state in the database, outside the portable store ([ADR-012](ADR-012-portable-save-store.md)): they must survive upgrades, and a rebuilt database starts from defaults.
- Precedence has to be visible to be believed: a Dash that silently ignored an edit because a variable is pinned would be worse than one that never offered it.
- A secret lives in the database in a form the server can replay, so anyone holding a copy of the database holds the owner's IGDB credentials. The mitigation is that they are scoped to a third-party read API the owner can revoke from Twitch.
- Write-only is easy to break by accident — one convenient debugging endpoint would undo it — so it is tested rather than remembered.
- Credentials are not checked when saved, so a wrong pair shows as configured while IGDB answers unavailable.
- A provider whose credentials change under a running server can answer differently between two requests; the catalog tolerates that because it already tolerates a provider that cannot answer.
- Environment values are strings, so the server parses and validates typed values itself, and nested provider settings cannot be grouped the way a file would allow.
