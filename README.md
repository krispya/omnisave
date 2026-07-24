# omnisave

This monorepo workspace was generated with create-krispya.

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

Choose which discovered games this machine should synchronize:

```sh
./bin/omnisave-client track
```

The tracking prompt is preselected from local state. Space toggles a game,
Enter saves the selection, and `/` filters the list.
