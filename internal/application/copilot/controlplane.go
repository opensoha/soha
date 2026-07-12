package copilot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmcp "github.com/opensoha/soha/internal/domain/mcp"
	aperrors "github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListDataSourceCapabilities(ctx context.Context, principal domainidentity.Principal) ([]domainmcp.Adapter, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIView); err != nil {
		return nil, err
	}
	if s.mcpRegistry == nil {
		return []domainmcp.Adapter{}, nil
	}
	return s.mcpRegistry.List(), nil
}

func (s *Service) GetWorkbenchCatalog(ctx context.Context, principal domainidentity.Principal) (domaincopilot.WorkbenchCatalog, error) {
	if err := s.authorizeAnyWorkbenchPermission(ctx, principal); err != nil {
		return domaincopilot.WorkbenchCatalog{}, err
	}
	catalog := domaincopilot.WorkbenchCatalog{
		Adapters:         []domainmcp.Adapter{},
		DataSources:      []domaincopilot.WorkbenchDataSource{},
		AnalysisProfiles: []domaincopilot.WorkbenchAnalysisProfile{},
		SkillsRegistry:   []domaincopilot.WorkbenchSkill{},
		AgentProviders:   s.agentProviderCatalog(),
		Capabilities:     defaultAgentCapabilities(),
	}
	catalog.ToolBindings = flattenToolBindings(catalog.Capabilities)
	catalog.SkillBindings = flattenSkillBindings(catalog.Capabilities)
	if s.mcpRegistry != nil {
		catalog.Adapters = s.mcpRegistry.List()
	}
	dataSources, err := s.dataSources.ListDataSources(ctx)
	if err != nil {
		return domaincopilot.WorkbenchCatalog{}, err
	}
	for _, item := range dataSources {
		catalog.DataSources = append(catalog.DataSources, domaincopilot.WorkbenchDataSource{
			ID:                item.ID,
			Name:              item.Name,
			SourceKind:        item.SourceKind,
			BackendType:       item.BackendType,
			Enabled:           item.Enabled,
			MCPAdapter:        item.MCPAdapter,
			ValidationStatus:  item.ValidationStatus,
			ValidationMessage: item.ValidationMessage,
		})
	}
	profiles, err := s.analysisProfiles.ListAnalysisProfiles(ctx)
	if err != nil {
		return domaincopilot.WorkbenchCatalog{}, err
	}
	for _, item := range profiles {
		catalog.AnalysisProfiles = append(catalog.AnalysisProfiles, domaincopilot.WorkbenchAnalysisProfile{
			ID:      item.ID,
			Name:    item.Name,
			Mode:    item.Mode,
			Enabled: item.Enabled,
		})
	}
	if s.settings != nil {
		skills, err := s.settings.ResolveAISkillsRegistry(ctx)
		if err != nil {
			return domaincopilot.WorkbenchCatalog{}, err
		}
		for _, item := range skills {
			catalog.SkillsRegistry = append(catalog.SkillsRegistry, domaincopilot.WorkbenchSkill{
				ID:             item.ID,
				Name:           item.Name,
				Category:       item.Category,
				OwnerModule:    item.OwnerModule,
				Description:    item.Description,
				Enabled:        item.Enabled,
				Scopes:         item.Scopes,
				CapabilityRefs: item.CapabilityRefs,
				BlueprintRefs:  item.BlueprintRefs,
				ScopeRules:     item.ScopeRules,
			})
		}
	}
	return catalog, nil
}

func (s *Service) authorizeAnyWorkbenchPermission(ctx context.Context, principal domainidentity.Principal) error {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return err
	}
	for _, permission := range []string{
		appaccess.PermObserveAIChatUse,
		appaccess.PermObserveAIView,
		appaccess.PermSettingsAIView,
	} {
		if slices.Contains(keys, permission) {
			return nil
		}
	}
	return s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse)
}

func (s *Service) ListDataSources(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.DataSource, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIView); err != nil {
		return nil, err
	}
	return s.dataSources.ListDataSources(ctx)
}

