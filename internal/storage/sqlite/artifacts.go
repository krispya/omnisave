package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/krisbaumgartner/omnisave/internal/storage"
	"github.com/krisbaumgartner/omnisave/internal/storage/store"
)

// Artifact content rests in the save store, which verifies every hash it writes
// and reads. The artifacts table is the online availability registry: a
// reference may be created only while a matching row says the object is
// available. Portable manifests independently carry sizes for recovery.

func (r *Repository) storeArtifact(artifact storage.Artifact, payload io.Reader) error {
	if !store.ValidHash(artifact.SHA256) || artifact.Size < 0 {
		return fmt.Errorf("%w: invalid descriptor", storage.ErrArtifactMismatch)
	}
	// The streaming write happens outside the mutation lock — an upload lasts
	// as long as the client's connection, and objects are content-addressed,
	// so concurrent writers of the same bytes are harmless.
	_, err := r.store.PutObject(artifact.SHA256, payload)
	if err != nil {
		if errors.Is(err, store.ErrContentMismatch) {
			return fmt.Errorf("%w: payload does not match descriptor", storage.ErrArtifactMismatch)
		}
		return err
	}

	// Publishing the availability capability is a short database transaction.
	// Reclamation uses the same transaction boundary, so publication and
	// removal have one serial order even across repository processes.
	r.mutate.Lock()
	defer r.mutate.Unlock()
	tx, err := r.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Acquire the SQLite write order before the final object verification.
	// Reclamation uses the same order, so another process cannot remove the
	// object between this proof and publication of the capability.
	if _, err := tx.Exec(`INSERT INTO artifacts(sha256, size, available) VALUES (?, ?, 0)
		ON CONFLICT(sha256) DO NOTHING`, artifact.SHA256, artifact.Size); err != nil {
		return err
	}
	measured, err := r.store.VerifyObject(artifact.SHA256)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return storage.ErrNotFound
		}
		return err
	}
	if measured != artifact.Size {
		return fmt.Errorf("%w: payload does not match descriptor", storage.ErrArtifactMismatch)
	}
	if _, err := tx.Exec(`UPDATE artifacts SET size = ?, available = 1 WHERE sha256 = ?`,
		measured, artifact.SHA256); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) openArtifact(hash string) (io.ReadCloser, error) {
	reader, err := r.store.OpenObject(hash)
	if errors.Is(err, store.ErrNotFound) {
		return nil, storage.ErrNotFound
	}
	return reader, err
}

func (r *Repository) statArtifact(hash string) (int64, error) {
	var size int64
	err := r.db.QueryRow(`SELECT size FROM artifacts WHERE sha256 = ? AND available = 1`, hash).Scan(&size)
	if err == nil && r.store.HasObject(hash) {
		return size, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	// A restored store can hold an object before its availability registry has
	// been rebuilt. Publish that capability under the same lock and transaction
	// used by uploads; Stat remains advisory, while reference creation checks
	// again in its own transaction.
	r.mutate.Lock()
	defer r.mutate.Unlock()
	tx, err := r.db.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO artifacts(sha256, size, available) VALUES (?, 0, 0)
		ON CONFLICT(sha256) DO NOTHING`, hash); err != nil {
		return 0, err
	}
	size, err = r.store.VerifyObject(hash)
	if errors.Is(err, store.ErrNotFound) {
		return 0, storage.ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE artifacts SET size = ?, available = 1 WHERE sha256 = ?`, size, hash); err != nil {
		return 0, err
	}
	return size, tx.Commit()
}

// requireAvailableArtifacts turns availability into a transaction-local
// capability. Every reference-creating path calls it; service preflight checks
// can improve an error but can never authorize a reference.
func (r *Repository) requireAvailableArtifacts(
	ctx context.Context,
	tx *sql.Tx,
	descriptors map[string]int64,
) error {
	missing := make([]string, 0)
	for hash, expectedSize := range descriptors {
		var size int64
		err := tx.QueryRowContext(ctx,
			`SELECT size FROM artifacts WHERE sha256 = ? AND available = 1`, hash).Scan(&size)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (size != expectedSize || !r.store.HasObject(hash)) {
			missing = append(missing, hash)
			continue
		}
		if err != nil {
			return err
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return &storage.ArtifactsUnavailable{SHA256: missing}
}

// reclaimArtifacts removes candidate content that nothing references any
// longer. A deletion gathers candidates inside its transaction, but liveness
// is decided here, at removal time, under the mutation lock: a commit or a
// media save landing after that transaction would otherwise re-reference a
// hash already condemned, and the removal would take content a surviving
// revision names. Failures are logged, not returned — the deletion they
// trail has durably happened, and the next open's sweep reclaims whatever
// could not be removed now.
func (r *Repository) reclaimArtifacts(ctx context.Context, candidates []string) {
	for _, hash := range candidates {
		tx, err := r.db.BeginTx(context.WithoutCancel(ctx), nil)
		if err != nil {
			r.noteStoreLag("reclamation of object "+hash, err)
			continue
		}
		var used bool
		if err := tx.QueryRowContext(context.WithoutCancel(ctx),
			`SELECT
				EXISTS(SELECT 1 FROM revision_files WHERE artifact_sha256 = ?) OR
				EXISTS(SELECT 1 FROM game_media WHERE sha256 = ?)`, hash, hash,
		).Scan(&used); err != nil {
			tx.Rollback()
			r.noteStoreLag("reclamation of object "+hash, err)
			continue
		}
		if used {
			tx.Rollback()
			continue
		}
		if _, err := tx.Exec(`UPDATE artifacts SET available = 0 WHERE sha256 = ?`, hash); err != nil {
			tx.Rollback()
			r.noteStoreLag("reclamation of object "+hash, err)
			continue
		}
		quarantined, err := r.store.QuarantineObject(hash)
		if err != nil {
			tx.Rollback()
			r.noteStoreLag("reclamation of object "+hash, err)
			continue
		}
		if _, err := tx.Exec(`DELETE FROM artifacts WHERE sha256 = ?`, hash); err != nil {
			tx.Rollback()
			if quarantined {
				r.noteStoreLag("rollback of object reclamation "+hash, r.store.RestoreQuarantinedObject(hash))
			}
			r.noteStoreLag("reclamation of object "+hash, err)
			continue
		}
		if err := tx.Commit(); err != nil {
			if quarantined {
				r.noteStoreLag("rollback of object reclamation "+hash, r.store.RestoreQuarantinedObject(hash))
			}
			r.noteStoreLag("reclamation of object "+hash, err)
			continue
		}
		if quarantined {
			r.noteStoreLag("finalization of object reclamation "+hash, r.store.FinalizeQuarantinedObject(hash))
		}
	}
}
