package directorysync

import (
	"context"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
)

type Connector interface {
	Validate(context.Context, domain.Connection) (domain.Capabilities, error)
	ListOrganizations(context.Context, domain.Connection) ([]domain.Organization, string, error)
	ListPeople(context.Context, domain.Connection) ([]domain.Person, error)
	ListMemberships(context.Context, domain.Connection) ([]domain.Membership, error)
}

type IncrementalConnector interface {
	ResolveDelta(context.Context, domain.Connection, domain.EventEnvelope) (domain.Delta, error)
}

// LocalProjector is implemented by the access/identity integration layer. It must
// preserve manually owned fields and bindings and honor identity suppressions.
type LocalProjector interface {
	Apply(context.Context, domain.Connection, domain.Policy, domain.Plan) error
}

type DeltaProjector interface {
	ApplyDelta(context.Context, domain.Connection, domain.Policy, domain.Delta) error
}
