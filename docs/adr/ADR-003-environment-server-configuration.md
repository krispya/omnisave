# ADR-003: Configure the Server Through the Environment

**Date:** 2026-07-22

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

## Decision

The server reads all runtime configuration from `OMNISAVE_*` environment
variables. It does not parse a configuration file or accept a configuration
path. Optional values have server-owned defaults, while the API token must be
provided through `OMNISAVE_TOKEN` or the mutually exclusive
`OMNISAVE_TOKEN_FILE` secret-file setting.

The repository's `.env.example` documents the complete contract. Local
development copies it to an ignored `.env`; Compose maps its deployment paths
and settings into the container environment; and the Synology lifecycle script
exports a package-owned environment file before launching the same binary.

## Consequences

Easier:

- Every deployment uses one set of names and one precedence-free contract.
- Container orchestrators and secret mounts configure the server directly.
- New settings have one documented example and must receive an environment
  variable to become configurable.

More difficult:

- Operators cannot group nested provider settings in YAML.
- Environment values are strings, so the server must parse and validate typed
  values explicitly.
- Launchers that use an environment file must export it before starting the
  server; the server intentionally does not load `.env` itself.
