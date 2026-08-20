# Recovering saves from a store directory

An Omnisave save store is one directory holding everything needed to recover the game saves in it — no server, no database, and no network are required.

The easiest recovery is no recovery: point an Omnisave server at the directory and it rebuilds its own index from what it finds there, so every save appears again on its own. The server arrives unclaimed — credentials never travel in the store — and devices pair with it afresh. The steps below are for when there is no server to point, or no wish to run one: they take a terminal and ordinary text and gzip tools, and no version of Omnisave has to run.

## Layout

    VERSION           the format marker for the directory
    objects/          save file content, gzip-compressed, named by SHA-256
    revisions/        one JSON manifest per saved snapshot
    omnisaves/        one JSON record per save lineage
    games/            one JSON record per game
    deletions/        one JSON marker per committed deletion, by kind
    reclaiming/       objects staged for removal; treat as already deleted

The JSON files are plain text on purpose. Open them in any editor.

## Recovering one save by hand

1. Find the game. Search `games/` for its title:

       grep -rl "Chrono Trigger" games/

   The "id" in the file that matches is the game's identifier.

2. Find its saves. Search `omnisaves/` for that game identifier:

       grep -rl '"game_id": "<game id>"' omnisaves/

   Each match is one save lineage. Its "display_name" is what it was called. A deletion leaves a marker rather than erasing what it deleted, so check `deletions/` before recovering:

       grep -rl '"target_id": "<omnisave id>"' deletions/omnisave/

   A match means that save was deliberately deleted. Markers under `deletions/revision/` name single snapshots deleted on their own — a manifest for one of those is a leftover, not a save to recover. A store from an older server may instead carry "deleted_at" or "deleted_revisions" fields on the lineage record itself; they mean the same thing.

3. Find the newest snapshot of that lineage. Search `revisions/` for the lineage's identifier:

       grep -rl '"id": "<omnisave id>"' revisions/

   Each match is one snapshot with a "created_at" timestamp. The newest one is almost always the latest save. Every snapshot is complete on its own — you do not need to assemble it from the ones before it.

   The exact rule, if the timestamps disagree or look wrong: each snapshot names its predecessor in "parent", and the latest is the one no other snapshot names. That is what the server uses, and it holds even when clocks do not.

4. Write the files out. The manifest's "files" array gives each file's "path" inside the save and the "sha256" of its content. For each entry:

       mkdir -p "$(dirname <path>)"
       gunzip -c objects/<first 2 characters of sha256>/<sha256>.gz > <path>

   The result is exactly the bytes the game wrote. The paths are relative to wherever the game keeps its saves — next to the ROM for most emulators, under `userdata/<account>/<app>/remote/` for Steam Cloud games — so put them where the game expects them and it will load the save.

## Checking a copy is intact

Every object's file name is the SHA-256 of its uncompressed content, so a copy can be checked without any reference to the original:

    gunzip -c objects/ab/abcd....gz | shasum -a 256

The output must equal the name of the file. A mismatch means that object was damaged in transit or on the medium; other objects are unaffected, and the snapshots that do not reference the damaged one are still complete.

## What is not in a store

Server credentials, device pairings, the owner token, and the owner PIN are deliberately excluded. A store holds save data only, so that it is safe to copy and to hand to somebody else.
