package networkruntime

import (
	"context"
	"database/sql"
	"errors"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	repo "github.com/opensoha/soha/internal/repository/networkaccess"
)

func (r *Repository) SaveEndpointPresence(ctx context.Context, record domain.PresenceRecord) (domain.PresenceRecord, error) {
	return repo.New(r.db).SaveEndpointPresence(ctx, record)
}

func (r *Repository) EndpointPresence(ctx context.Context, deviceID string) (domain.PresenceRecord, error) {
	return repo.New(r.db).EndpointPresence(ctx, deviceID)
}

func (r *Repository) StrictUISessionDeadline(ctx context.Context, subjectID, sessionID string, now time.Time) (time.Time, error) {
	var deadline time.Time
	err := r.db.WithContext(ctx).Raw(`SELECT s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.id=? AND s.user_id=? AND s.status='active' AND u.status='active'
		  AND s.provider_type <> 'cli_v2' AND s.authz_version>=u.authz_version AND s.expires_at>?`, sessionID, subjectID, now).Row().Scan(&deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	return deadline, err
}

func (r *Repository) PublishedEndpointResponsePolicies(ctx context.Context) ([]domain.EndpointResponsePolicy, error) {
	return repo.New(r.db).PublishedEndpointResponsePolicies(ctx)
}

func (r *Repository) EndpointRestriction(ctx context.Context, deviceID, siteID string) (domain.EndpointRestriction, error) {
	return repo.New(r.db).EndpointRestriction(ctx, deviceID, siteID)
}

func (r *Repository) RecordNASAccounting(ctx context.Context, runtimeID string, request networkprotocol.RadiusAccountingRequest, receivedAt time.Time, skew time.Duration) error {
	return repo.New(r.db).RecordNASAccounting(ctx, runtimeID, request, receivedAt, skew)
}
