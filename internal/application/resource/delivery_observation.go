package resource

import (
	"context"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
)

type deliveryObservationKey struct{}

type deliveryObservation struct {
	source     *appaccess.Service
	authorizer domainaccess.Authorizer
}

// WithDeliveryObservation shares authorization inputs for one bounded delivery
// read. Each named resource and collection still gets its own policy decision.
func (r *Runtime) WithDeliveryObservation(ctx context.Context) context.Context {
	if ctx.Value(deliveryObservationKey{}) != nil {
		return ctx
	}
	observation := deliveryObservation{}
	if source, ok := r.Workloads.authorizer.(*appaccess.Service); ok && source != nil {
		observation.source = source
		observation.authorizer = appaccess.SnapshotForRead(ctx, source)
	}
	ctx = context.WithValue(ctx, deliveryObservationKey{}, observation)
	if backend, ok := r.Workloads.direct.(interface {
		WithDeliveryPodLists(context.Context) context.Context
	}); ok {
		ctx = backend.WithDeliveryPodLists(ctx)
	}
	return ctx
}

func (s *resourceAccess) readAuthorizer(ctx context.Context) domainaccess.Authorizer {
	if observation, ok := ctx.Value(deliveryObservationKey{}).(deliveryObservation); ok {
		if source, ok := s.authorizer.(*appaccess.Service); ok && source != nil && source == observation.source {
			return observation.authorizer
		}
	}
	return s.authorizer
}