func (s *Service) CreateDataSource(ctx context.Context, principal domainidentity.Principal, input domaincopilot.DataSourceInput) (domaincopilot.DataSource, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domaincopilot.DataSource{}, err
	}
	item, err := s.normalizeDataSourceInput(input)
	if err != nil {
		return domaincopilot.DataSource{}, err
	}
	now := time.Now().UTC()
	return s.dataSources.CreateDataSource(ctx, domaincopilot.DataSource{
		ID:              item.ID,
		Name:            item.Name,
		SourceKind:      item.SourceKind,
		BackendType:     item.BackendType,
		Enabled:         item.Enabled,
		CredentialRef:   item.CredentialRef,
		Scope:           item.Scope,
		QueryBudget:     item.QueryBudget,
		RedactionPolicy: item.RedactionPolicy,
		MCPAdapter:      item.MCPAdapter,
		Config:          item.Config,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
}

func (s *Service) UpdateDataSource(ctx context.Context, principal domainidentity.Principal, dataSourceID string, input domaincopilot.DataSourceInput) (domaincopilot.DataSource, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domaincopilot.DataSource{}, err
	}
	input.ID = strings.TrimSpace(dataSourceID)
	item, err := s.normalizeDataSourceInput(input)
	if err != nil {
		return domaincopilot.DataSource{}, err
	}
	return s.dataSources.UpdateDataSource(ctx, item.ID, item)
}

func (s *Service) ValidateDataSource(ctx context.Context, principal domainidentity.Principal, dataSourceID string) (domaincopilot.DataSource, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domaincopilot.DataSource{}, err
	}
	item, err := s.dataSources.GetDataSource(ctx, strings.TrimSpace(dataSourceID))
	if err != nil {
		return domaincopilot.DataSource{}, err
	}
	validatedAt := time.Now().UTC()
	_, err = s.normalizeDataSourceInput(domaincopilot.DataSourceInput{
		ID:              item.ID,
		Name:            item.Name,
		SourceKind:      item.SourceKind,
		BackendType:     item.BackendType,
		Enabled:         item.Enabled,
		CredentialRef:   item.CredentialRef,
		Scope:           item.Scope,
		QueryBudget:     item.QueryBudget,
		RedactionPolicy: item.RedactionPolicy,
		MCPAdapter:      item.MCPAdapter,
		Config:          item.Config,
	})
	if err != nil {
		updated, updateErr := s.dataSources.UpdateDataSourceValidation(ctx, item.ID, "error", err.Error(), validatedAt)
		if updateErr != nil {
			return domaincopilot.DataSource{}, updateErr
		}
		return updated, err
	}
	return s.dataSources.UpdateDataSourceValidation(ctx, item.ID, "success", "", validatedAt)
}

func (s *Service) ListAnalysisProfiles(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.AnalysisProfile, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIView); err != nil {
		return nil, err
	}
	return s.analysisProfiles.ListAnalysisProfiles(ctx)
}

func (s *Service) CreateAnalysisProfile(ctx context.Context, principal domainidentity.Principal, input domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfile, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domaincopilot.AnalysisProfile{}, err
	}
	item, err := normalizeAnalysisProfileInput(input)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, err
	}
	now := time.Now().UTC()
	return s.analysisProfiles.CreateAnalysisProfile(ctx, domaincopilot.AnalysisProfile{
		ID:                      item.ID,
		Name:                    item.Name,
		Mode:                    item.Mode,
		EnabledSources:          item.EnabledSources,
		EnabledPlaybooks:        item.EnabledPlaybooks,
		QueryBudgets:            item.QueryBudgets,
		OutputStyle:             item.OutputStyle,
		RemediationPolicy:       item.RemediationPolicy,
		DefaultTimeRangeMinutes: item.DefaultTimeRangeMinutes,
		TimeoutSeconds:          item.TimeoutSeconds,
		Enabled:                 item.Enabled,
		CreatedAt:               now,
		UpdatedAt:               now,
	})
}

func (s *Service) UpdateAnalysisProfile(ctx context.Context, principal domainidentity.Principal, profileID string, input domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfile, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domaincopilot.AnalysisProfile{}, err
	}
	input.ID = strings.TrimSpace(profileID)
	item, err := normalizeAnalysisProfileInput(input)
	if err != nil {
		return domaincopilot.AnalysisProfile{}, err
	}
	return s.analysisProfiles.UpdateAnalysisProfile(ctx, item.ID, item)
}

func (s *Service) ListAutomationPolicies(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.AutomationPolicy, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIView); err != nil {
		return nil, err
	}
	return s.automationPolicies.ListAutomationPolicies(ctx)
}

