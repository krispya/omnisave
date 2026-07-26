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

	`CREATE TABLE devices (
		id           TEXT PRIMARY KEY,
		name         TEXT NOT NULL,
		platform     TEXT NOT NULL,
		created_at   TEXT NOT NULL,
		last_seen_at TEXT NOT NULL
	);

	CREATE TABLE game_tracking (
		game_id          TEXT NOT NULL REFERENCES games(id) ON DELETE CASCADE,
		device_id        TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		adapter          TEXT NOT NULL,
		installed        INTEGER NOT NULL,
		first_tracked_at TEXT NOT NULL,
		last_seen_at     TEXT NOT NULL,
		untracked_at     TEXT,
		PRIMARY KEY (game_id, device_id)
	);

	CREATE INDEX game_tracking_by_device ON game_tracking(device_id);`,

	// Omnisaves must always carry a display name; number existing unnamed
	// saves by their position in the game's creation order, counting only
	// columns this statement never modifies so the numbering is stable.
	`UPDATE omnisaves SET display_name = 'Save ' || (
		SELECT COUNT(*) FROM omnisaves AS earlier
		WHERE earlier.game_id = omnisaves.game_id
		  AND (earlier.created_at < omnisaves.created_at
		       OR (earlier.created_at = omnisaves.created_at AND earlier.id <= omnisaves.id))
	)
	WHERE display_name = '';`,

	// Artifact metadata: the logical (uncompressed) size of content stored
	// gzip-compressed on disk. Artifacts predating compression have no row
	// and are stat'ed directly as raw files.
	`CREATE TABLE artifacts (
		sha256 TEXT PRIMARY KEY,
		size   INTEGER NOT NULL
	);`,

	// Credentials the server has issued, and the pairing requests that mint
	// them (ADR-007). Secrets are stored hashed and are never recoverable;
	// pairing_requests.minted_token is the one exception, holding a token
	// between approval and the single poll that collects it.
	//
	// Neither table references devices(id): a pairing request names an
	// identity the client minted for itself, which the server may not have
	// seen yet and which nothing here can verify. The name is copied onto the
	// credential so a revocation list stays readable on its own.
	`CREATE TABLE credentials (
		id           TEXT PRIMARY KEY,
		token_hash   TEXT NOT NULL UNIQUE,
		kind         TEXT NOT NULL,
		device_id    TEXT NOT NULL DEFAULT '',
		device_name  TEXT NOT NULL,
		created_at   TEXT NOT NULL,
		last_used_at TEXT,
		revoked_at   TEXT
	);

	CREATE TABLE pairing_requests (
		id             TEXT PRIMARY KEY,
		code           TEXT NOT NULL,
		handle_hash    TEXT NOT NULL UNIQUE,
		device_id      TEXT NOT NULL,
		device_name    TEXT NOT NULL,
		platform       TEXT NOT NULL DEFAULT '',
		source_address TEXT NOT NULL DEFAULT '',
		status         TEXT NOT NULL,
		created_at     TEXT NOT NULL,
		expires_at     TEXT NOT NULL,
		minted_token   TEXT NOT NULL DEFAULT '',
		credential_id  TEXT
	);

	CREATE INDEX pairing_requests_by_status ON pairing_requests(status, expires_at);
	CREATE INDEX pairing_requests_by_source ON pairing_requests(source_address, created_at);

	CREATE TABLE owner_settings (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,

	// The owner's PIN (ADR-010). One row, because there is one owner. The
	// hash is slow to compute but a four-digit space is small enough to walk
	// regardless — what actually protects the PIN is the server refusing to
	// be hurried, which lives in the service rather than here.
	`CREATE TABLE owner_pin (
		id         INTEGER PRIMARY KEY CHECK (id = 1),
		salt       TEXT NOT NULL,
		hash       TEXT NOT NULL,
		iterations INTEGER NOT NULL,
		updated_at TEXT NOT NULL
	);`,
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
