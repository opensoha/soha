package networkruntime

import (
	"context"
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

func (r *Repository) PublishedEndpointResponsePolicies(ctx context.Context) ([]domain.EndpointResponsePolicy, error) {
	return repo.New(r.db).PublishedEndpointResponsePolicies(ctx)
}

func (r *Repository) EndpointRestriction(ctx context.Context, deviceID, siteID string) (domain.EndpointRestriction, error) {
	return repo.New(r.db).EndpointRestriction(ctx, deviceID, siteID)
}

func (r *Repository) RecordNASAccounting(ctx context.Context, runtimeID string, request networkprotocol.RadiusAccountingRequest, receivedAt time.Time, skew time.Duration) error {
	return repo.New(r.db).RecordNASAccounting(ctx, runtimeID, request, receivedAt, skew)
}
