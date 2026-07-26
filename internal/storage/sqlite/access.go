package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	"github.com/krisbaumgartner/omnisave/internal/storage"
)

func (r *Repository) InsertCredential(ctx context.Context, record storage.CredentialRecord) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO credentials(
		id, token_hash, kind, device_id, device_name, created_at, last_used_at, revoked_at
	) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)`,
		record.ID, record.TokenHash, string(record.Kind), record.DeviceID, record.DeviceName,
		record.CreatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (r *Repository) InsertFirstCredential(ctx context.Context, record storage.CredentialRecord) error {
	result, err := r.db.ExecContext(ctx, `INSERT INTO credentials(
		id, token_hash, kind, device_id, device_name, created_at, last_used_at, revoked_at
	)
	SELECT ?, ?, ?, ?, ?, ?, NULL, NULL
	WHERE NOT EXISTS (SELECT 1 FROM credentials)`,
		record.ID, record.TokenHash, string(record.Kind), record.DeviceID, record.DeviceName,
		record.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return storage.ErrConflict
	}
	return nil
}

func (r *Repository) FindCredentialByTokenHash(ctx context.Context, tokenHash string) (*access.Credential, error) {
	row := r.db.QueryRowContext(ctx, `SELECT
		id, kind, device_id, device_name, created_at, last_used_at, revoked_at
		FROM credentials WHERE token_hash = ?`, tokenHash)
	return scanCredential(row)
}

func (r *Repository) ListCredentials(ctx context.Context) ([]access.Credential, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT
		id, kind, device_id, device_name, created_at, last_used_at, revoked_at
		FROM credentials ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	credentials := []access.Credential{}
	for rows.Next() {
		credential, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, *credential)
	}
	return credentials, rows.Err()
}

func (r *Repository) TouchCredential(ctx context.Context, id string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE credentials SET last_used_at = ? WHERE id = ?`,
		at.Format(time.RFC3339Nano), id)
	return err
}

// RevokeCredential withdraws a credential without deleting it: the owner's
// list is more useful when a revoked Device stays on it.
func (r *Repository) RevokeCredential(ctx context.Context, id string, at time.Time) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE credentials SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		at.Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// Either no such credential, or it was already revoked. Both mean the
		// same thing to a caller: that credential does not authenticate.
		var exists bool
		if err := r.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM credentials WHERE id = ?)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return storage.ErrNotFound
		}
	}
	return nil
}

func (r *Repository) InsertPairingRequest(ctx context.Context, record storage.PairingRecord) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO pairing_requests(
		id, code, handle_hash, device_id, device_name, platform, source_address,
		status, created_at, expires_at, minted_token, credential_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', NULL)`,
		record.ID, record.Code, record.HandleHash, record.DeviceID, record.DeviceName,
		record.Platform, record.SourceAddress, string(record.Status),
		record.CreatedAt.Format(time.RFC3339Nano), record.ExpiresAt.Format(time.RFC3339Nano),
	)
	return err
}

func (r *Repository) GetPairingRequest(ctx context.Context, id string) (*access.PairingRequest, error) {
	row := r.db.QueryRowContext(ctx, `SELECT
		id, code, device_id, device_name, platform, source_address, status,
		created_at, expires_at
		FROM pairing_requests WHERE id = ?`, id)
	return scanPairingRequest(row)
}

