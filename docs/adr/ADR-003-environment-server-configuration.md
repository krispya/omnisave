# ADR-003: Configure the Deployment Through the Environment

**Date:** 2026-07-26

## Context

Omnisave runs the same server binary during local development, in an OCI
container, and in a native Synology package. Supporting both a YAML file and
environment variables would give those installations two configuration
contracts with precedence rules, incomplete field coverage, and separate
examples to maintain.

A local `.env` file is a development convenience rather than a second runtime
format: the development launcher reads it and exports its values before
starting the server. Container and package launchers can export the same names
using their native configuration mechanisms.

Not everything configurable belongs to the deployment, though. Some answers are
the owner's — whether to announce on the local network, which IGDB account to
use — and change while the server runs rather than when it is installed. Those
are the subject of [ADR-008](ADR-008-owner-settings-beside-environment.md), and
the line between the two tiers is drawn there.

## Decision

Everything the deployment owns comes from `OMNISAVE_*` environment variables,
read once at startup. The server parses no configuration file and accepts no
configuration path. Optional values have server-owned defaults.

That covers paths, the listen address, the server's announced name, provider
endpoints and timeouts, and the owner token. It does not cover owner settings,
which live in the database and are edited in the Dash; where a variable and an
owner setting name the same thing, the variable wins and the Dash says so.

The server writes exactly one thing: an owner token beside its database, when
no `OMNISAVE_TOKEN` or `OMNISAVE_TOKEN_FILE` was supplied. That is the single
exception to "reads, never writes", and it exists because a credential cannot
be handed to a server nobody has reached yet
([ADR-010](ADR-010-taking-ownership.md)). It is never re-read as configuration
and never merged with the environment: a deployment that supplies a token never
reaches it.

The repository's `.env.example` documents the complete contract. Local
development copies it to an ignored `.env`; Compose maps its deployment paths
and settings into the container environment; and the Synology lifecycle script
exports a package-owned environment file before launching the same binary.

## Consequences

Easier:

- Every deployment uses one set of names and one contract, with precedence
  arising only where the owner-settings tier deliberately overlaps it.
- Container orchestrators and secret mounts configure the server directly.
- A fleet operator can pin any owner setting by exporting its variable, and get
  identical behavior across machines without touching any of them twice.

More difficult:

- Configuration has two homes, so every new knob needs a deliberate answer about
  which one it belongs in, and the answer is easy to get wrong toward
  convenience.
- Operators cannot group nested provider settings in YAML.
- Environment values are strings, so the server must parse and validate typed
  values explicitly.
- Launchers that use an environment file must export it before starting the
  server; the server intentionally does not load `.env` itself.
- The one file the server writes is a boundary that has to be defended each
  time something else wants to be written beside it.
