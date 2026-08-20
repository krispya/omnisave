# ADR-008: Keep Owner Settings Beside Environment Configuration

**Date:** 2026-07-25

## Context

[ADR-003](ADR-003-environment-server-configuration.md) makes the environment the server's whole configuration contract: an operator sets variables, restarts, and the server does what they say. That has held because everything configurable so far belongs to the deployment — paths, listen address, token, provider credentials — and changes at the same moments a deployment changes.

Local network discovery breaks the pattern. Whether the server announces itself is not a property of where it is installed but a choice its owner makes about the network they are on that day, and the natural place to make it is the Dash, the way a media server exposes its network settings. Answering that with a variable and a restart puts a runtime choice behind a deployment ritual; adding a writable setting without a rule invites two sources of truth that disagree after the next restart.

## Decision

Keep two tiers, with the environment on top.

Deployment configuration stays exactly as ADR-003 describes: environment only, read at startup, owned by whoever runs the server. Nothing in this decision makes a path, a listener, a token, or a provider credential editable at runtime. One line of ADR-003 does give way — its consequence that a new setting must receive an environment variable to become configurable — and only that line.

Beside it, a small set of **owner settings** — choices the owner makes while the server runs, starting with local network discovery — is stored in the database and edited in the Dash. Owner settings apply immediately: turning discovery off stops the announcement rather than scheduling a restart.

The environment wins where the two meet. A setting pinned by a variable is not editable in the Dash; it is shown with its value and marked as coming from the deployment. An operator who wants a fleet to behave identically keeps pinning variables, and an owner who pins nothing gets a switch that works.

Owner settings are added one at a time and argued for individually. A setting qualifies only when the owner is the right person to decide it and the answer can change without the deployment changing. Everything else stays a variable. [ADR-011](ADR-011-owner-provider-credentials.md) is the second one to qualify and the one that widened this tier beyond yes-and-no: a setting now declares whether it holds a switch, text, or a secret that is written and never read back.

## Consequences

Easier:

- The choices an owner actually revisits live where they look, and take effect when they make them.
- Fleet operators keep a single source of truth by pinning variables, and see in the Dash which settings their deployment has taken over.
- ADR-003 keeps governing everything the deployment owns, and the one consequence this decision narrows is narrowed by naming the exception rather than leaving it to precedent.

More difficult:

- Configuration now has two homes, so every new knob needs a deliberate answer about which one it belongs in, and the answer is easy to get wrong toward convenience.
- Settings become durable state to migrate, back up, and reason about across upgrades, where the environment carried none.
- Precedence has to be visible to be believed: a Dash that silently ignores an edit because a variable is pinned is worse than one that never offered it.
