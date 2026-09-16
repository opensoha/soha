package networkaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

func (r *Repository) VPNScopes(ctx context.Context, filter domain.VPNDashboardFilter) ([]domain.VPNScope, error) {
	query := `SELECT DISTINCT d.id, v.configuration->>'siteId', v.configuration->>'networkSpaceId'
 FROM network_vpn_documents d JOIN network_vpn_document_revisions v ON v.document_id = d.id
 WHERE d.tenant_id = 'default' AND d.workspace_id = 'default' AND d.kind = 'profile'`
	args := []any{}
	query, args = addFilter(query, args, "d.id", filter.ProfileID)
	query, args = addFilter(query, args, "v.configuration->>'siteId'", filter.SiteID)
	query += ` ORDER BY 1, 2, 3 LIMIT 5001`
	return queryMany(ctx, r.db, query, args, func(row scanner) (domain.VPNScope, error) {
		var scope domain.VPNScope
		err := row.Scan(&scope.ProfileID, &scope.SiteID, &scope.NetworkSpaceID)
		return scope, err
	})
}

// Scope joins precede filtering and LIMIT. Empty scope always returns no data.
func (r *Repository) VPNDashboardRecords(ctx context.Context, q domain.VPNDashboardQuery, now time.Time) ([]domain.VPNDashboardRecord, error) {
	if len(q.Scopes) == 0 {
		return []domain.VPNDashboardRecord{}, nil
	}
	raw, err := json.Marshal(q.Scopes)
	if err != nil {
		return nil, err
	}
	query := `WITH authorized AS (SELECT * FROM jsonb_to_recordset(?::jsonb) AS b("profileId" text, "siteId" text, "networkSpaceId" text))
 SELECT d.payload, d.site_id, d.network_space_id, d.state, d.updated_at, d.established_at, i.runtime_id,
 COALESCE(g.runtime_id, ''), s.status, s.valid_until,
 CASE WHEN s.id IS NOT NULL THEN jsonb_build_object(
 'sessionId', s.id, 'profileId', d.profile_id, 'profileName', v.configuration->>'name',
 'subjectId', d.subject_id, 'deviceId', d.device_id, 'gatewayId', d.gateway_id,
 'gatewayName', g.name, 'providerCode', g.provider_code, 'selection', s.vpn_selection,
 'mode', s.mode, 'state', d.state, 'reasonCode', COALESCE(s.revoke_reason,d.payload->>'reasonCode',''),
 'startedAt', s.created_at, 'endedAt', s.revoked_at, 'decisionId', d.id,
 'tunnelIP', COALESCE((SELECT host(p.overlay_address) FROM network_wireguard_peers p WHERE p.session_id=s.id ORDER BY p.created_at DESC LIMIT 1),'')) END
 FROM network_vpn_decisions d
 JOIN authorized b ON d.profile_id=b."profileId" AND d.site_id=b."siteId" AND d.network_space_id=b."networkSpaceId"
 JOIN network_vpn_connection_intents i ON i.id=d.id
 JOIN network_vpn_document_revisions v ON v.document_id=d.profile_id AND v.revision=i.profile_revision
 LEFT JOIN network_runtime_sessions s ON s.id=d.session_id
 LEFT JOIN network_access_gateways g ON g.id=d.gateway_id
 WHERE d.tenant_id='default' AND d.workspace_id='default'
 AND d.created_at < ? AND (d.created_at >= ? OR s.valid_until > ?)`
	args := []any{string(raw), q.To, q.From, q.From}
	for _, filter := range []struct{ column, value string }{{"d.profile_id", q.ProfileID}, {"d.site_id", q.SiteID}, {"d.gateway_id", q.GatewayID}, {"d.subject_id", q.SubjectID}, {"d.device_id", q.DeviceID}, {"d.id", q.DecisionID}, {"g.provider_code", q.ProviderCode}} {
		query, args = addFilter(query, args, filter.column, filter.value)
	}
	if q.TeamID != "" {
		query += ` AND EXISTS (SELECT 1 FROM user_team_bindings t WHERE t.user_id::text=d.subject_id AND t.team_id=?)`
		args = append(args, q.TeamID)
	}
	query += ` ORDER BY d.created_at DESC, d.id DESC LIMIT 2001`
	return queryMany(ctx, r.db, query, args, func(row scanner) (domain.VPNDashboardRecord, error) {
		var item domain.VPNDashboardRecord
		var payload, connection []byte
		var state, gatewayRuntime string
		var status *string
		var validUntil *time.Time
		var updated time.Time
		var site, space string
		if err := row.Scan(&payload, &site, &space, &state, &updated, &item.EstablishedAt, &item.RuntimeID, &gatewayRuntime, &status, &validUntil, &connection); err != nil {
			return item, err
		}
		if err := json.Unmarshal(payload, &item.Decision); err != nil {
			return item, err
		}
		item.Decision.SiteID, item.Decision.NetworkSpaceID, item.Decision.State, item.Decision.UpdatedAt = site, space, state, updated
		if len(connection) > 0 {
			if err := json.Unmarshal(connection, &item.Connection); err != nil {
				return item, fmt.Errorf("decode VPN connection: %w", err)
			}
			item.Connection.RuntimeID, item.Connection.GatewayRuntimeID = item.RuntimeID, gatewayRuntime
			if status != nil && (*status == "revoked" || *status == "expired" || validUntil == nil || !validUntil.After(now)) {
				item.Decision.State = "disconnected"
				if item.EstablishedAt == nil {
					item.Decision.State = "failed"
				}
				if item.Connection.EndedAt == nil {
					item.Connection.EndedAt = validUntil
				}
			}
			item.Connection.State = item.Decision.State
		}
		return item, nil
	})
}
