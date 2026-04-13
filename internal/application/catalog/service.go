package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	catalogrepo "github.com/kubecrux/kubecrux/internal/repository/catalog"
)

type Service struct {
	repo domaincatalog.Repository
}

func New(repo domaincatalog.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListBusinessLines(ctx context.Context) ([]domaincatalog.BusinessLine, error) {
	return s.repo.ListBusinessLines(ctx)
}

func (s *Service) CreateBusinessLine(ctx context.Context, input domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error) {
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.BusinessLine{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	return s.repo.CreateBusinessLine(ctx, input)
}

func (s *Service) UpdateBusinessLine(ctx context.Context, id string, input domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error) {
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.BusinessLine{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.UpdateBusinessLine(ctx, id, input)
	return item, normalizeRepoError(err)
}

func (s *Service) DeleteBusinessLine(ctx context.Context, id string) error {
	return normalizeRepoError(s.repo.DeleteBusinessLine(ctx, id))
}

func (s *Service) ListEnvironments(ctx context.Context) ([]domaincatalog.Environment, error) {
	return s.repo.ListEnvironments(ctx)
}

func (s *Service) CreateEnvironment(ctx context.Context, input domaincatalog.EnvironmentInput) (domaincatalog.Environment, error) {
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.Environment{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	return s.repo.CreateEnvironment(ctx, input)
}

func (s *Service) UpdateEnvironment(ctx context.Context, id string, input domaincatalog.EnvironmentInput) (domaincatalog.Environment, error) {
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.Environment{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.UpdateEnvironment(ctx, id, input)
	return item, normalizeRepoError(err)
}

func (s *Service) DeleteEnvironment(ctx context.Context, id string) error {
	return normalizeRepoError(s.repo.DeleteEnvironment(ctx, id))
}

func (s *Service) ListApplicationEnvironments(ctx context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	return s.repo.ListApplicationEnvironments(ctx)
}

func (s *Service) GetApplicationEnvironment(ctx context.Context, id string) (domaincatalog.ApplicationEnvironment, error) {
	item, err := s.repo.GetApplicationEnvironment(ctx, id)
	return item, normalizeRepoError(err)
}

func (s *Service) CreateApplicationEnvironment(ctx context.Context, input domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	if strings.TrimSpace(input.ApplicationID) == "" || strings.TrimSpace(input.EnvironmentID) == "" {
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("%w: applicationId and environmentId are required", apperrors.ErrInvalidArgument)
	}
	return s.repo.CreateApplicationEnvironment(ctx, input)
}

func (s *Service) UpdateApplicationEnvironment(ctx context.Context, id string, input domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	if strings.TrimSpace(input.ApplicationID) == "" || strings.TrimSpace(input.EnvironmentID) == "" {
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("%w: applicationId and environmentId are required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.UpdateApplicationEnvironment(ctx, id, input)
	return item, normalizeRepoError(err)
}

func (s *Service) DeleteApplicationEnvironment(ctx context.Context, id string) error {
	return normalizeRepoError(s.repo.DeleteApplicationEnvironment(ctx, id))
}

func (s *Service) ListWorkflowTemplates(ctx context.Context) ([]domaincatalog.WorkflowTemplate, error) {
	return s.repo.ListWorkflowTemplates(ctx)
}

func (s *Service) CreateWorkflowTemplate(ctx context.Context, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	input = normalizeWorkflowTemplateInput(input)
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	if err := validateWorkflowTemplateDefinition(input.Definition); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	return s.repo.CreateWorkflowTemplate(ctx, input)
}

func (s *Service) UpdateWorkflowTemplate(ctx context.Context, id string, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	input = normalizeWorkflowTemplateInput(input)
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	if err := validateWorkflowTemplateDefinition(input.Definition); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	item, err := s.repo.UpdateWorkflowTemplate(ctx, id, input)
	return item, normalizeRepoError(err)
}

func (s *Service) DeleteWorkflowTemplate(ctx context.Context, id string) error {
	return normalizeRepoError(s.repo.DeleteWorkflowTemplate(ctx, id))
}

func normalizeRepoError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, catalogrepo.ErrNotFound) {
		return fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
	}
	return err
}

func normalizeWorkflowTemplateInput(input domaincatalog.WorkflowTemplateInput) domaincatalog.WorkflowTemplateInput {
	if strings.TrimSpace(input.Category) == "" {
		input.Category = "release"
	}
	if len(input.Definition) == 0 {
		input.Definition = defaultReleaseFlowDefinition()
	}
	return input
}

func defaultReleaseFlowDefinition() map[string]any {
	return map[string]any{
		"schemaVersion": 2,
		"mode":          "release_dag",
		"nodes": []map[string]any{
			{
				"id":                "approval",
				"name":              "审批",
				"type":              "manual_approval",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 120, "y": 120},
				"config":            map[string]any{"approverRoles": []string{"release-manager"}, "required": true},
			},
			{
				"id":                "deploy",
				"name":              "更新镜像",
				"type":              "deploy_update_image",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 420, "y": 120},
				"config":            map[string]any{"targetRef": "primary", "imageTagSource": "workflow_input"},
			},
			{
				"id":                "rollout",
				"name":              "等待 Rollout",
				"type":              "wait_rollout",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 720, "y": 120},
				"config":            map[string]any{"timeoutSeconds": 300},
			},
			{
				"id":                "verify",
				"name":              "HTTP 检查",
				"type":              "check_http",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 1020, "y": 120},
				"config":            map[string]any{"url": "", "expectedStatus": 200},
			},
			{
				"id":                "rollback",
				"name":              "失败回滚",
				"type":              "rollback_to_previous",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 720, "y": 360},
				"config":            map[string]any{},
			},
			{
				"id":                "notify",
				"name":              "发送通知",
				"type":              "notify",
				"timeoutSeconds":    60,
				"continueOnFailure": true,
				"position":          map[string]any{"x": 1020, "y": 360},
				"config":            map[string]any{"channel": "", "template": "release-result"},
			},
		},
		"edges": []map[string]any{
			{"id": "edge-approval-deploy", "source": "approval", "target": "deploy", "condition": "success"},
			{"id": "edge-deploy-rollout", "source": "deploy", "target": "rollout", "condition": "success"},
			{"id": "edge-rollout-verify", "source": "rollout", "target": "verify", "condition": "success"},
			{"id": "edge-rollout-rollback", "source": "rollout", "target": "rollback", "condition": "failure"},
			{"id": "edge-verify-notify", "source": "verify", "target": "notify", "condition": "success"},
			{"id": "edge-rollback-notify", "source": "rollback", "target": "notify", "condition": "always"},
		},
	}
}

