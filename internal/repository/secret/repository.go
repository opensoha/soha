package secret

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, filter domainsecret.Filter) ([]domainsecret.Secret, error) {
	query := `SELECT id, name, description, scope_type, scope_id, status, current_version, bindings, created_by, created_at, updated_at FROM secrets WHERE 1=1`
	args := make([]any, 0, 2)
	if filter.ScopeType != "" {
		query += ` AND scope_type = ?`
		args = append(args, filter.ScopeType)
	}
	if filter.ScopeID != "" {
		query += ` AND scope_id = ?`
		args = append(args, filter.ScopeID)
	}
	query += ` ORDER BY updated_at DESC, id ASC`
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainsecret.Secret, 0)
	for rows.Next() {
		item, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (domainsecret.Secret, error) {
	return getSecret(r.db.WithContext(ctx), id)
}

func (r *Repository) Create(ctx context.Context, item domainsecret.Secret, version domainsecret.Version) (domainsecret.Secret, error) {
	bindings, err := json.Marshal(item.Bindings)
	if err != nil {
		return domainsecret.Secret{}, fmt.Errorf("marshal secret bindings: %w", err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO secrets (id, name, description, scope_type, scope_id, status, current_version, bindings, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?::json, ?, ?, ?)
		`, item.ID, item.Name, item.Description, item.ScopeType, item.ScopeID, item.Status, item.CurrentVersion, string(bindings), item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return conflictError(err, "secret name already exists in this scope")
		}
		return tx.Exec(`
			INSERT INTO secret_versions (secret_id, version, source_type, ciphertext, vault_mount, vault_path, vault_key, vault_version, status, created_by, created_at, revoked_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, version.SecretID, version.Version, versionSource(version), nullableCiphertext(version), vaultMount(version), vaultPath(version), vaultKey(version), vaultVersion(version), version.Status, version.CreatedBy, version.CreatedAt, version.RevokedAt).Error
	})
	if err != nil {
		return domainsecret.Secret{}, err
	}
	return item, nil
}

func (r *Repository) Update(ctx context.Context, item domainsecret.Secret) (domainsecret.Secret, error) {
	bindings, err := json.Marshal(item.Bindings)
	if err != nil {
		return domainsecret.Secret{}, fmt.Errorf("marshal secret bindings: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE secrets
		SET name = ?, description = ?, status = ?, bindings = ?::json, updated_at = ?
		WHERE id = ?
	`, item.Name, item.Description, item.Status, string(bindings), item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainsecret.Secret{}, conflictError(result.Error, "secret name already exists in this scope")
	}
	if result.RowsAffected == 0 {
		return domainsecret.Secret{}, fmt.Errorf("%w: secret not found", apperrors.ErrNotFound)
	}
	return item, nil
}

func (r *Repository) ListVersions(ctx context.Context, secretID string) ([]domainsecret.Version, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT secret_id, version, source_type, COALESCE(ciphertext, ''), COALESCE(vault_mount, ''), COALESCE(vault_path, ''), COALESCE(vault_key, ''), COALESCE(vault_version, 0), status, created_by, created_at, revoked_at
		FROM secret_versions WHERE secret_id = ? ORDER BY version DESC
	`, secretID).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainsecret.Version, 0)
	for rows.Next() {
		item, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetVersion(ctx context.Context, secretID string, version int) (domainsecret.Version, error) {
	return getVersion(r.db.WithContext(ctx), secretID, version)
}

func (r *Repository) Rotate(ctx context.Context, secretID string, version domainsecret.Version) (domainsecret.Version, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current int
		var status string
		if err := tx.Raw(`SELECT current_version, status FROM secrets WHERE id = ? FOR UPDATE`, secretID).Row().Scan(&current, &status); err != nil {
			return notFoundError(err)
		}
		if domainsecret.Status(status) != domainsecret.StatusActive {
			return fmt.Errorf("%w: disabled secret cannot be rotated", apperrors.ErrConflict)
		}
		version.Version = current + 1
		if err := tx.Exec(`
			INSERT INTO secret_versions (secret_id, version, source_type, ciphertext, vault_mount, vault_path, vault_key, vault_version, status, created_by, created_at, revoked_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
		`, secretID, version.Version, versionSource(version), nullableCiphertext(version), vaultMount(version), vaultPath(version), vaultKey(version), vaultVersion(version), version.Status, version.CreatedBy, version.CreatedAt).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE secrets SET current_version = ?, updated_at = ? WHERE id = ?`, version.Version, version.CreatedAt, secretID).Error
	})
	return version, err
}

func (r *Repository) RevokeVersion(ctx context.Context, secretID string, version int, revokedAt time.Time) (domainsecret.Version, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current int
		if err := tx.Raw(`SELECT current_version FROM secrets WHERE id = ? FOR UPDATE`, secretID).Row().Scan(&current); err != nil {
			return notFoundError(err)
		}
		if current == version {
			return fmt.Errorf("%w: current secret version cannot be revoked", apperrors.ErrConflict)
		}
		result := tx.Exec(`
			UPDATE secret_versions SET status = ?, revoked_at = ?
			WHERE secret_id = ? AND version = ? AND status = ?
		`, domainsecret.VersionRevoked, revokedAt, secretID, version, domainsecret.VersionActive)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: secret version not found or already revoked", apperrors.ErrConflict)
		}
		return nil
	})
	if err != nil {
		return domainsecret.Version{}, err
	}
	return r.GetVersion(ctx, secretID, version)
}

func (r *Repository) CreateLease(ctx context.Context, lease domainsecret.Lease) error {
	refs, err := json.Marshal(lease.References)
	if err != nil {
		return fmt.Errorf("marshal secret lease refs: %w", err)
	}
	principal, err := json.Marshal(lease.Principal)
	if err != nil {
		return fmt.Errorf("marshal secret lease principal: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO secret_leases (id, token_hash, agent_id, subject_type, subject_id, target_type, target_ref, secret_refs, principal, expires_at, redeemed_at, revoked_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?::json, ?::json, ?, NULL, NULL, ?)
	`, lease.ID, lease.TokenHash, lease.AgentID, lease.SubjectType, lease.SubjectID, lease.Target.Type, lease.Target.Ref, string(refs), string(principal), lease.ExpiresAt, lease.CreatedAt).Error
}

func (r *Repository) RedeemLease(ctx context.Context, id, tokenHash, agentID string, now time.Time) (domainsecret.Lease, error) {
	row := r.db.WithContext(ctx).Raw(`
		UPDATE secret_leases SET redeemed_at = ?
		WHERE id = ? AND token_hash = ? AND agent_id = ? AND redeemed_at IS NULL AND revoked_at IS NULL AND expires_at > ?
		RETURNING id, token_hash, agent_id, subject_type, subject_id, target_type, target_ref, secret_refs, principal, expires_at, redeemed_at, revoked_at, created_at
	`, now, id, tokenHash, agentID, now).Row()
	lease, err := scanLease(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainsecret.Lease{}, fmt.Errorf("%w: secret lease is unavailable", apperrors.ErrNotFound)
	}
	return lease, err
}

func (r *Repository) RevokeSubjectLeases(ctx context.Context, subjectType, subjectID string, now time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE secret_leases SET revoked_at = ?
		WHERE subject_type = ? AND subject_id = ? AND redeemed_at IS NULL AND revoked_at IS NULL
	`, now, subjectType, subjectID).Error
}

type rowScanner interface {
	Scan(...any) error
}

func getSecret(db *gorm.DB, id string) (domainsecret.Secret, error) {
	item, err := scanSecret(db.Raw(`
		SELECT id, name, description, scope_type, scope_id, status, current_version, bindings, created_by, created_at, updated_at
		FROM secrets WHERE id = ?
	`, id).Row())
	if err != nil {
		return domainsecret.Secret{}, notFoundError(err)
	}
	return item, nil
}

func scanSecret(scanner rowScanner) (domainsecret.Secret, error) {
	var item domainsecret.Secret
	var bindings []byte
	if err := scanner.Scan(
		&item.ID, &item.Name, &item.Description, &item.ScopeType, &item.ScopeID, &item.Status, &item.CurrentVersion,
		&bindings, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return domainsecret.Secret{}, err
	}
	if len(bindings) == 0 {
		item.Bindings = []domainsecret.Binding{}
	} else if err := json.Unmarshal(bindings, &item.Bindings); err != nil {
		return domainsecret.Secret{}, fmt.Errorf("unmarshal secret bindings: %w", err)
	}
	return item, nil
}

func getVersion(db *gorm.DB, secretID string, version int) (domainsecret.Version, error) {
	item, err := scanVersion(db.Raw(`
		SELECT secret_id, version, source_type, COALESCE(ciphertext, ''), COALESCE(vault_mount, ''), COALESCE(vault_path, ''), COALESCE(vault_key, ''), COALESCE(vault_version, 0), status, created_by, created_at, revoked_at
		FROM secret_versions WHERE secret_id = ? AND version = ?
	`, secretID, version).Row())
	if err != nil {
		return domainsecret.Version{}, notFoundError(err)
	}
	return item, nil
}

func scanVersion(scanner rowScanner) (domainsecret.Version, error) {
	var item domainsecret.Version
	var vault domainsecret.VaultKV2Reference
	if err := scanner.Scan(
		&item.SecretID, &item.Version, &item.SourceType, &item.Ciphertext,
		&vault.Mount, &vault.Path, &vault.Key, &vault.Version,
		&item.Status, &item.CreatedBy, &item.CreatedAt, &item.RevokedAt,
	); err != nil {
		return domainsecret.Version{}, err
	}
	if item.SourceType == domainsecret.SourceVaultKV2 {
		item.VaultKV2 = &vault
	}
	return item, nil
}

func versionSource(version domainsecret.Version) domainsecret.SourceType {
	if version.SourceType == "" {
		return domainsecret.SourceLocal
	}
	return version.SourceType
}

func nullableCiphertext(version domainsecret.Version) any {
	if versionSource(version) == domainsecret.SourceLocal {
		return version.Ciphertext
	}
	return nil
}

func vaultMount(version domainsecret.Version) any {
	if version.VaultKV2 != nil {
		return version.VaultKV2.Mount
	}
	return nil
}

func vaultPath(version domainsecret.Version) any {
	if version.VaultKV2 != nil {
		return version.VaultKV2.Path
	}
	return nil
}

func vaultKey(version domainsecret.Version) any {
	if version.VaultKV2 != nil {
		return version.VaultKV2.Key
	}
	return nil
}

func vaultVersion(version domainsecret.Version) any {
	if version.VaultKV2 != nil {
		return version.VaultKV2.Version
	}
	return nil
}

func scanLease(scanner rowScanner) (domainsecret.Lease, error) {
	var lease domainsecret.Lease
	var refs, principal []byte
	if err := scanner.Scan(
		&lease.ID, &lease.TokenHash, &lease.AgentID, &lease.SubjectType, &lease.SubjectID, &lease.Target.Type, &lease.Target.Ref,
		&refs, &principal, &lease.ExpiresAt, &lease.RedeemedAt, &lease.RevokedAt, &lease.CreatedAt,
	); err != nil {
		return domainsecret.Lease{}, err
	}
	if err := json.Unmarshal(refs, &lease.References); err != nil {
		return domainsecret.Lease{}, fmt.Errorf("unmarshal secret lease refs: %w", err)
	}
	if err := json.Unmarshal(principal, &lease.Principal); err != nil {
		return domainsecret.Lease{}, fmt.Errorf("unmarshal secret lease principal: %w", err)
	}
	return lease, nil
}

func notFoundError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: secret not found", apperrors.ErrNotFound)
	}
	return err
}

func conflictError(err error, message string) error {
	type sqlState interface{ SQLState() string }
	var state sqlState
	if errors.As(err, &state) && state.SQLState() == "23505" {
		return fmt.Errorf("%w: %s", apperrors.ErrConflict, message)
	}
	return err
}
