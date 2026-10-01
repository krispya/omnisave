# ADR-006: Serve the Dash from an Index Fallback, Addressed by Path

**Date:** 2026-07-25

## Context

A Dash view, such as one game or the server settings, must survive a reload, open in a new tab, be shared as a link, and respond to the back button. Path routes need the server to answer a path it has no file for with the app. Fragment routes need nothing from the server, but they are not real addresses and never appear in a request.

## Decision

Serve the Dash from an index fallback, and address it with the URL path.

The server serves the built Dash from one directory mounted at `/`, beside the API under `/api/v1`, with one rule: a `GET` or `HEAD` for an extensionless path that names nothing on disk is a Dash route and answers with `index.html`. Paths carrying an extension are never routes, so a missing asset stays a 404 instead of becoming an HTML document with the wrong media type. The Dash builds with absolute asset URLs, since a route of any depth must resolve them the same way.

Routes are ordinary paths:

```
/                   the Library
/games/<game>       one game
/settings           the server
```

Only a whole view earns a route; state within a view stays in components. A route type that small is why the Dash uses a hand-written router rather than a routing dependency.

The fallback lives in the server binary that every packaging target ships ([ADR-004](ADR-004-oci-image-distribution.md), [ADR-005](ADR-005-synology-package-distribution.md)), so no target carries a routing rule of its own.

## Consequences

Easier:

- A link to a game is an ordinary URL that survives a reload, a new tab, and a shared message, and shows in any log that sees requests.
- Serving is one rule in one binary rather than a per-target concern.

More difficult:

- Absolute asset URLs mean the Dash can only be served from the root of its origin; mounting it under a subpath would need a rebuild.
- Routes are encoded and parsed by hand, so each new route is a deliberate change to the route type.
- State left out of the route is lost on reload and cannot be shared, which is the trade for linking only to views.
