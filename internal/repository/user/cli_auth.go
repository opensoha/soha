package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) WithinCLITransaction(ctx context.Context, apply func(context.Context) error) error {
	return dbtx.Within(ctx, r.db, apply)
}

func (r *Repository) CreateCLIGrant(ctx context.Context, grant domainidentity.CLIGrant, sourceLimit, globalLimit int) (bool, error) {
	db := dbtx.DB(ctx, r.db)
	// Serialize admission across replicas, including transactions on different IPs.
	if err := db.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('cli-grant-admission', 0))`).Error; err != nil {
		return false, err
	}
	var total, source int
	if err := db.Raw(`SELECT COUNT(*), COUNT(*) FILTER (WHERE source_hash = ? AND state IN ('pending','approved')) FROM cli_device_authorizations WHERE expires_at > CURRENT_TIMESTAMP`, grant.SourceHash).Row().Scan(&total, &source); err != nil {
		return false, err
	}
	if total >= globalLimit || source >= sourceLimit {
		return false, nil
	}
	raw, err := json.Marshal(grant)
	if err != nil {
		return false, err
	}
	err = db.Exec(`INSERT INTO cli_device_authorizations (id,device_hash,handle_hash,source_hash,state,expires_at,payload) VALUES (?,?,?,?,?,?,?::jsonb)`, grant.ID, grant.DeviceHash, grant.HandleHash, grant.SourceHash, grant.State, grant.ExpiresAt, string(raw)).Error
	return err == nil, err
}

func (r *Repository) GetCLIGrant(ctx context.Context, kind, hash string) (domainidentity.CLIGrant, error) {
	query := `SELECT payload FROM cli_device_authorizations WHERE device_hash = ? FOR UPDATE`
	if kind == "handle" {
		query = `SELECT payload FROM cli_device_authorizations WHERE handle_hash = ? FOR UPDATE`
	} else if kind != "device" {
		return domainidentity.CLIGrant{}, apperrors.ErrInvalidArgument
	}
	var raw []byte
	err := dbtx.DB(ctx, r.db).Raw(query, hash).Row().Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domainidentity.CLIGrant{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domainidentity.CLIGrant{}, err
	}
	var grant domainidentity.CLIGrant
	if err := json.Unmarshal(raw, &grant); err != nil {
		return grant, fmt.Errorf("decode CLI grant: %w", err)
	}
	return grant, nil
}

func (r *Repository) SaveCLIGrant(ctx context.Context, grant domainidentity.CLIGrant) error {
	raw, err := json.Marshal(grant)
	if err != nil {
		return err
	}
	result := dbtx.DB(ctx, r.db).Exec(`UPDATE cli_device_authorizations SET state=?, expires_at=?, receipt_expires_at=?, payload=?::jsonb WHERE id=?`, grant.State, grant.ExpiresAt, nullableCLITime(grant.ReceiptExpiresAt), string(raw), grant.ID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *Repository) GetCLIRefresh(ctx context.Context, hash string) (domainidentity.CLIRefreshCredential, error) {
	// Lock the existing session first. Two generations in the same family must
	// serialize with each other and with ordinary session revocation.
	var sessionID string
	db := dbtx.DB(ctx, r.db)
	if err := db.Raw(`SELECT s.id FROM sessions s JOIN cli_refresh_credentials c ON c.session_id=s.id WHERE c.hash=? FOR UPDATE OF s`, hash).Row().Scan(&sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainidentity.CLIRefreshCredential{}, apperrors.ErrNotFound
		}
		return domainidentity.CLIRefreshCredential{}, err
	}
	var record domainidentity.CLIRefreshCredential
	var receiptUntil sql.NullTime
	err := db.Raw(`SELECT hash,session_id,request_hash,receipt,receipt_expires_at,expires_at,generation,replacement_hash,issued_at,consumed_at FROM cli_refresh_credentials WHERE hash=?`, hash).Row().Scan(&record.Hash, &record.SessionID, &record.RequestHash, &record.Receipt, &receiptUntil, &record.ExpiresAt, &record.Generation, &record.ReplacementHash, &record.IssuedAt, &record.ConsumedAt)
	if err != nil {
		return record, err
	}
	if receiptUntil.Valid {
		record.ReceiptExpiresAt = receiptUntil.Time
	}
	return record, nil
}

func (r *Repository) SaveCLIRefresh(ctx context.Context, record domainidentity.CLIRefreshCredential) error {
	return dbtx.DB(ctx, r.db).Exec(`INSERT INTO cli_refresh_credentials (hash,session_id,request_hash,receipt,receipt_expires_at,expires_at,generation,replacement_hash,issued_at,consumed_at) VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT(hash) DO UPDATE SET request_hash=EXCLUDED.request_hash,receipt=EXCLUDED.receipt,receipt_expires_at=EXCLUDED.receipt_expires_at,replacement_hash=EXCLUDED.replacement_hash,consumed_at=EXCLUDED.consumed_at`, record.Hash, record.SessionID, record.RequestHash, record.Receipt, nullableCLITime(record.ReceiptExpiresAt), record.ExpiresAt, record.Generation, record.ReplacementHash, record.IssuedAt, record.ConsumedAt).Error
}

func (r *Repository) ConsumeCLIBudget(ctx context.Context, bucket string, limit int, window time.Duration) (bool, error) {
	var count int
	err := dbtx.DB(ctx, r.db).Raw(`INSERT INTO cli_auth_budgets (bucket,count,expires_at) VALUES (?,1,CURRENT_TIMESTAMP + ? * INTERVAL '1 second')
 ON CONFLICT(bucket) DO UPDATE SET count=CASE WHEN cli_auth_budgets.expires_at <= CURRENT_TIMESTAMP THEN 1 ELSE cli_auth_budgets.count+1 END,
 expires_at=CASE WHEN cli_auth_budgets.expires_at <= CURRENT_TIMESTAMP THEN EXCLUDED.expires_at ELSE cli_auth_budgets.expires_at END
 WHERE cli_auth_budgets.expires_at <= CURRENT_TIMESTAMP OR cli_auth_budgets.count < ? RETURNING count`, bucket, int64(window/time.Second), limit).Row().Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) UpdateCLISession(ctx context.Context, id string, expiresAt, lastSeenAt time.Time, authzVersion int64) error {
	result := dbtx.DB(ctx, r.db).Exec(`UPDATE sessions SET expires_at=?, last_seen_at=?, authz_version=?, updated_at=? WHERE id=? AND provider_type='cli_v2' AND status='active'`, expiresAt, lastSeenAt, authzVersion, lastSeenAt, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrUnauthorized
	}
	return nil
}

func (r *Repository) ListCLISessionRecords(ctx context.Context, userID, beforeID string, limit int) ([]domainidentity.SessionRecord, error) {
	// UUID cursor is bound to an existing session owned by this same user.
	query := `SELECT id FROM sessions WHERE user_id=? AND provider_type='cli_v2'`
	values := []any{userID}
	if beforeID != "" {
		query += ` AND (created_at,id) < (SELECT created_at,id FROM sessions WHERE id=? AND user_id=? AND provider_type='cli_v2')`
		values = append(values, beforeID, userID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	values = append(values, limit)
	rows, err := dbtx.DB(ctx, r.db).Raw(query, values...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	out := make([]domainidentity.SessionRecord, 0, len(ids))
	for _, id := range ids {
		item, err := r.GetSessionByID(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *Repository) CleanupCLIAuth(ctx context.Context) error {
	db := dbtx.DB(ctx, r.db)
	// Batches bound lock duration and table scans. Expiry is always checked by
	// authentication itself; cleanup cannot revive an expired credential.
	queries := []string{
		`UPDATE cli_device_authorizations SET payload=jsonb_set(jsonb_set(payload,'{Receipt}','""'::jsonb),'{ReceiptExpiresAt}','"0001-01-01T00:00:00Z"'::jsonb),receipt_expires_at=NULL WHERE id IN (SELECT id FROM cli_device_authorizations WHERE receipt_expires_at <= CURRENT_TIMESTAMP LIMIT 2000)`,
		`UPDATE cli_refresh_credentials SET receipt='',receipt_expires_at=NULL WHERE hash IN (SELECT hash FROM cli_refresh_credentials WHERE receipt_expires_at <= CURRENT_TIMESTAMP LIMIT 2000)`,
		`DELETE FROM cli_device_authorizations WHERE id IN (SELECT id FROM cli_device_authorizations WHERE expires_at < CURRENT_TIMESTAMP - INTERVAL '5 minutes' AND (receipt_expires_at IS NULL OR receipt_expires_at <= CURRENT_TIMESTAMP) LIMIT 2000)`,
		`DELETE FROM cli_refresh_credentials WHERE hash IN (SELECT hash FROM cli_refresh_credentials WHERE expires_at <= CURRENT_TIMESTAMP LIMIT 2000)`,
		`DELETE FROM cli_auth_budgets WHERE bucket IN (SELECT bucket FROM cli_auth_budgets WHERE expires_at <= CURRENT_TIMESTAMP LIMIT 2000)`,
	}
	for _, query := range queries {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	return nil
}

func nullableCLITime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func (r *Repository) GetCLIBrowserSession(ctx context.Context, id string) (domainidentity.Session, error) {
	var lockedID string
	if err := dbtx.DB(ctx, r.db).Raw(`SELECT id FROM sessions WHERE id=? FOR UPDATE`, id).Row().Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainidentity.Session{}, apperrors.ErrNotFound
		}
		return domainidentity.Session{}, err
	}
	return r.GetAuthSessionByID(ctx, id)
}
