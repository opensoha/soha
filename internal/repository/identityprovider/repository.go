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
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListProviders(ctx context.Context, filter domainprovider.ProviderFilter) ([]domainprovider.Provider, error) {
	var builder strings.Builder
	args := make([]any, 0)
	builder.WriteString(`
		SELECT id, application_id, name, type, enabled, config, secret_refs, status,
		       created_by, updated_by, created_at, updated_at
		FROM identity_providers
		WHERE 1 = 1
	`)
	if strings.TrimSpace(filter.ApplicationID) != "" {
		builder.WriteString(` AND application_id = ?`)
		args = append(args, strings.TrimSpace(filter.ApplicationID))
	}
	if strings.TrimSpace(filter.Type) != "" {
		builder.WriteString(` AND type = ?`)
		args = append(args, strings.TrimSpace(filter.Type))
	}
	if strings.TrimSpace(filter.Status) != "" {
		builder.WriteString(` AND status = ?`)
		args = append(args, strings.TrimSpace(filter.Status))
	}
	builder.WriteString(` ORDER BY name ASC, id ASC`)
	if filter.Limit > 0 {
		builder.WriteString(` LIMIT ?`)
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		builder.WriteString(` OFFSET ?`)
		args = append(args, filter.Offset)
	}
	rows, err := r.db.WithContext(ctx).Raw(builder.String(), args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domainprovider.Provider, 0)
	for rows.Next() {
		item, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetProvider(ctx context.Context, providerID string) (domainprovider.Provider, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, application_id, name, type, enabled, config, secret_refs, status,
		       created_by, updated_by, created_at, updated_at
		FROM identity_providers
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(providerID)).Row()
	item, err := scanProvider(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainprovider.Provider{}, fmt.Errorf("%w: identity provider not found", apperrors.ErrNotFound)
		}
		return domainprovider.Provider{}, err
	}
	return item, nil
}

func (r *Repository) CreateProvider(ctx context.Context, item domainprovider.Provider) (domainprovider.Provider, error) {
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domainprovider.Provider{}, fmt.Errorf("marshal provider config: %w", err)
	}
	secretRefs, err := marshalJSON(item.SecretRefs)
	if err != nil {
		return domainprovider.Provider{}, fmt.Errorf("marshal provider secret refs: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_providers (
			id, application_id, name, type, enabled, config, secret_refs, status,
			created_by, updated_by, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?, ?, ?)
	`, item.ID, item.ApplicationID, item.Name, item.Type, item.Enabled, config, secretRefs,
		item.Status, item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainprovider.Provider{}, err
	}
	return r.GetProvider(ctx, item.ID)
}

func (r *Repository) UpdateProvider(ctx context.Context, item domainprovider.Provider) (domainprovider.Provider, error) {
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domainprovider.Provider{}, fmt.Errorf("marshal provider config: %w", err)
	}
	secretRefs, err := marshalJSON(item.SecretRefs)
	if err != nil {
		return domainprovider.Provider{}, fmt.Errorf("marshal provider secret refs: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE identity_providers
		SET application_id = ?, name = ?, type = ?, enabled = ?, config = ?::jsonb,
		    secret_refs = ?::jsonb, status = ?, updated_by = ?, updated_at = ?
		WHERE id = ?
	`, item.ApplicationID, item.Name, item.Type, item.Enabled, config, secretRefs,
		item.Status, item.UpdatedBy, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainprovider.Provider{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainprovider.Provider{}, fmt.Errorf("%w: identity provider not found", apperrors.ErrNotFound)
	}
	return r.GetProvider(ctx, item.ID)
}

func (r *Repository) DeleteProvider(ctx context.Context, providerID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM identity_providers WHERE id = ?`, strings.TrimSpace(providerID))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: identity provider not found", apperrors.ErrNotFound)
	}
	return nil
}

func (r *Repository) GetProviderApplication(ctx context.Context, providerID string) (domainportal.Application, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT a.id, a.slug, a.name, a.description, a.icon_url, a.category, a.tags, a.launch_url, a.provider_id,
		       a.provider_type, a.portal_visible, a.featured, a.sort_order, a.status, a.metadata,
		       a.created_by, a.updated_by, a.created_at, a.updated_at
		FROM identity_providers p
		INNER JOIN identity_applications a ON a.id = p.application_id
		WHERE p.id = ?
		LIMIT 1
	`, strings.TrimSpace(providerID)).Row()
	item, err := scanApplication(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainportal.Application{}, fmt.Errorf("%w: identity provider application not found", apperrors.ErrNotFound)
		}
		return domainportal.Application{}, err
	}
	assignments, err := r.listAssignments(ctx, []string{item.ID})
	if err != nil {
		return domainportal.Application{}, err
	}
	item.Assignments = assignments[item.ID]
	return item, nil
}

