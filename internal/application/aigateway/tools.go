package aigateway

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
	domainapp "github.com/soha/soha/internal/domain/application"
	domaincatalog "github.com/soha/soha/internal/domain/catalog"
	domaincopilot "github.com/soha/soha/internal/domain/copilot"
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainresource "github.com/soha/soha/internal/domain/resource"
	"github.com/soha/soha/internal/platform/apperrors"
)

func (s *Service) InvokeTool(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	toolName := strings.TrimSpace(input.ToolName)
	tool, ok := s.toolByName(toolName)
	if !ok {
		return domainaigateway.ToolInvocationResult{}, fmt.Errorf("%w: unknown AI Gateway tool %s", apperrors.ErrInvalidArgument, toolName)
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "deny", err.Error(), nil)
		return domainaigateway.ToolInvocationResult{}, err
	}
	if err := s.authorizeTool(ctx, principal, tool); err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "deny", err.Error(), nil)
		return domainaigateway.ToolInvocationResult{}, err
	}
	invocationScope := standardGatewayScope(input.Input, nil)
	grantRequiresApproval, err := s.authorizeToolGrant(ctx, principal, input.AIClientID, tool, invocationScope)
	if err != nil {
		_ = s.recordToolAudit(ctx, principal, input, tool, "deny", err.Error(), nil)
		return domainaigateway.ToolInvocationResult{}, err
	}
	decision, policyInput, redactionSummary, err := s.authorizeAccessPolicy(ctx, principal, input.AIClientID, input.SkillID, &tool, invocationScope, input.Input)
	if err != nil {
		_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "deny", err.Error(), nil, redactionSummary)
		return domainaigateway.ToolInvocationResult{}, err
	}
	input.Input = policyInput
	if grantRequiresApproval {
		decision = mergeGatewayRiskDecision(decision, gatewayRiskDecision{
			Strategy: gatewayRiskRequireApproval,
			Reason:   "matching MCP tool grant requires approval",
		})
		tool.RequiresApproval = true
	}
	if err := s.authorizeSkillBinding(ctx, principal, input.AIClientID, input.SkillID, tool); err != nil {
		_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "deny", err.Error(), nil, redactionSummary)
		return domainaigateway.ToolInvocationResult{}, err
	}
	if decision.shouldHoldExecution() {
		return s.holdToolInvocation(ctx, principal, input, tool, decision, redactionSummary)
	}

	output, relatedIDs, err := s.invokeGatewayTool(ctx, principal, tool, input.Input)
	if err != nil {
		_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "failure", err.Error(), relatedIDs, redactionSummary)
		return domainaigateway.ToolInvocationResult{}, err
	}
	var outputRedactionSummary gatewayRedactionAuditSummary
	output, outputRedactionSummary, err = s.sanitizeToolOutputByAccessPolicy(ctx, principal, input.AIClientID, input.SkillID, tool, invocationScope, output)
	redactionSummary.merge(outputRedactionSummary)
	if err != nil {
		_ = s.recordToolAuditWithRedaction(ctx, principal, input, tool, "deny", err.Error(), relatedIDs, redactionSummary)
		return domainaigateway.ToolInvocationResult{}, err
	}
	usageSummary := gatewayProviderUsageSummary(output, relatedIDs)
	_ = s.recordToolAuditWithMetadata(ctx, principal, input, tool, "success", "invoked AI Gateway tool", relatedIDs, redactionSummary, usageSummary)
	audit := map[string]any{
		"riskLevel":        tool.RiskLevel,
		"requiresApproval": tool.RequiresApproval,
	}
	addGatewayRedactionAuditMetadata(audit, redactionSummary)
	addGatewayUsageAuditMetadata(audit, usageSummary)
	return domainaigateway.ToolInvocationResult{
		ToolName:         tool.Name,
		RiskLevel:        tool.RiskLevel,
		RequiresApproval: tool.RequiresApproval,
		Result:           "success",
		Output:           output,
		RelatedIDs:       relatedIDs,
		Audit:            audit,
	}, nil
}
func (s *Service) ReadResource(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ResourceReadRequest) (domainaigateway.ResourceReadResult, error) {
	resourceName := normalizeGatewayResourceURI(firstNonEmpty(input.Name, input.URI))
	input.Name = resourceName
	input.URI = resourceName
	if input.Context == nil {
		input.Context = map[string]any{}
	}
	resource, ok := s.resourceByName(resourceName)
	if !ok {
		resource = domainaigateway.ResourceCapability{Name: resourceName}
		err := fmt.Errorf("%w: unknown AI Gateway resource %s", apperrors.ErrInvalidArgument, resourceName)
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	if err := s.authorizeResource(ctx, principal, resource); err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	if _, err := s.authorizeSkillContext(ctx, principal, input.AIClientID, input.SkillID); err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	bindings, err := s.activeSkillBindings(ctx, principal, input.AIClientID)
	if err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	toolRefs := s.resourceToolRefs(resource.Name)
	promptRefs := s.resourcePromptRefs(resource.Name)
	skillRefs := s.resourceSkillRefs(resource.Name)
	if err := authorizeResourceSkillBindingWithRefs(resource, bindings, input.SkillID, toolRefs, s.gatewaySkills()); err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "deny", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}

	document := gatewayResourceDocumentWithCapabilities(resource, input, bindings, input.SkillID, toolRefs, promptRefs, skillRefs, s.gatewayTools(), s.gatewayPrompts(), s.gatewaySkills())
	text, err := marshalGatewayDocument(document)
	if err != nil {
		_ = s.recordResourceAudit(ctx, principal, input, resource, "failure", err.Error(), nil)
		return domainaigateway.ResourceReadResult{}, err
	}
	resourceRefs := ResourceCapabilityRefs{Resource: resource.Name, Tools: toolRefs, Prompts: promptRefs, Skills: skillRefs}
	relatedToolRefs := filterToolRefsBySkillBindingsWithSkills(toolRefs, bindings, input.SkillID, s.gatewaySkills())
	relatedPromptRefs := filterPromptRefsBySkillBindingsWithResourceRefs(promptRefs, bindings, input.SkillID, resourceRefs, s.gatewaySkills())
	relatedSkillRefs := filterSkillRefsByBindingsWithSkills(skillRefs, bindings, input.SkillID, s.gatewaySkills())
	relatedIDs := map[string]any{
		"resourceUri":        resource.Name,
		"relatedToolCount":   len(relatedToolRefs),
		"relatedPromptCount": len(relatedPromptRefs),
		"relatedSkillCount":  len(relatedSkillRefs),
	}
	_ = s.recordResourceAudit(ctx, principal, input, resource, "success", "read AI Gateway resource manifest", relatedIDs)
	return domainaigateway.ResourceReadResult{
		Name:       resource.Name,
		URI:        resource.Name,
		MIMEType:   "application/json",
		Text:       text,
		Data:       document,
		RelatedIDs: relatedIDs,
		Audit: map[string]any{
			"riskLevel": domainaigateway.RiskLevelRead,
		},
	}, nil
}
func (s *Service) GetPrompt(ctx context.Context, principal domainidentity.Principal, input domainaigateway.PromptGetRequest) (domainaigateway.PromptGetResult, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Arguments == nil {
		input.Arguments = map[string]any{}
	}
	if input.Context == nil {
		input.Context = map[string]any{}
	}
	prompt, ok := s.promptByName(input.Name)
	if !ok {
		prompt = domainaigateway.PromptCapability{Name: input.Name}
		err := fmt.Errorf("%w: unknown AI Gateway prompt %s", apperrors.ErrInvalidArgument, input.Name)
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	if err := s.authorizePrompt(ctx, principal, prompt); err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	skill, err := s.authorizeSkillContext(ctx, principal, input.AIClientID, input.SkillID)
	if err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	bindings, err := s.activeSkillBindings(ctx, principal, input.AIClientID)
	if err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	if err := authorizePromptSkillBindingWithCapabilities(prompt, bindings, input.SkillID, s.gatewayResources(), s.gatewayResourceCapabilityRefs(), s.gatewaySkills()); err != nil {
		_ = s.recordPromptAudit(ctx, principal, input, prompt, "deny", err.Error(), nil)
		return domainaigateway.PromptGetResult{}, err
	}
	if skill != nil {
		*skill = narrowSkillCapabilityByBindingsWithSkills(*skill, bindings, input.SkillID, s.gatewaySkills())
	}

	messages := gatewayPromptMessages(prompt, input, skill)
	relatedIDs := map[string]any{
		"promptName":   prompt.Name,
		"messageCount": len(messages),
	}
	if skill != nil {
		relatedIDs["skillId"] = skill.ID
	}
	_ = s.recordPromptAudit(ctx, principal, input, prompt, "success", "read AI Gateway prompt template", relatedIDs)
	return domainaigateway.PromptGetResult{
		Name:        prompt.Name,
		Description: prompt.Description,
		Messages:    messages,
		RelatedIDs:  relatedIDs,
		Audit: map[string]any{
			"riskLevel": domainaigateway.RiskLevelAnalyze,
			"skillId":   strings.TrimSpace(input.SkillID),
		},
	}, nil
}
func (s *Service) invokeGatewayTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	switch {
	case strings.HasPrefix(tool.Name, "delivery."):
		return s.invokeDeliveryTool(ctx, principal, tool, input)
	case strings.HasPrefix(tool.Name, "k8s."):
		return s.invokeKubernetesTool(ctx, principal, tool, input)
	case tool.Name == "diagnosis.release_failure.analyze":
		return s.invokeReleaseFailureDiagnosis(ctx, principal, input)
	default:
		output, relatedIDs, handled, err := s.gatewayRegistry().InvokeTool(ctx, principal, tool, input)
		if handled {
			return output, relatedIDs, err
		}
		return nil, nil, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
}
func (s *Service) invokeDeliveryTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if !strings.HasPrefix(tool.Name, "delivery.") {
		return nil, nil, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
	if s.apps == nil || s.delivery == nil {
		return nil, nil, fmt.Errorf("%w: delivery gateway services are not configured", apperrors.ErrInvalidArgument)
	}
	switch tool.Name {
	case "delivery.applications.list":
		var req struct {
			Search string `json:"search"`
			Limit  int    `json:"limit"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		items, err := s.apps.List(ctx, principal, domainapp.Filter{Search: req.Search, Limit: req.Limit})
		return items, map[string]any{"count": len(items)}, err
	case "delivery.applications.detail":
		applicationID := stringInput(input, "applicationId")
		if applicationID == "" {
			return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
		}
		item, err := s.delivery.GetApplicationDetail(ctx, principal, applicationID)
		return item, map[string]any{"applicationId": applicationID, "bindingCount": len(item.Bindings)}, err
	case "delivery.applications.create":
		var req domainapp.UpsertInput
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		item, err := s.apps.Create(ctx, principal, req)
		return item, map[string]any{"applicationId": item.ID}, err
	case "delivery.application_environments.list":
		applicationID := stringInput(input, "applicationId")
		bindingID := firstNonEmpty(stringInput(input, "applicationEnvironmentId"), stringInput(input, "bindingId"))
		if bindingID != "" {
			item, err := s.delivery.GetApplicationEnvironmentDetail(ctx, principal, bindingID)
			return item, map[string]any{"applicationEnvironmentId": bindingID}, err
		}
		if applicationID == "" {
			return nil, nil, fmt.Errorf("%w: applicationId or applicationEnvironmentId is required", apperrors.ErrInvalidArgument)
		}
		detail, err := s.delivery.GetApplicationDetail(ctx, principal, applicationID)
		if err != nil {
			return nil, nil, err
		}
		return detail.Bindings, map[string]any{"applicationId": applicationID, "count": len(detail.Bindings)}, nil
	case "delivery.application_services.list":
		applicationID := stringInput(input, "applicationId")
		if applicationID == "" {
			return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
		}
		items, err := s.apps.ListServices(ctx, principal, applicationID)
		items = redactedApplicationServices(items)
		return items, map[string]any{"applicationId": applicationID, "count": len(items)}, err
	case "delivery.build_sources.list":
		var req struct {
			ApplicationID string `json:"applicationId"`
			WithBindings  bool   `json:"withBindings"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(req.ApplicationID) == "" {
			return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
		}
		detail, err := s.delivery.GetApplicationDetail(ctx, principal, req.ApplicationID)
		if err != nil {
			return nil, nil, err
		}
		output := map[string]any{
			"applicationId": req.ApplicationID,
			"buildSources":  redactedBuildSources(detail.Application.BuildSources),
		}
		related := map[string]any{"applicationId": req.ApplicationID, "count": len(detail.Application.BuildSources)}
		if req.WithBindings {
			output["bindingUsage"] = buildSourceBindingUsage(detail)
			related["bindingCount"] = len(detail.Bindings)
		}
		return output, related, nil
	case "delivery.release_targets.list":
		applicationID := stringInput(input, "applicationId")
		bindingID := firstNonEmpty(stringInput(input, "applicationEnvironmentId"), stringInput(input, "bindingId"))
		if bindingID != "" {
			detail, err := s.delivery.GetApplicationEnvironmentDetail(ctx, principal, bindingID)
			if err != nil {
				return nil, nil, err
			}
			return detail.Binding.Targets, map[string]any{
				"applicationId":            detail.Application.ID,
				"applicationEnvironmentId": bindingID,
				"count":                    len(detail.Binding.Targets),
			}, nil
		}
		if applicationID == "" {
			return nil, nil, fmt.Errorf("%w: applicationId or applicationEnvironmentId is required", apperrors.ErrInvalidArgument)
		}
		detail, err := s.delivery.GetApplicationDetail(ctx, principal, applicationID)
		if err != nil {
			return nil, nil, err
		}
		targets := releaseTargetsFromApplicationDetail(detail)
		return targets, map[string]any{"applicationId": applicationID, "count": len(targets)}, nil
	case "delivery.actions.trigger":
		var req struct {
			ApplicationID string `json:"applicationId"`
			domaindelivery.ApplicationDeliveryActionInput
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(req.ApplicationID) == "" {
			return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
		}
		item, err := s.delivery.TriggerApplicationDeliveryAction(ctx, principal, req.ApplicationID, req.ApplicationDeliveryActionInput)
		return item, map[string]any{
			"applicationId":            item.ApplicationID,
			"applicationEnvironmentId": item.ApplicationEnvironmentID,
			"releaseBundleId":          item.RelatedIDs.ReleaseBundleID,
			"executionTaskId":          item.RelatedIDs.ExecutionTaskID,
			"workflowRunId":            item.RelatedIDs.WorkflowRunID,
		}, err
	case "delivery.release_bundles.list":
		var req struct {
			ApplicationID            string `json:"applicationId"`
			ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
			Limit                    int    `json:"limit"`
			BundleID                 string `json:"bundleId"`
			Artifacts                bool   `json:"artifacts"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if req.Artifacts && strings.TrimSpace(req.BundleID) != "" {
			items, err := s.delivery.ListReleaseBundleArtifacts(ctx, principal, req.BundleID)
			return items, map[string]any{"releaseBundleId": req.BundleID, "count": len(items)}, err
		}
		items, err := s.delivery.ListReleaseBundles(ctx, principal, domaindelivery.ReleaseBundleFilter{
			ApplicationID:            req.ApplicationID,
			ApplicationEnvironmentID: req.ApplicationEnvironmentID,
			Limit:                    req.Limit,
		})
		return items, map[string]any{"count": len(items)}, err
	case "delivery.execution_tasks.list":
		var req struct {
			ApplicationID            string `json:"applicationId"`
			ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
			ReleaseBundleID          string `json:"releaseBundleId"`
			Status                   string `json:"status"`
			ProviderKind             string `json:"providerKind"`
			Limit                    int    `json:"limit"`
			TaskID                   string `json:"taskId"`
			Logs                     bool   `json:"logs"`
			LogLimit                 int    `json:"logLimit"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if req.Logs && strings.TrimSpace(req.TaskID) != "" {
			items, err := s.delivery.ListExecutionLogs(ctx, principal, req.TaskID, req.LogLimit)
			items = redactExecutionLogs(items)
			return items, map[string]any{"executionTaskId": req.TaskID, "count": len(items)}, err
		}
		items, err := s.delivery.ListExecutionTasks(ctx, principal, domaindelivery.ExecutionTaskFilter{
			ApplicationID:            req.ApplicationID,
			ApplicationEnvironmentID: req.ApplicationEnvironmentID,
			ReleaseBundleID:          req.ReleaseBundleID,
			Status:                   req.Status,
			ProviderKind:             req.ProviderKind,
			Limit:                    req.Limit,
		})
		return items, map[string]any{"count": len(items)}, err
	case "delivery.execution_logs.list":
		var req struct {
			TaskID string `json:"taskId"`
			Limit  int    `json:"limit"`
		}
		if err := mapInput(input, &req); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(req.TaskID) == "" {
			return nil, nil, fmt.Errorf("%w: taskId is required", apperrors.ErrInvalidArgument)
		}
		items, err := s.delivery.ListExecutionLogs(ctx, principal, req.TaskID, req.Limit)
		items = redactExecutionLogs(items)
		return items, map[string]any{"executionTaskId": req.TaskID, "count": len(items)}, err
	case "delivery.approval_policies.list":
		items, err := s.delivery.ListApprovalPolicies(ctx, principal)
		return items, map[string]any{"count": len(items)}, err
	case "delivery.workflow_templates.list":
		if s.catalog == nil {
			return nil, nil, fmt.Errorf("%w: catalog gateway services are not configured", apperrors.ErrInvalidArgument)
		}
		items, err := s.catalog.ListWorkflowTemplates(ctx, principal)
		return items, map[string]any{"count": len(items)}, err
	case "delivery.release_context.diff":
		return s.buildReleaseContextDiff(ctx, principal, input)
	case "delivery.rollback.context":
		return s.buildRollbackContext(ctx, principal, input)
	default:
		return nil, nil, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
}
func (s *Service) invokeKubernetesTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	if s.resources == nil {
		return nil, nil, fmt.Errorf("%w: Kubernetes resource gateway service is not configured", apperrors.ErrInvalidArgument)
	}
	var req struct {
		ClusterID      string `json:"clusterId"`
		Namespace      string `json:"namespace"`
		PodName        string `json:"podName"`
		DeploymentName string `json:"deploymentName"`
		ServiceName    string `json:"serviceName"`
		NodeName       string `json:"nodeName"`
		Container      string `json:"container"`
		TailLines      int64  `json:"tailLines"`
		SinceSeconds   int64  `json:"sinceSeconds"`
		Previous       bool   `json:"previous"`
		Limit          int    `json:"limit"`
	}
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	req.ClusterID = strings.TrimSpace(req.ClusterID)
	req.Namespace = strings.TrimSpace(req.Namespace)
	if req.ClusterID == "" {
		return nil, nil, fmt.Errorf("%w: clusterId is required", apperrors.ErrInvalidArgument)
	}
	related := map[string]any{"clusterId": req.ClusterID, "namespace": req.Namespace}
	switch tool.Name {
	case "k8s.pods.list":
		items, err := s.resources.ListPods(ctx, principal, req.ClusterID, req.Namespace)
		related["count"] = len(items)
		return items, related, err
	case "k8s.pods.logs":
		req.PodName = strings.TrimSpace(req.PodName)
		if req.PodName == "" {
			return nil, related, fmt.Errorf("%w: podName is required", apperrors.ErrInvalidArgument)
		}
		item, err := s.resources.GetPodLogs(ctx, principal, req.ClusterID, req.Namespace, req.PodName, req.Container, req.TailLines, req.SinceSeconds, req.Previous)
		item = redactPodLogs(item)
		related["podName"] = req.PodName
		related["container"] = strings.TrimSpace(req.Container)
		return item, related, err
	case "k8s.pods.describe":
		req.PodName = strings.TrimSpace(req.PodName)
		if req.Namespace == "" || req.PodName == "" {
			return nil, related, fmt.Errorf("%w: namespace and podName are required", apperrors.ErrInvalidArgument)
		}
		item, err := s.resources.GetPodDetail(ctx, principal, req.ClusterID, req.Namespace, req.PodName)
		related["podName"] = req.PodName
		return podDescribeContext(item), related, err
	case "k8s.deployments.list":
		items, err := s.resources.ListDeployments(ctx, principal, req.ClusterID, req.Namespace)
		related["count"] = len(items)
		return items, related, err
	case "k8s.deployments.rollout_status":
		req.DeploymentName = strings.TrimSpace(req.DeploymentName)
		if req.Namespace == "" || req.DeploymentName == "" {
			return nil, related, fmt.Errorf("%w: namespace and deploymentName are required", apperrors.ErrInvalidArgument)
		}
		item, err := s.resources.GetDeploymentRolloutStatus(ctx, principal, req.ClusterID, req.Namespace, req.DeploymentName)
		related["deploymentName"] = req.DeploymentName
		return item, related, err
	case "k8s.deployments.events":
		req.DeploymentName = strings.TrimSpace(req.DeploymentName)
		if req.Namespace == "" || req.DeploymentName == "" {
			return nil, related, fmt.Errorf("%w: namespace and deploymentName are required", apperrors.ErrInvalidArgument)
		}
		limit := req.Limit
		if limit <= 0 {
			limit = 100
		}
		items, err := s.resources.ListClusterEvents(ctx, principal, req.ClusterID, req.Namespace, limit)
		filtered := filterEventsForDiagnosis(items, "", req.DeploymentName)
		related["deploymentName"] = req.DeploymentName
		related["count"] = len(filtered)
		related["limit"] = limit
		return filtered, related, err
	case "k8s.services.list":
		items, err := s.resources.ListServices(ctx, principal, req.ClusterID, req.Namespace)
		related["count"] = len(items)
		return items, related, err
	case "k8s.services.backends":
		req.ServiceName = strings.TrimSpace(req.ServiceName)
		if req.Namespace == "" || req.ServiceName == "" {
			return nil, related, fmt.Errorf("%w: namespace and serviceName are required", apperrors.ErrInvalidArgument)
		}
		item, err := s.serviceBackendContext(ctx, principal, req.ClusterID, req.Namespace, req.ServiceName)
		related["serviceName"] = req.ServiceName
		related["backendPodCount"] = item["backendPodCount"]
		return item, related, err
	case "k8s.routes.context":
		item, err := s.routeContext(ctx, principal, req.ClusterID, req.Namespace, req.ServiceName)
		related["serviceName"] = strings.TrimSpace(req.ServiceName)
		related["ingressCount"] = item["ingressCount"]
		related["httpRouteCount"] = item["httpRouteCount"]
		return item, related, err
	case "k8s.storage.context":
		item, err := s.storageContext(ctx, principal, req.ClusterID, req.Namespace)
		related["persistentVolumeClaimCount"] = item["persistentVolumeClaimCount"]
		related["persistentVolumeCount"] = item["persistentVolumeCount"]
		related["storageClassCount"] = item["storageClassCount"]
		return item, related, err
	case "k8s.nodes.detail":
		req.NodeName = strings.TrimSpace(req.NodeName)
		if req.NodeName == "" {
			return nil, related, fmt.Errorf("%w: nodeName is required", apperrors.ErrInvalidArgument)
		}
		item, err := s.resources.GetNodeDetail(ctx, principal, req.ClusterID, req.NodeName)
		related["nodeName"] = req.NodeName
		related["scheduledPodCount"] = len(item.Pods)
		return item, related, err
	case "k8s.events.list":
		limit := req.Limit
		if limit <= 0 {
			limit = 100
		}
		items, err := s.resources.ListClusterEvents(ctx, principal, req.ClusterID, req.Namespace, limit)
		related["count"] = len(items)
		related["limit"] = limit
		return items, related, err
	default:
		return nil, related, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, tool.Name)
	}
}
func (s *Service) buildReleaseContextDiff(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var req struct {
		ApplicationID            string `json:"applicationId"`
		ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
		SourceBundleID           string `json:"sourceBundleId"`
		TargetBundleID           string `json:"targetBundleId"`
		ReleaseBundleID          string `json:"releaseBundleId"`
		Limit                    int    `json:"limit"`
	}
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	req.ApplicationID = strings.TrimSpace(req.ApplicationID)
	req.ApplicationEnvironmentID = strings.TrimSpace(req.ApplicationEnvironmentID)
	req.SourceBundleID = strings.TrimSpace(req.SourceBundleID)
	req.TargetBundleID = firstNonEmpty(req.TargetBundleID, req.ReleaseBundleID)
	if req.ApplicationID == "" {
		return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	detail, err := s.delivery.GetApplicationDetail(ctx, principal, req.ApplicationID)
	if err != nil {
		return nil, nil, err
	}
	bundles, err := s.delivery.ListReleaseBundles(ctx, principal, domaindelivery.ReleaseBundleFilter{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		Limit:                    limit,
	})
	if err != nil {
		return nil, nil, err
	}
	tasks, err := s.delivery.ListExecutionTasks(ctx, principal, domaindelivery.ExecutionTaskFilter{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		ReleaseBundleID:          firstNonEmpty(req.TargetBundleID, req.SourceBundleID),
		Limit:                    limit,
	})
	if err != nil {
		return nil, nil, err
	}

	var sourceBundle *domaindelivery.ReleaseBundle
	var targetBundle *domaindelivery.ReleaseBundle
	if req.SourceBundleID != "" {
		sourceBundle, err = s.releaseBundleForContext(ctx, principal, req.SourceBundleID, bundles)
		if err != nil {
			return nil, nil, err
		}
	}
	if req.TargetBundleID != "" {
		targetBundle, err = s.releaseBundleForContext(ctx, principal, req.TargetBundleID, bundles)
		if err != nil {
			return nil, nil, err
		}
	}
	if targetBundle == nil && len(bundles) > 0 {
		copyItem := bundles[0]
		targetBundle = &copyItem
	}

	bindingSummaries := filterBindingSummaries(detail.Bindings, req.ApplicationEnvironmentID)
	output := map[string]any{
		"summary": "collected delivery release diff and promotion context",
		"scope": map[string]any{
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"sourceBundleId":           req.SourceBundleID,
			"targetBundleId":           req.TargetBundleID,
		},
		"application":    redactedApplication(detail.Application),
		"bindings":       redactedBindingSummaries(bindingSummaries),
		"releaseBundles": redactedReleaseBundles(bundles),
		"sourceBundle":   redactedReleaseBundlePtr(sourceBundle),
		"targetBundle":   redactedReleaseBundlePtr(targetBundle),
		"executionTasks": redactedExecutionTasks(tasks),
		"comparison":     compareReleaseBundles(sourceBundle, targetBundle),
		"nextChecks": []string{
			"Verify target binding release policy, approval policy, and enabled release targets before triggering a promotion.",
			"Inspect execution task logs for the candidate bundle if recent tasks are not successful.",
		},
	}
	related := map[string]any{
		"applicationId":            req.ApplicationID,
		"applicationEnvironmentId": req.ApplicationEnvironmentID,
		"releaseBundleCount":       len(bundles),
		"executionTaskCount":       len(tasks),
	}
	if sourceBundle != nil {
		related["sourceBundleId"] = sourceBundle.ID
	}
	if targetBundle != nil {
		related["targetBundleId"] = targetBundle.ID
	}
	return output, related, nil
}
func (s *Service) buildRollbackContext(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var req struct {
		ApplicationID            string `json:"applicationId"`
		ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
		ReleaseBundleID          string `json:"releaseBundleId"`
		ExecutionTaskID          string `json:"executionTaskId"`
		Limit                    int    `json:"limit"`
		LogLimit                 int    `json:"logLimit"`
	}
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	req.ApplicationID = strings.TrimSpace(req.ApplicationID)
	req.ApplicationEnvironmentID = strings.TrimSpace(req.ApplicationEnvironmentID)
	req.ReleaseBundleID = strings.TrimSpace(req.ReleaseBundleID)
	req.ExecutionTaskID = strings.TrimSpace(req.ExecutionTaskID)
	if req.ApplicationID == "" {
		return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	logLimit := req.LogLimit
	if logLimit <= 0 {
		logLimit = 100
	}

	detail, err := s.delivery.GetApplicationDetail(ctx, principal, req.ApplicationID)
	if err != nil {
		return nil, nil, err
	}
	bundles, err := s.delivery.ListReleaseBundles(ctx, principal, domaindelivery.ReleaseBundleFilter{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		Limit:                    limit,
	})
	if err != nil {
		return nil, nil, err
	}
	tasks, err := s.delivery.ListExecutionTasks(ctx, principal, domaindelivery.ExecutionTaskFilter{
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		ReleaseBundleID:          req.ReleaseBundleID,
		Limit:                    limit,
	})
	if err != nil {
		return nil, nil, err
	}

	var currentTask *domaindelivery.ExecutionTask
	if req.ExecutionTaskID != "" {
		currentTask, err = s.executionTaskForContext(ctx, principal, req.ExecutionTaskID, tasks)
		if err != nil {
			return nil, nil, err
		}
	}
	if currentTask == nil && len(tasks) > 0 {
		copyItem := tasks[0]
		currentTask = &copyItem
	}
	logs := []domaindelivery.ExecutionLog{}
	if currentTask != nil {
		logs, err = s.delivery.ListExecutionLogs(ctx, principal, currentTask.ID, logLimit)
		if err != nil {
			return nil, nil, err
		}
		logs = redactExecutionLogs(logs)
	}

	bindingSummaries := filterBindingSummaries(detail.Bindings, req.ApplicationEnvironmentID)
	output := map[string]any{
		"summary": "collected read-only rollback suggestion context",
		"scope": map[string]any{
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"releaseBundleId":          req.ReleaseBundleID,
			"executionTaskId":          req.ExecutionTaskID,
		},
		"application":    redactedApplication(detail.Application),
		"bindings":       redactedBindingSummaries(bindingSummaries),
		"releaseBundles": redactedReleaseBundles(bundles),
		"executionTasks": redactedExecutionTasks(tasks),
		"currentTask":    redactedExecutionTaskPtr(currentTask),
		"executionLogs":  logs,
		"suggestions":    rollbackSuggestions(bindingSummaries, bundles, currentTask),
		"nextChecks": []string{
			"Confirm the selected previous bundle or image tag is valid for the target environment.",
			"Triggering rollback is intentionally outside this read-only tool and must use delivery.actions.trigger with policy approval.",
		},
	}
	related := map[string]any{
		"applicationId":            req.ApplicationID,
		"applicationEnvironmentId": req.ApplicationEnvironmentID,
		"releaseBundleCount":       len(bundles),
		"executionTaskCount":       len(tasks),
		"executionLogCount":        len(logs),
	}
	if currentTask != nil {
		related["executionTaskId"] = currentTask.ID
	}
	return output, related, nil
}
func (s *Service) invokeReleaseFailureDiagnosis(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var req struct {
		ApplicationID            string `json:"applicationId"`
		ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
		ReleaseBundleID          string `json:"releaseBundleId"`
		ExecutionTaskID          string `json:"executionTaskId"`
		ClusterID                string `json:"clusterId"`
		Namespace                string `json:"namespace"`
		WorkloadKind             string `json:"workloadKind"`
		WorkloadName             string `json:"workloadName"`
		PodName                  string `json:"podName"`
		Container                string `json:"container"`
		LogLimit                 int    `json:"logLimit"`
		EventLimit               int    `json:"eventLimit"`
		AgentProviderID          string `json:"agentProviderId"`
		ProviderID               string `json:"providerId"`
		DeepAnalysis             bool   `json:"deepAnalysis"`
		ExternalAnalysis         bool   `json:"externalAnalysis"`
		TimeoutSeconds           int    `json:"timeoutSeconds"`
	}
	if err := mapInput(input, &req); err != nil {
		return nil, nil, err
	}
	agentProviderID := firstNonEmpty(strings.TrimSpace(req.AgentProviderID), strings.TrimSpace(req.ProviderID))
	diagnosisReq := releaseFailureDiagnosisRequest{
		ApplicationID:            strings.TrimSpace(req.ApplicationID),
		ApplicationEnvironmentID: strings.TrimSpace(req.ApplicationEnvironmentID),
		ReleaseBundleID:          strings.TrimSpace(req.ReleaseBundleID),
		ExecutionTaskID:          strings.TrimSpace(req.ExecutionTaskID),
		ClusterID:                strings.TrimSpace(req.ClusterID),
		Namespace:                strings.TrimSpace(req.Namespace),
		WorkloadKind:             strings.TrimSpace(req.WorkloadKind),
		WorkloadName:             strings.TrimSpace(req.WorkloadName),
		PodName:                  strings.TrimSpace(req.PodName),
		Container:                strings.TrimSpace(req.Container),
		AgentProviderID:          agentProviderID,
		DeepAnalysis:             req.DeepAnalysis || req.ExternalAnalysis || agentProviderID != "",
		TimeoutSeconds:           req.TimeoutSeconds,
	}
	req.ApplicationID = strings.TrimSpace(req.ApplicationID)
	req.ApplicationEnvironmentID = strings.TrimSpace(req.ApplicationEnvironmentID)
	req.ReleaseBundleID = strings.TrimSpace(req.ReleaseBundleID)
	req.ExecutionTaskID = strings.TrimSpace(req.ExecutionTaskID)
	req.ClusterID = strings.TrimSpace(req.ClusterID)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.WorkloadKind = strings.TrimSpace(req.WorkloadKind)
	req.WorkloadName = strings.TrimSpace(req.WorkloadName)
	req.PodName = strings.TrimSpace(req.PodName)
	req.Container = strings.TrimSpace(req.Container)
	related := map[string]any{
		"applicationId":            req.ApplicationID,
		"applicationEnvironmentId": req.ApplicationEnvironmentID,
		"releaseBundleId":          req.ReleaseBundleID,
		"executionTaskId":          req.ExecutionTaskID,
		"clusterId":                req.ClusterID,
		"namespace":                req.Namespace,
	}
	contextView := map[string]any{
		"summary": "collected release failure diagnosis context",
		"scope": map[string]any{
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"releaseBundleId":          req.ReleaseBundleID,
			"executionTaskId":          req.ExecutionTaskID,
			"clusterId":                req.ClusterID,
			"namespace":                req.Namespace,
			"workloadKind":             req.WorkloadKind,
			"workloadName":             req.WorkloadName,
			"podName":                  req.PodName,
		},
		"delivery": map[string]any{},
		"runtime":  map[string]any{},
		"findings": []string{
			"Evidence is collected through soha application services; this Gateway tool does not execute cluster mutations.",
		},
		"nextChecks": []string{},
	}
	deliveryEvidence := contextView["delivery"].(map[string]any)
	runtimeEvidence := contextView["runtime"].(map[string]any)
	nextChecks := []string{}

	if s.delivery != nil {
		if taskID := req.ExecutionTaskID; taskID != "" {
			limit := req.LogLimit
			if limit <= 0 {
				limit = 100
			}
			logs, err := s.delivery.ListExecutionLogs(ctx, principal, taskID, limit)
			if err != nil {
				deliveryEvidence["executionLogsError"] = err.Error()
				nextChecks = append(nextChecks, "Re-check execution task logs after the delivery control plane is reachable.")
			} else {
				logs = redactExecutionLogs(logs)
				deliveryEvidence["executionLogs"] = logs
				deliveryEvidence["executionLogCount"] = len(logs)
				related["executionLogCount"] = len(logs)
			}
		}
		if bundleID := req.ReleaseBundleID; bundleID != "" {
			artifacts, err := s.delivery.ListReleaseBundleArtifacts(ctx, principal, bundleID)
			if err != nil {
				deliveryEvidence["releaseBundleArtifactsError"] = err.Error()
			} else {
				deliveryEvidence["releaseBundleArtifacts"] = artifacts
				deliveryEvidence["releaseBundleArtifactCount"] = len(artifacts)
				related["releaseBundleArtifactCount"] = len(artifacts)
			}
		}
		if req.ApplicationID != "" || req.ApplicationEnvironmentID != "" || req.ReleaseBundleID != "" {
			tasks, err := s.delivery.ListExecutionTasks(ctx, principal, domaindelivery.ExecutionTaskFilter{
				ApplicationID:            req.ApplicationID,
				ApplicationEnvironmentID: req.ApplicationEnvironmentID,
				ReleaseBundleID:          req.ReleaseBundleID,
				Limit:                    10,
			})
			if err != nil {
				deliveryEvidence["executionTasksError"] = err.Error()
			} else {
				deliveryEvidence["executionTasks"] = tasks
				deliveryEvidence["executionTaskCount"] = len(tasks)
			}
		}
	} else {
		deliveryEvidence["error"] = "delivery gateway services are not configured"
		nextChecks = append(nextChecks, "Configure delivery services before collecting release execution evidence.")
	}

	if s.resources != nil && req.ClusterID != "" {
		clusterID := req.ClusterID
		namespace := req.Namespace
		eventLimit := req.EventLimit
		if eventLimit <= 0 {
			eventLimit = 100
		}
		if pods, err := s.resources.ListPods(ctx, principal, clusterID, namespace); err != nil {
			runtimeEvidence["podsError"] = err.Error()
		} else {
			runtimeEvidence["pods"] = filterPodsForDiagnosis(pods, req.PodName, req.WorkloadName)
		}
		if deployments, err := s.resources.ListDeployments(ctx, principal, clusterID, namespace); err != nil {
			runtimeEvidence["deploymentsError"] = err.Error()
		} else {
			runtimeEvidence["deployments"] = filterDeploymentsForDiagnosis(deployments, req.WorkloadName)
		}
		if services, err := s.resources.ListServices(ctx, principal, clusterID, namespace); err != nil {
			runtimeEvidence["servicesError"] = err.Error()
		} else {
			runtimeEvidence["services"] = services
		}
		if events, err := s.resources.ListClusterEvents(ctx, principal, clusterID, namespace, eventLimit); err != nil {
			runtimeEvidence["eventsError"] = err.Error()
		} else {
			runtimeEvidence["events"] = filterEventsForDiagnosis(events, req.PodName, req.WorkloadName)
		}
		if podName := req.PodName; podName != "" {
			logs, err := s.resources.GetPodLogs(ctx, principal, clusterID, namespace, podName, req.Container, 200, 0, false)
			if err != nil {
				runtimeEvidence["podLogsError"] = err.Error()
			} else {
				runtimeEvidence["podLogs"] = redactPodLogs(logs)
			}
		}
	} else if req.ClusterID == "" {
		nextChecks = append(nextChecks, "Provide clusterId and namespace to collect runtime Kubernetes evidence.")
	} else {
		runtimeEvidence["error"] = "Kubernetes resource gateway service is not configured"
	}

	contextView["nextChecks"] = nextChecks
	if s.copilot != nil {
		artifactInput := buildReleaseFailureArtifactInput(diagnosisReq, input, contextView)
		if diagnosisReq.DeepAnalysis {
			run, err := s.copilot.QueueGatewayAnalysisAgentRun(ctx, principal, domaincopilot.GatewayAnalysisAgentRunInput{
				GatewayAnalysisArtifactInput: artifactInput,
				AgentProviderID:              agentProviderID,
				TimeoutSeconds:               req.TimeoutSeconds,
			})
			if err != nil {
				contextView["analysisArtifactError"] = err.Error()
				nextChecks = append(nextChecks, "Retry external Agent Runtime queueing after the provider is available.")
				contextView["nextChecks"] = nextChecks
				related["analysisArtifactError"] = err.Error()
			} else {
				contextView["analysisArtifact"] = map[string]any{
					"agentRunId":     run.ID,
					"capabilityId":   run.CapabilityID,
					"providerId":     run.ProviderID,
					"providerKind":   run.ProviderKind,
					"status":         run.Status,
					"queued":         true,
					"artifactStored": false,
					"runtime":        "agent_runtime_claim_callback",
				}
				related["agentRunId"] = run.ID
				related["agentProviderId"] = run.ProviderID
				related["agentRunStatus"] = run.Status
			}
		} else {
			run, err := s.copilot.RecordGatewayAnalysisArtifact(ctx, principal, artifactInput)
			if err != nil {
				contextView["analysisArtifactError"] = err.Error()
				nextChecks = append(nextChecks, "Retry analysis artifact persistence after the AI Workbench runtime is available.")
				contextView["nextChecks"] = nextChecks
				related["analysisArtifactError"] = err.Error()
			} else {
				contextView["analysisArtifact"] = map[string]any{
					"agentRunId":     run.ID,
					"capabilityId":   run.CapabilityID,
					"status":         run.Status,
					"artifactCount":  len(run.AnalysisArtifacts),
					"artifactKind":   firstAnalysisArtifactKind(run.AnalysisArtifacts),
					"artifactRunId":  firstAnalysisArtifactRunID(run.AnalysisArtifacts),
					"artifactTitle":  firstAnalysisArtifactTitle(run.AnalysisArtifacts),
					"artifactStored": len(run.AnalysisArtifacts) > 0,
				}
				related["agentRunId"] = run.ID
				related["analysisArtifactCount"] = len(run.AnalysisArtifacts)
			}
		}
	} else {
		contextView["analysisArtifact"] = map[string]any{
			"artifactStored": false,
			"reason":         "AI Workbench artifact recorder is not configured",
		}
	}
	return contextView, related, nil
}

type releaseFailureDiagnosisRequest struct {
	ApplicationID            string
	ApplicationEnvironmentID string
	ReleaseBundleID          string
	ExecutionTaskID          string
	ClusterID                string
	Namespace                string
	WorkloadKind             string
	WorkloadName             string
	PodName                  string
	Container                string
	AgentProviderID          string
	DeepAnalysis             bool
	TimeoutSeconds           int
}

func buildReleaseFailureArtifactInput(req releaseFailureDiagnosisRequest, input map[string]any, contextView map[string]any) domaincopilot.GatewayAnalysisArtifactInput {
	delivery := mapValue(contextView["delivery"])
	runtime := mapValue(contextView["runtime"])
	nextChecks := stringSliceValue(contextView["nextChecks"])
	evidence := releaseFailureEvidence(req, delivery, runtime)
	recommendations := normalizeStringSlice(append([]string{
		"Review the persisted Gateway evidence before attempting any rollback, restart, or redeploy action.",
		"Use delivery.release_context.diff or delivery.rollback.context for read-only release comparison before triggering a mutation.",
	}, nextChecks...))
	hypotheses := releaseFailureHypotheses(evidence, recommendations)
	scope := domaincopilot.SessionScope{
		ClusterID: req.ClusterID,
		Namespace: req.Namespace,
		Workload:  firstNonEmpty(req.WorkloadName, req.PodName),
	}
	output := map[string]any{
		"summary":             contextView["summary"],
		"scope":               contextView["scope"],
		"evidenceSummary":     artifactEvidenceSnapshot(delivery, runtime),
		"nextChecks":          nextChecks,
		"recommendationCount": len(recommendations),
	}
	return domaincopilot.GatewayAnalysisArtifactInput{
		CapabilityID:    "delivery_failure",
		Title:           releaseFailureArtifactTitle(req),
		Summary:         releaseFailureArtifactSummary(req, evidence),
		SkillIDs:        []string{"delivery-tester", "k8s-sre"},
		Scope:           scope,
		Input:           sanitizeGatewayMap(input),
		Output:          sanitizeGatewayMap(output),
		Evidence:        evidence,
		Hypotheses:      hypotheses,
		Recommendations: recommendations,
		ToolExecutions: []domaincopilot.ToolExecution{{
			ID:        "gateway:" + uuid.NewString(),
			AdapterID: "platform-native.v1",
			ToolName:  "diagnosis.release_failure.analyze",
			Status:    "completed",
			Summary:   "Collected release failure evidence through AI Gateway application services.",
			Input: map[string]any{
				"applicationId":            req.ApplicationID,
				"applicationEnvironmentId": req.ApplicationEnvironmentID,
				"releaseBundleId":          req.ReleaseBundleID,
				"executionTaskId":          req.ExecutionTaskID,
				"clusterId":                req.ClusterID,
				"namespace":                req.Namespace,
				"workloadKind":             req.WorkloadKind,
				"workloadName":             req.WorkloadName,
				"podName":                  req.PodName,
			},
			Output:    artifactEvidenceSnapshot(delivery, runtime),
			StartedAt: time.Now().UTC(),
		}},
		Graph: buildReleaseFailureGraph(scope, req, evidence),
		DataSourceSnapshot: map[string]any{
			"source":                   "ai-gateway",
			"toolName":                 "diagnosis.release_failure.analyze",
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"releaseBundleId":          req.ReleaseBundleID,
			"executionTaskId":          req.ExecutionTaskID,
			"clusterId":                req.ClusterID,
			"namespace":                req.Namespace,
			"workloadKind":             req.WorkloadKind,
			"workloadName":             req.WorkloadName,
			"podName":                  req.PodName,
			"agentProviderId":          req.AgentProviderID,
			"deepAnalysis":             req.DeepAnalysis,
			"redactionBoundary":        "gateway",
			"rawLogsPersisted":         false,
			"deliveryEvidence":         artifactDeliverySnapshot(delivery),
			"runtimeEvidence":          artifactRuntimeSnapshot(runtime),
		},
	}
}
func releaseFailureArtifactTitle(req releaseFailureDiagnosisRequest) string {
	target := firstNonEmpty(req.WorkloadName, req.PodName, req.ExecutionTaskID, req.ReleaseBundleID, req.ApplicationID, "release failure")
	return "Delivery failure diagnosis: " + target
}
func releaseFailureArtifactSummary(req releaseFailureDiagnosisRequest, evidence []domaincopilot.RootCauseEvidence) string {
	parts := []string{"Gateway collected a read-only delivery failure diagnosis artifact"}
	if target := firstNonEmpty(req.ApplicationID, req.ApplicationEnvironmentID, req.ReleaseBundleID, req.ExecutionTaskID); target != "" {
		parts = append(parts, "for "+target)
	}
	if runtime := firstNonEmpty(req.ClusterID, req.Namespace, req.WorkloadName, req.PodName); runtime != "" {
		parts = append(parts, "with runtime scope "+runtime)
	}
	parts = append(parts, fmt.Sprintf("and %d evidence summaries.", len(evidence)))
	return strings.Join(parts, " ")
}
func releaseFailureEvidence(req releaseFailureDiagnosisRequest, delivery, runtime map[string]any) []domaincopilot.RootCauseEvidence {
	items := make([]domaincopilot.RootCauseEvidence, 0)
	if count := intFromAny(delivery["executionLogCount"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "delivery:execution-logs:" + firstNonEmpty(req.ExecutionTaskID, "current"),
			Kind:      "delivery.execution_logs",
			Title:     "Delivery execution logs",
			Summary:   fmt.Sprintf("%d redacted execution log entries were collected through delivery service.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"executionTaskId": req.ExecutionTaskID,
				"logCount":        count,
				"rawLogsStored":   false,
			},
		})
	}
	if count := intFromAny(delivery["executionTaskCount"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "delivery:execution-tasks:" + firstNonEmpty(req.ReleaseBundleID, req.ApplicationID, "current"),
			Kind:      "delivery.execution_tasks",
			Title:     "Related execution tasks",
			Summary:   fmt.Sprintf("%d execution task summaries matched the release failure scope.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"applicationId":            req.ApplicationID,
				"applicationEnvironmentId": req.ApplicationEnvironmentID,
				"releaseBundleId":          req.ReleaseBundleID,
				"executionTaskCount":       count,
			},
		})
	}
	if count := intFromAny(delivery["releaseBundleArtifactCount"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "delivery:release-bundle-artifacts:" + firstNonEmpty(req.ReleaseBundleID, "current"),
			Kind:      "delivery.release_bundle_artifacts",
			Title:     "Release bundle artifacts",
			Summary:   fmt.Sprintf("%d release bundle artifact summaries were collected.", count),
			Severity:  "info",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"releaseBundleId": req.ReleaseBundleID,
				"artifactCount":   count,
			},
		})
	}
	if count := sliceLen(runtime["pods"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "runtime:pods:" + firstNonEmpty(req.PodName, req.WorkloadName, "selected"),
			Kind:      "k8s.pods",
			Title:     "Runtime pods",
			Summary:   fmt.Sprintf("%d pod summaries matched the diagnosis scope.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"podName":      req.PodName,
				"workloadName": req.WorkloadName,
				"podCount":     count,
			},
		})
	}
	if count := sliceLen(runtime["deployments"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "runtime:deployments:" + firstNonEmpty(req.WorkloadName, "selected"),
			Kind:      "k8s.deployments",
			Title:     "Runtime deployments",
			Summary:   fmt.Sprintf("%d deployment summaries matched the diagnosis scope.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"workloadName":    req.WorkloadName,
				"deploymentCount": count,
			},
		})
	}
	if count := sliceLen(runtime["services"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:         "runtime:services:" + firstNonEmpty(req.Namespace, req.ClusterID, "selected"),
			Kind:       "k8s.services",
			Title:      "Runtime services",
			Summary:    fmt.Sprintf("%d service summaries were collected for backend correlation.", count),
			Severity:   "info",
			ClusterID:  req.ClusterID,
			Namespace:  req.Namespace,
			Attributes: map[string]any{"serviceCount": count},
		})
	}
	if count := sliceLen(runtime["events"]); count > 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "runtime:events:" + firstNonEmpty(req.PodName, req.WorkloadName, "selected"),
			Kind:      "k8s.events",
			Title:     "Runtime events",
			Summary:   fmt.Sprintf("%d Kubernetes event summaries matched the diagnosis scope.", count),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"eventCount":   count,
				"podName":      req.PodName,
				"workloadName": req.WorkloadName,
			},
		})
	}
	if podLogs, ok := runtime["podLogs"].(domainresource.PodLogsView); ok && strings.TrimSpace(podLogs.Content) != "" {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "runtime:pod-logs:" + firstNonEmpty(req.PodName, podLogs.PodName, "selected"),
			Kind:      "k8s.pod_logs",
			Title:     "Runtime pod logs",
			Summary:   fmt.Sprintf("Redacted pod log sample was collected for %s/%s.", podLogs.Namespace, podLogs.PodName),
			Severity:  "warning",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"podName":       podLogs.PodName,
				"container":     podLogs.Container,
				"contentBytes":  podLogs.ContentBytes,
				"truncated":     podLogs.Truncated,
				"rawLogsStored": false,
			},
		})
	}
	for key, value := range delivery {
		if !strings.HasSuffix(key, "Error") || strings.TrimSpace(fmt.Sprint(value)) == "" {
			continue
		}
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:         "delivery:error:" + strings.TrimSuffix(key, "Error"),
			Kind:       "delivery.error",
			Title:      key,
			Summary:    redactSensitiveText(fmt.Sprint(value)),
			Severity:   "warning",
			ClusterID:  req.ClusterID,
			Namespace:  req.Namespace,
			Attributes: map[string]any{"source": "delivery", "field": key},
		})
	}
	for key, value := range runtime {
		if !strings.HasSuffix(key, "Error") || strings.TrimSpace(fmt.Sprint(value)) == "" {
			continue
		}
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:         "runtime:error:" + strings.TrimSuffix(key, "Error"),
			Kind:       "k8s.error",
			Title:      key,
			Summary:    redactSensitiveText(fmt.Sprint(value)),
			Severity:   "warning",
			ClusterID:  req.ClusterID,
			Namespace:  req.Namespace,
			Attributes: map[string]any{"source": "runtime", "field": key},
		})
	}
	if len(items) == 0 {
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "gateway:diagnosis-context",
			Kind:      "gateway.context",
			Title:     "Gateway diagnosis context",
			Summary:   "Gateway completed the release failure diagnosis request, but no concrete delivery or runtime evidence summaries were available.",
			Severity:  "info",
			ClusterID: req.ClusterID,
			Namespace: req.Namespace,
			Attributes: map[string]any{
				"applicationId":   req.ApplicationID,
				"releaseBundleId": req.ReleaseBundleID,
				"executionTaskId": req.ExecutionTaskID,
			},
		})
	}
	return items
}
func releaseFailureHypotheses(evidence []domaincopilot.RootCauseEvidence, recommendations []string) []domaincopilot.RootCauseHypothesis {
	evidenceIDs := make([]string, 0, len(evidence))
	hasDeliveryFailure := false
	hasRuntimeSignal := false
	for _, item := range evidence {
		evidenceIDs = append(evidenceIDs, item.ID)
		if strings.HasPrefix(item.Kind, "delivery.") {
			hasDeliveryFailure = true
		}
		if strings.HasPrefix(item.Kind, "k8s.") {
			hasRuntimeSignal = true
		}
	}
	if hasDeliveryFailure && hasRuntimeSignal {
		return []domaincopilot.RootCauseHypothesis{{
			ID:              "hypothesis:release-runtime-correlation",
			Title:           "Delivery failure correlates with runtime evidence",
			Summary:         "Both delivery control-plane evidence and Kubernetes runtime evidence were collected for the same release scope.",
			Confidence:      70,
			EvidenceIDs:     evidenceIDs,
			Recommendations: recommendations,
		}}
	}
	if hasDeliveryFailure {
		return []domaincopilot.RootCauseHypothesis{{
			ID:              "hypothesis:delivery-control-plane",
			Title:           "Delivery control-plane failure is the primary signal",
			Summary:         "Delivery task, bundle, artifact, or log summaries are available, but runtime evidence is missing or inconclusive.",
			Confidence:      60,
			EvidenceIDs:     evidenceIDs,
			Recommendations: recommendations,
		}}
	}
	if hasRuntimeSignal {
		return []domaincopilot.RootCauseHypothesis{{
			ID:              "hypothesis:runtime-state",
			Title:           "Runtime state is the primary signal",
			Summary:         "Kubernetes runtime summaries are available, but delivery control-plane evidence is missing or inconclusive.",
			Confidence:      55,
			EvidenceIDs:     evidenceIDs,
			Recommendations: recommendations,
		}}
	}
	return []domaincopilot.RootCauseHypothesis{{
		ID:              "hypothesis:insufficient-evidence",
		Title:           "Insufficient evidence",
		Summary:         "The Gateway request completed without enough evidence to identify a likely release failure source.",
		Confidence:      30,
		EvidenceIDs:     evidenceIDs,
		Recommendations: recommendations,
	}}
}
func buildReleaseFailureGraph(scope domaincopilot.SessionScope, req releaseFailureDiagnosisRequest, evidence []domaincopilot.RootCauseEvidence) *domaincopilot.AnalysisGraph {
	rootID := "scope:" + firstNonEmpty(scope.Workload, scope.Namespace, scope.ClusterID, req.ApplicationID, "release-failure")
	nodes := []domaincopilot.AnalysisGraphNode{{
		ID:         rootID,
		Kind:       "scope",
		Title:      firstNonEmpty(scope.Workload, scope.Namespace, scope.ClusterID, req.ApplicationID, "release failure"),
		Subtitle:   strings.Join(compactStrings(req.ApplicationID, req.ApplicationEnvironmentID, req.ReleaseBundleID, req.ExecutionTaskID, req.ClusterID, req.Namespace), " / "),
		SourceRefs: []string{"ai-gateway"},
		Attributes: map[string]any{
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"releaseBundleId":          req.ReleaseBundleID,
			"executionTaskId":          req.ExecutionTaskID,
			"clusterId":                req.ClusterID,
			"namespace":                req.Namespace,
			"workloadName":             req.WorkloadName,
			"podName":                  req.PodName,
		},
	}}
	edges := make([]domaincopilot.AnalysisGraphEdge, 0, len(evidence))
	for _, item := range evidence {
		nodeID := "evidence:" + item.ID
		nodes = append(nodes, domaincopilot.AnalysisGraphNode{
			ID:          nodeID,
			Kind:        item.Kind,
			Title:       item.Title,
			Subtitle:    item.Summary,
			Severity:    item.Severity,
			EvidenceIDs: []string{item.ID},
			SourceRefs:  []string{"ai-gateway"},
			Attributes:  item.Attributes,
		})
		edges = append(edges, domaincopilot.AnalysisGraphEdge{
			ID:          rootID + "->" + nodeID,
			Source:      rootID,
			Target:      nodeID,
			Relation:    "uses",
			Severity:    item.Severity,
			EvidenceIDs: []string{item.ID},
		})
	}
	return &domaincopilot.AnalysisGraph{Layout: "LR", FocusNodeID: rootID, Nodes: nodes, Edges: edges}
}
func artifactEvidenceSnapshot(delivery, runtime map[string]any) map[string]any {
	return map[string]any{
		"delivery": artifactDeliverySnapshot(delivery),
		"runtime":  artifactRuntimeSnapshot(runtime),
	}
}
func artifactDeliverySnapshot(delivery map[string]any) map[string]any {
	return map[string]any{
		"executionLogCount":           intFromAny(delivery["executionLogCount"]),
		"executionTaskCount":          intFromAny(delivery["executionTaskCount"]),
		"releaseBundleArtifactCount":  intFromAny(delivery["releaseBundleArtifactCount"]),
		"executionLogsError":          redactSensitiveText(strings.TrimSpace(fmt.Sprint(delivery["executionLogsError"]))),
		"executionTasksError":         redactSensitiveText(strings.TrimSpace(fmt.Sprint(delivery["executionTasksError"]))),
		"releaseBundleArtifactsError": redactSensitiveText(strings.TrimSpace(fmt.Sprint(delivery["releaseBundleArtifactsError"]))),
	}
}
func artifactRuntimeSnapshot(runtime map[string]any) map[string]any {
	podLogBytes := 0
	if podLogs, ok := runtime["podLogs"].(domainresource.PodLogsView); ok {
		podLogBytes = int(podLogs.ContentBytes)
	}
	return map[string]any{
		"podCount":         sliceLen(runtime["pods"]),
		"deploymentCount":  sliceLen(runtime["deployments"]),
		"serviceCount":     sliceLen(runtime["services"]),
		"eventCount":       sliceLen(runtime["events"]),
		"podLogBytes":      podLogBytes,
		"podsError":        redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["podsError"]))),
		"deploymentsError": redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["deploymentsError"]))),
		"servicesError":    redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["servicesError"]))),
		"eventsError":      redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["eventsError"]))),
		"podLogsError":     redactSensitiveText(strings.TrimSpace(fmt.Sprint(runtime["podLogsError"]))),
	}
}
func firstAnalysisArtifactKind(items []domaincopilot.AnalysisArtifact) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].Kind
}
func firstAnalysisArtifactRunID(items []domaincopilot.AnalysisArtifact) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].RunID
}
func firstAnalysisArtifactTitle(items []domaincopilot.AnalysisArtifact) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].Title
}
func buildSourceBindingUsage(detail domaindelivery.ApplicationDetail) []map[string]any {
	items := make([]map[string]any, 0, len(detail.Bindings))
	for _, binding := range detail.Bindings {
		items = append(items, map[string]any{
			"applicationEnvironmentId": binding.ApplicationEnvironmentID,
			"environmentId":            binding.EnvironmentID,
			"environmentName":          binding.EnvironmentName,
			"environmentKey":           binding.EnvironmentKey,
			"buildSourceId":            binding.BuildSourceID,
			"buildPolicy":              redactedBuildPolicy(binding.BuildPolicy),
			"latestBundleId":           optionalReleaseBundleID(binding.LatestBundle),
			"latestExecutionTaskId":    optionalExecutionTaskID(binding.LatestExecutionTask),
		})
	}
	return items
}
func redactedApplication(app domainapp.App) domainapp.App {
	app.Metadata = redactMap(app.Metadata)
	app.BuildSources = redactedBuildSources(app.BuildSources)
	return app
}
func redactedBuildSources(items []domainapp.BuildSource) []domainapp.BuildSource {
	out := make([]domainapp.BuildSource, len(items))
	copy(out, items)
	for index := range out {
		out[index].Config = redactMap(out[index].Config)
	}
	return out
}
func redactedApplicationServices(items []domainapp.Service) []domainapp.Service {
	out := make([]domainapp.Service, len(items))
	copy(out, items)
	for index := range out {
		out[index].Metadata = redactMap(out[index].Metadata)
		out[index].Containers = redactedServiceContainers(out[index].Containers)
	}
	return out
}
func redactedServiceContainers(items []domainapp.ServiceContainer) []domainapp.ServiceContainer {
	out := make([]domainapp.ServiceContainer, len(items))
	copy(out, items)
	for index := range out {
		out[index].EnvSchema = redactMap(out[index].EnvSchema)
		out[index].ResourceProfile = redactMap(out[index].ResourceProfile)
		out[index].HealthCheck = redactMap(out[index].HealthCheck)
		out[index].Metadata = redactMap(out[index].Metadata)
	}
	return out
}
func redactedBuildPolicy(policy domaincatalog.BuildPolicy) map[string]any {
	return map[string]any{
		"sourceId":         policy.SourceID,
		"refType":          policy.RefType,
		"refValue":         policy.RefValue,
		"imageTagMode":     policy.ImageTagMode,
		"imageTagTemplate": policy.ImageTagTemplate,
		"variables":        sanitizeGatewayValue(policy.Variables),
		"buildArgs":        sanitizeGatewayValue(policy.BuildArgs),
	}
}
func redactedBindingSummaries(items []domaindelivery.ApplicationBindingSummary) []domaindelivery.ApplicationBindingSummary {
	out := make([]domaindelivery.ApplicationBindingSummary, len(items))
	copy(out, items)
	for index := range out {
		if out[index].BuildSource != nil {
			copySource := *out[index].BuildSource
			copySource.Config = redactMap(copySource.Config)
			out[index].BuildSource = &copySource
		}
		if out[index].LatestBundle != nil {
			out[index].LatestBundle = redactedReleaseBundlePtr(out[index].LatestBundle)
		}
		if out[index].LatestExecutionTask != nil {
			out[index].LatestExecutionTask = redactedExecutionTaskPtr(out[index].LatestExecutionTask)
		}
		out[index].LatestBuild = nil
		out[index].LatestWorkflow = nil
		out[index].LatestRelease = nil
	}
	return out
}
func redactedReleaseBundles(items []domaindelivery.ReleaseBundle) []domaindelivery.ReleaseBundle {
	out := make([]domaindelivery.ReleaseBundle, len(items))
	copy(out, items)
	for index := range out {
		out[index].Metadata = redactMap(out[index].Metadata)
		out[index].Artifacts = redactedExecutionArtifacts(out[index].Artifacts)
	}
	return out
}
func redactedReleaseBundlePtr(item *domaindelivery.ReleaseBundle) *domaindelivery.ReleaseBundle {
	if item == nil {
		return nil
	}
	out := *item
	out.Metadata = redactMap(out.Metadata)
	out.Artifacts = redactedExecutionArtifacts(out.Artifacts)
	return &out
}
func redactedExecutionTasks(items []domaindelivery.ExecutionTask) []domaindelivery.ExecutionTask {
	out := make([]domaindelivery.ExecutionTask, len(items))
	copy(out, items)
	for index := range out {
		out[index] = redactedExecutionTask(out[index])
	}
	return out
}
func redactedExecutionTaskPtr(item *domaindelivery.ExecutionTask) *domaindelivery.ExecutionTask {
	if item == nil {
		return nil
	}
	out := redactedExecutionTask(*item)
	return &out
}
func redactedExecutionTask(item domaindelivery.ExecutionTask) domaindelivery.ExecutionTask {
	item.CallbackToken = ""
	item.Payload = redactMap(item.Payload)
	item.Result = redactMap(item.Result)
	item.Artifacts = redactedExecutionArtifacts(item.Artifacts)
	return item
}
func redactedExecutionArtifacts(items []domaindelivery.ExecutionArtifact) []domaindelivery.ExecutionArtifact {
	out := make([]domaindelivery.ExecutionArtifact, len(items))
	copy(out, items)
	for index := range out {
		out[index].Metadata = redactMap(out[index].Metadata)
	}
	return out
}
func releaseTargetsFromApplicationDetail(detail domaindelivery.ApplicationDetail) []map[string]any {
	items := make([]map[string]any, 0)
	for _, binding := range detail.Bindings {
		for _, target := range binding.Targets {
			items = append(items, map[string]any{
				"applicationId":            detail.Application.ID,
				"applicationEnvironmentId": binding.ApplicationEnvironmentID,
				"environmentId":            binding.EnvironmentID,
				"environmentName":          binding.EnvironmentName,
				"environmentKey":           binding.EnvironmentKey,
				"requiresApproval":         binding.RequiresApproval,
				"actionKind":               binding.ActionKind,
				"target":                   target,
			})
		}
	}
	return items
}
func filterBindingSummaries(items []domaindelivery.ApplicationBindingSummary, bindingID string) []domaindelivery.ApplicationBindingSummary {
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return items
	}
	out := make([]domaindelivery.ApplicationBindingSummary, 0, 1)
	for _, item := range items {
		if item.ApplicationEnvironmentID == bindingID {
			out = append(out, item)
		}
	}
	return out
}
func podDescribeContext(item domainresource.PodDetailView) map[string]any {
	return map[string]any{
		"name":               item.Name,
		"namespace":          item.Namespace,
		"phase":              item.Phase,
		"podIp":              item.PodIP,
		"hostIp":             item.HostIP,
		"nodeName":           item.NodeName,
		"serviceAccountName": item.ServiceAccountName,
		"qosClass":           item.QOSClass,
		"startTime":          item.StartTime,
		"requests":           item.Requests,
		"limits":             item.Limits,
		"labels":             item.Labels,
		"containers":         item.Containers,
		"conditions":         item.Conditions,
		"volumes":            item.Volumes,
		"relatedResources":   item.RelatedResources,
		"allowedActions":     item.AllowedActions,
		"summary": map[string]any{
			"containerCount":       len(item.Containers),
			"conditionCount":       len(item.Conditions),
			"volumeCount":          len(item.Volumes),
			"relatedResourceCount": len(item.RelatedResources),
			"restarts":             totalContainerRestarts(item.Containers),
		},
	}
}
func (s *Service) serviceBackendContext(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, serviceName string) (map[string]any, error) {
	services, err := s.resources.ListServices(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	var selected *domainresource.ServiceView
	for _, item := range services {
		if item.Namespace == namespace && item.Name == serviceName {
			copyItem := item
			selected = &copyItem
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("%w: service %s/%s was not found", apperrors.ErrNotFound, namespace, serviceName)
	}
	pods, err := s.resources.ListPods(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	backendPods := filterPodsByLabels(pods, selected.Selector)
	ingresses, err := s.resources.ListIngresses(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	relatedIngresses := filterIngressesByBackendService(ingresses, serviceName)
	return map[string]any{
		"service":           selected,
		"backendPods":       backendPods,
		"relatedIngresses":  relatedIngresses,
		"backendPodCount":   len(backendPods),
		"relatedRouteCount": len(relatedIngresses),
		"summary": map[string]any{
			"selector":          selected.Selector,
			"hasSelector":       len(selected.Selector) > 0,
			"readyBackendPods":  countReadyPods(backendPods),
			"totalBackendPods":  len(backendPods),
			"relatedIngresses":  len(relatedIngresses),
			"unmatchedSelector": len(selected.Selector) > 0 && len(backendPods) == 0,
		},
	}, nil
}
func (s *Service) routeContext(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, serviceName string) (map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	ingresses, err := s.resources.ListIngresses(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	gatewayClasses, gatewayClassErr := s.resources.ListGatewayClasses(ctx, principal, clusterID)
	gateways, gatewayErr := s.resources.ListGateways(ctx, principal, clusterID, namespace)
	httpRoutes, httpRouteErr := s.resources.ListHTTPRoutes(ctx, principal, clusterID, namespace)
	backendTLSPolicies, backendTLSErr := s.resources.ListBackendTLSPolicies(ctx, principal, clusterID, namespace)
	grpcRoutes, grpcRouteErr := s.resources.ListGRPCRoutes(ctx, principal, clusterID, namespace)
	referenceGrants, referenceGrantErr := s.resources.ListReferenceGrants(ctx, principal, clusterID, namespace)
	if serviceName != "" {
		ingresses = filterIngressesByBackendService(ingresses, serviceName)
		httpRoutes = filterHTTPRoutesByBackendService(httpRoutes, serviceName)
		grpcRoutes = filterGRPCRoutesByBackendService(grpcRoutes, serviceName)
	}
	output := map[string]any{
		"namespace":             namespace,
		"serviceName":           serviceName,
		"ingresses":             ingresses,
		"gatewayClasses":        gatewayClasses,
		"gateways":              gateways,
		"httpRoutes":            httpRoutes,
		"backendTLSPolicies":    backendTLSPolicies,
		"grpcRoutes":            grpcRoutes,
		"referenceGrants":       referenceGrants,
		"ingressCount":          len(ingresses),
		"gatewayClassCount":     len(gatewayClasses),
		"gatewayCount":          len(gateways),
		"httpRouteCount":        len(httpRoutes),
		"backendTLSPolicyCount": len(backendTLSPolicies),
		"grpcRouteCount":        len(grpcRoutes),
		"referenceGrantCount":   len(referenceGrants),
	}
	errors := gatewayRouteErrors(gatewayClassErr, gatewayErr, httpRouteErr, backendTLSErr, grpcRouteErr, referenceGrantErr)
	if len(errors) > 0 {
		output["capabilityWarnings"] = errors
	}
	return output, nil
}
func (s *Service) storageContext(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) (map[string]any, error) {
	pvcs, err := s.resources.ListPersistentVolumeClaims(ctx, principal, clusterID, namespace)
	if err != nil {
		return nil, err
	}
	pvs, err := s.resources.ListPersistentVolumes(ctx, principal, clusterID)
	if err != nil {
		return nil, err
	}
	storageClasses, err := s.resources.ListStorageClasses(ctx, principal, clusterID)
	if err != nil {
		return nil, err
	}
	if namespace != "" {
		pvs = filterPersistentVolumesByClaims(pvs, pvcs)
	}
	return map[string]any{
		"namespace":                     namespace,
		"persistentVolumeClaims":        pvcs,
		"persistentVolumes":             pvs,
		"storageClasses":                storageClasses,
		"persistentVolumeClaimCount":    len(pvcs),
		"persistentVolumeCount":         len(pvs),
		"storageClassCount":             len(storageClasses),
		"unboundPersistentVolumeClaims": unboundPVCNames(pvcs),
	}, nil
}
func (s *Service) releaseBundleForContext(ctx context.Context, principal domainidentity.Principal, bundleID string, fallback []domaindelivery.ReleaseBundle) (*domaindelivery.ReleaseBundle, error) {
	bundleID = strings.TrimSpace(bundleID)
	if bundleID == "" {
		return nil, nil
	}
	for _, item := range fallback {
		if item.ID == bundleID {
			copyItem := item
			return &copyItem, nil
		}
	}
	item, err := s.delivery.GetReleaseBundle(ctx, principal, bundleID)
	if err != nil {
		return nil, err
	}
	return &item, nil
}
func (s *Service) executionTaskForContext(ctx context.Context, principal domainidentity.Principal, taskID string, fallback []domaindelivery.ExecutionTask) (*domaindelivery.ExecutionTask, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, nil
	}
	for _, item := range fallback {
		if item.ID == taskID {
			copyItem := item
			return &copyItem, nil
		}
	}
	item, err := s.delivery.GetExecutionTask(ctx, principal, taskID)
	if err != nil {
		return nil, err
	}
	return &item, nil
}
func compareReleaseBundles(source, target *domaindelivery.ReleaseBundle) map[string]any {
	out := map[string]any{
		"hasSource": source != nil,
		"hasTarget": target != nil,
		"changes":   []string{},
	}
	if source == nil || target == nil {
		out["summary"] = "source and target bundle comparison requires both bundle ids or recent bundle history"
		return out
	}
	changes := make([]string, 0)
	if source.Version != target.Version {
		changes = append(changes, "version")
	}
	if source.ArtifactRef != target.ArtifactRef {
		changes = append(changes, "artifactRef")
	}
	if source.ArtifactDigest != target.ArtifactDigest {
		changes = append(changes, "artifactDigest")
	}
	if source.SourceType != target.SourceType {
		changes = append(changes, "sourceType")
	}
	out["sourceBundleId"] = source.ID
	out["targetBundleId"] = target.ID
	out["changes"] = changes
	if len(changes) == 0 {
		out["summary"] = "source and target bundles have the same version, artifact reference, digest, and source type"
	} else {
		out["summary"] = "source and target bundles differ in " + strings.Join(changes, ", ")
	}
	return out
}
func rollbackSuggestions(bindings []domaindelivery.ApplicationBindingSummary, bundles []domaindelivery.ReleaseBundle, currentTask *domaindelivery.ExecutionTask) []map[string]any {
	items := make([]map[string]any, 0)
	if currentTask != nil && strings.TrimSpace(currentTask.Status) != "" && !executionTaskSucceeded(currentTask.Status) {
		items = append(items, map[string]any{
			"type":            "investigate_failed_execution",
			"executionTaskId": currentTask.ID,
			"status":          currentTask.Status,
			"reason":          "current execution task is not successful",
		})
	}
	for _, binding := range bindings {
		if binding.LatestBundle != nil {
			items = append(items, map[string]any{
				"type":                         "consider_latest_stable_bundle",
				"applicationEnvironmentId":     binding.ApplicationEnvironmentID,
				"candidateReleaseBundleId":     binding.LatestBundle.ID,
				"candidateReleaseBundleStatus": binding.LatestBundle.Status,
			})
			break
		}
	}
	for _, bundle := range bundles {
		if strings.EqualFold(strings.TrimSpace(bundle.Status), "ready") || strings.EqualFold(strings.TrimSpace(bundle.Status), "succeeded") || strings.EqualFold(strings.TrimSpace(bundle.Status), "completed") {
			items = append(items, map[string]any{
				"type":                     "candidate_previous_bundle",
				"candidateReleaseBundleId": bundle.ID,
				"version":                  bundle.Version,
				"artifactRef":              bundle.ArtifactRef,
			})
			break
		}
	}
	if len(items) == 0 {
		items = append(items, map[string]any{
			"type":   "manual_review_required",
			"reason": "no successful prior release bundle is visible in the collected context",
		})
	}
	return items
}
func executionTaskSucceeded(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "success", "completed":
		return true
	default:
		return false
	}
}
func totalContainerRestarts(items []domainresource.WorkloadContainerView) int32 {
	var total int32
	for _, item := range items {
		total += item.RestartCount
	}
	return total
}
func filterPodsByLabels(items []domainresource.PodView, selector map[string]string) []domainresource.PodView {
	if len(selector) == 0 {
		return []domainresource.PodView{}
	}
	out := make([]domainresource.PodView, 0, len(items))
	for _, item := range items {
		if labelsMatchSelector(item.Labels, selector) {
			out = append(out, item)
		}
	}
	return out
}
func labelsMatchSelector(labels, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, expected := range selector {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if labels[key] != expected {
			return false
		}
	}
	return true
}
func countReadyPods(items []domainresource.PodView) int {
	count := 0
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Phase), "Running") {
			count++
		}
	}
	return count
}
func filterIngressesByBackendService(items []domainresource.IngressView, serviceName string) []domainresource.IngressView {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return items
	}
	out := make([]domainresource.IngressView, 0, len(items))
	for _, item := range items {
		if slices.Contains(item.BackendServices, serviceName) {
			out = append(out, item)
		}
	}
	return out
}
func filterHTTPRoutesByBackendService(items []domainresource.HTTPRouteView, serviceName string) []domainresource.HTTPRouteView {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return items
	}
	out := make([]domainresource.HTTPRouteView, 0, len(items))
	for _, item := range items {
		if slices.Contains(item.BackendServices, serviceName) {
			out = append(out, item)
		}
	}
	return out
}
func filterGRPCRoutesByBackendService(items []domainresource.GRPCRouteView, serviceName string) []domainresource.GRPCRouteView {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return items
	}
	out := make([]domainresource.GRPCRouteView, 0, len(items))
	for _, item := range items {
		if slices.Contains(item.BackendServices, serviceName) {
			out = append(out, item)
		}
	}
	return out
}
func gatewayRouteErrors(errs ...error) []string {
	out := make([]string, 0)
	for _, err := range errs {
		if err != nil {
			out = append(out, err.Error())
		}
	}
	return out
}
func filterPersistentVolumesByClaims(items []domainresource.PersistentVolumeView, claims []domainresource.PersistentVolumeClaimView) []domainresource.PersistentVolumeView {
	if len(claims) == 0 {
		return []domainresource.PersistentVolumeView{}
	}
	volumeNames := map[string]struct{}{}
	for _, claim := range claims {
		if strings.TrimSpace(claim.VolumeName) != "" {
			volumeNames[claim.VolumeName] = struct{}{}
		}
	}
	out := make([]domainresource.PersistentVolumeView, 0, len(items))
	for _, item := range items {
		if _, ok := volumeNames[item.Name]; ok {
			out = append(out, item)
		}
	}
	return out
}
func unboundPVCNames(items []domainresource.PersistentVolumeClaimView) []string {
	out := make([]string, 0)
	for _, item := range items {
		if !strings.EqualFold(strings.TrimSpace(item.Status), "Bound") {
			out = append(out, item.Namespace+"/"+item.Name)
		}
	}
	return out
}
func optionalReleaseBundleID(item *domaindelivery.ReleaseBundle) string {
	if item == nil {
		return ""
	}
	return item.ID
}
