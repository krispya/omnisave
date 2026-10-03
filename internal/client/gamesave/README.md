# Game Save Adapters

Save-location providers find whole native saves. A Game Save Adapter optionally
interprets the save slots inside those locations: the profiles, characters, or
slots a game keeps independently. Each adapter is a sandboxed Starlark extension. Revision labelers remain a separate server
capability; save extensions run on the client.

To add support, add `builtin/<game>/rules.star` and `tests.json`. Both are discovered
by their embedded globs: no scanner branch or per-game Go registration is needed.
Run `go test ./internal/client/gamesave ./internal/client/savesync`.

Each Starlark module declares:

- `ADAPTER_ID`: stable portable compatibility identity, such as `sts2.vanilla`.
- `GAME_KEYS`: game identifiers in `namespace:value` form, as labelers use.
- `discover(snapshot)`: returns the save slots on this target. Return an empty
  list when the game has no slots here, including on a launcher the extension
  does not support; the client then tracks the whole save. Fail only when
  discovery could not finish: the client holds the game and reports it rather
  than falling back to the whole save, which would otherwise become permanent.

The snapshot supplies `target` (launcher name), `roots` (resolved directory roots,
with opaque integer `id`, basename `name`, and parent basename `parent`), and
`directories(root_id)` (sorted immediate child directories, excluding symlinks).
Scripts have bounded execution, no imports, no file-content access, and no write
or network capability. Print output and error payloads are suppressed because
local directory names can contain private account identifiers.

Each returned dictionary describes one complete save slot directory:

| Field | Contract |
| --- | --- |
| `root` | Opaque root ID from the snapshot. |
| `parent` | Relative anchor directory within that root; `""` uses the root itself. |
| `directory` | Nonempty relative directory beneath the anchor; may be an empty destination. |
| `key` | Stable slot key within the anchor. With the anchor, it forms the slot's opaque local identity, independent of absolute paths. |
| `label` | Local UI name in the game's own terms, such as `Profile 1`, without raw account identifiers. |
| `location` | Stable canonical prefix for portable revision paths, independent of account and slot. |
| `cloud` | Optional store membership: private `account_id`, relative `files`, and `directories` with relative `path` and exact `extension`. |

Paths use `/`. The host rejects absolute paths, traversal, symlink components,
duplicate identities, and overlapping slots. It captures every regular file in
the slot; scripts cannot selectively omit dependencies or recovery copies.
Cloud membership authorizes only eligible files within the slot. The launcher
still verifies the connected account and preserved baseline before mutation.
Binding and restore policy remain in their respective domains.

Keep adapter IDs and location prefixes stable for compatible snapshots. A change
that makes older content incompatible needs a different adapter ID. Slot keys
identify native slots; they are never server history identity. Shared settings or
selectors cannot be independently owned by overlapping slots. Packed-file slots
and shared-metadata transformations are outside this extension contract.

`tests.json` holds synthetic directory fixtures and expected slot labels, captured
paths, and cloud eligibility. Every shipped extension must supply nonempty test
cases. See the STS2 bundle for the schema. The generic host tests enforce sandbox
and ownership invariants; the sync tests exercise real server-backed selection,
matching, rewind, and placement behavior through the same extension contract.
