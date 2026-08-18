package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/storage"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

const selectGame = `SELECT id, title, sort_title, platform, platform_company, publisher, description,
	provider, metadata, refreshed_at FROM games`

func (r *Repository) FindGameByIdentifier(ctx context.Context, identifier catalog.GameIdentifier) (*catalog.Game, error) {
	game, err := scanGame(r.db.QueryRowContext(ctx, selectGame+` WHERE id = (
		SELECT game_id FROM game_identifiers WHERE namespace = ? AND value = ?
	)`, identifier.Namespace, identifier.Value))
	if err != nil {
		return nil, translateNotFound(err)
	}
	return r.withGameDetails(ctx, game)
}

func (r *Repository) FindGameByFingerprint(ctx context.Context, fingerprint catalog.GameFingerprint) (*catalog.Game, error) {
	game, err := scanGame(r.db.QueryRowContext(ctx, selectGame+` WHERE id = (
		SELECT game_id FROM game_fingerprints WHERE platform = ? AND algorithm = ? AND value = ?
	)`, fingerprint.Platform, fingerprint.Algorithm, fingerprint.Value))
	if err != nil {
		return nil, translateNotFound(err)
	}
	return r.withGameDetails(ctx, game)
}

func (r *Repository) GetGame(ctx context.Context, id string) (*catalog.Game, error) {
	game, err := scanGame(r.db.QueryRowContext(ctx, selectGame+` WHERE id = ?`, id))
	if err != nil {
		return nil, translateNotFound(err)
	}
	return r.withGameDetails(ctx, game)
}

func (r *Repository) ListGames(ctx context.Context) ([]catalog.Game, error) {
	rows, err := r.db.QueryContext(ctx, selectGame+` ORDER BY sort_title, title, id`)
	if err != nil {
		return nil, err
	}
	var games []catalog.Game
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		games = append(games, *game)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range games {
		game, err := r.withGameDetails(ctx, &games[index])
		if err != nil {
			return nil, err
		}
		games[index] = *game
	}
	return games, nil
}

