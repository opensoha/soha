package resource

import (
	"context"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

// ResourceCreationBatchRepository is intentionally narrow so orchestration
// does not depend on persistence implementation details.
type ResourceCreationBatchRepository interface {
	Claim(context.Context, string, string, string, string, []domainresource.ResourceCreateExecutionDocument) (domainresource.ResourceCreateBatchClaim, error)
	Get(context.Context, string) (domainresource.ResourceCreateBatch, error)
	GetByIdentity(context.Context, string, string, string) (domainresource.ResourceCreateBatch, error)
	UpdateDocument(context.Context, string, domainresource.ResourceCreateExecutionDocument) error
	Complete(context.Context, string, domainresource.ResourceCreateBatchStatus) (domainresource.ResourceCreateBatch, error)
}