func (s *Service) CreateAutomationPolicy(ctx context.Context, principal domainidentity.Principal, input domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicy, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domaincopilot.AutomationPolicy{}, err
	}
	item, err := normalizeAutomationPolicyInput(input)
	if err != nil {
		return domaincopilot.AutomationPolicy{}, err
	}
	now := time.Now().UTC()
	return s.automationPolicies.CreateAutomationPolicy(ctx, domaincopilot.AutomationPolicy{
		ID:                 item.ID,
		Name:               item.Name,
		Enabled:            item.Enabled,
		TriggerType:        item.TriggerType,
		AnalysisKinds:      item.AnalysisKinds,
		AgentProviderID:    item.AgentProviderID,
		TriggerConditions:  item.TriggerConditions,
		DedupWindowSeconds: item.DedupWindowSeconds,
		AnalysisProfileID:  item.AnalysisProfileID,
		RemediationPolicy:  item.RemediationPolicy,
		ApprovalPolicy:     item.ApprovalPolicy,
		CooldownSeconds:    item.CooldownSeconds,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
}

func (s *Service) UpdateAutomationPolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicy, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return domaincopilot.AutomationPolicy{}, err
	}
	input.ID = strings.TrimSpace(policyID)
	item, err := normalizeAutomationPolicyInput(input)
	if err != nil {
		return domaincopilot.AutomationPolicy{}, err
	}
	return s.automationPolicies.UpdateAutomationPolicy(ctx, item.ID, item)
}

func (s *Service) DeleteAutomationPolicy(ctx context.Context, principal domainidentity.Principal, policyID string) error {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIManage); err != nil {
		return err
	}
	return s.automationPolicies.DeleteAutomationPolicy(ctx, strings.TrimSpace(policyID))
}

func (s *Service) normalizeDataSourceInput(input domaincopilot.DataSourceInput) (domaincopilot.DataSourceInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.SourceKind = strings.TrimSpace(input.SourceKind)
	input.BackendType = strings.TrimSpace(input.BackendType)
	input.CredentialRef = strings.TrimSpace(input.CredentialRef)
	input.MCPAdapter = strings.TrimSpace(input.MCPAdapter)
	if input.ID == "" {
		input.ID = "ds:" + uuid.NewString()
	}
	if input.Name == "" {
		return domaincopilot.DataSourceInput{}, fmt.Errorf("%w: data source name is required", aperrors.ErrInvalidArgument)
	}
	if input.SourceKind == "" {
		return domaincopilot.DataSourceInput{}, fmt.Errorf("%w: sourceKind is required", aperrors.ErrInvalidArgument)
	}
	if input.BackendType == "" {
		return domaincopilot.DataSourceInput{}, fmt.Errorf("%w: backendType is required", aperrors.ErrInvalidArgument)
	}
	if input.MCPAdapter == "" {
		return domaincopilot.DataSourceInput{}, fmt.Errorf("%w: mcpAdapter is required", aperrors.ErrInvalidArgument)
	}
	if s.mcpRegistry == nil {
		return domaincopilot.DataSourceInput{}, fmt.Errorf("%w: MCP registry is not configured", aperrors.ErrInvalidArgument)
	}
	adapter, ok := s.mcpRegistry.Get(input.MCPAdapter)
	if !ok {
		return domaincopilot.DataSourceInput{}, fmt.Errorf("%w: unsupported mcp adapter %s", aperrors.ErrInvalidArgument, input.MCPAdapter)
	}
	if adapter.SourceKind != "" && adapter.SourceKind != input.SourceKind {
		return domaincopilot.DataSourceInput{}, fmt.Errorf("%w: adapter %s only supports source kind %s", aperrors.ErrInvalidArgument, input.MCPAdapter, adapter.SourceKind)
	}
	if len(adapter.SupportedBackends) > 0 && !containsString(adapter.SupportedBackends, input.BackendType) {
		return domaincopilot.DataSourceInput{}, fmt.Errorf("%w: adapter %s does not support backend %s", aperrors.ErrInvalidArgument, input.MCPAdapter, input.BackendType)
	}
	if input.SourceKind == "logs" {
		if err := s.logBackend().Validate(input.BackendType, input.Config); err != nil {
			return domaincopilot.DataSourceInput{}, fmt.Errorf("%w: %v", aperrors.ErrInvalidArgument, err)
		}
	}
	if input.Scope == nil {
		input.Scope = map[string]any{}
	}
	if input.QueryBudget == nil {
		input.QueryBudget = map[string]any{}
	}
	if input.RedactionPolicy == nil {
		input.RedactionPolicy = map[string]any{}
	}
	if input.Config == nil {
		input.Config = map[string]any{}
	}
	return input, nil
}

