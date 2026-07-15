package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

const selectGame = `SELECT id, title, sort_title, platform, publisher, description,
	provider, provider_id, metadata, refreshed_at FROM games`

func (r *Repository) FindGameByFingerprint(ctx context.Context, fingerprint catalog.Fingerprint) (*catalog.Game, error) {
	game, err := scanGame(r.db.QueryRowContext(ctx, selectGame+` WHERE id = (
		SELECT game_id FROM game_roms WHERE
			(? <> '' AND sha256 = ?) OR
			(? <> '' AND sha1 = ?) OR
			(? <> '' AND md5 = ?) OR
			(? <> '' AND crc32 = ?)
		ORDER BY CASE
			WHEN ? <> '' AND sha256 = ? THEN 1
			WHEN ? <> '' AND sha1 = ? THEN 2
			WHEN ? <> '' AND md5 = ? THEN 3
			ELSE 4 END
		LIMIT 1
	)`,
		fingerprint.SHA256, fingerprint.SHA256,
		fingerprint.SHA1, fingerprint.SHA1,
		fingerprint.MD5, fingerprint.MD5,
		fingerprint.CRC32, fingerprint.CRC32,
		fingerprint.SHA256, fingerprint.SHA256,
		fingerprint.SHA1, fingerprint.SHA1,
		fingerprint.MD5, fingerprint.MD5,
	))
	if err != nil {
		return nil, translateNotFound(err)
	}
	return r.withGameMedia(ctx, game)
}

func (r *Repository) FindGameByProvider(ctx context.Context, provider, providerID string) (*catalog.Game, error) {
	game, err := scanGame(r.db.QueryRowContext(ctx,
		selectGame+` WHERE provider = ? AND provider_id = ? ORDER BY refreshed_at DESC LIMIT 1`,
		provider, providerID,
	))
	if err != nil {
		return nil, translateNotFound(err)
	}
	return r.withGameMedia(ctx, game)
}

func (r *Repository) GetGame(ctx context.Context, id string) (*catalog.Game, error) {
	game, err := scanGame(r.db.QueryRowContext(ctx, selectGame+` WHERE id = ?`, id))
	if err != nil {
		return nil, translateNotFound(err)
	}
	return r.withGameMedia(ctx, game)
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
		media, err := r.listGameMedia(ctx, games[index].ID)
		if err != nil {
			return nil, err
		}
		games[index].Media = media
	}
	return games, nil
}

func (r *Repository) SaveGame(ctx context.Context, game catalog.Game, rom catalog.GameROM) error {
	languages, err := json.Marshal(rom.Languages)
	if err != nil {
		return err
	}
	attributes, err := json.Marshal(rom.Attributes)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveGameMetadata(ctx, tx, game); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO game_roms(
		id, game_id, system, name, region, languages, size, crc32, md5, sha1, sha256, source, source_id, attributes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		game_id = excluded.game_id,
		system = excluded.system,
		name = excluded.name,
		region = excluded.region,
		languages = excluded.languages,
		size = excluded.size,
		crc32 = excluded.crc32,
		md5 = excluded.md5,
		sha1 = excluded.sha1,
		sha256 = excluded.sha256,
		source = excluded.source,
		source_id = excluded.source_id,
		attributes = excluded.attributes`,
		rom.ID, rom.GameID, rom.System, rom.Name, rom.Region, string(languages), rom.Size,
		rom.CRC32, rom.MD5, rom.SHA1, rom.SHA256, rom.Source, rom.SourceID, string(attributes),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) SaveGameMetadata(ctx context.Context, game catalog.Game) error {
	return saveGameMetadata(ctx, r.db, game)
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
		id, title, sort_title, platform, publisher, description, provider, provider_id, metadata, refreshed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title = excluded.title,
		sort_title = excluded.sort_title,
		platform = excluded.platform,
		publisher = excluded.publisher,
		description = excluded.description,
		provider = excluded.provider,
		provider_id = excluded.provider_id,
		metadata = excluded.metadata,
		refreshed_at = excluded.refreshed_at`,
		game.ID, game.Title, game.SortTitle, game.Platform, game.Publisher, game.Description,
		game.Provider, game.ProviderID, string(metadata), game.RefreshedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (r *Repository) SaveGameMedia(ctx context.Context, media catalog.GameMedia) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO game_media(
		id, game_id, kind, position, format, sha256, size, provider, provider_id, attribution
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(game_id, kind, position) DO UPDATE SET
		format = excluded.format,
		sha256 = excluded.sha256,
		size = excluded.size,
		provider = excluded.provider,
		provider_id = excluded.provider_id,
		attribution = excluded.attribution`,
		media.ID, media.GameID, media.Kind, media.Position, media.Format, media.SHA256,
		media.Size, media.Provider, media.ProviderID, media.Attribution,
	)
	return err
}

func (r *Repository) ClearGameMedia(ctx context.Context, gameID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM game_media WHERE game_id = ?`, gameID)
	return err
}

func (r *Repository) GetGameMedia(ctx context.Context, gameID, mediaID string) (*catalog.GameMedia, error) {
	media, err := scanGameMedia(r.db.QueryRowContext(ctx, `SELECT
		id, game_id, kind, position, format, sha256, size, provider, provider_id, attribution
		FROM game_media WHERE game_id = ? AND id = ?`, gameID, mediaID))
	return media, translateNotFound(err)
}

func (r *Repository) withGameMedia(ctx context.Context, game *catalog.Game) (*catalog.Game, error) {
	media, err := r.listGameMedia(ctx, game.ID)
	if err != nil {
		return nil, err
	}
	game.Media = media
	return game, nil
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
		&game.ID, &game.Title, &game.SortTitle, &game.Platform, &game.Publisher,
		&game.Description, &game.Provider, &game.ProviderID, &metadata, &refreshedAt,
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
