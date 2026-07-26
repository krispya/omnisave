# Omnisave

Omnisave is a self-hosted, versioned game-save synchronization service. The
server owns the Library and save history; clients discover native saves and
keep them synchronized across devices.

## Self-host with Compose

The supported generic deployment is the multi-architecture OCI image and the
included `compose.yaml`.

```sh
cp .env.example .env
openssl rand -hex 32
# Replace OMNISAVE_TOKEN in .env with the generated value.
docker compose up -d
```

Open `http://your-server:8080`. The Compose project stores the database and
artifacts in its `data` volume, runs the server without root privileges, and
restarts it unless explicitly stopped.

Upgrade in place:

```sh
docker compose pull
docker compose up -d
```

Back up the complete Compose data volume while the service is stopped. The
SQLite database and `artifacts/` directory form one backup unit; neither alone
is a complete server backup.

The image accepts these deployment environment variables:

| Variable                      | Purpose                                                    | Image default       |
| ----------------------------- | ---------------------------------------------------------- | ------------------- |
| `OMNISAVE_TOKEN`              | API bearer token; must be at least 32 characters           | Required            |
| `OMNISAVE_TOKEN_FILE`         | File containing the token; use instead of `OMNISAVE_TOKEN` | —                   |
| `OMNISAVE_LISTEN_ADDR`        | HTTP listen address                                        | `:8080`             |
| `OMNISAVE_DB_PATH`            | SQLite database path                                       | `/data/omnisave.db` |
| `OMNISAVE_ARTIFACT_DIR`       | Content-addressed artifact directory                       | `/data/artifacts`   |
| `OMNISAVE_WEB_DIR`            | Built Dashboard directory                                  | `/app/web`          |
| `OMNISAVE_IGDB_CLIENT_ID`     | Optional IGDB client ID                                    | —                   |
| `OMNISAVE_IGDB_CLIENT_SECRET` | Optional IGDB client secret                                | —                   |

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
./bin/omnisave-client scan
```

The default view reports adapter totals followed by compact save statistics for
each discovered game. Use `./bin/omnisave-client scan --verbose` to include
target locations, save sets, and individual files.

Run Omnisave:

```sh
./bin/omnisave-client
```

The commandless run is the whole app. It asks for a server connection only if
this machine has none, asks which games to track only if none are tracked,
syncs everything once, and then keeps watching those saves until you quit it.

Change the selection later with an explicit run, which always asks and then
exits:

```sh
./bin/omnisave-client track
```

The tracking prompt is preselected from local state. Space toggles a game,
Enter saves the selection, and `/` filters the list.
