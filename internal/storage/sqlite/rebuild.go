package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// Rebuild imports portable records missing from SQLite after a complete store inspection.

// rebuild imports into the database what the store holds and the database
// lacks. It runs on open, before reconcile, so the two passes together leave
// database and store agreeing whichever of them survived.
func (r *Repository) rebuild(ctx context.Context, inventory recoveryInventory) (bool, error) {
	var imported struct{ games, saves, revisions, missingObjects int }
	if err := r.applyDeletionMarkers(ctx, inventory); err != nil {
		return false, err
	}
	if !inventory.deletionsComplete {
		log.Printf("save store: deletion inventory is incomplete; absence-based imports and reclamation are disabled")
		return false, nil
	}

	knownGames, err := tableIDs(ctx, r.db, "games")
	if err != nil {
		return false, err
	}
	knownSaves, err := tableIDs(ctx, r.db, "omnisaves")
	if err != nil {
		return false, err
	}
	knownRevisions, err := tableIDs(ctx, r.db, "revisions")
	if err != nil {
		return false, err
	}
	revisionNames := make(map[string]map[string]string)
	revisionNameSources := make(map[string]map[string]string)
	storeCurrents := make(map[string]*string)
	// Saves absent from the database need a legacy fallback when their portable
	// record predates the explicit Current Revision field.
	importedSaves := make(map[string]bool)

	// Games first: they reference nothing, and the lineages imported after
	// them read better with their titles already in place.
	for _, id := range sortedKeys(inventory.games) {
		if _, deleted := inventory.deletedGames[id]; deleted {
			continue
		}
		record := inventory.games[id]
		if err := r.importGame(ctx, record, knownGames[id]); err != nil {
			return false, err
		}
		if !knownGames[id] {
			imported.games++
		}
		knownGames[id] = true
	}

	// Portable lineage records are the acknowledged mutable state. The outbox
	// is replayed before this pass, so applying them to existing rows also
	// repairs a database restored from an older backup.
	for _, id := range sortedKeys(inventory.saves) {
		if _, deleted := inventory.deletedSaves[id]; deleted {
			continue
		}
		record := inventory.saves[id]
		revisionNames[id] = record.RevisionNames
		revisionNameSources[id] = record.RevisionNameSources
		storeCurrents[id] = record.CurrentRevisionID
		if knownSaves[id] {
			r.noteRecordRowDivergence(ctx, record)
		}
		if err := r.importOmnisave(ctx, record); err != nil {
			return false, err
		}
		if !knownSaves[id] {
			importedSaves[id] = true
			imported.saves++
		}
		knownSaves[id] = true
	}

	// Manifests the database lacks, grouped by lineage. Immutable deletion
	// markers override stale manifests from a restored store backup.
	arrivals := make(map[string][]store.Revision)
	for _, id := range sortedKeys(inventory.revisions) {
		manifest := inventory.revisions[id]
		if knownRevisions[id] {
			continue
		}
		if _, deleted := inventory.deletedRevisions[id]; deleted {
			continue
		}
		if _, deleted := inventory.deletedSaves[manifest.Omnisave.ID]; deleted {
			continue
		}
		arrivals[manifest.Omnisave.ID] = append(arrivals[manifest.Omnisave.ID], manifest)
	}

	for omnisaveID, manifests := range arrivals {
		if !knownSaves[omnisaveID] {
			// No lineage record and no row: the record was lost. The manifests
			// carry the lineage's identity themselves, so the save is
			// reconstructed from them rather than abandoned with them.
			record := lineageFromManifests(manifests)
			log.Printf("save store: reconstructing save %s (%q) from its manifests; its record was lost",
				record.ID, record.DisplayName)
			if err := r.importOmnisave(ctx, record); err != nil {
				return false, err
			}
			knownSaves[omnisaveID] = true
			importedSaves[omnisaveID] = true
			imported.saves++
		}
		// A game known nowhere else is reconstructed the same way, whether its
		// lineage's record survived or not — the manifests name the game
		// regardless of which other record was lost.
		if game, found := gameFromManifests(manifests, knownGames); found {
			if err := r.importGame(ctx, game); err != nil {
				return false, err
			}
			knownGames[game.ID] = true
			imported.games++
		}
	}
	count, missing, err := r.importRevisions(ctx, arrivals, knownRevisions, revisionNames, revisionNameSources, importedSaves, storeCurrents)
	if err != nil {
		return false, err
	}
	imported.revisions += count
	imported.missingObjects += missing
	// Vocabulary repair, in order: recorded renames are replayed over
	// whatever paths survived — imported manifests still speak the
	// vocabulary they were minted in — and then every lineage's path format
	// is re-derived from the history that resulted. Records and rows
	// restored from different backups can disagree about the version; the
	// paths themselves are what a Device will be handed, so they decide.
	unreplayable, err := r.replayPathMigrations(ctx)
	if err != nil {
		return false, err
	}
	if err := r.classifyPathFormats(ctx); err != nil {
		return false, err
	}
	for id := range unreplayable {
		if _, err := r.db.ExecContext(ctx,
			`UPDATE omnisaves SET path_format_version = ? WHERE id = ?`,
			omnisave.PathFormatUnclassified, id); err != nil {
			return false, err
		}
	}

	// Marks last, because each one names the revision it landed on and that
	// node has to be in the database before it can be pointed at.
	for _, id := range sortedKeys(inventory.saves) {
		if _, deleted := inventory.deletedSaves[id]; deleted {
			continue
		}
		if err := r.importAchievements(ctx, inventory.saves[id]); err != nil {
			return false, err
		}
	}

	if imported.games+imported.saves+imported.revisions > 0 {
		log.Printf("save store: rebuilt index from the store: %d game(s), %d save(s), %d revision(s) imported",
			imported.games, imported.saves, imported.revisions)
	}
	if imported.missingObjects > 0 {
		log.Printf("save store: %d file(s) named by imported manifests have no object in the store; those snapshots are incomplete here and their other files remain recoverable", imported.missingObjects)
	}
	return inventory.complete, nil
}

