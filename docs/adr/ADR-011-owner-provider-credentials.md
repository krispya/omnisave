# ADR-011: Let the Owner Hold Provider Credentials, and Give Owner Settings Values Other Than Yes and No

**Date:** 2026-07-26

## Context

IGDB is what turns a scanned folder into a game with a title, a platform, and
cover art. Reaching it needs a client ID and secret from a Twitch developer
account, and today those arrive only as `OMNISAVE_IGDB_CLIENT_ID` and
`OMNISAVE_IGDB_CLIENT_SECRET`, read once at startup
([ADR-003](ADR-003-environment-server-configuration.md)). An owner who signs up
after installing has to find the environment their deployment uses, edit it,
and restart the server — for a credential that belongs to them personally and
has nothing to do with where the server runs.

[ADR-008](ADR-008-owner-settings-beside-environment.md) already drew the line
this falls on: a setting belongs to the owner when they are the right person to
decide it and the answer can change without the deployment changing. It also
required that each one be argued individually rather than admitted by the
category, and it built a tier that stores exactly one kind of answer — a
boolean, because local network discovery is on or off.

A credential is neither a boolean nor something to hand back out. It also
cannot be hashed the way the PIN is ([ADR-010](ADR-010-taking-ownership.md)): the
server has to replay it to Twitch, so it must keep the value itself.

## Decision

Provider credentials are owner settings, and owner settings can hold text and
secrets as well as switches.

**IGDB qualifies, on ADR-008's own test.** The credentials come from the
owner's Twitch account, not from the machine or the container — two people
running identical deployments hold different ones, and the same person keeps
theirs across a reinstall. Nothing about them is a property of the deployment,
which is exactly the line ADR-008 drew.

A setting now declares what kind of answer it takes. A switch answers yes or
no. Text is shown and edited plainly. **A secret is written and never read
back**: the API reports whether one is stored and nothing more, so a client ID
appears in the Dash while its secret shows only as configured or not. Replacing
it is how it changes; there is no reading it.

The secret is stored as it was given. It is a bearer credential for another
service that has to arrive at Twitch intact, so hashing it is not available the
way it is for the PIN. It sits in the database beside the saves and the issued
credential hashes, which is where this deployment's other secrets already are.

The environment still wins, exactly as ADR-008 says. A deployment that sets
either variable pins it, the Dash shows it as coming from the deployment, and
nothing in the Dash can change it.

Credentials take effect when they are saved. The catalog holds a provider whose
credentials can be replaced underneath it, and reports itself unavailable while
it has none — which the resolution and search paths already skip, because a
provider that cannot answer today has always been a thing they tolerate. No
restart, and no difference between a server that started with credentials and
one that was given them later.

## Consequences

Easier:

- The owner signs up for IGDB when they get to it, pastes two fields, and
  artwork starts appearing — instead of learning where their deployment keeps
  its environment.
- The tier can hold the settings that come next. Every provider Omnisave adds
  will have this shape, and none of them will need a decision of its own now
  that the question is answered once.
- A fleet operator loses nothing: pinning the variables is unchanged, and now
  visibly so in the Dash.

More difficult:

- A secret lives in the database in a form the server can replay, so anyone
  holding a copy of that database holds the owner's IGDB credentials. That is a
  real widening of what a stolen backup costs, and the honest mitigation is
  that the credential is scoped to a third-party read API the owner can revoke
  from Twitch.
- Owner settings are no longer one shape, so the Dash has to render three and
  the API has to keep a secret write-only. Write-only is easy to break by
  accident — one convenient debugging endpoint would undo it — so it is
  something to test rather than to remember.
- Configuration that changes under a running server is harder to reason about
  than configuration read once at startup. The catalog now has a provider that
  may answer differently between two requests, which is the price of not
  restarting.
