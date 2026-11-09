package identityprovider

import (
	"context"
	"encoding/json"
	"fmt"

	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *Repository) RecordOutpostClaim(ctx context.Context, item domainprovider.Outpost) (domainprovider.Outpost, error) {
	metadata, err := outpostContactMetadata(item.Metadata)
	if err != nil {
		return domainprovider.Outpost{}, err
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE identity_outposts
		SET status = 'online', version = COALESCE(NULLIF(?, ''), version),
		    last_seen_at = ?, updated_at = ?, metadata = COALESCE(?::jsonb, metadata),
		    claimed_agent_id = ?, protocol_version = COALESCE(NULLIF(?, ''), protocol_version),
		    runtime_version = COALESCE(NULLIF(?, ''), runtime_version)
		WHERE id = ? AND token_hash = ?
	`, item.Version, item.LastSeenAt, item.UpdatedAt, metadata,
		item.ClaimedAgentID, item.ProtocolVersion, item.RuntimeVersion, item.ID, item.TokenHash)
	if result.Error != nil {
		return domainprovider.Outpost{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainprovider.Outpost{}, fmt.Errorf("%w: outpost registration token changed", apperrors.ErrAccessDenied)
	}
	return r.GetOutpost(ctx, item.ID)
}

func (r *Repository) RecordOutpostHeartbeat(ctx context.Context, item domainprovider.Outpost) (domainprovider.Outpost, error) {
	metadata, err := outpostContactMetadata(item.Metadata)
	if err != nil {
		return domainprovider.Outpost{}, err
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE identity_outposts
		SET status = ?, version = COALESCE(NULLIF(?, ''), version),
		    last_seen_at = ?, last_heartbeat_at = ?, updated_at = ?,
		    runtime_version = COALESCE(NULLIF(?, ''), runtime_version),
		    applied_configuration_version = ?, configuration_expires_at = ?,
		    runtime_status = ?, runtime_reason = ?, metadata = COALESCE(?::jsonb, metadata)
		WHERE id = ? AND token_hash = ?
          AND (COALESCE(protocol_version, '') <> 'v1' OR ? = 'legacy_runtime'
            OR (? = 0 AND ?::timestamptz IS NULL AND ? <> 'available')
            OR EXISTS (
              SELECT 1 FROM identity_outpost_runtime_leases l
              WHERE l.outpost_id = identity_outposts.id AND l.token_hash = identity_outposts.token_hash
                AND l.configuration_version = ? AND l.issued_at <= CURRENT_TIMESTAMP
                AND l.expires_at > CURRENT_TIMESTAMP
                AND ?::timestamptz > CURRENT_TIMESTAMP AND ?::timestamptz <= l.expires_at
            ))
	`, item.Status, item.Version, item.LastSeenAt, item.LastHeartbeatAt, item.UpdatedAt,
		item.RuntimeVersion, item.AppliedConfigurationVersion, item.ConfigurationExpiresAt,
		item.RuntimeStatus, item.RuntimeReason, metadata, item.ID, item.TokenHash,
		item.RuntimeReason, item.AppliedConfigurationVersion, item.ConfigurationExpiresAt, item.RuntimeStatus,
		item.AppliedConfigurationVersion, item.ConfigurationExpiresAt, item.ConfigurationExpiresAt)
	if result.Error != nil {
		return domainprovider.Outpost{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainprovider.Outpost{}, fmt.Errorf("%w: outpost heartbeat token or granted lease changed", apperrors.ErrAccessDenied)
	}
	return r.GetOutpost(ctx, item.ID)
}

func outpostContactMetadata(metadata map[string]any) (any, error) {
	if metadata == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal outpost metadata: %w", err)
	}
	return string(encoded), nil
}

func (r *Repository) RotateOutpostToken(ctx context.Context, item domainprovider.Outpost) (domainprovider.Outpost, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE identity_outposts SET token_hash = ?, updated_by = ?, updated_at = ?,
		    status = 'offline', claimed_agent_id = NULL,
		    runtime_version = NULL, last_heartbeat_at = NULL, configuration_expires_at = NULL,
		    applied_configuration_version = 0, runtime_status = 'unavailable', runtime_reason = ''
		WHERE id = ?
	`, item.TokenHash, item.UpdatedBy, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainprovider.Outpost{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainprovider.Outpost{}, fmt.Errorf("%w: identity outpost not found", apperrors.ErrNotFound)
	}
	return r.GetOutpost(ctx, item.ID)
}

// An unsigned check is only for a legacy remote registration. A signed
// registration must still present a current lease after a credential rotation.
func (r *Repository) AuthorizeLegacyOutpostAccess(ctx context.Context, id, tokenHash string) error {
	var allowed bool
	err := r.db.WithContext(ctx).Raw(`
		SELECT EXISTS (
			SELECT 1 FROM identity_outposts o
			WHERE o.id = ? AND o.token_hash = ?
			  AND o.mode IN ('agent', 'kubernetes', 'external')
			  AND COALESCE(o.protocol_version, '') = ''
			  AND NOT EXISTS (SELECT 1 FROM identity_outpost_runtime_leases l WHERE l.outpost_id = o.id)
			  AND NOT EXISTS (
				SELECT 1 FROM identity_outpost_runtime_deployments d
				WHERE d.outpost_id = o.id AND d.state->>'owner' IS DISTINCT FROM 'external'
			  )
		)
	`, id, tokenHash).Row().Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("%w: outpost requires a signed runtime configuration", apperrors.ErrAccessDenied)
	}
	return nil
}
