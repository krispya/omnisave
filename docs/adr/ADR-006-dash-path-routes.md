# ADR-006: Serve the Dash from an Index Fallback, Addressed by Path

**Date:** 2026-07-25

## Context

How the Dash reaches a browser has never been written down, though two decisions
already depend on it: [ADR-004](ADR-004-oci-image-distribution.md) and
[ADR-005](ADR-005-synology-package-distribution.md) both ship one Go process
serving one Dash, and neither says what that serving is allowed to assume.

It came up because the Dash had no addressable state. Which game was open lived
in component state, so a game could not be linked, bookmarked, reopened in a
second tab, or reached with the back button, and a reload always landed on the
Library. Fixing that means choosing where a route lives, and that choice is
decided by how the Dash is served rather than by the Dash itself.

The server previously handed out the built Dash from a plain file handler that
rewrote nothing, so a request either named a file or got a 404. Under that
contract only the URL fragment could carry a route, because the fragment is the
one part of a URL the browser never sends. Fragments cost nothing to serve but
read as an artifact of the server rather than as addresses of the thing being
looked at, and they are invisible to anything that only observes requests.

## Decision

Serve the Dash from an index fallback, and address it with the URL path.

The server keeps serving files from one directory mounted at `/`, beside the API
under `/api/v1`, and adds one rule: a `GET` or `HEAD` for an extensionless path
that names nothing on disk is a Dash route and answers with `index.html`. Paths
carrying an extension are never routes, so a missing asset stays a 404 instead of
becoming an HTML document with the wrong media type. The Dash builds with
absolute asset URLs, since a route of any depth must resolve them the same way.

Routes are ordinary paths:

```
/                   the Library
/games/<game>       one game
/settings           the server
```

Only a whole view earns a route. Which save has its history open and which way a
game's saves are drawn are things a reader does inside a game, not places to
link to, so they stay component state; a focused save view, if one is ever
added, would be its own route. A route type that small is what keeps a
hand-written router preferable to a routing dependency: navigation the reader
asks for pushes history, corrections replace it, and links are real anchors
whose ordinary clicks are handled in place while modified clicks are left to the
browser.

The fallback lives in the server binary that every packaging target already
ships, so no target carries a routing rule of its own.

## Consequences

Easier:

- A link to a game is an ordinary URL: it survives a reload, a new tab, a
  shared message, and shows in any log that sees requests.
- The serving contract behind ADR-004 and ADR-005 is now explicit, and it is one
  rule in one binary rather than a per-target concern.
- Routing stays a small module the app owns, with no dependency to track.

More difficult:

- The server can no longer be a bare file handler: the fallback is behavior that
  has to keep working, so it is covered by a test that pins routes, assets,
  missing assets, and API paths.
- Absolute asset URLs mean the Dash can only be served from the root of its
  origin; mounting it under a subpath would need a rebuild.
- Anything addressable must be encoded and parsed by hand, so each new route is
  a deliberate change to the route type.
- State left out of the route is lost on reload and cannot be shared, which is
  the trade for keeping links to views only.