// applyDeletionMarkers is monotonic repair: every row it removes has a valid,
// immutable marker proving that the logical delete committed. It is therefore
// safe even when the rest of the store inventory is damaged.
func (r *Repository) applyDeletionMarkers(ctx context.Context, inventory recoveryInventory) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, marker := range inventory.deletedSaves {
		if _, err := tx.ExecContext(ctx, `INSERT INTO deletion_ledger(target_kind, target_id, deleted_at)
			VALUES (?, ?, ?) ON CONFLICT(target_kind, target_id) DO NOTHING`, marker.TargetKind,
			marker.TargetID, marker.DeletedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM omnisaves WHERE id = ?`, id); err != nil {
			return err
		}
	}
	for id, marker := range inventory.deletedRevisions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO deletion_ledger(target_kind, target_id, deleted_at)
			VALUES (?, ?, ?) ON CONFLICT(target_kind, target_id) DO NOTHING`, marker.TargetKind,
			marker.TargetID, marker.DeletedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM revisions WHERE id = ?`, id); err != nil {
			return err
		}
	}
	for id, marker := range inventory.deletedGames {
		if _, err := tx.ExecContext(ctx, `INSERT INTO deletion_ledger(target_kind, target_id, deleted_at)
			VALUES (?, ?, ?) ON CONFLICT(target_kind, target_id) DO NOTHING`, marker.TargetKind,
			marker.TargetID, marker.DeletedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM games WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// noteRecordRowDivergence reports before a portable record replaces conflicting row state.
func (r *Repository) noteRecordRowDivergence(ctx context.Context, record store.Omnisave) {
	var displayName string
	var current sql.NullString
	if err := r.db.QueryRowContext(ctx,
		`SELECT display_name, current_revision_id FROM omnisaves WHERE id = ?`, record.ID,
	).Scan(&displayName, &current); err != nil {
		return
	}
	if displayName != record.DisplayName {
		log.Printf("save store: record for save %s carries the name %q over the database's %q; the portable record is the acknowledged state",
			record.ID, record.DisplayName, displayName)
	}
	if !sameNullableString(current, record.CurrentRevisionID) {
		recorded := "none"
		if record.CurrentRevisionID != nil {
			recorded = *record.CurrentRevisionID
		}
		held := "none"
		if current.Valid {
			held = current.String
		}
		log.Printf("save store: record for save %s moves the current revision from %s to %s; the portable record is the acknowledged state",
			record.ID, held, recorded)
	}
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// importGame writes a store game record through the same path a provider
// lookup would take, so identity claims merge instead of colliding.
func (r *Repository) importGame(ctx context.Context, record store.Game, exists ...bool) error {
	game := catalog.Game{
		ID:              record.ID,
		Title:           record.Title,
		SortTitle:       record.SortTitle,
		Platform:        record.Platform,
		PlatformCompany: record.PlatformCompany,
		Publisher:       record.Publisher,
		Identifiers:     record.Identifiers,
		Fingerprints:    record.Fingerprints,
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(exists) > 0 && exists[0] {
		_, err = tx.ExecContext(ctx, `UPDATE games SET title = ?, sort_title = ?, platform = ?,
			platform_company = ?, publisher = ? WHERE id = ?`, game.Title, game.SortTitle,
			game.Platform, game.PlatformCompany, game.Publisher, game.ID)
	} else {
		err = saveGameMetadata(ctx, tx, game)
	}
	if err != nil {
		return err
	}
	for _, identifier := range game.Identifiers {
		if err := claimIdentity(ctx, tx, "game_identifiers", game.ID,
			[]string{"namespace", "value"}, []any{identifier.Namespace, identifier.Value}); err != nil {
			return err
		}
	}
	for _, fingerprint := range game.Fingerprints {
		if err := claimIdentity(ctx, tx, "game_fingerprints", game.ID,
			[]string{"platform", "algorithm", "value"},
			[]any{fingerprint.Platform, fingerprint.Algorithm, fingerprint.Value}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// importAchievements restores a lineage's marks. Placement is deferred to the
// revisions pass: a mark naming a revision the store no longer holds imports
// unplaced, which the next commit on the save then claims.
func (r *Repository) importAchievements(ctx context.Context, record store.Omnisave) error {
	if len(record.Achievements) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, achievement := range record.Achievements {
		if achievement.ID == "" || achievement.Name == "" || achievement.UnlockedAt.IsZero() {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO achievements(
			omnisave_id, achievement_id, name, description, unlocked_at, revision_id
		) VALUES (?, ?, ?, ?, ?, (SELECT id FROM revisions WHERE id = ?))
		ON CONFLICT(omnisave_id, achievement_id) DO UPDATE SET
			name = excluded.name, description = excluded.description,
			unlocked_at = excluded.unlocked_at,
			revision_id = COALESCE(achievements.revision_id, excluded.revision_id)`,
			record.ID, achievement.ID, achievement.Name, achievement.Description,
			achievement.UnlockedAt.Unix(), nullableID(achievement.RevisionID)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullableID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func (r *Repository) importOmnisave(ctx context.Context, record store.Omnisave) error {
	if !record.Scope.Valid() {
		return fmt.Errorf("invalid save scope in portable record")
	}
	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return err
	}
	migrations, err := r.mergedPathMigrations(ctx, record)
	if err != nil {
		return err
	}
	// The record's version is written as its provisional claim — including 0,
	// the unclassified state every mutation guard holds. classifyPathFormats
	// re-derives the truth from the history once manifests are in place, so
	// a rebuild that stops early leaves lineages held, never misfiled.
	_, err = r.db.ExecContext(ctx, `INSERT INTO omnisaves(
		id, game_id, display_name, path_format_version, path_migrations, current_revision_id,
		forked_from_omnisave_id, forked_from_revision_id, created_at, metadata, scope
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		game_id = excluded.game_id, display_name = excluded.display_name,
		path_format_version = excluded.path_format_version,
		path_migrations = excluded.path_migrations,
		current_revision_id = excluded.current_revision_id,
		forked_from_omnisave_id = excluded.forked_from_omnisave_id,
		forked_from_revision_id = excluded.forked_from_revision_id,
		created_at = excluded.created_at, metadata = excluded.metadata, scope = excluded.scope`,
		record.ID, record.GameID, record.DisplayName, record.PathFormatVersion, migrations,
		record.CurrentRevisionID, forkOmnisaveID(record.ForkedFrom), forkRevisionID(record.ForkedFrom),
		record.CreatedAt.Format(time.RFC3339Nano), string(metadata), encodeScope(record.Scope))
	return err
}

// mergedPathMigrations joins the record's migration facts with any the row
// already holds. Facts are append-only: whichever side of a backup skew
// remembers more renames wins, so an applied rename is never forgotten by
// restoring the older artifact.
func (r *Repository) mergedPathMigrations(ctx context.Context, record store.Omnisave) (string, error) {
	held := "[]"
	err := r.db.QueryRowContext(ctx,
		`SELECT path_migrations FROM omnisaves WHERE id = ?`, record.ID,
	).Scan(&held)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	known, err := decodePathMigrations(held)
	if err != nil {
		return "", err
	}
	merged := known
	for _, fact := range record.PathMigrations {
		applied := false
		for _, kept := range known {
			if kept.Kind == fact.Kind && kept.FromVersion == fact.FromVersion &&
				kept.ToVersion == fact.ToVersion && kept.From == fact.From && kept.To == fact.To {
				applied = true
				break
			}
		}
		if !applied {
			merged = append(merged, fact)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].FromVersion < merged[j].FromVersion
	})
	if len(merged) == 0 {
		return "[]", nil
	}
	encoded, err := json.Marshal(merged)
	return string(encoded), err
}