func (r *Repository) ListOutposts(ctx context.Context, filter domainprovider.OutpostFilter) ([]domainprovider.Outpost, error) {
	var builder strings.Builder
	args := make([]any, 0)
	builder.WriteString(`
		SELECT id, name, mode, endpoint, token_hash, status, version, last_seen_at, metadata,
		       created_by, updated_by, created_at, updated_at
		FROM identity_outposts
		WHERE 1 = 1
	`)
	if strings.TrimSpace(filter.Mode) != "" {
		builder.WriteString(` AND mode = ?`)
		args = append(args, strings.TrimSpace(filter.Mode))
	}
	if strings.TrimSpace(filter.Status) != "" {
		builder.WriteString(` AND status = ?`)
		args = append(args, strings.TrimSpace(filter.Status))
	}
	builder.WriteString(` ORDER BY name ASC, id ASC`)
	if filter.Limit > 0 {
		builder.WriteString(` LIMIT ?`)
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		builder.WriteString(` OFFSET ?`)
		args = append(args, filter.Offset)
	}
	rows, err := r.db.WithContext(ctx).Raw(builder.String(), args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domainprovider.Outpost, 0)
	for rows.Next() {
		item, err := scanOutpost(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetOutpost(ctx context.Context, outpostID string) (domainprovider.Outpost, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, mode, endpoint, token_hash, status, version, last_seen_at, metadata,
		       created_by, updated_by, created_at, updated_at
		FROM identity_outposts
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(outpostID)).Row()
	item, err := scanOutpost(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainprovider.Outpost{}, fmt.Errorf("%w: identity outpost not found", apperrors.ErrNotFound)
		}
		return domainprovider.Outpost{}, err
	}
	return item, nil
}

func (r *Repository) CreateOutpost(ctx context.Context, item domainprovider.Outpost) (domainprovider.Outpost, error) {
	metadata, err := marshalJSON(item.Metadata)
	if err != nil {
		return domainprovider.Outpost{}, fmt.Errorf("marshal outpost metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_outposts (
			id, name, mode, endpoint, token_hash, status, version, last_seen_at, metadata,
			created_by, updated_by, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?)
	`, item.ID, item.Name, item.Mode, nullableString(item.Endpoint), nullableString(item.TokenHash),
		item.Status, nullableString(item.Version), item.LastSeenAt, metadata,
		item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainprovider.Outpost{}, err
	}
	return r.GetOutpost(ctx, item.ID)
}

func (r *Repository) UpdateOutpost(ctx context.Context, item domainprovider.Outpost) (domainprovider.Outpost, error) {
	metadata, err := marshalJSON(item.Metadata)
	if err != nil {
		return domainprovider.Outpost{}, fmt.Errorf("marshal outpost metadata: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE identity_outposts
		SET name = ?, mode = ?, endpoint = ?, token_hash = ?, status = ?, version = ?,
		    last_seen_at = ?, metadata = ?::jsonb, updated_by = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, item.Mode, nullableString(item.Endpoint), nullableString(item.TokenHash), item.Status,
		nullableString(item.Version), item.LastSeenAt, metadata, item.UpdatedBy, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainprovider.Outpost{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainprovider.Outpost{}, fmt.Errorf("%w: identity outpost not found", apperrors.ErrNotFound)
	}
	return r.GetOutpost(ctx, item.ID)
}

func (r *Repository) DeleteOutpost(ctx context.Context, outpostID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM identity_outposts WHERE id = ?`, strings.TrimSpace(outpostID))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: identity outpost not found", apperrors.ErrNotFound)
	}
	return nil
}

