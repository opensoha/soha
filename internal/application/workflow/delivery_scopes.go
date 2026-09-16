package workflow

import (
	"context"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func (s *Service) ResolveDeliveryScopes(ctx context.Context, principal domainidentity.Principal, definition domainworkflow.DeliveryWorkflowDefinition) ([]map[string]string, error) {
	return s.deliverySnapshotScopes(ctx, principal, deliveryUnresolvedTargets(definition.Targets))
}

func (s *Service) deliverySnapshotScopes(ctx context.Context, principal domainidentity.Principal, targets []domainworkflow.DeliveryTargetSnapshot) ([]map[string]string, error) {
	if s.deliveryRuntime == nil {
		return nil, domainworkflow.CheckDeliveryScopes(ctx, nil)
	}
	var scopes []map[string]string
	for _, target := range targets {
		resolved, err := s.deliveryRuntime.ResolveDeliveryTargetScopes(ctx, principal, target)
		if err != nil {
			return nil, err
		}
		scopes = append(scopes, resolved...)
	}
	return scopes, domainworkflow.CheckDeliveryScopes(ctx, scopes)
}

func (s *Service) scopedDeliveryWorkflow(ctx context.Context, principal domainidentity.Principal, item domainworkflow.DeliveryWorkflow) (domainworkflow.DeliveryWorkflow, error) {
	var err error
	item.InvocationScopes, err = s.ResolveDeliveryScopes(ctx, principal, item.Definition)
	return item, err
}

func (s *Service) scopedDeliveryBatch(ctx context.Context, principal domainidentity.Principal, batch domainworkflow.DeliveryBatch, run domainworkflow.Run) (domainworkflow.DeliveryBatch, error) {
	var err error
	batch.InvocationScopes, err = s.deliverySnapshotScopes(ctx, principal, batch.Targets)
	return projectDeliveryBatch(batch, run), err
}