func (r *Repository) SaveGame(ctx context.Context, game catalog.Game, rom *catalog.GameROM) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	if r.store.HasDeletion(store.DeletionGame, game.ID) {
		return storage.ErrConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveGameMetadata(ctx, tx, game); err != nil {
		return translateUniqueViolation(err)
	}
	for _, identifier := range game.Identifiers {
		if err := claimIdentity(ctx, tx, "game_identifiers", game.ID,
			[]string{"namespace", "value"}, []any{identifier.Namespace, identifier.Value}); err != nil {
			return err
		}
	}
	for _, fingerprint := range game.Fingerprints {
		if err := claimIdentity(ctx, tx, "game_fingerprints", game.ID,
			[]string{"platform", "algorithm", "value"}, []any{fingerprint.Platform, fingerprint.Algorithm, fingerprint.Value}); err != nil {
			return err
		}
	}
	if rom != nil {
		if err := saveGameROM(ctx, tx, *rom); err != nil {
			return err
		}
	}
	if err := r.enqueueGameProjection(ctx, tx, game.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.projectStore(ctx)
}

func claimIdentity(ctx context.Context, tx *sql.Tx, table, gameID string, columns []string, values []any) error {
	query := "INSERT OR IGNORE INTO " + table + "(game_id"
	placeholders := "?"
	for _, column := range columns {
		query += ", " + column
		placeholders += ", ?"
	}
	query += ") VALUES (" + placeholders + ")"
	arguments := append([]any{gameID}, values...)
	if _, err := tx.ExecContext(ctx, query, arguments...); err != nil {
		return err
	}
	where := ""
	for index, column := range columns {
		if index > 0 {
			where += " AND "
		}
		where += column + " = ?"
	}
	var owner string
	if err := tx.QueryRowContext(ctx, "SELECT game_id FROM "+table+" WHERE "+where, values...).Scan(&owner); err != nil {
		return err
	}
	if owner != gameID {
		return storage.ErrConflict
	}
	return nil
}

func saveGameROM(ctx context.Context, tx *sql.Tx, rom catalog.GameROM) error {
	languages, err := json.Marshal(rom.Languages)
	if err != nil {
		return err
	}
	attributes, err := json.Marshal(rom.Attributes)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO game_roms(
		id, game_id, system, name, region, languages, size, crc32, md5, sha1, sha256, source, source_id, attributes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		game_id = excluded.game_id, system = excluded.system, name = excluded.name,
		region = excluded.region, languages = excluded.languages, size = excluded.size,
		crc32 = excluded.crc32, md5 = excluded.md5, sha1 = excluded.sha1,
		sha256 = excluded.sha256, source = excluded.source, source_id = excluded.source_id,
		attributes = excluded.attributes`,
		rom.ID, rom.GameID, rom.System, rom.Name, rom.Region, string(languages), rom.Size,
		rom.CRC32, rom.MD5, rom.SHA1, rom.SHA256, rom.Source, rom.SourceID, string(attributes),
	)
	return err
}

type contextExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func saveGameMetadata(ctx context.Context, executor contextExecutor, game catalog.Game) error {
	metadata, err := json.Marshal(game.Metadata)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO games(
		id, title, sort_title, platform, platform_company, publisher, description, provider, provider_id, metadata, refreshed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title = excluded.title, sort_title = excluded.sort_title, platform = excluded.platform,
		platform_company = excluded.platform_company, publisher = excluded.publisher,
		description = excluded.description, provider = excluded.provider,
		provider_id = '', metadata = excluded.metadata, refreshed_at = excluded.refreshed_at`,
		game.ID, game.Title, game.SortTitle, game.Platform, game.PlatformCompany, game.Publisher,
		game.Description, game.MetadataSource, string(metadata), game.RefreshedAt.Format(time.RFC3339Nano),
	)
	return err
}

// DeleteGame removes a game, its saves with their revision history, and any
// artifacts left unreferenced. Media and identity rows cascade via foreign keys.
func (r *Repository) DeleteGame(ctx context.Context, id string) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Capture all lineage and graph identifiers before the database cascade.
	saveIDs, err := omnisaveIDsOfGame(ctx, tx, id)
	if err != nil {
		return err
	}
	revisionIDs, err := revisionIDsOfGame(ctx, tx, id)
	if err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT sha256 FROM game_media WHERE game_id = ?
		UNION
		SELECT artifact_sha256 FROM revision_files WHERE revision_id IN (
			SELECT id FROM revisions WHERE game_id = ?
		)`, id, id,
	)
	if err != nil {
		return err
	}
	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			rows.Close()
			return err
		}
		hashes = append(hashes, hash)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM omnisaves WHERE game_id = ?`, id); err != nil {
		return err
	}
	// Revisions deliberately do not cascade from their creator Omnisave: a
	// surviving fork may share them. Deleting the whole game is the one case
	// where every save and therefore every node in its graph is leaving.
	if _, err := tx.ExecContext(ctx, `DELETE FROM revisions WHERE game_id = ?`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM games WHERE id = ?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		// Idempotent with respect to a committed deletion, like the save and
		// revision deletions it cascades to. The ledger commits with the
		// delete; the portable marker may lag in deferred cleanup.
		committed, err := deletionCommitted(ctx, tx, store.DeletionGame, id)
		if err != nil {
			return err
		}
		if committed || r.store.HasDeletion(store.DeletionGame, id) {
			return nil
		}
		return storage.ErrNotFound
	}

	deletedAt := time.Now().UTC()
	if err := enqueueDeletion(ctx, tx, store.Deletion{
		TargetKind: store.DeletionGame,
		TargetID:   id,
		DeletedAt:  deletedAt,
	}); err != nil {
		return err
	}
	for _, omnisaveID := range saveIDs {
		if err := enqueueDeletion(ctx, tx, store.Deletion{
			TargetKind: store.DeletionOmnisave,
			TargetID:   omnisaveID,
			DeletedAt:  deletedAt,
		}); err != nil {
			return err
		}
	}
	for _, revisionID := range revisionIDs {
		if err := enqueueDeletion(ctx, tx, store.Deletion{
			TargetKind: store.DeletionRevision,
			TargetID:   revisionID,
			DeletedAt:  deletedAt,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.deferDeletionCleanup("deletion of game "+id, func(ctx context.Context) {
		r.noteStoreLag("deletion of game "+id, r.dropRevisions(revisionIDs))
		r.noteStoreLag("deletion of game "+id, r.store.RemoveGame(id))
		r.reclaimArtifacts(ctx, hashes)
	})
	return nil
}

func (r *Repository) SaveGameMedia(ctx context.Context, media catalog.GameMedia) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.requireAvailableArtifacts(ctx, tx, map[string]int64{media.SHA256: media.Size}); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO game_media(
		id, game_id, kind, position, format, sha256, size, provider, provider_id, attribution
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(game_id, kind, position) DO UPDATE SET
		format = excluded.format, sha256 = excluded.sha256, size = excluded.size,
		provider = excluded.provider, provider_id = excluded.provider_id, attribution = excluded.attribution`,
		media.ID, media.GameID, media.Kind, media.Position, media.Format, media.SHA256,
		media.Size, media.Provider, media.ProviderID, media.Attribution,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) ClearGameMedia(ctx context.Context, gameID string) error {
	r.mutate.Lock()
	defer r.mutate.Unlock()
	if err := r.requireStoreReady(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	hashes, err := queryIDs(ctx, tx, `SELECT DISTINCT sha256 FROM game_media WHERE game_id = ?`, gameID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM game_media WHERE game_id = ?`, gameID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.reclaimArtifacts(ctx, hashes)
	return nil
}

func (r *Repository) GetGameMedia(ctx context.Context, gameID, mediaID string) (*catalog.GameMedia, error) {
	media, err := scanGameMedia(r.db.QueryRowContext(ctx, `SELECT
		id, game_id, kind, position, format, sha256, size, provider, provider_id, attribution
		FROM game_media WHERE game_id = ? AND id = ?`, gameID, mediaID))
	return media, translateNotFound(err)
}

func (r *Repository) withGameDetails(ctx context.Context, game *catalog.Game) (*catalog.Game, error) {
	var err error
	if game.Identifiers, err = r.listGameIdentifiers(ctx, game.ID); err != nil {
		return nil, err
	}
	if game.Fingerprints, err = r.listGameFingerprints(ctx, game.ID); err != nil {
		return nil, err
	}
	if game.Media, err = r.listGameMedia(ctx, game.ID); err != nil {
		return nil, err
	}
	if game.Provenance, err = r.ListGameProvenance(ctx, game.ID); err != nil {
		return nil, err
	}
	return game, nil
}

func (r *Repository) listGameIdentifiers(ctx context.Context, gameID string) ([]catalog.GameIdentifier, error) {
	return listGameIdentifiersFrom(ctx, r.db, gameID)
}

func listGameIdentifiersFrom(ctx context.Context, queryer storeQueryer, gameID string) ([]catalog.GameIdentifier, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT namespace, value FROM game_identifiers
		WHERE game_id = ? ORDER BY namespace, value`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]catalog.GameIdentifier, 0)
	for rows.Next() {
		var item catalog.GameIdentifier
		if err := rows.Scan(&item.Namespace, &item.Value); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) listGameFingerprints(ctx context.Context, gameID string) ([]catalog.GameFingerprint, error) {
	return listGameFingerprintsFrom(ctx, r.db, gameID)
}

func listGameFingerprintsFrom(ctx context.Context, queryer storeQueryer, gameID string) ([]catalog.GameFingerprint, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT platform, algorithm, value FROM game_fingerprints
		WHERE game_id = ? ORDER BY platform, algorithm, value`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]catalog.GameFingerprint, 0)
	for rows.Next() {
		var item catalog.GameFingerprint
		if err := rows.Scan(&item.Platform, &item.Algorithm, &item.Value); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) listGameMedia(ctx context.Context, gameID string) ([]catalog.GameMedia, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT
		id, game_id, kind, position, format, sha256, size, provider, provider_id, attribution
		FROM game_media WHERE game_id = ? ORDER BY kind, position, id`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	media := make([]catalog.GameMedia, 0)
	for rows.Next() {
		item, err := scanGameMedia(rows)
		if err != nil {
			return nil, err
		}
		media = append(media, *item)
	}
	return media, rows.Err()
}

func scanGame(row scanner) (*catalog.Game, error) {
	var game catalog.Game
	var metadata, refreshedAt string
	if err := row.Scan(
		&game.ID, &game.Title, &game.SortTitle, &game.Platform, &game.PlatformCompany,
		&game.Publisher, &game.Description, &game.MetadataSource, &metadata, &refreshedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(metadata), &game.Metadata); err != nil {
		return nil, err
	}
	var err error
	game.RefreshedAt, err = time.Parse(time.RFC3339Nano, refreshedAt)
	return &game, err
}

func scanGameMedia(row scanner) (*catalog.GameMedia, error) {
	var media catalog.GameMedia
	err := row.Scan(
		&media.ID, &media.GameID, &media.Kind, &media.Position, &media.Format,
		&media.SHA256, &media.Size, &media.Provider, &media.ProviderID, &media.Attribution,
	)
	return &media, err
}

var _ storage.CatalogRepository = (*Repository)(nil)
