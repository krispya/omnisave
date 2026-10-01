# Recovering saves from a store directory

An Omnisave Portable Store is one directory holding everything needed to recover the game saves in it. You don't need a server, a database, or a network connection.

The easiest recovery is no recovery: point an Omnisave server at the directory and it rebuilds its own index from what it finds there, so every save appears again on its own. The server arrives unclaimed — credentials never travel in the store — and devices pair with it afresh. The steps below are for when there is no server to point, or no wish to run one: they take a terminal and ordinary text and gzip tools, and no version of Omnisave has to run.

## Layout

    VERSION           the format marker for the directory
    objects/          file content, gzip-compressed, named by SHA-256
    revisions/        one JSON manifest per saved snapshot
    omnisaves/        one JSON record per save
    games/            one JSON record per game
    deletions/        one JSON marker per committed deletion, by kind
    reclaiming/       objects being removed; check here if an object is missing from objects/

Every record is named `<id>.json`, so a record whose identifier you know can be found with `find`. The JSON files are plain text on purpose. Open them in any editor.

## Recovering one save by hand

1. Find the game. Search `games/` for its title:

       grep -rl "Chrono Trigger" games/

   The "id" in the file that matches is the game's identifier.

2. Find its saves. Search `omnisaves/` for that game identifier:

       grep -rl '"game_id": "<game id>"' omnisaves/

   Each match is one save. Its "display_name" is what it was called. A deletion leaves a marker rather than erasing what it deleted, so check `deletions/` before recovering:

       find deletions/omnisave -name '<omnisave id>.json'

   A match means that save was deliberately deleted. A store from an older server may instead carry "deleted_at" or "deleted_revisions" fields on the save's record itself; they mean the same thing.

3. Find the snapshot the save is at. The save's record names it in "current_revision_id". That is the save, whatever the timestamps say: restoring can make any older snapshot current, and a fork starts at a snapshot another save made. Open its manifest:

       find revisions -name '<current revision id>.json'

   Every snapshot is complete on its own. You don't need to assemble it from the ones before it. Any other snapshot can be recovered the same way, by its "id"; each names its predecessor in "parent". A manifest named by a marker under `deletions/revision/` was deliberately deleted and is a leftover, not a snapshot to recover.

4. Write the files out. The manifest's "files" array gives each file's "path" and the "sha256" of its content.

   A path starts with the location the file belongs to: `battery` for RetroArch, a short hex identifier for one of a Steam game's save folders. Drop that first part; the rest is relative to that location's folder. `battery/Chrono Trigger.srm` becomes `Chrono Trigger.srm`, which goes next to the ROM unless RetroArch is set to keep saves elsewhere. For a Steam game, `omnisave scan --verbose` lists each of the game's save folders and the files found in them; most games have one.

   If the save's record lists "path_migrations", snapshots made before a migration may still start with its "from" value, such as `remote`. Replace that with its "to" value first, then drop the location as above: with a "to" of `aaaa1111/76561198027955092`, `remote/profile1/run.save` becomes `76561198027955092/profile1/run.save` inside the `aaaa1111` location's folder.

   For each file, with `<rest>` the path after the location:

       mkdir -p "<save folder>/$(dirname "<rest>")"
       gunzip -c objects/<first 2 characters of sha256>/<sha256>.gz > "<save folder>/<rest>"

   The result is exactly the bytes the game wrote. Never write them into Steam's `userdata/<account>/<app>/remote/`: that is Steam Cloud's staging area, not a save the game reads, and content placed there can be replaced or ignored at the next launch ([FDR-003](fdr/FDR-003-automatic-save-binding.md)). Put the files where the game expects them and it will load the save.

## Checking a copy is intact

Every object's file name is the SHA-256 of its uncompressed content, so a copy can be checked without any reference to the original:

    gunzip -c objects/ab/abcd....gz | shasum -a 256

The output must equal the name of the file. A mismatch means that object was damaged in transit or on the medium. Other objects are unaffected, and the snapshots that don't reference the damaged one are still complete.

## What is not in a store

Server credentials, device pairings, the owner token, the owner PIN, owner settings, the list of Devices, and each game's Provenance are deliberately excluded, so a store is safe to copy and to hand to somebody else. Cover art may sit in `objects/` without any record naming it; the server fetches it again rather than recovering it.
