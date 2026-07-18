package sqlite

import (
	"database/sql"
	"fmt"
)

var migrations = []string{
	`CREATE TABLE omnisaves (
		id           TEXT PRIMARY KEY,
		game_id      TEXT NOT NULL,
		display_name TEXT NOT NULL,
		created_at   TEXT NOT NULL,
		metadata     TEXT NOT NULL
	);

	CREATE TABLE revisions (
		id              TEXT PRIMARY KEY,
		omnisave_id     TEXT NOT NULL REFERENCES omnisaves(id) ON DELETE CASCADE,
		created_at      TEXT NOT NULL,
		artifact_format TEXT NOT NULL,
		artifact_sha256 TEXT NOT NULL,
		artifact_size   INTEGER NOT NULL,
		metadata        TEXT NOT NULL
	);

	CREATE INDEX revisions_by_omnisave ON revisions(omnisave_id, created_at, id);

	CREATE TABLE revision_parents (
		revision_id TEXT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
		parent_id   TEXT NOT NULL REFERENCES revisions(id) ON DELETE RESTRICT,
		position    INTEGER NOT NULL,
		PRIMARY KEY (revision_id, parent_id)
	);

	CREATE TABLE games (
		id           TEXT PRIMARY KEY,
		title        TEXT NOT NULL,
		sort_title   TEXT NOT NULL,
		platform     TEXT NOT NULL,
		publisher    TEXT NOT NULL,
		description  TEXT NOT NULL,
		provider     TEXT NOT NULL,
		provider_id  TEXT NOT NULL,
		metadata     TEXT NOT NULL,
		refreshed_at TEXT NOT NULL
	);

	CREATE INDEX games_by_provider ON games(provider, provider_id);

	CREATE TABLE game_roms (
		id         TEXT PRIMARY KEY,
		game_id    TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		system     TEXT NOT NULL,
		name       TEXT NOT NULL,
		region     TEXT NOT NULL,
		languages  TEXT NOT NULL,
		size       INTEGER NOT NULL,
		crc32      TEXT NOT NULL,
		md5        TEXT NOT NULL,
		sha1       TEXT NOT NULL,
		sha256     TEXT NOT NULL,
		source     TEXT NOT NULL,
		source_id  TEXT NOT NULL,
		attributes TEXT NOT NULL
	);

	CREATE INDEX game_roms_by_crc32 ON game_roms(crc32) WHERE crc32 <> '';
	CREATE INDEX game_roms_by_md5 ON game_roms(md5) WHERE md5 <> '';
	CREATE INDEX game_roms_by_sha1 ON game_roms(sha1) WHERE sha1 <> '';
	CREATE INDEX game_roms_by_sha256 ON game_roms(sha256) WHERE sha256 <> '';

	CREATE TABLE game_media (
		id           TEXT PRIMARY KEY,
		game_id      TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		kind         TEXT NOT NULL,
		position     INTEGER NOT NULL,
		format       TEXT NOT NULL,
		sha256       TEXT NOT NULL,
		size         INTEGER NOT NULL,
		provider     TEXT NOT NULL,
		provider_id  TEXT NOT NULL,
		attribution  TEXT NOT NULL,
		UNIQUE(game_id, kind, position)
	);

	CREATE INDEX game_media_by_artifact ON game_media(sha256);`,
	`DROP TABLE revision_parents;
	DROP TABLE revisions;
	DROP TABLE omnisaves;

	CREATE TABLE omnisaves (
		id           TEXT PRIMARY KEY,
		game_id      TEXT NOT NULL,
		display_name TEXT NOT NULL,
		created_at   TEXT NOT NULL,
		metadata     TEXT NOT NULL
	);

	CREATE TABLE revisions (
		id          TEXT PRIMARY KEY,
		omnisave_id TEXT NOT NULL REFERENCES omnisaves(id) ON DELETE CASCADE,
		created_at  TEXT NOT NULL,
		metadata    TEXT NOT NULL
	);

	CREATE INDEX revisions_by_omnisave ON revisions(omnisave_id, created_at, id);

	CREATE TABLE revision_parents (
		revision_id TEXT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
		parent_id   TEXT NOT NULL REFERENCES revisions(id) ON DELETE RESTRICT,
		position    INTEGER NOT NULL,
		PRIMARY KEY (revision_id, parent_id),
		UNIQUE (revision_id, position)
	);

	CREATE TABLE revision_files (
		revision_id     TEXT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
		path            TEXT NOT NULL,
		artifact_format TEXT NOT NULL,
		artifact_sha256 TEXT NOT NULL,
		artifact_size   INTEGER NOT NULL,
		PRIMARY KEY (revision_id, path)
	);

	CREATE INDEX revision_files_by_artifact ON revision_files(artifact_sha256);

	CREATE TABLE refs (
		omnisave_id TEXT NOT NULL REFERENCES omnisaves(id) ON DELETE CASCADE,
		name        TEXT NOT NULL,
		revision_id TEXT NOT NULL REFERENCES revisions(id) ON DELETE RESTRICT,
		updated_at  TEXT NOT NULL,
		PRIMARY KEY (omnisave_id, name)
	);`,
	`DROP TABLE refs;
	DROP TABLE revision_parents;
	DROP TABLE revision_files;
	DROP TABLE revisions;
	DROP TABLE omnisaves;

	CREATE TABLE omnisaves (
		id                          TEXT PRIMARY KEY,
		game_id                     TEXT NOT NULL,
		display_name                TEXT NOT NULL,
		head_revision_id            TEXT,
		forked_from_omnisave_id     TEXT,
		forked_from_revision_id     TEXT,
		created_at                  TEXT NOT NULL,
		metadata                    TEXT NOT NULL,
		CHECK ((forked_from_omnisave_id IS NULL) = (forked_from_revision_id IS NULL))
	);

	CREATE TABLE revisions (
		id          TEXT PRIMARY KEY,
		omnisave_id TEXT NOT NULL REFERENCES omnisaves(id) ON DELETE CASCADE,
		parent_id   TEXT REFERENCES revisions(id) ON DELETE SET NULL,
		created_at  TEXT NOT NULL,
		metadata    TEXT NOT NULL
	);

	CREATE INDEX revisions_by_omnisave ON revisions(omnisave_id, created_at, id);

	CREATE TABLE revision_files (
		revision_id     TEXT NOT NULL REFERENCES revisions(id) ON DELETE CASCADE,
		path            TEXT NOT NULL,
		artifact_format TEXT NOT NULL,
		artifact_sha256 TEXT NOT NULL,
		artifact_size   INTEGER NOT NULL,
		PRIMARY KEY (revision_id, path)
	);

	CREATE INDEX revision_files_by_artifact ON revision_files(artifact_sha256);`,
	`CREATE TABLE game_identifiers (
		game_id   TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		namespace TEXT NOT NULL,
		value     TEXT NOT NULL,
		PRIMARY KEY (namespace, value)
	);

	CREATE INDEX game_identifiers_by_game ON game_identifiers(game_id);

	INSERT OR IGNORE INTO game_identifiers(game_id, namespace, value)
	SELECT id,
		CASE WHEN instr(provider, '.') > 0 THEN provider ELSE provider || '.game' END,
		provider_id
	FROM games
	WHERE provider <> '' AND provider_id <> '';

	CREATE TABLE game_fingerprints (
		game_id   TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		platform  TEXT NOT NULL,
		algorithm TEXT NOT NULL,
		value     TEXT NOT NULL,
		PRIMARY KEY (platform, algorithm, value)
	);

	CREATE INDEX game_fingerprints_by_game ON game_fingerprints(game_id);

	INSERT OR IGNORE INTO game_fingerprints(game_id, platform, algorithm, value)
	SELECT game_id, lower(system), 'crc32', lower(crc32) FROM game_roms WHERE system <> '' AND crc32 <> '';
	INSERT OR IGNORE INTO game_fingerprints(game_id, platform, algorithm, value)
	SELECT game_id, lower(system), 'md5', lower(md5) FROM game_roms WHERE system <> '' AND md5 <> '';
	INSERT OR IGNORE INTO game_fingerprints(game_id, platform, algorithm, value)
	SELECT game_id, lower(system), 'sha1', lower(sha1) FROM game_roms WHERE system <> '' AND sha1 <> '';
	INSERT OR IGNORE INTO game_fingerprints(game_id, platform, algorithm, value)
	SELECT game_id, lower(system), 'sha256', lower(sha256) FROM game_roms WHERE system <> '' AND sha256 <> '';`,

	`ALTER TABLE games ADD COLUMN platform_company TEXT NOT NULL DEFAULT '';`,
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than supported version %d", current, len(migrations))
	}

	for index := current; index < len(migrations); index++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[index]); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", index+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, index+1); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", index+1, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
