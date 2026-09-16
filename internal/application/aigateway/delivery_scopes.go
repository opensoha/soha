package aigateway

import (
	"context"
	"errors"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"reflect"
)

func (p *deliveryCapabilityProvider) ToolInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) ([]map[string]string, error) {
	scopes, err := p.deliveryInvocationScopes(ctx, principal, tool, input)
	if err == nil && len(scopes) == 0 {
		return nil, apperrors.ErrAccessDenied
	}
	return scopes, err
}

func (p *deliveryCapabilityProvider) deliveryInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) ([]map[string]string, error) {
	switch tool.Name {
	case "delivery.workflows.create":
		var request domainworkflow.DeliveryWorkflowInput
		if err := mapInput(input, &request); err != nil {
			return nil, err
		}
		existing, err := p.service.FindDeliveryWorkflowCreation(ctx, principal, request)
		if err == nil {
			return existing.InvocationScopes, nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return nil, err
		}
		prepared, err := p.service.PrepareDeliveryWorkflow(ctx, principal, "", request)
		if err != nil {
			return nil, err
		}
		return p.service.ResolveDeliveryScopes(ctx, principal, prepared.Definition)
	case "delivery.workflows.get":
		item, err := p.service.GetDeliveryWorkflow(ctx, principal, firstMapString(input, "workflowId"))
		return item.InvocationScopes, err
	case "delivery.batches.create":
		var request domainworkflow.DeliveryBatchInput
		if err := mapInput(input, &request); err != nil {
			return nil, err
		}
		existing, err := p.service.FindDeliveryBatch(ctx, principal, request)
		if err == nil {
			return existing.InvocationScopes, nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return nil, err
		}
		definition, err := p.service.PrepareDeliveryBatch(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		return p.service.ResolveDeliveryScopes(ctx, principal, definition)
	case "delivery.batches.get", "delivery.batches.cancel", "delivery.batches.assess":
		item, err := p.service.GetDeliveryBatch(ctx, principal, firstMapString(input, "batchId"))
		if item.PartialView {
			return nil, apperrors.ErrAccessDenied
		}
		return item.InvocationScopes, err
	default:
		return nil, apperrors.ErrUnsupportedOperation
	}
}

func withDeliveryScopeCheck(ctx context.Context, tool domainaigateway.ToolCapability, input map[string]any) context.Context {
	resolved, _ := ctx.Value(capabilityScopeContextKey{}).(capabilityScopeContext)
	if resolved.tool != tool.Name {
		return ctx
	}
	return domainworkflow.WithDeliveryScopeCheck(ctx, func(scopes []map[string]string) error {
		actual := make([]map[string]string, len(scopes))
		for i, scope := range scopes {
			merged, err := mergeCapabilityScope(capabilityGatewayScope(tool, input), scope)
			if err != nil {
				return err
			}
			actual[i] = merged
		}
		if !reflect.DeepEqual(actual, resolved.scopes) {
			return apperrors.ErrConflict
		}
		return nil
	})
}

func verifyDeliveryOutputScopes(ctx context.Context, tool domainaigateway.ToolCapability, input map[string]any, output any) error {
	ctx = withDeliveryScopeCheck(ctx, tool, input)
	switch value := output.(type) {
	case domainworkflow.DeliveryWorkflow:
		return domainworkflow.CheckDeliveryScopes(ctx, value.InvocationScopes)
	case domainworkflow.DeliveryBatch:
		return domainworkflow.CheckDeliveryScopes(ctx, value.InvocationScopes)
	case sohaapi.DeliveryBatchAssessment:
		// Workflow checked every private snapshot before assessment. The public
		// result intentionally contains no scope or encrypted configuration copy.
		return nil
	default:
		return apperrors.ErrInvalidArgument
	}
}