func validateWorkflowTemplateDefinition(definition map[string]any) error {
	if len(definition) == 0 {
		return nil
	}
	if nodes, ok := toSliceAny(definition["nodes"]); ok && len(nodes) > 0 {
		return validateWorkflowTemplateGraph(nodes, definition["edges"])
	}
	if stages, ok := toSliceAny(definition["stages"]); ok {
		if len(stages) == 0 {
			return fmt.Errorf("%w: definition.stages cannot be empty", apperrors.ErrInvalidArgument)
		}
		for _, rawStage := range stages {
			stage, ok := rawStage.(map[string]any)
			if !ok {
				return fmt.Errorf("%w: definition.stages must contain objects", apperrors.ErrInvalidArgument)
			}
			if strings.TrimSpace(fmt.Sprint(stage["name"])) == "" {
				return fmt.Errorf("%w: each stage requires a name", apperrors.ErrInvalidArgument)
			}
			steps, ok := toSliceAny(stage["steps"])
			if !ok || len(steps) == 0 {
				return fmt.Errorf("%w: each stage requires at least one step", apperrors.ErrInvalidArgument)
			}
			if err := validateWorkflowTemplateSteps(steps); err != nil {
				return err
			}
		}
		if onFailure, ok := toSliceAny(definition["onFailure"]); ok && len(onFailure) > 0 {
			if err := validateWorkflowTemplateSteps(onFailure); err != nil {
				return err
			}
		}
		return nil
	}
	if legacySteps, ok := toSliceAny(definition["steps"]); ok && len(legacySteps) > 0 {
		return validateWorkflowTemplateSteps(legacySteps)
	}
	return fmt.Errorf("%w: definition must contain stages", apperrors.ErrInvalidArgument)
}