// replayPathMigrations reapplies each lineage's recorded renames to any of
// its revision paths still speaking the renamed-away vocabulary. Imported
// manifests always arrive in the vocabulary they were minted in — snapshot
// manifests are immutable — so the recorded facts are what carry them
// forward to the vocabulary the lineage reached. Idempotent: a path already
// renamed no longer matches its fact's source vocabulary. A fact whose
// operation, endpoints, source vocabulary, or ordering this build cannot
// reproduce is not applied; its lineage is returned for classification to
// hold as unclassified.
func (r *Repository) replayPathMigrations(ctx context.Context) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, path_migrations FROM omnisaves WHERE path_migrations != '[]'`)
	if err != nil {
		return nil, err
	}
	type recordedMigrations struct {
		id    string
		facts []omnisave.PathMigration
	}
	var lineages []recordedMigrations
	for rows.Next() {
		var id, recorded string
		if err := rows.Scan(&id, &recorded); err != nil {
			rows.Close()
			return nil, err
		}
		facts, err := decodePathMigrations(recorded)
		if err != nil {
			rows.Close()
			return nil, err
		}
		lineages = append(lineages, recordedMigrations{id: id, facts: facts})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	unreplayable := make(map[string]bool)
	var replayed int64
	for _, lineage := range lineages {
		for index, fact := range lineage.facts {
			migration, known := omnisave.MigrationFrom(fact.FromVersion)
			ordered := index == 0 || lineage.facts[index-1].ToVersion == fact.FromVersion
			if !known || !ordered || fact.Kind != migration.Kind ||
				fact.ToVersion != migration.ToVersion || fact.From != migration.Location {
				unreplayable[lineage.id] = true
				log.Printf("save store: save %s records a path migration this build cannot replay; holding its path format",
					lineage.id)
				break
			}
		}
		if unreplayable[lineage.id] {
			continue
		}
		for _, fact := range lineage.facts {
			prefix := fact.From + "/"
			result, err := r.db.ExecContext(ctx,
				`UPDATE revision_files SET path = ? || substr(path, ?)
				WHERE revision_id IN (SELECT id FROM revisions WHERE omnisave_id = ?)
				AND substr(path, 1, ?) = ?`,
				fact.To+"/", len(prefix)+1, lineage.id, len(prefix), prefix)
			if err != nil {
				return nil, err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return nil, err
			}
			replayed += count
		}
	}
	if replayed > 0 {
		log.Printf("save store: replayed recorded path migrations over %d recovered file path(s)", replayed)
	}
	return unreplayable, nil
}

// classifyPathFormats re-derives every lineage's path format from the
// vocabulary its reachable history speaks, after recorded renames are
// replayed. Unclassified rows get their first answer here; a classified row
// is corrected only when its claim disagrees with its paths, which is what
// repairs a database and a store restored from different moments. The
// schema migration holds a frozen snapshot of this classification for the
// one-time upgrade; this is the live copy recovery runs on every open.
func (r *Repository) classifyPathFormats(ctx context.Context) error {
	const speaks = `EXISTS (
		WITH RECURSIVE members(id) AS (
			SELECT id FROM revisions WHERE omnisave_id = candidate.id
			UNION SELECT current_revision_id FROM omnisaves
				WHERE id = candidate.id AND current_revision_id IS NOT NULL
			UNION SELECT forked_from_revision_id FROM omnisaves
				WHERE id = candidate.id AND forked_from_revision_id IS NOT NULL
			UNION SELECT revisions.parent_id FROM revisions JOIN members ON revisions.id = members.id
				WHERE revisions.parent_id IS NOT NULL
		)
		SELECT 1 FROM revision_files WHERE revision_id IN (SELECT id FROM members)
		AND substr(path, 1, ?) = ?
	)`
	var classified int64
	// Oldest retired format first; a history speaking several keeps the
	// oldest claim (it is held and refused as mixed either way). Only
	// location renames are recognizable from paths; a future migration kind
	// brings its own classifier.
	notOlder := ""
	var olderArgs []any
	for _, migration := range omnisave.PathFormatMigrations() {
		if migration.Kind != omnisave.MigrationKindLocationRename {
			continue
		}
		query := `UPDATE omnisaves AS candidate SET path_format_version = ?
			WHERE path_format_version != ? AND ` + speaks + notOlder
		args := append([]any{migration.FromVersion, migration.FromVersion,
			len(migration.Location) + 1, migration.Location + "/"}, olderArgs...)
		result, err := r.db.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		classified += count
		notOlder += ` AND NOT ` + speaks
		olderArgs = append(olderArgs, len(migration.Location)+1, migration.Location+"/")
	}
	query := `UPDATE omnisaves AS candidate SET path_format_version = ?
		WHERE path_format_version != ?` + notOlder
	args := append([]any{omnisave.PathFormatNative, omnisave.PathFormatNative}, olderArgs...)
	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	classified += count
	if classified > 0 {
		log.Printf("save store: classified the path format of %d lineage(s) from their history", classified)
	}
	return nil
}

// importRevisions inserts all lineages parents-first, then restores imported
// saves' current pointers. Missing objects are counted without dropping manifests.
func (r *Repository) importRevisions(
	ctx context.Context,
	arrivals map[string][]store.Revision,
	knownRevisions map[string]bool,
	revisionNames map[string]map[string]string,
	revisionNameSources map[string]map[string]string,
	importedSaves map[string]bool,
	storeCurrents map[string]*string,
) (int, int, error) {
	var manifests []store.Revision
	for _, batch := range arrivals {
		manifests = append(manifests, batch...)
	}
	ordered, orphaned := orderForImport(manifests, knownRevisions)
	for _, manifest := range orphaned {
		log.Printf("save store: revision %s of save %s names parent %s, which is nowhere; importing it as a root",
			manifest.ID, manifest.Omnisave.ID, *manifest.Parent)
	}

	missingObjects := 0
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	for _, manifest := range ordered {
		metadata, err := json.Marshal(manifest.Metadata)
		if err != nil {
			return 0, 0, err
		}
		parent := manifest.Parent
		if parent != nil && !knownRevisions[*parent] {
			parent = nil
		}
		displayName := revisionNames[manifest.Omnisave.ID][manifest.ID]
		nameSource := revisionNameSources[manifest.Omnisave.ID][manifest.ID]
		// A named revision whose record predates name sources was named by a
		// person; an explicit relabel may replace it later.
		if displayName != "" && nameSource == "" {
			nameSource = omnisave.NameSourceManual
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO revisions(
			id, game_id, omnisave_id, display_name, name_source, parent_id, created_at, saved_at, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, manifest.ID, manifest.Game.ID, manifest.Omnisave.ID,
			displayName, nameSource, parent,
			manifest.CreatedAt.Format(time.RFC3339Nano), formatNullableTime(manifest.SavedAt),
			string(metadata)); err != nil {
			return 0, 0, err
		}
		for _, file := range manifest.Files {
			available := r.store.HasObject(file.SHA256)
			// Recovery preserves manifests even when a partial copy omitted an
			// object. Publish the row briefly so the reference trigger can check
			// descriptor agreement, then mark the capability unavailable before
			// commit when the bytes are absent.
			if _, err := tx.ExecContext(ctx, `INSERT INTO artifacts(sha256, size, available)
				VALUES (?, ?, 1)
				ON CONFLICT(sha256) DO UPDATE SET size = excluded.size, available = 1`,
				file.SHA256, file.Size); err != nil {
				return 0, 0, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO revision_files(
				revision_id, path, artifact_format, artifact_sha256, artifact_size
			) VALUES (?, ?, ?, ?, ?)`, manifest.ID, file.Path, file.Format, file.SHA256, file.Size); err != nil {
				return 0, 0, err
			}
			if !available {
				missingObjects++
				if _, err := tx.ExecContext(ctx,
					`UPDATE artifacts SET available = 0 WHERE sha256 = ?`, file.SHA256); err != nil {
					return 0, 0, err
				}
			}
		}
		knownRevisions[manifest.ID] = true
	}

	for omnisaveID := range importedSaves {
		if storeCurrents[omnisaveID] != nil {
			// The record's pointer was written when the save was imported.
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE omnisaves SET current_revision_id = COALESCE(current_revision_id, (
			SELECT r.id FROM revisions r WHERE r.omnisave_id = omnisaves.id
			AND NOT EXISTS (SELECT 1 FROM revisions c
				WHERE c.omnisave_id = r.omnisave_id AND c.parent_id = r.id)
			ORDER BY r.created_at DESC, r.id DESC LIMIT 1
		)) WHERE id = ?`, omnisaveID); err != nil {
			return 0, 0, err
		}
	}
	return len(ordered), missingObjects, tx.Commit()
}

// orderForImport sorts manifests so every parent precedes its children, which
// is the order the parent_id constraint accepts. A manifest whose parent is
// neither here nor already in the database is returned in orphaned as well:
// its snapshot is complete — manifests never depend on their parents for
// content — but its place in the chain is lost, so it imports as a root.
func orderForImport(manifests []store.Revision, knownRevisions map[string]bool) (ordered, orphaned []store.Revision) {
	sort.Slice(manifests, func(a, b int) bool {
		if !manifests[a].CreatedAt.Equal(manifests[b].CreatedAt) {
			return manifests[a].CreatedAt.Before(manifests[b].CreatedAt)
		}
		return manifests[a].ID < manifests[b].ID
	})

	arriving := make(map[string]bool, len(manifests))
	for _, manifest := range manifests {
		arriving[manifest.ID] = true
	}
	placed := make(map[string]bool, len(manifests))
	remaining := manifests
	for len(remaining) > 0 {
		var deferred []store.Revision
		progressed := false
		for _, manifest := range remaining {
			switch {
			case manifest.Parent == nil,
				knownRevisions[*manifest.Parent],
				placed[*manifest.Parent]:
				ordered = append(ordered, manifest)
				placed[manifest.ID] = true
				progressed = true
			case !arriving[*manifest.Parent]:
				orphaned = append(orphaned, manifest)
				ordered = append(ordered, manifest)
				placed[manifest.ID] = true
				progressed = true
			default:
				deferred = append(deferred, manifest)
			}
		}
		if !progressed {
			// A parent cycle cannot be written by this program; a hand-edited
			// or damaged store could hold one. Import the rest as roots.
			for _, manifest := range remaining {
				orphaned = append(orphaned, manifest)
				ordered = append(ordered, manifest)
			}
			break
		}
		remaining = deferred
	}
	return ordered, orphaned
}

// lineageFromManifests reconstructs a lineage record from what its manifests
// carry. The newest manifest names the save as it was last committed — a
// rename after the final commit is lost, which is the price of losing the
// record — and the oldest bounds when the lineage began.
func lineageFromManifests(manifests []store.Revision) store.Omnisave {
	newest, oldest := manifests[0], manifests[0]
	for _, manifest := range manifests[1:] {
		if manifest.CreatedAt.After(newest.CreatedAt) {
			newest = manifest
		}
		if manifest.CreatedAt.Before(oldest.CreatedAt) {
			oldest = manifest
		}
	}
	return store.Omnisave{
		ID:                newest.Omnisave.ID,
		GameID:            newest.Game.ID,
		DisplayName:       newest.Omnisave.DisplayName,
		CurrentRevisionID: &newest.ID,
		CreatedAt:         oldest.CreatedAt,
	}
}

// gameFromManifests synthesizes a catalog identity for a game whose record was
// lost along with the database, from the identity its manifests carry. Only a
// game with a title is worth inventing a record for; an identifier alone adds
// nothing a manifest does not already hold.
func gameFromManifests(manifests []store.Revision, knownGames map[string]bool) (store.Game, bool) {
	newest := manifests[0]
	for _, manifest := range manifests[1:] {
		if manifest.CreatedAt.After(newest.CreatedAt) {
			newest = manifest
		}
	}
	game := newest.Game
	if game.ID == "" || game.Title == "" || knownGames[game.ID] {
		return store.Game{}, false
	}
	return store.Game{
		ID:          game.ID,
		Title:       game.Title,
		Platform:    game.Platform,
		Identifiers: game.Identifiers,
	}, true
}

func tableIDs(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM `+table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}
