# Instructions for Agents

Read this file first. It contains repo-wide rules that should not be hidden in path-specific guidance.

## Project Status

This repo is pre-release with not users. We can make breaking changes and should when refactoring.

## Prime Directives

- Prefer simple, clear changes over clever abstractions.
- Add concise code documentation for public APIs and for otherwise important fields, functions, types, invariants, and lifecycle behavior that future maintainers should not have to infer from call sites. Keep tests and documentation up to date when changing behavior.
- Run verification that would actually catch regressions in the area touched.
- Never log PII: no raw login names, display names, email addresses, submitted auth identifiers, OAuth/OIDC provider subjects, tokens, passwords, auth codes, åreset links, raw IPs, or full query strings.
- Treat optional operational telemetry as best-effort: its failure must not make broader diagnostics unavailable. Preserve an explicit unavailable state across API and UI boundaries instead of replacing unknown values with healthy-looking zeroes, empty strings, or timestamps.
- Comments should be concise and explain the concept and use. Tests can explain the overall story.

## Test Strategy

Tests should document the intended feature boundary and show how the public API is used.

- Prefer a small number of readable, story-shaped tests over exhaustive unit coverage.
- Story tests should be short. If they are long, they should be split up.
- Give each test its own setup so it can run independently.
- A reader should be able to understand the supported workflow from the test body alone.
- Test observable behavior and contracts, not implementation details.
- Add focused edge-case tests only when they protect an important invariant or regression.

## Documentation Updates

- Use FDRs for feature behavior/rationale and ADRs for cross-cutting decisions.
- Update `docs/GLOSSARY.md` when introducing, renaming, or clarifying canonical vocabulary.

## TypeScript

### After Editing

✅ After editing TS files, format and lint only the files changed for the current task.

```sh
# Example
# Run format and lint for only files modified
pnpm exec prettier --config .config/prettier/base.json --ignore-path .config/prettier/prettierignore --write src/App.tsx src/core/systems/move-entity.ts
pnpm exec oxlint src/App.tsx src/core/systems/move-entity.ts
```

❌ Avoid unless explicitly approved:

```sh
pnpm format
pnpm lint
```