func validateWorkflowTemplateGraph(nodes []any, rawEdges any) error {
	nodeIDs := make(map[string]struct{}, len(nodes))
	for _, rawNode := range nodes {
		node, ok := rawNode.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: definition.nodes must contain objects", apperrors.ErrInvalidArgument)
		}
		nodeID := strings.TrimSpace(fmt.Sprint(node["id"]))
		if nodeID == "" {
			return fmt.Errorf("%w: each node requires an id", apperrors.ErrInvalidArgument)
		}
		if _, exists := nodeIDs[nodeID]; exists {
			return fmt.Errorf("%w: duplicated node id %s", apperrors.ErrInvalidArgument, nodeID)
		}
		nodeIDs[nodeID] = struct{}{}
		stepType := strings.TrimSpace(fmt.Sprint(node["type"]))
		if stepType == "" {
			return fmt.Errorf("%w: each node requires a type", apperrors.ErrInvalidArgument)
		}
		if err := validateWorkflowTemplateSteps([]any{map[string]any{"type": stepType}}); err != nil {
			return err
		}
	}

	adjacency := make(map[string][]string, len(nodes))
	if edges, ok := toSliceAny(rawEdges); ok {
		for _, rawEdge := range edges {
			edge, ok := rawEdge.(map[string]any)
			if !ok {
				return fmt.Errorf("%w: definition.edges must contain objects", apperrors.ErrInvalidArgument)
			}
			source := strings.TrimSpace(fmt.Sprint(edge["source"]))
			target := strings.TrimSpace(fmt.Sprint(edge["target"]))
			if source == "" || target == "" {
				return fmt.Errorf("%w: each edge requires source and target", apperrors.ErrInvalidArgument)
			}
			if _, ok := nodeIDs[source]; !ok {
				return fmt.Errorf("%w: edge source %s not found", apperrors.ErrInvalidArgument, source)
			}
			if _, ok := nodeIDs[target]; !ok {
				return fmt.Errorf("%w: edge target %s not found", apperrors.ErrInvalidArgument, target)
			}
			if source == target {
				return fmt.Errorf("%w: self-loop edge is not allowed", apperrors.ErrInvalidArgument)
			}
			adjacency[source] = append(adjacency[source], target)
		}
	}

	if hasCycleInWorkflowGraph(adjacency) {
		return fmt.Errorf("%w: workflow graph must be a DAG", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateWorkflowTemplateSteps(steps []any) error {
	allowedTypes := map[string]struct{}{
		"manual_approval":      {},
		"deploy_update_image":  {},
		"wait_rollout":         {},
		"check_http":           {},
		"check_k8s_event":      {},
		"smoke_test":           {},
		"notify":               {},
		"rollback_to_previous": {},
		"build":                {},
		"release":              {},
		"verify":               {},
		"check":                {},
	}
	for _, rawStep := range steps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: steps must contain objects", apperrors.ErrInvalidArgument)
		}
		stepType := strings.TrimSpace(fmt.Sprint(step["type"]))
		if stepType == "" {
			return fmt.Errorf("%w: each step requires a type", apperrors.ErrInvalidArgument)
		}
		if _, ok := allowedTypes[stepType]; !ok {
			return fmt.Errorf("%w: unsupported step type %s", apperrors.ErrInvalidArgument, stepType)
		}
	}
	return nil
}

func toSliceAny(value any) ([]any, bool) {
	switch current := value.(type) {
	case []any:
		return current, true
	case []map[string]any:
		items := make([]any, 0, len(current))
		for _, item := range current {
			items = append(items, item)
		}
		return items, true
	default:
		return nil, false
	}
}

func hasCycleInWorkflowGraph(adjacency map[string][]string) bool {
	visiting := make(map[string]bool, len(adjacency))
	visited := make(map[string]bool, len(adjacency))

	var walk func(node string) bool
	walk = func(node string) bool {
		if visited[node] {
			return false
		}
		if visiting[node] {
			return true
		}
		visiting[node] = true
		for _, next := range adjacency[node] {
			if walk(next) {
				return true
			}
		}
		visiting[node] = false
		visited[node] = true
		return false
	}

	for node := range adjacency {
		if walk(node) {
			return true
		}
	}
	return false
}