func normalizeAnalysisProfileInput(input domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfileInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.Mode = strings.TrimSpace(input.Mode)
	input.RemediationPolicy = strings.TrimSpace(input.RemediationPolicy)
	if input.ID == "" {
		input.ID = "profile:" + uuid.NewString()
	}
	if input.Name == "" {
		return domaincopilot.AnalysisProfileInput{}, fmt.Errorf("%w: profile name is required", aperrors.ErrInvalidArgument)
	}
	if input.Mode != "root_cause" && input.Mode != "inspection" && input.Mode != "performance" && input.Mode != "trace" {
		return domaincopilot.AnalysisProfileInput{}, fmt.Errorf("%w: mode must be root_cause, inspection, performance, or trace", aperrors.ErrInvalidArgument)
	}
	if input.RemediationPolicy == "" {
		input.RemediationPolicy = "suggest_only"
	}
	if input.DefaultTimeRangeMinutes <= 0 {
		input.DefaultTimeRangeMinutes = 60
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = 90
	}
	if input.QueryBudgets == nil {
		input.QueryBudgets = map[string]any{}
	}
	if input.OutputStyle == nil {
		input.OutputStyle = map[string]any{}
	}
	return input, nil
}

func normalizeAutomationPolicyInput(input domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicyInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Name = strings.TrimSpace(input.Name)
	input.TriggerType = strings.TrimSpace(input.TriggerType)
	input.AnalysisProfileID = strings.TrimSpace(input.AnalysisProfileID)
	input.AgentProviderID = normalizeAgentProviderID(input.AgentProviderID)
	input.RemediationPolicy = strings.TrimSpace(input.RemediationPolicy)
	if input.ID == "" {
		input.ID = "policy:" + uuid.NewString()
	}
	if input.Name == "" {
		return domaincopilot.AutomationPolicyInput{}, fmt.Errorf("%w: automation policy name is required", aperrors.ErrInvalidArgument)
	}
	if input.TriggerType == "" {
		return domaincopilot.AutomationPolicyInput{}, fmt.Errorf("%w: triggerType is required", aperrors.ErrInvalidArgument)
	}
	if input.TriggerType != "alert_webhook" {
		return domaincopilot.AutomationPolicyInput{}, fmt.Errorf("%w: triggerType must be alert_webhook", aperrors.ErrInvalidArgument)
	}
	if input.AnalysisProfileID == "" {
		return domaincopilot.AutomationPolicyInput{}, fmt.Errorf("%w: analysisProfileId is required", aperrors.ErrInvalidArgument)
	}
	if input.RemediationPolicy == "" {
		input.RemediationPolicy = "suggest_only"
	}
	if input.DedupWindowSeconds <= 0 {
		input.DedupWindowSeconds = 900
	}
	if input.TriggerConditions == nil {
		input.TriggerConditions = map[string]any{}
	}
	if len(input.AnalysisKinds) == 0 {
		input.AnalysisKinds = []string{"root_cause"}
	}
	analysisKinds, err := normalizeAutomationAnalysisKinds(input.AnalysisKinds)
	if err != nil {
		return domaincopilot.AutomationPolicyInput{}, err
	}
	input.AnalysisKinds = analysisKinds
	if input.ApprovalPolicy == nil {
		input.ApprovalPolicy = map[string]any{}
	}
	return input, nil
}

func normalizeAutomationAnalysisKinds(items []string) ([]string, error) {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		kind := strings.TrimSpace(item)
		if kind == "" {
			continue
		}
		switch kind {
		case "root_cause", "performance", "trace", "inspection_review":
		default:
			return nil, fmt.Errorf("%w: unsupported analysis kind %s", aperrors.ErrInvalidArgument, kind)
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		out = append(out, kind)
	}
	if len(out) == 0 {
		out = append(out, "root_cause")
	}
	return out, nil
}