func (r *Repository) ListOIDCClients(ctx context.Context, providerID string) ([]domainprovider.OIDCClient, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, provider_id, client_id, client_secret_hash, redirect_uris, allowed_scopes,
		       allowed_grant_types, require_pkce, access_token_ttl_seconds, id_token_ttl_seconds,
		       refresh_token_ttl_seconds, status, created_at, updated_at
		FROM identity_oidc_clients
		WHERE provider_id = ?
		ORDER BY client_id ASC, id ASC
	`, strings.TrimSpace(providerID)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domainprovider.OIDCClient, 0)
	for rows.Next() {
		item, err := scanOIDCClient(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetOIDCClient(ctx context.Context, id string) (domainprovider.OIDCClient, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, provider_id, client_id, client_secret_hash, redirect_uris, allowed_scopes,
		       allowed_grant_types, require_pkce, access_token_ttl_seconds, id_token_ttl_seconds,
		       refresh_token_ttl_seconds, status, created_at, updated_at
		FROM identity_oidc_clients
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	item, err := scanOIDCClient(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainprovider.OIDCClient{}, fmt.Errorf("%w: oidc client not found", apperrors.ErrNotFound)
		}
		return domainprovider.OIDCClient{}, err
	}
	return item, nil
}

func (r *Repository) GetOIDCClientByClientID(ctx context.Context, clientID string) (domainprovider.OIDCClient, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, provider_id, client_id, client_secret_hash, redirect_uris, allowed_scopes,
		       allowed_grant_types, require_pkce, access_token_ttl_seconds, id_token_ttl_seconds,
		       refresh_token_ttl_seconds, status, created_at, updated_at
		FROM identity_oidc_clients
		WHERE client_id = ?
		LIMIT 1
	`, strings.TrimSpace(clientID)).Row()
	item, err := scanOIDCClient(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainprovider.OIDCClient{}, fmt.Errorf("%w: oidc client not found", apperrors.ErrNotFound)
		}
		return domainprovider.OIDCClient{}, err
	}
	return item, nil
}

func (r *Repository) CreateOIDCClient(ctx context.Context, item domainprovider.OIDCClient) (domainprovider.OIDCClient, error) {
	redirectURIs, err := marshalJSON(item.RedirectURIs)
	if err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("marshal redirect uris: %w", err)
	}
	scopes, err := marshalJSON(item.AllowedScopes)
	if err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("marshal allowed scopes: %w", err)
	}
	grantTypes, err := marshalJSON(item.AllowedGrantTypes)
	if err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("marshal allowed grant types: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_oidc_clients (
			id, provider_id, client_id, client_secret_hash, redirect_uris, allowed_scopes,
			allowed_grant_types, require_pkce, access_token_ttl_seconds, id_token_ttl_seconds,
			refresh_token_ttl_seconds, status, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ProviderID, item.ClientID, item.ClientSecretHash, redirectURIs, scopes, grantTypes,
		item.RequirePKCE, item.AccessTokenTTLSeconds, item.IDTokenTTLSeconds, item.RefreshTokenTTLSeconds,
		item.Status, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainprovider.OIDCClient{}, err
	}
	return r.GetOIDCClient(ctx, item.ID)
}

func (r *Repository) UpdateOIDCClient(ctx context.Context, item domainprovider.OIDCClient) (domainprovider.OIDCClient, error) {
	redirectURIs, err := marshalJSON(item.RedirectURIs)
	if err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("marshal redirect uris: %w", err)
	}
	scopes, err := marshalJSON(item.AllowedScopes)
	if err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("marshal allowed scopes: %w", err)
	}
	grantTypes, err := marshalJSON(item.AllowedGrantTypes)
	if err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("marshal allowed grant types: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE identity_oidc_clients
		SET provider_id = ?, client_id = ?, client_secret_hash = ?, redirect_uris = ?::jsonb,
		    allowed_scopes = ?::jsonb, allowed_grant_types = ?::jsonb, require_pkce = ?,
		    access_token_ttl_seconds = ?, id_token_ttl_seconds = ?, refresh_token_ttl_seconds = ?,
		    status = ?, updated_at = ?
		WHERE id = ?
	`, item.ProviderID, item.ClientID, item.ClientSecretHash, redirectURIs, scopes, grantTypes,
		item.RequirePKCE, item.AccessTokenTTLSeconds, item.IDTokenTTLSeconds, item.RefreshTokenTTLSeconds,
		item.Status, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainprovider.OIDCClient{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainprovider.OIDCClient{}, fmt.Errorf("%w: oidc client not found", apperrors.ErrNotFound)
	}
	return r.GetOIDCClient(ctx, item.ID)
}

func (r *Repository) DeleteOIDCClient(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM identity_oidc_clients WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: oidc client not found", apperrors.ErrNotFound)
	}
	return nil
}

func (r *Repository) GetActiveSigningKey(ctx context.Context, providerID string) (domainprovider.SigningKey, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, provider_id, key_id, algorithm, encrypted_private_key, public_jwk, active, created_at, rotated_at
		FROM identity_provider_signing_keys
		WHERE provider_id = ? AND active = TRUE
		ORDER BY created_at DESC
		LIMIT 1
	`, strings.TrimSpace(providerID)).Row()
	item, err := scanSigningKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainprovider.SigningKey{}, fmt.Errorf("%w: oidc signing key not found", apperrors.ErrNotFound)
		}
		return domainprovider.SigningKey{}, err
	}
	return item, nil
}

