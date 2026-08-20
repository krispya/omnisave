# Ludusavi Manifest Patches

Each YAML file in this directory is a reviewed, temporary correction layered
onto the upstream Ludusavi manifest before OmniSave prunes and embeds it. Keep
one patch per Steam game and name it `<steam-id>-<game>.yaml` so the directory
is its own inventory.

A patch must identify the exact upstream title and Steam id, explain the data
problem, link to the upstream page that should eventually carry the fix, and
add at least one save path. Refreshing the embedded profiles fails if upstream
renames the title or changes its Steam id, making stale patches visible.

Remove a patch after its paths appear upstream, then run
`make refresh-save-profiles`. The generated manifest should not change when a
patch is removed after the equivalent upstream fix arrives.
