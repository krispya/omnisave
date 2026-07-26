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

Back up the complete Compose data volume while the service is stopped. The
SQLite database and `artifacts/` directory form one backup unit; neither alone
is a complete server backup.

The image accepts these deployment environment variables:

| Variable                      | Purpose                                                    | Image default            |
| ----------------------------- | ---------------------------------------------------------- | ------------------------ |
| `OMNISAVE_TOKEN`              | Owner token; at least 32 characters                        | Generated on first start |
| `OMNISAVE_TOKEN_FILE`         | File containing the token; use instead of `OMNISAVE_TOKEN` | —                        |
| `OMNISAVE_LISTEN_ADDR`        | HTTP listen address                                        | `:8080`                  |
| `OMNISAVE_NAME`               | Name announced on the local network                        | The host name            |
| `OMNISAVE_DISCOVERY`          | Pins local network announcing; owner setting when unset    | On, editable             |
| `OMNISAVE_DB_PATH`            | SQLite database path                                       | `/data/omnisave.db`      |
| `OMNISAVE_ARTIFACT_DIR`       | Content-addressed artifact directory                       | `/data/artifacts`        |
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
