# Omnisave

Omnisave is a self-hosted, versioned game-save synchronization service. The
server owns the Library and save history; clients discover native saves and
keep them synchronized across devices.

## Self-host with Compose

The supported generic deployment is the multi-architecture OCI image and the
included `compose.yaml`.

```sh
cp .env.example .env
docker compose up -d
```

Open `http://your-server:8080` from the same network, claim the server, and
choose a four-digit PIN. That browser becomes the owner and gets a credential of
its own; nobody else can claim it afterward. Every other browser opens the same
address and enters the PIN.

The first start also generates an owner token and prints it once. It is the way
in from outside the network and the way back if the PIN is forgotten — set
`OMNISAVE_TOKEN` in `.env` before starting to supply your own instead, or mount
one with `OMNISAVE_TOKEN_FILE`. The Compose project stores the database and
artifacts in its `data` volume, runs the server without root privileges, and
restarts it unless explicitly stopped.

Connect a device with `omnisave connect`, then approve the code it shows
under **Server** in the Dash. On a local network the client finds the server by
itself; a bridged container cannot announce over mDNS, so this deployment needs
`omnisave connect --server http://your-server:8080`.

Upgrade in place:

```sh
docker compose pull
docker compose up -d
```

## The save store

Everything needed to recover your saves lives in one directory — `/data/store`
in the Compose deployment, `./store` in development. Copy it anywhere and every
save in it can be recovered from that copy alone: no server, no database, and
no network. It holds the save content, a manifest per snapshot naming the files
in it, and a record per save and per game — all plain JSON and gzip, so
[docs/RECOVERY.md](docs/RECOVERY.md) can walk anyone through pulling a save
out by hand.

The SQLite database beside it is an index over the store, and it lives outside
the store on purpose: it carries credentials and the owner PIN, and those must
not travel with a directory meant to be copied and shared.

The store is the backup. A server that loses its database rebuilds its index
from the store on the next start: every game, every save, every snapshot in
its history returns, under the identifiers connected devices already know. What
does not return is deployment state — the rebuilt server is unclaimed, and
browsers and devices claim and pair with it again. Without any server at all,
[docs/RECOVERY.md](docs/RECOVERY.md) explains how to extract a save with
`gunzip` alone.

Take the backup while the service is stopped, or accept that a copy taken
during a commit may miss the snapshot in flight — everything already
committed is intact either way, because nothing in the store is ever rewritten.
Every object is named by the SHA-256 of its content, so a copy can be verified
without reference to the original:

```sh
gunzip -c store/objects/ab/abcd….gz | shasum -a 256
```

The image accepts these deployment environment variables:

| Variable                      | Purpose                                                    | Image default            |
| ----------------------------- | ---------------------------------------------------------- | ------------------------ |
| `OMNISAVE_TOKEN`              | Owner token; at least 32 characters                        | Generated on first start |
| `OMNISAVE_TOKEN_FILE`         | File containing the token; use instead of `OMNISAVE_TOKEN` | —                        |
| `OMNISAVE_LISTEN_ADDR`        | HTTP listen address                                        | `:8080`                  |
| `OMNISAVE_NAME`               | Name announced on the local network                        | The host name            |
| `OMNISAVE_DISCOVERY`          | Pins local network announcing; owner setting when unset    | On, editable             |
| `OMNISAVE_STORE_DIR`          | Save store; the directory to back up                       | `/data/store`            |
| `OMNISAVE_DB_PATH`            | SQLite index over the store                                | `/data/omnisave.db`      |
| `OMNISAVE_WEB_DIR`            | Built Dashboard directory                                  | `/app/web`               |
| `OMNISAVE_IGDB_CLIENT_ID`     | Optional IGDB client ID                                    | —                        |
| `OMNISAVE_IGDB_CLIENT_SECRET` | Optional IGDB client secret                                | —                        |

`.env.example` documents the complete environment contract, including the
catalog provider settings `compose.yaml` passes through.

Publish a release image with Docker Buildx:

```sh
make push-oci VERSION=0.1.0
```

This publishes `linux/amd64` and `linux/arm64` manifests by default. Override
`OCI_IMAGE` to publish under another registry name.

## Structure

- `apps/` - Applications
- `packages/` - Shared packages and libraries
- `.config/` - Shared configuration packages

## Development Commands

- `pnpm install` to install all dependencies
- `pnpm run dev` to create a local `.env` when needed and run all applications
  in development mode
- `pnpm run build` to build all packages and applications
- `pnpm run test` to run tests across the workspace
- `pnpm run lint` to lint all code
- `pnpm run format` to format all code

The server is configured only through `OMNISAVE_*` environment variables.
[`.env.example`](.env.example) documents every setting; `.env` is an ignored
local-development convenience loaded by the development scripts.

## Local save discovery

Build and run the client scanner:

```sh
make build-client
./bin/omnisave scan
```

The default view reports adapter totals followed by compact save statistics for
each discovered game. Use `./bin/omnisave scan --verbose` to include
target locations, save sets, and individual files.

Run Omnisave:

```sh
./bin/omnisave
```

The commandless run is the whole app. It asks for a server connection only if
this machine has none, asks which games to track only if none are tracked,
syncs everything once, and then keeps watching those saves until you quit it.

Change the selection later with an explicit run, which always asks and then
exits:

```sh
./bin/omnisave track
```

The tracking prompt is preselected from local state. Space toggles a game,
Enter saves the selection, and `/` filters the list.
