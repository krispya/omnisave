package sqlite

import (
	"database/sql"
	"fmt"
)

var migrations = []string{
	`CREATE TABLE omnisaves (
		id         TEXT PRIMARY KEY,
		game_id    TEXT NOT NULL,
		slot       TEXT NOT NULL,
		created_at TEXT NOT NULL,
		metadata   TEXT NOT NULL
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
