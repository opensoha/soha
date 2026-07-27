package identityprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) CreateSAMLProvider(ctx context.Context, provider domainprovider.Provider, serviceProvider domainprovider.SAMLServiceProvider, key domainprovider.SAMLSigningKey) (domainprovider.Provider, error) {
	transaction := r.db.WithContext(ctx).Begin()
	if transaction.Error != nil {
		return domainprovider.Provider{}, transaction.Error
	}
	repository := &Repository{db: transaction}
	created, err := repository.CreateProvider(ctx, provider)
	if err == nil {
		err = repository.UpsertSAMLServiceProvider(ctx, serviceProvider)
	}
	if err == nil {
		err = repository.CreateSAMLSigningKey(ctx, key)
	}
	if err != nil {
		_ = transaction.Rollback().Error
		return domainprovider.Provider{}, err
	}
	if err := transaction.Commit().Error; err != nil {
		return domainprovider.Provider{}, err
	}
	return created, nil
}

func (r *Repository) UpdateSAMLProvider(ctx context.Context, provider domainprovider.Provider, serviceProvider domainprovider.SAMLServiceProvider) (domainprovider.Provider, error) {
	transaction := r.db.WithContext(ctx).Begin()
	if transaction.Error != nil {
		return domainprovider.Provider{}, transaction.Error
	}
	repository := &Repository{db: transaction}
	updated, err := repository.UpdateProvider(ctx, provider)
	if err == nil {
		err = repository.UpsertSAMLServiceProvider(ctx, serviceProvider)
	}
	if err != nil {
		_ = transaction.Rollback().Error
		return domainprovider.Provider{}, err
	}
	if err := transaction.Commit().Error; err != nil {
		return domainprovider.Provider{}, err
	}
	return updated, nil
}

func (r *Repository) UpsertSAMLServiceProvider(ctx context.Context, item domainprovider.SAMLServiceProvider) error {
	acs, err := json.Marshal(item.AssertionConsumerServiceURLs)
	if err != nil {
		return fmt.Errorf("marshal SAML ACS URLs: %w", err)
	}
	mappings, err := json.Marshal(item.AttributeMappings)
	if err != nil {
		return fmt.Errorf("marshal SAML attribute mappings: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_saml_service_providers (
			provider_id, entity_id, assertion_consumer_service_urls, name_id_format,
			want_authn_requests_signed, want_assertions_signed, signing_certificate_pem,
			attribute_mappings, created_at, updated_at
		) VALUES (?, ?, ?::jsonb, ?, ?, ?, ?, ?::jsonb, ?, ?)
		ON CONFLICT (provider_id) DO UPDATE SET
			entity_id = EXCLUDED.entity_id,
			assertion_consumer_service_urls = EXCLUDED.assertion_consumer_service_urls,
			name_id_format = EXCLUDED.name_id_format,
			want_authn_requests_signed = EXCLUDED.want_authn_requests_signed,
			want_assertions_signed = EXCLUDED.want_assertions_signed,
			signing_certificate_pem = EXCLUDED.signing_certificate_pem,
			attribute_mappings = EXCLUDED.attribute_mappings,
			updated_at = EXCLUDED.updated_at
	`, item.ProviderID, item.EntityID, string(acs), item.NameIDFormat, item.WantAuthnRequestsSigned,
		item.WantAssertionsSigned, item.SigningCertificatePEM, string(mappings), item.CreatedAt, item.UpdatedAt).Error
}

func (r *Repository) GetSAMLServiceProvider(ctx context.Context, providerID string) (domainprovider.SAMLServiceProvider, error) {
	var item domainprovider.SAMLServiceProvider
	var acs, mappings []byte
	err := r.db.WithContext(ctx).Raw(`
		SELECT provider_id, entity_id, assertion_consumer_service_urls, name_id_format,
		       want_authn_requests_signed, want_assertions_signed, signing_certificate_pem,
		       attribute_mappings, created_at, updated_at
		FROM identity_saml_service_providers WHERE provider_id = ? LIMIT 1
	`, strings.TrimSpace(providerID)).Row().Scan(&item.ProviderID, &item.EntityID, &acs, &item.NameIDFormat,
		&item.WantAuthnRequestsSigned, &item.WantAssertionsSigned, &item.SigningCertificatePEM,
		&mappings, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, fmt.Errorf("%w: SAML service provider not found", apperrors.ErrNotFound)
	}
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal(acs, &item.AssertionConsumerServiceURLs); err != nil {
		return item, err
	}
	if err := json.Unmarshal(mappings, &item.AttributeMappings); err != nil {
		return item, err
	}
	return item, nil
}

func (r *Repository) CreateSAMLSigningKey(ctx context.Context, item domainprovider.SAMLSigningKey) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_saml_signing_keys (
			id, provider_id, encrypted_private_key, certificate_pem, fingerprint_sha256,
			active, not_before, not_after, retire_after, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ProviderID, item.EncryptedPrivateKey, item.CertificatePEM, item.FingerprintSHA256,
		item.Active, item.NotBefore, item.NotAfter, item.RetireAfter, item.CreatedAt).Error
}

func (r *Repository) GetActiveSAMLSigningKey(ctx context.Context, providerID string) (domainprovider.SAMLSigningKey, error) {
	var item domainprovider.SAMLSigningKey
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, provider_id, encrypted_private_key, certificate_pem, fingerprint_sha256,
		       active, not_before, not_after, retire_after, created_at
		FROM identity_saml_signing_keys
		WHERE provider_id = ? AND active = TRUE AND not_before <= ? AND not_after > ?
		ORDER BY created_at DESC LIMIT 1
	`, strings.TrimSpace(providerID), time.Now().UTC(), time.Now().UTC()).Row().Scan(
		&item.ID, &item.ProviderID, &item.EncryptedPrivateKey, &item.CertificatePEM, &item.FingerprintSHA256,
		&item.Active, &item.NotBefore, &item.NotAfter, &item.RetireAfter, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return item, fmt.Errorf("%w: active SAML signing key not found", apperrors.ErrNotFound)
	}
	return item, err
}

