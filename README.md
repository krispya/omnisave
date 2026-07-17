# omnisave

This monorepo workspace was generated with create-krispya.

## Structure

- `apps/` - Applications
- `packages/` - Shared packages and libraries
- `.config/` - Shared configuration packages

## Development Commands

- `pnpm install` to install all dependencies
- `pnpm run dev` to run all applications in development mode
- `pnpm run build` to build all packages and applications
- `pnpm run test` to run tests across the workspace
- `pnpm run lint` to lint all code
- `pnpm run format` to format all code

## Local save discovery

Build and run the client scanner:

```sh
make build-client
./bin/omnisave-client
```

The default view reports adapter totals followed by compact save statistics for
each discovered game. Use `./bin/omnisave-client --verbose` to include target
locations, save sets, and individual files.

## Adding Packages

To add a new package to this workspace, run create-krispya from this directory and it will detect the monorepo.
