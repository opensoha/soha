package workflow

import (
	"context"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func (s *Service) ListDeliveryBatches(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID, workflowID string, limit int) ([]domainworkflow.DeliveryBatch, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return nil, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return nil, err
	}
	ids, err := repo.ListDeliveryBatchIDs(ctx, applicationID, serviceID, workflowID, limit)
	if err != nil {
		return nil, err
	}
	items := []domainworkflow.DeliveryBatch{}
	for _, id := range ids {
		batch, run, err := repo.GetDeliveryBatch(ctx, id)
		if err != nil {
			return nil, err
		}
		visible := s.visibleDeliveryTargets(ctx, principal, batch, applicationID, serviceID)
		if len(visible) == 0 {
			continue
		}
		items = append(items, filterDeliveryBatch(projectDeliveryBatch(batch, run), visible))
	}
	return items, nil
}

func filterDeliveryBatch(batch domainworkflow.DeliveryBatch, visible map[string]bool) domainworkflow.DeliveryBatch {
	if len(visible) == len(batch.Targets) {
		return batch
	}
	batch.Targets = append([]domainworkflow.DeliveryTargetSnapshot(nil), batch.Targets...)
	batch.Definition.Targets = []domainworkflow.DeliveryTargetInput{}
	targets, buildAliases := []domainworkflow.DeliveryTargetSnapshot{}, map[string]string{}
	for _, snapshot := range batch.Targets {
		if !visible[snapshot.Target.ID] {
			continue
		}
		snapshot.Target.DependsOn = visibleDeliveryDependencies(snapshot.Target.DependsOn, visible)
		if snapshot.BuildNodeID != "" {
			if buildAliases[snapshot.BuildNodeID] == "" {
				buildAliases[snapshot.BuildNodeID] = snapshot.Target.ID + ":build"
			}
			snapshot.BuildNodeID = buildAliases[snapshot.BuildNodeID]
		}
		targets = append(targets, snapshot)
		batch.Definition.Targets = append(batch.Definition.Targets, snapshot.Target)
	}
	batch.Targets = targets
	nodes := []domainworkflow.NodeRun{}
	for _, node := range batch.Nodes {
		if alias := buildAliases[node.NodeID]; alias != "" {
			node.NodeID, node.Name, node.TargetID = alias, "build", strings.TrimSuffix(alias, ":build")
			node.Summary = ""
		} else if !visible[node.TargetID] {
			continue
		}
		nodes = append(nodes, node)
	}
	// A partial view exposes only its own facts; global names and stop details
	// can contain applications or failures the caller cannot view.
	batch.PartialView = true
	batch.Definition.Name = "Application delivery"
	batch.RootRunID = ""
	batch.WorkflowID, batch.RetryOfBatchID = "", ""
	batch.WorkflowVersion = 0
	batch.StopReason, batch.StopSummary = "", ""
	run := domainworkflow.Run{NodeRuns: nodes, Status: deliveryVisibleStatus(nodes), UpdatedAt: batch.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")}
	return projectDeliveryBatch(batch, run)
}

func visibleDeliveryDependencies(dependencies []string, visible map[string]bool) []string {
	out := []string{}
	for _, id := range dependencies {
		if visible[id] {
			out = append(out, id)
		}
	}
	return out
}

func deliveryVisibleStatus(nodes []domainworkflow.NodeRun) string {
	completed, failed, canceled := 0, false, false
	for _, node := range nodes {
		switch node.Status {
		case "pending", "queued", "running":
			return "running"
		case "waiting_execution", "waiting_approval", "canceling":
			return node.Status
		case "failed", "skipped":
			failed = true
		case "canceled":
			canceled = true
		case "completed":
			if node.Stage == "health" || node.Stage == "barrier" {
				completed++
			}
		}
	}
	if failed {
		if completed > 0 {
			return "partially_completed"
		}
		return "failed"
	}
	if canceled {
		return "canceled"
	}
	return "completed"
}

func (s *Service) visibleDeliveryTargets(ctx context.Context, principal domainidentity.Principal, batch domainworkflow.DeliveryBatch, applicationID, serviceID string) map[string]bool {
	visible := map[string]bool{}
	for _, snapshot := range batch.Targets {
		target := snapshot.Target
		if applicationID != "" && target.ApplicationID != applicationID || serviceID != "" && target.ServiceID != serviceID {
			continue
		}
		if s.authorizeDeliveryTarget(ctx, principal, target, domainaccess.ActionView) == nil {
			visible[target.ID] = true
		}
	}
	return visible
}