func (r *Repository) CreateSigningKey(ctx context.Context, item domainprovider.SigningKey) (domainprovider.SigningKey, error) {
	publicJWK, err := marshalJSON(item.PublicJWK)
	if err != nil {
		return domainprovider.SigningKey{}, fmt.Errorf("marshal public jwk: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_provider_signing_keys (
			id, provider_id, key_id, algorithm, encrypted_private_key, public_jwk, active, created_at, rotated_at
		)
		VALUES (?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?)
	`, item.ID, item.ProviderID, item.KeyID, item.Algorithm, item.EncryptedPrivateKey, publicJWK,
		item.Active, item.CreatedAt, item.RotatedAt).Error; err != nil {
		return domainprovider.SigningKey{}, err
	}
	return r.GetActiveSigningKey(ctx, item.ProviderID)
}

func (r *Repository) ListActivePublicKeys(ctx context.Context) ([]domainprovider.SigningKey, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, provider_id, key_id, algorithm, encrypted_private_key, public_jwk, active, created_at, rotated_at
		FROM identity_provider_signing_keys
		WHERE active = TRUE
		ORDER BY created_at DESC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domainprovider.SigningKey, 0)
	for rows.Next() {
		item, err := scanSigningKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateAuthorizationCode(ctx context.Context, item domainprovider.AuthorizationCode) error {
	scopes, err := marshalJSON(item.Scopes)
	if err != nil {
		return fmt.Errorf("marshal authorization code scopes: %w", err)
	}
	metadata, err := marshalJSON(item.Metadata)
	if err != nil {
		return fmt.Errorf("marshal authorization code metadata: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_authorization_codes (
			id, provider_id, client_id, user_id, code_hash, redirect_uri, scopes, nonce,
			code_challenge, code_challenge_method, expires_at, consumed_at, created_at, metadata
		)
		VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?, ?::jsonb)
	`, item.ID, item.ProviderID, item.ClientID, item.UserID, item.CodeHash, item.RedirectURI, scopes,
		nullableString(item.Nonce), nullableString(item.CodeChallenge), nullableString(item.CodeChallengeMethod),
		item.ExpiresAt, item.ConsumedAt, item.CreatedAt, metadata).Error
}