func (r *Repository) GetSAMLSigningKey(ctx context.Context, certificateID string) (domainprovider.SAMLSigningKey, error) {
	return r.getSAMLSigningKey(ctx, r.db, certificateID, false)
}

func (r *Repository) ListSAMLMetadataSigningKeys(ctx context.Context, providerID string, now time.Time) ([]domainprovider.SAMLSigningKey, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, provider_id, encrypted_private_key, certificate_pem, fingerprint_sha256,
		       active, not_before, not_after, retire_after, created_at
		FROM identity_saml_signing_keys
		WHERE provider_id = ? AND not_before <= ? AND not_after > ?
		  AND (active = TRUE OR retire_after > ?)
		ORDER BY active DESC, created_at DESC
	`, strings.TrimSpace(providerID), now, now, now).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainprovider.SAMLSigningKey, 0)
	for rows.Next() {
		item, scanErr := scanSAMLSigningKey(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) RotateSAMLSigningKey(ctx context.Context, certificateID string, next domainprovider.SAMLSigningKey, retireAfter time.Time) (domainprovider.SAMLSigningKey, domainprovider.SAMLSigningKey, error) {
	var retiring domainprovider.SAMLSigningKey
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		retiring, err = r.getSAMLSigningKey(ctx, tx, certificateID, true)
		if err != nil {
			return err
		}
		if !retiring.Active || retiring.ProviderID != next.ProviderID {
			return fmt.Errorf("%w: SAML certificate is not the active provider certificate", apperrors.ErrConflict)
		}
		result := tx.Exec(`UPDATE identity_saml_signing_keys SET active = FALSE, retire_after = ? WHERE id = ? AND active = TRUE`, retireAfter, retiring.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: SAML certificate was already rotated", apperrors.ErrConflict)
		}
		return (&Repository{db: tx}).CreateSAMLSigningKey(ctx, next)
	})
	if err != nil {
		return domainprovider.SAMLSigningKey{}, domainprovider.SAMLSigningKey{}, err
	}
	retiring.Active, retiring.RetireAfter = false, &retireAfter
	return retiring, next, nil
}

func (r *Repository) getSAMLSigningKey(ctx context.Context, db *gorm.DB, certificateID string, forUpdate bool) (domainprovider.SAMLSigningKey, error) {
	query := `SELECT id, provider_id, encrypted_private_key, certificate_pem, fingerprint_sha256, active, not_before, not_after, retire_after, created_at FROM identity_saml_signing_keys WHERE id = ? LIMIT 1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	item, err := scanSAMLSigningKey(db.WithContext(ctx).Raw(query, strings.TrimSpace(certificateID)).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return item, fmt.Errorf("%w: SAML certificate not found", apperrors.ErrNotFound)
	}
	return item, err
}

func scanSAMLSigningKey(row interface{ Scan(...any) error }) (domainprovider.SAMLSigningKey, error) {
	var item domainprovider.SAMLSigningKey
	err := row.Scan(&item.ID, &item.ProviderID, &item.EncryptedPrivateKey, &item.CertificatePEM, &item.FingerprintSHA256, &item.Active, &item.NotBefore, &item.NotAfter, &item.RetireAfter, &item.CreatedAt)
	return item, err
}

func (r *Repository) ConsumeSAMLReplayKey(ctx context.Context, providerID, kind, replayKey string, expiresAt time.Time) error {
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_saml_replay_keys (replay_key, provider_id, kind, expires_at)
		VALUES (?, ?, ?, ?) ON CONFLICT (replay_key) DO NOTHING
	`, strings.TrimSpace(replayKey), strings.TrimSpace(providerID), strings.TrimSpace(kind), expiresAt)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: SAML %s was already consumed", apperrors.ErrConflict, kind)
	}
	return nil
}

func (r *Repository) CreateSAMLPendingRequest(ctx context.Context, item domainprovider.SAMLPendingRequest) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM identity_saml_pending_requests WHERE expires_at <= ?`, item.CreatedAt).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_saml_pending_requests (
			token, provider_id, method, encoded_request, relay_state, raw_query, expires_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, item.Token, item.ProviderID, item.Method, item.Encoded, item.RelayState, item.RawQuery, item.ExpiresAt, item.CreatedAt).Error
}

func (r *Repository) ConsumeSAMLPendingRequest(ctx context.Context, token, providerID string, now time.Time) (domainprovider.SAMLPendingRequest, error) {
	var item domainprovider.SAMLPendingRequest
	err := r.db.WithContext(ctx).Raw(`
		DELETE FROM identity_saml_pending_requests
		WHERE token = ? AND provider_id = ? AND expires_at > ?
		RETURNING token, provider_id, method, encoded_request, relay_state, raw_query, expires_at, created_at
	`, token, providerID, now).Row().Scan(
		&item.Token, &item.ProviderID, &item.Method, &item.Encoded, &item.RelayState,
		&item.RawQuery, &item.ExpiresAt, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return item, fmt.Errorf("%w: SAML login request is missing or expired", apperrors.ErrUnauthorized)
	}
	return item, err
}
