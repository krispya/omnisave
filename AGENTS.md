## Comments

Comments should be concise and explain the concept and use. Tests can explain the overall story.

## Test Strategy

Tests should document the intended feature boundary and show how the public API is used.

- Prefer a small number of readable, story-shaped tests over exhaustive unit coverage.
- Story tests should be short. If they are long, they should be split up.
- Give each test its own setup so it can run independently.
- A reader should be able to understand the supported workflow from the test body alone.
- Test observable behavior and contracts, not implementation details.
- Add focused edge-case tests only when they protect an important invariant or regression.
