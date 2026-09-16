package workflow

import (
	"context"
	"errors"
	"fmt"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) executeDeliveryNode(ctx context.Context, node dagWorkflowNode, run domainworkflow.Run) dagExecutionResult {
	result := dagExecutionResult{nodeID: node.ID}
	entry := restoreNodeRuns(dagWorkflowDefinition{Nodes: []dagWorkflowNode{node}}, run.NodeRuns)[node.ID]
	updated, err := s.reconcileDeliveryNode(ctx, node, run, entry)
	if err != nil {
		result.err = err
		return result
	}
	// Adapters return references and facts; node identity is engine-owned.
	updated.NodeID, updated.TargetID, updated.Stage = entry.NodeID, entry.TargetID, entry.Stage
	updated.Name, updated.Type, updated.StartedAt = entry.Name, entry.Type, entry.StartedAt
	if updated.Status == "" || updated.Status == "pending" {
		result.err = fmt.Errorf("%w: delivery adapter did not report an execution state", apperrors.ErrConflict)
		return result
	}
	result.status, result.summary, result.nodeRun = updated.Status, updated.Summary, &updated
	if updated.Status != entry.Status || updated.Summary != entry.Summary {
		result.events = []map[string]any{{"type": "delivery_stage_observed"}}
	}
	return result
}

func (s *Service) reconcileDeliveryNode(ctx context.Context, node dagWorkflowNode, run domainworkflow.Run, entry domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	repo, err := s.deliveryRepository()
	if err != nil {
		return entry, err
	}
	batch, current, err := repo.GetDeliveryBatch(ctx, run.DeliveryBatchID)
	if err != nil {
		return entry, err
	}
	if !sameDeliveryLease(run, current) {
		return entry, apperrors.ErrConflict
	}
	if node.Stage == "barrier" {
		entry.Status = "completed"
		return entry, nil
	}
	if s.deliveryRuntime == nil || s.deliveryPrincipals == nil {
		return entry, fmt.Errorf("%w: delivery runtime is unavailable", apperrors.ErrConflict)
	}
	if run.StopReason != "" || entry.Status == "canceling" {
		return s.cancelDeliveryNode(ctx, run, batch, entry)
	}
	tokenID, _ := run.Metadata["executionTokenId"].(string)
	principal, err := s.deliveryPrincipals.CurrentExecutionPrincipal(ctx, batch.CreatedBy, tokenID)
	if err == nil {
		err = s.authorizeDeliveryNode(ctx, principal, node, batch)
	}
	if err == nil {
		ctx, err = s.restoreDeliveryGatewayAuthorization(ctx, principal, run.GatewayAuthorization)
	}
	if authorizerID, _ := run.Metadata["triggerAuthorizerId"].(string); err == nil && authorizerID != "" {
		authorizerTokenID, _ := run.Metadata["triggerAuthorizerTokenId"].(string)
		var authorizer domainidentity.Principal
		authorizer, err = s.deliveryPrincipals.CurrentExecutionPrincipal(ctx, authorizerID, authorizerTokenID)
		if err == nil {
			err = s.authorizeDeliveryNode(ctx, authorizer, node, batch)
		}
	}
	if err != nil {
		if !errors.Is(err, apperrors.ErrAccessDenied) && !errors.Is(err, apperrors.ErrNotFound) && !errors.Is(err, apperrors.ErrUnauthorized) {
			return entry, err
		}
		entry.Status = "canceling"
		return s.cancelDeliveryNode(ctx, run, batch, entry)
	}
	ctx = domainworkflow.WithNodeExecution(ctx, run, entry)
	return s.deliveryRuntime.ExecuteDeliveryStage(ctx, principal, run, batch, entry)
}

func (s *Service) authorizeDeliveryNode(ctx context.Context, principal domainidentity.Principal, node dagWorkflowNode, batch domainworkflow.DeliveryBatch) error {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return err
	}
	for _, snapshot := range batch.Targets {
		if snapshot.Target.ID == node.TargetID {
			return s.authorizeDeliveryTarget(ctx, principal, snapshot.Target, domainaccess.ActionTrigger)
		}
	}
	return apperrors.ErrNotFound
}

func (s *Service) cancelDeliveryNode(ctx context.Context, run domainworkflow.Run, batch domainworkflow.DeliveryBatch, entry domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	updated, err := s.deliveryRuntime.CancelDeliveryStage(ctx, run, batch, entry)
	if err != nil {
		return entry, err
	}
	if !deliveryNodeTerminal(updated.Status) {
		updated.Status, updated.Summary = "canceling", "executor stop is not yet confirmed"
	} else if updated.Status == "canceled" && run.StopReason == "" {
		updated.Status, updated.Summary = "failed", "target authorization is no longer valid; executor stop confirmed"
	}
	return updated, nil
}