func (r *Repository) ListPendingPairingRequests(ctx context.Context, now time.Time) ([]access.PairingRequest, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT
		id, code, device_id, device_name, platform, source_address, status,
		created_at, expires_at
		FROM pairing_requests
		WHERE status = ? AND expires_at > ?
		ORDER BY created_at ASC, id ASC`,
		string(access.PairingPending), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	requests := []access.PairingRequest{}
	for rows.Next() {
		request, err := scanPairingRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, *request)
	}
	return requests, rows.Err()
}

func (r *Repository) ResolvePairingRequest(
	ctx context.Context, id string, status access.PairingStatus, credentialID, mintedToken string,
) error {
	var credential any
	if credentialID != "" {
		credential = credentialID
	}
	result, err := r.db.ExecContext(ctx,
		`UPDATE pairing_requests SET status = ?, credential_id = ?, minted_token = ?
		 WHERE id = ? AND status = ?`,
		string(status), credential, mintedToken, id, string(access.PairingPending))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// TakePairingToken reads a request by handle hash and takes any token waiting
// on it in the same transaction. Two pollers racing here is exactly the case
// that must not hand out one credential twice.
func (r *Repository) TakePairingToken(ctx context.Context, handleHash string, now time.Time) (*storage.PairingRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var record storage.PairingRecord
	var createdAt, expiresAt, status string
	err = tx.QueryRowContext(ctx, `SELECT
		id, code, device_id, device_name, platform, source_address, status,
		created_at, expires_at, minted_token
		FROM pairing_requests WHERE handle_hash = ?`, handleHash).Scan(
		&record.ID, &record.Code, &record.DeviceID, &record.DeviceName, &record.Platform,
		&record.SourceAddress, &status, &createdAt, &expiresAt, &record.MintedToken,
	)
	if err != nil {
		return nil, translateNotFound(err)
	}
	record.Status = access.PairingStatus(status)
	if record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return nil, err
	}
	if record.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt); err != nil {
		return nil, err
	}

	if record.Status == access.PairingApproved && record.MintedToken != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE pairing_requests SET minted_token = '' WHERE id = ?`, record.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *Repository) CountRecentPairingRequests(ctx context.Context, sourceAddress string, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pairing_requests WHERE source_address = ? AND created_at >= ?`,
		sourceAddress, since.Format(time.RFC3339Nano)).Scan(&count)
	return count, err
}

// DeleteExpiredPairingRequests removes rows past their retention. Requests are
// kept well beyond expiry so the rate limit still counts attempts that came to
// nothing — deleting at expiry would make refusal a way to reset the limit.
func (r *Repository) DeleteExpiredPairingRequests(ctx context.Context, before time.Time) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM pairing_requests WHERE created_at < ?`,
		before.Format(time.RFC3339Nano))
	return err
}

func (r *Repository) GetOwnerPIN(ctx context.Context) (*storage.OwnerPIN, error) {
	var pin storage.OwnerPIN
	var updatedAt string
	err := r.db.QueryRowContext(ctx,
		`SELECT salt, hash, iterations, updated_at FROM owner_pin WHERE id = 1`).
		Scan(&pin.Salt, &pin.Hash, &pin.Iterations, &updatedAt)
	if err != nil {
		return nil, translateNotFound(err)
	}
	if pin.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return nil, err
	}
	return &pin, nil
}

func (r *Repository) SetOwnerPIN(ctx context.Context, pin storage.OwnerPIN) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO owner_pin(id, salt, hash, iterations, updated_at)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			salt = excluded.salt, hash = excluded.hash,
			iterations = excluded.iterations, updated_at = excluded.updated_at`,
		pin.Salt, pin.Hash, pin.Iterations, pin.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (r *Repository) GetOwnerSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM owner_settings WHERE key = ?`, key).Scan(&value)
	return value, translateNotFound(err)
}

func (r *Repository) SetOwnerSetting(ctx context.Context, key, value string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO owner_settings(key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, at.Format(time.RFC3339Nano))
	return err
}

func scanCredential(row scanner) (*access.Credential, error) {
	var credential access.Credential
	var kind, createdAt string
	var lastUsedAt, revokedAt sql.NullString
	err := row.Scan(&credential.ID, &kind, &credential.DeviceID, &credential.DeviceName,
		&createdAt, &lastUsedAt, &revokedAt)
	if err != nil {
		return nil, translateNotFound(err)
	}
	credential.Kind = access.CredentialKind(kind)
	if credential.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return nil, err
	}
	if credential.LastUsedAt, err = parseNullableTime(lastUsedAt); err != nil {
		return nil, err
	}
	if credential.RevokedAt, err = parseNullableTime(revokedAt); err != nil {
		return nil, err
	}
	return &credential, nil
}

func scanPairingRequest(row scanner) (*access.PairingRequest, error) {
	var request access.PairingRequest
	var status, createdAt, expiresAt string
	err := row.Scan(&request.ID, &request.Code, &request.DeviceID, &request.DeviceName,
		&request.Platform, &request.SourceAddress, &status, &createdAt, &expiresAt)
	if err != nil {
		return nil, translateNotFound(err)
	}
	request.Status = access.PairingStatus(status)
	if request.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return nil, err
	}
	if request.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt); err != nil {
		return nil, err
	}
	return &request, nil
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