func (r *Repository) GetAuthorizationCode(ctx context.Context, codeHash string, now time.Time) (domainprovider.AuthorizationCode, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, provider_id, client_id, user_id, code_hash, redirect_uri, scopes, nonce,
		       code_challenge, code_challenge_method, expires_at, consumed_at, created_at, metadata
		FROM identity_authorization_codes
		WHERE code_hash = ? AND consumed_at IS NULL AND expires_at > ?
		LIMIT 1
	`, strings.TrimSpace(codeHash), now).Row()
	item, err := scanAuthorizationCode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainprovider.AuthorizationCode{}, fmt.Errorf("%w: authorization code is invalid or expired", apperrors.ErrUnauthorized)
		}
		return domainprovider.AuthorizationCode{}, err
	}
	return item, nil
}

func (r *Repository) ConsumeAuthorizationCode(ctx context.Context, codeHash string, now time.Time) (domainprovider.AuthorizationCode, error) {
	row := r.db.WithContext(ctx).Raw(`
		UPDATE identity_authorization_codes
		SET consumed_at = ?
		WHERE code_hash = ? AND consumed_at IS NULL AND expires_at > ?
		RETURNING id, provider_id, client_id, user_id, code_hash, redirect_uri, scopes, nonce,
		          code_challenge, code_challenge_method, expires_at, consumed_at, created_at, metadata
	`, now, strings.TrimSpace(codeHash), now).Row()
	item, err := scanAuthorizationCode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainprovider.AuthorizationCode{}, fmt.Errorf("%w: authorization code is invalid or expired", apperrors.ErrUnauthorized)
		}
		return domainprovider.AuthorizationCode{}, err
	}
	return item, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProvider(row scanner) (domainprovider.Provider, error) {
	var item domainprovider.Provider
	var configRaw, secretRefsRaw []byte
	if err := row.Scan(
		&item.ID,
		&item.ApplicationID,
		&item.Name,
		&item.Type,
		&item.Enabled,
		&configRaw,
		&secretRefsRaw,
		&item.Status,
		&item.CreatedBy,
		&item.UpdatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainprovider.Provider{}, err
	}
	if err := unmarshalJSON(configRaw, &item.Config, true); err != nil {
		return domainprovider.Provider{}, fmt.Errorf("decode provider config: %w", err)
	}
	if err := unmarshalJSON(secretRefsRaw, &item.SecretRefs, true); err != nil {
		return domainprovider.Provider{}, fmt.Errorf("decode provider secret refs: %w", err)
	}
	return item, nil
}

func scanOutpost(row scanner) (domainprovider.Outpost, error) {
	var item domainprovider.Outpost
	var endpoint, tokenHash, version sql.NullString
	var lastSeenAt sql.NullTime
	var metadataRaw []byte
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.Mode,
		&endpoint,
		&tokenHash,
		&item.Status,
		&version,
		&lastSeenAt,
		&metadataRaw,
		&item.CreatedBy,
		&item.UpdatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainprovider.Outpost{}, err
	}
	if endpoint.Valid {
		item.Endpoint = endpoint.String
	}
	if tokenHash.Valid {
		item.TokenHash = tokenHash.String
	}
	if version.Valid {
		item.Version = version.String
	}
	if lastSeenAt.Valid {
		item.LastSeenAt = &lastSeenAt.Time
	}
	if err := unmarshalJSON(metadataRaw, &item.Metadata, true); err != nil {
		return domainprovider.Outpost{}, fmt.Errorf("decode outpost metadata: %w", err)
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanOIDCClient(row scanner) (domainprovider.OIDCClient, error) {
	var item domainprovider.OIDCClient
	var redirectURIsRaw, scopesRaw, grantTypesRaw []byte
	if err := row.Scan(
		&item.ID,
		&item.ProviderID,
		&item.ClientID,
		&item.ClientSecretHash,
		&redirectURIsRaw,
		&scopesRaw,
		&grantTypesRaw,
		&item.RequirePKCE,
		&item.AccessTokenTTLSeconds,
		&item.IDTokenTTLSeconds,
		&item.RefreshTokenTTLSeconds,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainprovider.OIDCClient{}, err
	}
	if err := unmarshalJSON(redirectURIsRaw, &item.RedirectURIs, false); err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("decode redirect uris: %w", err)
	}
	if err := unmarshalJSON(scopesRaw, &item.AllowedScopes, false); err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("decode allowed scopes: %w", err)
	}
	if err := unmarshalJSON(grantTypesRaw, &item.AllowedGrantTypes, false); err != nil {
		return domainprovider.OIDCClient{}, fmt.Errorf("decode allowed grant types: %w", err)
	}
	if item.RedirectURIs == nil {
		item.RedirectURIs = []string{}
	}
	if item.AllowedScopes == nil {
		item.AllowedScopes = []string{}
	}
	if item.AllowedGrantTypes == nil {
		item.AllowedGrantTypes = []string{}
	}
	return item, nil
}

func scanSigningKey(row scanner) (domainprovider.SigningKey, error) {
	var item domainprovider.SigningKey
	var publicJWKRaw []byte
	var rotatedAt sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.ProviderID,
		&item.KeyID,
		&item.Algorithm,
		&item.EncryptedPrivateKey,
		&publicJWKRaw,
		&item.Active,
		&item.CreatedAt,
		&rotatedAt,
	); err != nil {
		return domainprovider.SigningKey{}, err
	}
	if rotatedAt.Valid {
		item.RotatedAt = &rotatedAt.Time
	}
	if err := unmarshalJSON(publicJWKRaw, &item.PublicJWK, true); err != nil {
		return domainprovider.SigningKey{}, fmt.Errorf("decode public jwk: %w", err)
	}
	return item, nil
}

func scanAuthorizationCode(row scanner) (domainprovider.AuthorizationCode, error) {
	var item domainprovider.AuthorizationCode
	var scopesRaw, metadataRaw []byte
	var nonce, codeChallenge, codeChallengeMethod sql.NullString
	var consumedAt sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.ProviderID,
		&item.ClientID,
		&item.UserID,
		&item.CodeHash,
		&item.RedirectURI,
		&scopesRaw,
		&nonce,
		&codeChallenge,
		&codeChallengeMethod,
		&item.ExpiresAt,
		&consumedAt,
		&item.CreatedAt,
		&metadataRaw,
	); err != nil {
		return domainprovider.AuthorizationCode{}, err
	}
	if nonce.Valid {
		item.Nonce = nonce.String
	}
	if codeChallenge.Valid {
		item.CodeChallenge = codeChallenge.String
	}
	if codeChallengeMethod.Valid {
		item.CodeChallengeMethod = codeChallengeMethod.String
	}
	if consumedAt.Valid {
		item.ConsumedAt = &consumedAt.Time
	}
	if err := unmarshalJSON(scopesRaw, &item.Scopes, false); err != nil {
		return domainprovider.AuthorizationCode{}, fmt.Errorf("decode authorization code scopes: %w", err)
	}
	if err := unmarshalJSON(metadataRaw, &item.Metadata, true); err != nil {
		return domainprovider.AuthorizationCode{}, fmt.Errorf("decode authorization code metadata: %w", err)
	}
	if item.Scopes == nil {
		item.Scopes = []string{}
	}
	return item, nil
}

func scanApplication(row scanner) (domainportal.Application, error) {
	var item domainportal.Application
	var tagsRaw, metadataRaw []byte
	var iconURL, providerID sql.NullString
	if err := row.Scan(
		&item.ID,
		&item.Slug,
		&item.Name,
		&item.Description,
		&iconURL,
		&item.Category,
		&tagsRaw,
		&item.LaunchURL,
		&providerID,
		&item.ProviderType,
		&item.PortalVisible,
		&item.Featured,
		&item.SortOrder,
		&item.Status,
		&metadataRaw,
		&item.CreatedBy,
		&item.UpdatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainportal.Application{}, err
	}
	if iconURL.Valid {
		item.IconURL = iconURL.String
	}
	if providerID.Valid {
		item.ProviderID = providerID.String
	}
	if err := unmarshalJSON(tagsRaw, &item.Tags, false); err != nil {
		return domainportal.Application{}, fmt.Errorf("decode application tags: %w", err)
	}
	if err := unmarshalJSON(metadataRaw, &item.Metadata, true); err != nil {
		return domainportal.Application{}, fmt.Errorf("decode application metadata: %w", err)
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func (r *Repository) listAssignments(ctx context.Context, applicationIDs []string) (map[string][]domainportal.ApplicationAssignment, error) {
	out := map[string][]domainportal.ApplicationAssignment{}
	ids := compactStrings(applicationIDs)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, application_id, subject_type, subject_id, effect, created_by, created_at
		FROM identity_application_assignments
		WHERE application_id IN ?
		ORDER BY subject_type ASC, subject_id ASC, effect ASC, id ASC
	`, ids).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domainportal.ApplicationAssignment
		if err := rows.Scan(&item.ID, &item.ApplicationID, &item.SubjectType, &item.SubjectID, &item.Effect, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		out[item.ApplicationID] = append(out[item.ApplicationID], item)
	}
	return out, rows.Err()
}

func marshalJSON(value any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalJSON(raw []byte, out any, objectDefault bool) error {
	if len(raw) == 0 {
		if objectDefault {
			raw = []byte("{}")
		} else {
			raw = []byte("[]")
		}
	}
	return json.Unmarshal(raw, out)
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
