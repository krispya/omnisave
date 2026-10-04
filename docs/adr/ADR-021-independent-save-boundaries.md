# ADR-021: Track Save Slots Through Game Save Adapters, and Whole Saves Otherwise

**Date:** 2026-10-01
**Status:** Accepted for separate-file boundaries. STS2 restores were validated in the game on 2026-10-03; resuming a restored run and rewinding with Steam Cloud remain experimental.

## Context

Save-location rules currently answer both where a game's files live and which
files belong to one Local Save. The profile resolver combines every matching
file into one save, including files from different store accounts and in-game
profiles. A directory rule recursively includes files without distinguishing
gameplay state, device settings, or recovery copies.

Slay the Spire 2 exposes the problem. Its account directory contains separate
profile directories alongside shared files. In the installed version inspected
during this investigation, `profile.save` contains `last_profile_id` and a schema
version; `settings.save` contains display, audio, language, and input settings.
Profile directories hold progress, active runs, preferences, and run history.
The local Steam cache lists the account selector but excludes device settings.
That is evidence about this version, not a universal schema guarantee.

A visible save slot need not correspond to a separate file. Dark Souls III
stores character entries and shared metadata inside one encrypted `.sl2`
container. Independent character restoration therefore needs knowledge of its
format, not another filesystem glob. The distinction is demonstrated by the
[DS3 save-manager implementation](https://github.com/Hapfel1/er-save-manager/blob/main/src/er_save_manager/games/ds3_steamid.py).

Omnisave already supports multiple independently named histories for one Game
and a binding per Local Save. What is missing is an explicit definition of a
save boundary, per-slot discovery and destination selection, and placement
that preserves content outside the selected boundary.

## Decision

**Without game-specific knowledge, the default is a whole-save snapshot.** Save-location rules find the game's
native save files. A directory rule includes every readable regular file
recursively, retaining the resolver's existing symlink exclusions. Device
settings, backups, replays, and other files inside that boundary travel with the
snapshot. This broad coverage is intentional: without game-specific knowledge,
Omnisave does not guess which nearby files are dispensable. Steam's cloud file
set does not restrict this default. Several location rules may together form
the whole save.

**Optional Game Save Adapters offer finer binding scopes.** These are
game-specific save interpreters, separate from launcher adapters such as Steam
and GameHub. An adapter identifies a game's **save slots**: the profiles,
characters, or slots the game keeps independently, labelled in the game's own
terms. It defines their complete capture and restore behavior. Folder-based
slots need explicit membership and ownership rules; packed slots additionally
need format-aware readers and writers. A slot is offered only when its contents
can be restored coherently while preserving unrelated progress. The portable
scope kind is the generic `slot`; the adapter ID carries the game-specific
meaning, so renaming how a game labels its slots never splits history.

**The first implementation supports separate files or directories.** Each
supported slot must own a separable, coherent file set; separate files alone
do not prove safe restoration. Packed character slots remain whole-save histories
until a format-aware adapter has been verified. The packed-slot requirements
below describe future support, not a requirement for the first implementation.

**Per-game rules are sandboxed extensions, with their own fixtures.** Like
revision labelers, save extensions register by game identifiers through embedded
Starlark modules. The client-side game-save domain owns their contract and host
validation; scanner and sync code contain no per-game branches. The initial
extension capability describes complete independent directories and explicit
cloud eligibility. Extensions receive read-only directory discovery within
resolved native roots, with bounded execution and no write or network access.
The host rejects escaping paths, symlink components, and overlapping slots before
returning usable saves. Script output and error payloads cannot expose native
account context. Each extension ships synthetic fixtures that the same host
test harness discovers and runs; shared sync tests cover binding and restoration.
Packed containers and necessary shared-metadata edits require a future verified
capability rather than arbitrary extension writes.

**Save slots are the default wherever an adapter offers them.** Without a Game
Save Adapter, the whole save goes through the normal binding process, as it does
when the adapter cannot divide the save on this target or finds no slot yet.
When it finds slots, every slot is tracked without asking, attended or not:
each occupied slot becomes a distinct Local Save and goes through normal
content matching, seeding, adoption, and synchronization with its own Omnisave,
and each empty slot is a destination. Asking would leave a newly tracked game
unprotected until someone answered, and slots are what the adapter exists to
provide. The default is recorded the first time a pass applies it, so losing
adapter support later reports the slots unavailable rather than silently
reverting to whole-save capture. An adapter distinguishes "no slots here",
which leaves the whole save, from a discovery that could not finish, which
holds the game and is reported: read as "no slots", a transient failure on a
first pass would pick the whole save permanently. The whole save is an explicit per-game choice
(`omnisave bind --choose-scope`), and an existing whole-save binding or restore
keeps the whole save until that choice is revisited. A slot label attached to
whole-folder snapshots does not provide independent slot history.

**Slot membership is the adapter's responsibility.** Progress, active runs,
and dependent history travel together where the game requires them. The adapter
explicitly decides whether slot preferences, backups, or replays belong to
that slot. Shared device settings and account selection can remain local when
independent progress loads without them. Where restoration requires changing
shared metadata, the adapter updates only necessary fields and preserves
unrelated state. Independently synchronized slots do not each own a full
copy of the same mutable shared file. Where that separation cannot be proven,
the adapter offers only the larger coherent boundary. Selecting a slot to
play is separate from restoring its contents.

**Active bindings do not overlap in logical ownership.** Whole-save binding and
bindings for its contained slots cannot simultaneously manage the same
native content on one Device. Distinct slots inside one physical container may
have independent bindings only through an adapter that preserves their logical
ownership during writes. Switching scope is explicit and preserves existing
history rather than silently broadening or narrowing it.

**Separate a playthrough from its destination slot.** A slot is a local place
where a playthrough can live, not the playthrough's server identity. Account,
installation, slot number, and vanilla or modded namespace identify local
placement context. Portable revision paths describe content within the save
boundary. On one game installation a history has exactly one owner: matching,
placement, and manual binding skip histories another discovered save already
owns, so equal bytes in a sibling slot start their own history, and copying a
playthrough into a second slot needs a fork. Moving a history into another slot
requires explicit compatible placement; moving between accounts requires
verified support for any embedded ownership data. Raw account identifiers are
not logged or used as server display names.

**Tracking selects Games; binding selects native save scopes.** Tracking a Game
establishes Library membership and the intent to protect its saves. Whole-save
binding automatically includes new files within its existing boundary. Slot
binding likewise includes slots the adapter finds later; each starts or adopts
its own history and cannot change another slot's. An empty
slot is a destination, not an empty history to seed, and remains available for
placement even when other slots already hold progress. Placement explicitly
selects the history and destination when more than one is compatible.

**Independent restoration preserves other slots.** Folder-based placement may
replace and remove only files owned by the selected save. Steam reconciliation
uses the same ownership boundary: another slot's entries are neither deletion
candidates nor unexplained extras that block this restore. Relevant extras
inside the selected boundary still require resolution. Native save membership
and eligibility for Steam Cloud remain separate decisions.

**Packed slots require a verified reader and writer.** A game-specific adapter
must enumerate slots, capture their logical state, validate compatible
destinations, and rebuild a container while preserving other slots and required
shared metadata. Changes to another slot do not advance this slot's history or
create a false divergence. Writes to one physical container are serialized and
revalidate the current container before atomic replacement. Checksums,
encryption, and game-version differences belong to that adapter. Read-only slot
inspection does not imply support for independent sync or restore.

**Existing aggregate histories keep their meaning.** Account-wide history cannot
silently become one slot's history. A split requires an explicit conversion
that preserves the source history, or fresh independent histories with the
aggregate retained for recovery. Any durable conversion follows
[ADR-019](ADR-019-versioned-replayable-data-migrations.md).

## Consequences

Easier:

- Playing or restoring one supported slot need not change another slot's
  baseline, history, or contents.
- Games without specialized support retain broad backup coverage and their
  existing whole-save workflow.
- A Game Save Adapter can keep device settings and incidental files outside
  individual playthrough histories without changing the generic fallback.
- Discovery can explain the included content, its ownership, and the supported
  restore scope instead of presenting every rule match as equally meaningful.
- Directory profiles and packed character slots fit the same user-facing model
  while retaining different placement implementations.

More difficult:

- Independent support needs verified game knowledge beyond save-folder paths.
- Each extension needs capture, empty-destination, and cloud-membership fixtures;
  schema and ownership changes cannot bypass the host's validation contract.
- Packed formats require fixtures and round-trip validation proving unrelated
  slots survive restoration; generic file replacement cannot supply that proof.
- Binding, first placement, and reporting must identify save scopes and
  destination slots, not only Games.
- Scope selection precedes content matching, including when an existing
  whole-save binding is switched to independent slots.
- Game- or account-wide achievements cannot be assigned to every independent
  slot without evidence identifying which slot earned them.
- Whole-save snapshots deliberately include settings and incidental files;
  their changes can create revisions or divergence and restoration replaces
  them with the selected snapshot's versions.
- Files outside every slot, such as STS2's device settings and profile
  selector, are not backed up by default. Full coverage is the explicit
  whole-save choice.
- Every empty slot is a destination, so an interactive run offers a compatible
  history no save of the installation owns until it is placed, each time it
  is declined.
- A Device that keeps an existing whole-save binding and a Device using slots
  keep separate histories for the same game until one switches scope; nothing
  prompts that switch.

## Validation and Limits

### Slay the Spire 2 investigation, 2026-10-01

**Result: implemented experimental support for independent vanilla profiles,
validated in the game on 2026-10-03 (below).** The inspected installed macOS arm64 build
reports `v0.107.1`. Live saves, the installed assembly, and Steam's local cached
registry were inspected read-only. No game was launched and no live save or cloud
entry was changed. Public decompiled source corroborates the findings, but differs
from this installed build and is not a versioned specification.

Each STS2 profile is a save slot. The implemented native boundary is the entire
selected `profileN/` directory under one account, for fixed slots 1–3. Revision paths are relative to that directory;
the account and slot number belong to destination context. Include progress,
preferences, single-player and multiplayer active runs when present, their
backups, run history, and other regular files beneath that profile. Exclude the
account-level `profile.save` selector, `settings.save`, and sibling profiles.
Capture membership is broader than the game's cloud upload list.

Evidence from the installed build:

- `UserDataPathProvider.GetProfileDir` selects `profileN` or
  `modded/profileN`. Progress and preferences managers derive their paths from
  the current profile. The same directory scoping is visible in the
  [decompiled path provider](https://github.com/Hexpion/Slay-the-spire-2/blob/main/Slay%20the%20Spire%202/src/Core/Saves/UserDataPathProvider.cs).
- `profile.save` contains a schema version and `last_profile_id`, not a profile
  inventory. `ProfileSave` defaults to slot 1; `ProfileSaveManager.LoadProfile`
  constructs a new selector when loading fails. The profile menu checks each
  slot's own `saves/progress.save` to distinguish occupied from empty. Restoring
  slot 2 therefore need not replace shared metadata to register it. Selection
  remains a separate game action. See the
  [decompiled selector manager](https://github.com/Hexpion/Slay-the-spire-2/blob/main/Slay%20the%20Spire%202/src/Core/Saves/Managers/ProfileSaveManager.cs).
- Active runs, recovery copies, and history are coupled: the staleness check
  finds the run's start time in that same profile's history and deletes the
  active run and its backup when a terminal history entry exists. Rewinding
  only `current_run.save` is insufficient.
- Cloud synchronization enumerates the selector and all three profiles'
  progress, preferences, active runs, and history. The installed build also
  explicitly deletes stale `settings.save` entries from cloud storage. Its
  cloud-to-local implementation deletes a local file and its backup when the
  corresponding remote entry is absent. File placement alone cannot establish
  a completed restore. The public
  [decompiled cloud store](https://github.com/Hexpion/Slay-the-spire-2/blob/main/Slay%20the%20Spire%202/src/Core/Saves/CloudSaveStore.cs)
  shows the same missing-remote behavior.

Scratch verification used synthetic content and the existing public
`binding.Materialize`, `binding.ApplyCurrent`, `binding.RemovedFiles`, and
`steamworks.PlanReconciliation` APIs:

- Explicit placement into empty profile 2 preserved occupied profile 1,
  `profile.save`, and `settings.save` byte for byte.
- A profile-2 rewind restored its active run and backup, removed terminal
  history absent from the selected revision, and preserved unrelated files.
Before implementation, generic cloud planning classified sibling entries as
blocking extras and could not anchor an empty profile. The implemented scoped
planner instead consumes game-provided ownership and eligibility. Automated
checks verify registration without profile precedent or even an existing
registry, preserve sibling and selector entries, delete only preserved owned
files, and retain unknown extras inside the selected profile. The helper checks
the connected Steam account before any scoped cloud mutation.

The real server-stack tests cover the slot default before matching, its
record across client restart, a new slot starting its own history,
whole/slot history separation, existing whole-save bindings keeping their
meaning, the explicit whole-save choice, empty destinations beside occupied
slots, explicit destination selection, one owner per history (a copied slot
starts its own history, and playing one slot never overwrites another),
coherent rewinds, refusal to broaden when the adapter disappears, and
whole-save binding when no slot is offered. Server tests
cover scope inheritance by forks and recovery from the portable store. Dash and
TUI tests cover persistent scope labels and distinct local slot rows.

A read-only scan through the shipped extension found all three vanilla profile
destinations on the inspected installation: profile 1 occupied, profiles 2 and 3
empty. This checks native discovery, not game acceptance of a restored snapshot.

Scope is an immutable save-history contract owned by `omnisave`; it contains
no account or slot identity. Native scope selection and binding labels are
owned by client tracking. Game Save Adapters interpret native membership;
launcher adapters perform store operations using the provided cloud contract.
Dash identifies a history as a save slot; TUI also names its local slot in the
game's terms, such as `Profile 1`. The server does not receive or claim a
Device-local slot number.

Initial support is vanilla STS2 through Steam. Account directories must already
be known from the native save tree; without one, the client cannot safely offer
independent destinations. Modded namespaces and cross-account multiplayer
ownership are not supported. Profile paths support explicit slot placement, and
the game loads a history placed into another slot number (validated below). Changing
scope retires incompatible local mappings and leaves their server histories
available; it does not convert aggregate history into slot history.

### Live validation, 2026-10-03

The current build placed a Profile 1 history into empty Profile 2 on a real
macOS installation with Steam Cloud enabled, then launched the game through
Steam with Profile 2 selected. Everything was rolled back afterwards.

- Omnisave verified the connected Steam account through the game's Steamworks
  library and registered 227 eligible files under `profile2/`; no other cloud
  entry changed.
- The game's startup sync deleted nothing. It copied each registered file down
  once, because its cloud timestamp was newer, with identical bytes.
- The game opened `profile2` and parsed the restored progress without
  replacing it. Its only warnings were the four Profile 1 produces on every
  launch. The main menu loaded the restored run's character.
- No profile, progress, run, or history file names a profile number or Steam
  account, so a history placed into another slot number loads as that slot.

### Remaining game-level validation

- Resume the restored active run through Continue.
- Rewind an occupied slot with Steam Cloud enabled, including the deletion of
  newer run history from the cloud, and restart.
- Cross-account co-op and mods need their own verification.
- Future packed-slot support requires a verified container reader and writer;
  Dark Souls currently retains whole-save histories.
