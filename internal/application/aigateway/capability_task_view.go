package aigateway

import (
	"context"
	"encoding/json"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) VisibleCapabilityTask(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run) (domainaigateway.CapabilityTask, error) {
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil || intent.ActorID != principal.UserID {
		return domainaigateway.CapabilityTask{}, apperrors.ErrNotFound
	}
	plan, err := s.visibleCapabilityPlan(ctx, principal, intent, run.NodeRuns)
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	created, _ := time.Parse(time.RFC3339, run.CreatedAt)
	updated, _ := time.Parse(time.RFC3339, run.UpdatedAt)
	view := domainaigateway.CapabilityTask{ID: run.ID, Version: run.Version, PlanVersion: intent.PlanVersion, Status: run.Status, CreatedBy: intent.ActorID, Plan: plan, CreatedAt: created, UpdatedAt: updated, Nodes: []domainaigateway.CapabilityTaskNode{}}
	for _, node := range run.NodeRuns {
		entry, err := s.visibleCapabilityNode(ctx, principal, node)
		if err != nil {
			return domainaigateway.CapabilityTask{}, err
		}
		if node.PreparedCall == nil {
			for _, step := range intent.Input.Plan.Steps {
				if step.ID == node.NodeID && len(step.Bindings) > 0 {
					entry.Summary = "input withheld until dependency bindings establish an authorized target"
				}
			}
		}
		view.Nodes = append(view.Nodes, entry)
	}
	visibleRun := run
	visibleRun.NodeRuns = make([]domainworkflow.NodeRun, len(view.Nodes))
	for i, node := range view.Nodes {
		visibleRun.NodeRuns[i] = domainworkflow.NodeRun{NodeID: node.ID, Status: node.Status, Invocation: node.Invocation}
	}
	assessment := domainworkflow.AssessCapabilityRun(visibleRun, plan)
	view.Assessment = &assessment
	return view, nil
}

func (s *Service) visibleCapabilityPlan(ctx context.Context, principal domainidentity.Principal, intent domainworkflow.CapabilityIntent, nodes []domainworkflow.NodeRun) (domainaigateway.CapabilityPlan, error) {
	// Clone before applying today's visibility policy to a persisted plan.
	data, err := json.Marshal(intent.Input.Plan)
	if err != nil {
		return domainaigateway.CapabilityPlan{}, err
	}
	var plan domainaigateway.CapabilityPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return domainaigateway.CapabilityPlan{}, err
	}
	prepared := map[string]*domainaigateway.ToolInvocationRequest{}
	for _, node := range nodes {
		prepared[node.NodeID] = node.PreparedCall
	}
	for i, step := range plan.Steps {
		call := domainaigateway.ToolInvocationRequest{ToolName: step.Call.ToolName, CapabilityVersion: step.Call.CapabilityVersion, Input: step.Call.Input, AIClientID: intent.Input.AIClientID, SkillID: intent.Input.SkillID}
		plan.Steps[i].Call.SecretRefs = nil
		if prepared[step.ID] != nil {
			call = *prepared[step.ID]
		} else if len(step.Bindings) > 0 {
			// This actor owns the submitted intent, but the target does not yet
			// exist. Return dependency structure only; do not resolve a fake ID
			// or expose inputs under an unverified resource scope.
			plan.Steps[i].Call.Input = map[string]any{}
			continue
		}
		ctx, err = s.authorizeCapabilityCall(ctx, principal, call)
		if err != nil {
			return domainaigateway.CapabilityPlan{}, err
		}
		tool, _ := s.toolByName(call.ToolName)
		visible, _, err := s.sanitizeToolOutputByAccessPolicy(ctx, principal, call.AIClientID, call.SkillID, tool, capabilityGatewayScope(tool, call.Input), step.Call.Input)
		if err != nil {
			return domainaigateway.CapabilityPlan{}, err
		}
		value, err := capabilityJSON(visible)
		if err != nil {
			return domainaigateway.CapabilityPlan{}, err
		}
		plan.Steps[i].Call.Input, _ = value.(map[string]any)
		if plan.Steps[i].Call.Input == nil {
			plan.Steps[i].Call.Input = map[string]any{}
		}
		plan.Steps[i].Call.SecretRefs = nil
	}
	return plan, nil
}

func (s *Service) visibleCapabilityNode(ctx context.Context, principal domainidentity.Principal, node domainworkflow.NodeRun) (domainaigateway.CapabilityTaskNode, error) {
	entry := domainaigateway.CapabilityTaskNode{ID: node.NodeID, Status: node.Status, Summary: node.Summary}
	var err error
	if node.Invocation != nil && node.PreparedCall != nil {
		call := *node.PreparedCall
		if node.Invocation.Task != nil {
			call = capabilityInvocationCall(node.Invocation.Task.StatusCall, call)
		}
		ctx, err = s.authorizeCapabilityCall(ctx, principal, call)
		if err != nil {
			return domainaigateway.CapabilityTaskNode{}, err
		}
		tool, _ := s.toolByName(call.ToolName)
		output, _, err := s.sanitizeToolOutputByAccessPolicy(ctx, principal, call.AIClientID, call.SkillID, tool, capabilityGatewayScope(tool, call.Input), node.Invocation.Output)
		if err != nil {
			return domainaigateway.CapabilityTaskNode{}, err
		}
		invocation := *node.Invocation
		invocation.Output = output
		invocation.Task = visibleCapabilityTaskRef(invocation.Task, output)
		invocation.Assessment = visibleCapabilityAssessment(tool, output)
		// Related IDs and audit metadata are not needed to resume a goal;
		// don't restore fields removed from the policy-filtered output.
		invocation.RelatedIDs, invocation.Audit = nil, nil
		entry.Invocation = &invocation
	}
	if node.Status == "waiting_approval" {
		call := node.PreparedCall
		if node.ControlCall != nil {
			call = node.ControlCall
		}
		if call != nil {
			ctx, err = s.authorizeCapabilityCall(ctx, principal, *call)
			if err != nil {
				return domainaigateway.CapabilityTaskNode{}, err
			}
			tool, _ := s.toolByName(call.ToolName)
			visible, _, err := s.sanitizeToolOutputByAccessPolicy(ctx, principal, call.AIClientID, call.SkillID, tool, capabilityGatewayScope(tool, call.Input), map[string]any{"approvalRequestId": capabilityApprovalID(call.RequestID)})
			if err != nil {
				return domainaigateway.CapabilityTaskNode{}, err
			}
			value, _ := capabilityJSON(visible)
			if object, ok := value.(map[string]any); ok {
				if id, ok := object["approvalRequestId"].(string); ok && id == capabilityApprovalID(call.RequestID) {
					entry.ApprovalRequestID = id
				}
			}
		}
	}
	return entry, nil
}
