package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appcompute "github.com/opensoha/soha/internal/application/compute"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) invokeComputeTool(ctx context.Context, principal domainidentity.Principal, toolName string, input map[string]any) (any, map[string]any, error) {
	if s.compute == nil {
		return nil, nil, fmt.Errorf("%w: compute service is not configured", apperrors.ErrUnsupportedOperation)
	}
	switch toolName {
	case "compute.overview.read":
		item, err := s.compute.Overview(ctx, principal)
		return sanitizeComputeToolOutput(item), map[string]any{"scope": "compute"}, err
	case "compute.resources.read":
		var req computeResourceToolInput
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		item, err := s.compute.GetResource(ctx, principal, req.Domain, req.Kind, req.ID)
		return sanitizeComputeToolOutput(item), computeResourceRelatedIDs(req), err
	case "compute.resource_relations.list":
		var req computeResourceToolInput
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		item, err := s.compute.ListResourceRelations(ctx, principal, req.Domain, req.Kind, req.ID, req.Cursor, computeReadLimit(req.Limit, 50))
		return sanitizeComputeToolOutput(item), computeResourceRelatedIDs(req), err
	case "compute.tasks.list":
		var req computeTaskListToolInput
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		item, err := s.compute.ListTasks(ctx, principal, appcompute.TaskFilter{
			Domain: req.Domain, ProviderKey: req.ProviderKey, Status: req.Status, Category: req.Category,
			ResourceKind: req.ResourceKind, ResourceID: req.ResourceID, SortBy: req.SortBy, SortOrder: req.SortOrder,
			Cursor: req.Cursor, Limit: computeReadLimit(req.Limit, 50),
		})
		return sanitizeComputeToolOutput(item), computeTaskListRelatedIDs(item), err
	case "compute.tasks.get":
		var req computeTaskToolInput
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		item, err := s.compute.GetTask(ctx, principal, req.Domain, req.TaskID)
		return sanitizeComputeToolOutput(item), computeTaskRelatedIDs(req), err
	case "compute.task_logs.list":
		var req computeTaskToolInput
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		item, err := s.compute.ListTaskLogs(ctx, principal, req.Domain, req.TaskID)
		return redactComputeTaskLogs(item), computeTaskRelatedIDs(req), err
	default:
		return nil, nil, fmt.Errorf("%w: tool %s is not implemented", apperrors.ErrUnsupportedOperation, toolName)
	}
}

type computeResourceToolInput struct {
	Domain string `json:"domain"`
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

type computeTaskListToolInput struct {
	Domain       string `json:"domain"`
	ProviderKey  string `json:"providerKey"`
	Status       string `json:"status"`
	Category     string `json:"category"`
	ResourceKind string `json:"resourceKind"`
	ResourceID   string `json:"resourceId"`
	SortBy       string `json:"sortBy"`
	SortOrder    string `json:"sortOrder"`
	Cursor       string `json:"cursor"`
	Limit        int    `json:"limit"`
}

type computeTaskToolInput struct {
	Domain string `json:"domain"`
	TaskID string `json:"taskId"`
}

func computeReadLimit(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	if value > 200 {
		return 200
	}
	return value
}

func computeResourceRelatedIDs(input computeResourceToolInput) map[string]any {
	domain, kind, id := strings.TrimSpace(input.Domain), strings.TrimSpace(input.Kind), strings.TrimSpace(input.ID)
	return map[string]any{
		"resourceDomain": domain,
		"resourceKind":   kind,
		"resourceId":     id,
		"resourceUri":    "soha://compute/resources/" + domain + "/" + kind + "/" + id,
	}
}

func computeTaskRelatedIDs(input computeTaskToolInput) map[string]any {
	domain, taskID := strings.TrimSpace(input.Domain), strings.TrimSpace(input.TaskID)
	return map[string]any{
		"taskDomain": domain,
		"taskId":     taskID,
		"taskUri":    "soha://compute/tasks/" + domain + "/" + taskID,
	}
}

func computeTaskListRelatedIDs(items sohaapi.ComputeTaskListEnvelope) map[string]any {
	ids := make([]string, 0, len(items.Items))
	for _, item := range items.Items {
		ids = append(ids, string(item.Domain)+"/"+item.ID)
	}
	return map[string]any{"taskIds": ids, "nextCursor": items.NextCursor}
}

func sanitizeComputeToolOutput(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return value
	}
	return sanitizeGatewayValue(normalized)
}

func redactComputeTaskLogs(item sohaapi.ComputeTaskLogListEnvelope) any {
	for index := range item.Items {
		item.Items[index].Message = redactSensitiveText(item.Items[index].Message)
		if strings.TrimSpace(item.Items[index].Payload) == "" {
			continue
		}
		var payload any
		if json.Unmarshal([]byte(item.Items[index].Payload), &payload) == nil {
			if raw, err := json.Marshal(sanitizeGatewayValue(payload)); err == nil {
				item.Items[index].Payload = string(raw)
				continue
			}
		}
		item.Items[index].Payload = redactSensitiveText(item.Items[index].Payload)
	}
	return sanitizeComputeToolOutput(item)
}
