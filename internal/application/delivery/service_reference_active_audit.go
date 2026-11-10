package delivery

import (
	"context"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

func (s *Service) recordActiveReference(ctx context.Context, principal domainidentity.Principal, action, bindingID, serviceID, referenceID string, revision int64) {
	metadata := map[string]any{"applicationEnvironmentId": bindingID, "serviceId": serviceID, "referenceId": referenceID, "revision": revision, "state": "active"}
	operation := "delivery.service_reference." + action
	if s.audit != nil {
		meta := requestctx.FromContext(ctx)
		_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams, ResourceKind: "ServiceReference", ResourceName: referenceID, Action: operation, Result: "success", Summary: "service reference " + action, RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: metadata})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(ctx, principal, operation, map[string]any{"resourceKind": "ServiceReference", "referenceId": referenceID}, "success", "service reference "+action, metadata))
	}
}
