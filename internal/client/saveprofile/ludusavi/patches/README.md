# Ludusavi Manifest Patches

Each YAML file in this directory is a reviewed, temporary patch layered onto
the upstream Ludusavi manifest before OmniSave prunes and embeds it. Patches
are additive for now: they can add save paths, but cannot remove or replace
upstream rules. Keep one patch per Steam game and name it
`<steam-id>-<game>.yaml` so the directory is its own inventory.

A patch must identify the exact upstream title and Steam id, explain the data
problem, link to the upstream page that should eventually carry the fix, and
add at least one save path. Refreshing the embedded profiles fails if upstream
renames the title, changes its Steam id, or already provides every effective
rule in a patch, making stale patches visible.

After adding or changing a patch, run `make refresh-save-profiles` and add a
story test that resolves the affected save from the embedded manifest. Remove
a patch after its paths appear upstream, then refresh the embedded profiles.
The test suite also fails when a checked-in patch is missing from the generated
manifest.
