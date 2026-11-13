package resource

import (
	"context"
	"fmt"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ConditionalWorkloadActionAgent interface {
	ExecuteWorkloadActionConditional(context.Context, string, string, string, string, contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error)
}

type ConditionalWorkloadActionDirect interface {
	ExecuteWorkloadActionConditional(context.Context, string, string, string, string, string, contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error)
}

func (g *GenericResources) ExecuteWorkloadActionConditional(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error) {
	kind, err := conditionalResourceKind(namespace, kind, name)
	if err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, err
	}
	if err := input.Validate(kind, action); err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.Action(action))
	if err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, err
	}
	var item contractresource.ConditionalWorkloadActionReceipt
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, clientErr := g.genericResourceAgentClient(connection)
		if clientErr != nil {
			return item, clientErr
		}
		adapter, ok := client.(ConditionalWorkloadActionAgent)
		if !ok {
			return item, unsupportedAgentOperation("Agent does not support conditional workload actions")
		}
		item, err = adapter.ExecuteWorkloadActionConditional(ctx, namespace, kind, name, action, input)
	} else {
		adapter, ok := g.direct.(ConditionalWorkloadActionDirect)
		if !ok {
			return item, fmt.Errorf("%w: direct adapter does not support conditional workload actions", apperrors.ErrUnsupportedOperation)
		}
		item, err = adapter.ExecuteWorkloadActionConditional(ctx, clusterID, namespace, kind, name, action, input)
	}
	if err != nil {
		_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, action, "failure", "conditional workload action failed")
		return contractresource.ConditionalWorkloadActionReceipt{}, wrapAgentResourceError(err)
	}
	summary := "workload action accepted; rollout readiness has not been observed"
	if input.DryRun {
		summary = "workload action preflight completed without writing"
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, action, "success", summary)
	if !input.DryRun {
		g.recordOperation(ctx, principal, "platform.workload."+action, connection.Summary.ID, namespace, kind, name, summary, nil)
	}
	return item, nil
}
