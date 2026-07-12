package server

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/shared"
	_ "modernc.org/sqlite"
)

// DB wraps a SQLite database for version tracking.
type DB struct {
	db *sql.DB
}

// NewDB opens (or creates) the SQLite database at path and runs migrations.
func NewDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("db: migrate: %w", err)
	}
	return &DB{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS versions (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			system        TEXT    NOT NULL,
			game          TEXT    NOT NULL,
			sha256        TEXT    NOT NULL,
			client_id     TEXT    NOT NULL,
			timestamp     INTEGER NOT NULL,
			is_active     INTEGER NOT NULL DEFAULT 0,
			parent_sha256 TEXT    NOT NULL DEFAULT ''
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_versions_sha256 ON versions(system, game, sha256);
		CREATE INDEX IF NOT EXISTS idx_versions_game ON versions(system, game);
	`)
	return err
}

// AddVersion inserts a new version and applies conflict resolution.
// Conflict: if active version's sha256 != parentSHA256, it's a conflict.
// Newer timestamp wins and becomes active.
// Returns (isActive, conflict, error).
func (d *DB) AddVersion(system, game, sha256, clientID, parentSHA256 string, ts time.Time) (isActive bool, conflict bool, err error) {
	tx, err := d.db.Begin()
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback()

	var activeSHA256 string
	var activeTS int64
	row := tx.QueryRow(
		`SELECT sha256, timestamp FROM versions WHERE system=? AND game=? AND is_active=1`,
		system, game,
	)
	noActive := false
	if err := row.Scan(&activeSHA256, &activeTS); err != nil {
		if err == sql.ErrNoRows {
			noActive = true
		} else {
			return false, false, err
		}
	}

	conflict = !noActive && activeSHA256 != parentSHA256

	_, err = tx.Exec(
		`INSERT OR IGNORE INTO versions (system, game, sha256, client_id, timestamp, is_active, parent_sha256)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		system, game, sha256, clientID, ts.Unix(), parentSHA256,
	)
	if err != nil {
		return false, false, err
	}

	newIsActive := noActive || ts.Unix() > activeTS
	if newIsActive {
		if _, err := tx.Exec(
			`UPDATE versions SET is_active=0 WHERE system=? AND game=?`,
			system, game,
		); err != nil {
			return false, false, err
		}
		if _, err := tx.Exec(
			`UPDATE versions SET is_active=1 WHERE system=? AND game=? AND sha256=?`,
			system, game, sha256,
		); err != nil {
			return false, false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, false, err
	}
	return newIsActive, conflict, nil
}

// GetActiveVersion returns the active version for system/game, or nil if none.
func (d *DB) GetActiveVersion(system, game string) (*shared.VersionInfo, error) {
	row := d.db.QueryRow(
		`SELECT id, system, game, sha256, client_id, timestamp, is_active, parent_sha256
		 FROM versions WHERE system=? AND game=? AND is_active=1`,
		system, game,
	)
	return scanVersion(row)
}

// ListVersions returns all versions for system/game ordered by timestamp desc.
func (d *DB) ListVersions(system, game string) ([]shared.VersionInfo, error) {
	rows, err := d.db.Query(
		`SELECT id, system, game, sha256, client_id, timestamp, is_active, parent_sha256
		 FROM versions WHERE system=? AND game=? ORDER BY timestamp DESC`,
		system, game,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []shared.VersionInfo
	for rows.Next() {
		v, err := scanVersionRow(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, *v)
	}
	return versions, rows.Err()
}

// SetActive marks the given sha256 as active for system/game.
func (d *DB) SetActive(system, game, sha256 string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE versions SET is_active=0 WHERE system=? AND game=?`, system, game); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE versions SET is_active=1 WHERE system=? AND game=? AND sha256=?`, system, game, sha256); err != nil {
		return err
	}
	return tx.Commit()
}

// ListSystems returns all distinct systems.
func (d *DB) ListSystems() ([]string, error) {
	rows, err := d.db.Query(`SELECT DISTINCT system FROM versions ORDER BY system`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var systems []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		systems = append(systems, s)
	}
	return systems, rows.Err()
}

// ListGames returns all distinct games for a system.
func (d *DB) ListGames(system string) ([]string, error) {
	rows, err := d.db.Query(`SELECT DISTINCT game FROM versions WHERE system=? ORDER BY game`, system)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var games []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

func scanVersion(row *sql.Row) (*shared.VersionInfo, error) {
	var v shared.VersionInfo
	var ts int64
	var isActive int
	err := row.Scan(&v.ID, &v.System, &v.Game, &v.SHA256, &v.ClientID, &ts, &isActive, &v.ParentSHA256)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.Timestamp = time.Unix(ts, 0)
	v.IsActive = isActive == 1
	return &v, nil
}

func scanVersionRow(rows *sql.Rows) (*shared.VersionInfo, error) {
	var v shared.VersionInfo
	var ts int64
	var isActive int
	if err := rows.Scan(&v.ID, &v.System, &v.Game, &v.SHA256, &v.ClientID, &ts, &isActive, &v.ParentSHA256); err != nil {
		return nil, err
	}
	v.Timestamp = time.Unix(ts, 0)
	v.IsActive = isActive == 1
	return &v, nil
}
