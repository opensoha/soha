package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	repo        domaincatalog.Repository
	authorizer  domainaccess.Authorizer
	apps        ApplicationReader
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
	runtime     TemplateUsageRuntimeReaders
}

type catalogLookupRepository interface {
	domaincatalog.Repository
	GetApplicationEnvironment(context.Context, string) (domaincatalog.ApplicationEnvironment, error)
}

type ApplicationReader interface {
	List(context.Context, domainapp.Filter) ([]domainapp.App, error)
	Get(context.Context, string) (domainapp.App, error)
}

type BuildRuntimeReader interface {
	List(context.Context, domainidentity.Principal, domainbuild.Filter) ([]domainbuild.Record, error)
}

type WorkflowRuntimeReader interface {
	List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error)
}

type ReleaseRuntimeReader interface {
	List(context.Context, domainidentity.Principal, domainrelease.Filter) ([]domainrelease.Record, error)
}

type DeliveryRuntimeReader interface {
	ListReleaseBundles(context.Context, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error)
	ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error)
}

type TemplateUsageRuntimeReaders struct {
	Builds    BuildRuntimeReader
	Workflows WorkflowRuntimeReader
	Releases  ReleaseRuntimeReader
	Delivery  DeliveryRuntimeReader
}

func New(repo domaincatalog.Repository, authorizer domainaccess.Authorizer, apps ApplicationReader, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder) *Service {
	return &Service{repo: repo, authorizer: authorizer, apps: apps, permissions: permissions, audit: audit, operations: operations}
}

func (s *Service) SetTemplateUsageRuntimeReaders(readers TemplateUsageRuntimeReaders) {
	s.runtime = readers
}

func (s *Service) ListEnvironments(ctx context.Context, principal domainidentity.Principal) ([]domaincatalog.Environment, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationEnvView); err != nil {
		return nil, err
	}
	return s.repo.ListEnvironments(ctx)
}

func (s *Service) ListApplicationEnvironments(ctx context.Context, principal domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationEnvView); err != nil {
		return nil, err
	}
	return s.repo.ListApplicationEnvironments(ctx)
}

func (s *Service) GetApplicationEnvironment(ctx context.Context, principal domainidentity.Principal, id string) (domaincatalog.ApplicationEnvironment, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationEnvView); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	item, err := s.repo.GetApplicationEnvironment(ctx, id)
	if err != nil {
		return item, normalizeRepoError(err)
	}
	if err := s.authorizeApplicationEnvironment(ctx, principal, domainaccess.ActionView, item); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	return item, nil
}

func (s *Service) CreateApplicationEnvironment(ctx context.Context, principal domainidentity.Principal, input domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationEnvManage); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	if strings.TrimSpace(input.ApplicationID) == "" || strings.TrimSpace(input.EnvironmentID) == "" {
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("%w: applicationId and environmentId are required", apperrors.ErrInvalidArgument)
	}
	if err := s.authorizeApplicationEnvironmentInput(ctx, principal, domainaccess.ActionCreate, input); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	item, err := s.repo.CreateApplicationEnvironment(ctx, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.application_environment.create", "ApplicationEnvironment", item.ID, item.ID, "created application environment binding")
	}
	return item, err
}

func (s *Service) UpdateApplicationEnvironment(ctx context.Context, principal domainidentity.Principal, id string, input domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationEnvManage); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	if strings.TrimSpace(input.ApplicationID) == "" || strings.TrimSpace(input.EnvironmentID) == "" {
		return domaincatalog.ApplicationEnvironment{}, fmt.Errorf("%w: applicationId and environmentId are required", apperrors.ErrInvalidArgument)
	}
	current, err := s.repo.GetApplicationEnvironment(ctx, id)
	if err != nil {
		return domaincatalog.ApplicationEnvironment{}, normalizeRepoError(err)
	}
	if err := s.authorizeApplicationEnvironment(ctx, principal, domainaccess.ActionUpdate, current); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	if err := s.authorizeApplicationEnvironmentInput(ctx, principal, domainaccess.ActionUpdate, input); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	item, err := s.repo.UpdateApplicationEnvironment(ctx, id, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.application_environment.update", "ApplicationEnvironment", item.ID, item.ID, "updated application environment binding")
	}
	return item, normalizeRepoError(err)
}

func (s *Service) DeleteApplicationEnvironment(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationEnvManage); err != nil {
		return err
	}
	item, err := s.repo.GetApplicationEnvironment(ctx, id)
	if err != nil {
		return normalizeRepoError(err)
	}
	if err := s.authorizeApplicationEnvironment(ctx, principal, domainaccess.ActionDelete, item); err != nil {
		return err
	}
	if err := normalizeRepoError(s.repo.DeleteApplicationEnvironment(ctx, id)); err != nil {
		return err
	}
	s.recordWriteLogs(ctx, principal, "delivery.application_environment.delete", "ApplicationEnvironment", id, id, "deleted application environment binding")
	return nil
}

func (s *Service) ListBuildTemplates(ctx context.Context, principal domainidentity.Principal) ([]domaincatalog.BuildTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBuildTemplatesView); err != nil {
		return nil, err
	}
	return s.repo.ListBuildTemplates(ctx)
}

func (s *Service) GetBuildTemplateUsage(ctx context.Context, principal domainidentity.Principal, id string) (domaincatalog.TemplateUsageSummary, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBuildTemplatesView); err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	return s.buildTemplateUsage(ctx, principal, strings.TrimSpace(id))
}

func (s *Service) CreateBuildTemplate(ctx context.Context, principal domainidentity.Principal, input domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBuildTemplatesManage); err != nil {
		return domaincatalog.BuildTemplate{}, err
	}
	input = normalizeBuildTemplateInput(input)
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	if len(input.BuildCommands) == 0 {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("%w: buildCommands are required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.CreateBuildTemplate(ctx, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.build_template.create", "BuildTemplate", item.ID, item.Name, "created build template")
	}
	return item, err
}

func (s *Service) UpdateBuildTemplate(ctx context.Context, principal domainidentity.Principal, id string, input domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBuildTemplatesManage); err != nil {
		return domaincatalog.BuildTemplate{}, err
	}
	input = normalizeBuildTemplateInput(input)
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	if len(input.BuildCommands) == 0 {
		return domaincatalog.BuildTemplate{}, fmt.Errorf("%w: buildCommands are required", apperrors.ErrInvalidArgument)
	}
	beforeUsage := s.mustBuildTemplateUsage(ctx, principal, strings.TrimSpace(id))
	item, err := s.repo.UpdateBuildTemplate(ctx, id, input)
	if err == nil {
		s.recordTemplateWriteLogs(ctx, principal, "delivery.build_template.update", "BuildTemplate", item.ID, item.Name, "updated build template", templateUsageAuditChangeSnapshot(beforeUsage, s.mustBuildTemplateUsage(ctx, principal, item.ID)))
	}
	return item, normalizeRepoError(err)
}

func (s *Service) DeleteBuildTemplate(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBuildTemplatesManage); err != nil {
		return err
	}
	if err := normalizeRepoError(s.repo.DeleteBuildTemplate(ctx, id)); err != nil {
		return err
	}
	s.recordWriteLogs(ctx, principal, "delivery.build_template.delete", "BuildTemplate", id, id, "deleted build template")
	return nil
}

func (s *Service) ListWorkflowTemplates(ctx context.Context, principal domainidentity.Principal) ([]domaincatalog.WorkflowTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryWorkflowTemplatesView); err != nil {
		return nil, err
	}
	return s.repo.ListWorkflowTemplates(ctx)
}

func (s *Service) GetWorkflowTemplateUsage(ctx context.Context, principal domainidentity.Principal, id string) (domaincatalog.TemplateUsageSummary, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryWorkflowTemplatesView); err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	return s.workflowTemplateUsage(ctx, principal, strings.TrimSpace(id))
}

func (s *Service) CreateWorkflowTemplate(ctx context.Context, principal domainidentity.Principal, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryWorkflowTemplatesManage); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	input = normalizeWorkflowTemplateInput(input)
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	if err := validateWorkflowTemplateDefinition(input.Definition); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	item, err := s.repo.CreateWorkflowTemplate(ctx, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.workflow_template.create", "WorkflowTemplate", item.ID, item.Name, "created workflow template")
	}
	return item, err
}

func (s *Service) UpdateWorkflowTemplate(ctx context.Context, principal domainidentity.Principal, id string, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryWorkflowTemplatesManage); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	input = normalizeWorkflowTemplateInput(input)
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.WorkflowTemplate{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	if err := validateWorkflowTemplateDefinition(input.Definition); err != nil {
		return domaincatalog.WorkflowTemplate{}, err
	}
	beforeUsage := s.mustWorkflowTemplateUsage(ctx, principal, strings.TrimSpace(id))
	item, err := s.repo.UpdateWorkflowTemplate(ctx, id, input)
	if err == nil {
		s.recordTemplateWriteLogs(ctx, principal, "delivery.workflow_template.update", "WorkflowTemplate", item.ID, item.Name, "updated workflow template", templateUsageAuditChangeSnapshot(beforeUsage, s.mustWorkflowTemplateUsage(ctx, principal, item.ID)))
	}
	return item, normalizeRepoError(err)
}

func (s *Service) DeleteWorkflowTemplate(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryWorkflowTemplatesManage); err != nil {
		return err
	}
	if err := normalizeRepoError(s.repo.DeleteWorkflowTemplate(ctx, id)); err != nil {
		return err
	}
	s.recordWriteLogs(ctx, principal, "delivery.workflow_template.delete", "WorkflowTemplate", id, id, "deleted workflow template")
	return nil
}

func normalizeRepoError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		return err
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

func normalizeBuildTemplateInput(input domaincatalog.BuildTemplateInput) domaincatalog.BuildTemplateInput {
	if strings.TrimSpace(input.BuilderKind) == "" {
		input.BuilderKind = "custom"
	}
	if input.VariableSchema == nil {
		input.VariableSchema = map[string]any{}
	}
	if input.DefaultVariables == nil {
		input.DefaultVariables = map[string]any{}
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
	mode := strings.TrimSpace(fmt.Sprint(definition["mode"]))
	if mode != "" && mode != "release_dag" && mode != "delivery_dag" {
		return fmt.Errorf("%w: unsupported workflow definition mode %s", apperrors.ErrInvalidArgument, mode)
	}
	if nodes, ok := toSliceAny(definition["nodes"]); ok && len(nodes) > 0 {
		return validateWorkflowTemplateGraph(mode, nodes, definition["edges"])
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

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func (s *Service) recordWriteLogs(ctx context.Context, principal domainidentity.Principal, operationType, resourceKind, targetID, targetLabel, summary string) {
	s.recordTemplateWriteLogs(ctx, principal, operationType, resourceKind, targetID, targetLabel, summary, nil)
}

func (s *Service) recordTemplateWriteLogs(ctx context.Context, principal domainidentity.Principal, operationType, resourceKind, targetID, targetLabel, summary string, usageSnapshot map[string]any) {
	meta := requestctx.FromContext(ctx)
	auditMetadata := map[string]any{
		"targetId": targetID,
		"source":   meta.Source,
	}
	operationMetadata := map[string]any{
		"targetId": targetID,
	}
	if usageSnapshot != nil {
		auditMetadata["usageSnapshot"] = usageSnapshot
		operationMetadata["usageSnapshot"] = usageSnapshot
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID:       principal.UserID,
			ActorName:     principal.UserName,
			Roles:         principal.Roles,
			Teams:         principal.Teams,
			ResourceKind:  resourceKind,
			ResourceName:  targetLabel,
			Action:        strings.TrimPrefix(operationType, "delivery."),
			Result:        "success",
			Summary:       summary,
			RequestPath:   meta.Path,
			RequestMethod: meta.Method,
			RequestID:     meta.RequestID,
			SourceIP:      meta.SourceIP,
			Metadata:      auditMetadata,
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(
			ctx,
			principal,
			operationType,
			map[string]any{
				"module":       "delivery",
				"resourceKind": resourceKind,
				"targetId":     targetID,
				"targetLabel":  targetLabel,
			},
			"success",
			summary,
			operationMetadata,
		))
	}
}

func (s *Service) workflowTemplateUsage(ctx context.Context, principal domainidentity.Principal, templateID string) (domaincatalog.TemplateUsageSummary, error) {
	if templateID == "" {
		return domaincatalog.TemplateUsageSummary{}, fmt.Errorf("%w: workflowTemplateID is required", apperrors.ErrInvalidArgument)
	}
	bindings, err := s.repo.ListApplicationEnvironments(ctx)
	if err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	environments, err := s.repo.ListEnvironments(ctx)
	if err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	apps := s.listApplicationsForUsage(ctx)
	appByID := mapApplicationsByID(apps)
	envByID := mapEnvironmentsByID(environments)
	summary := domaincatalog.TemplateUsageSummary{
		TemplateKind:         domaincatalog.TemplateUsageKindWorkflow,
		TemplateID:           templateID,
		Bindings:             []domaincatalog.TemplateUsageBinding{},
		LastExecutionSummary: map[string]any{},
	}
	applicationIDs := map[string]struct{}{}
	environmentIDs := map[string]struct{}{}
	for _, binding := range bindings {
		if strings.TrimSpace(binding.WorkflowTemplateID) != templateID {
			continue
		}
		app := appByID[binding.ApplicationID]
		env := envByID[binding.EnvironmentID]
		requiresApproval := binding.ReleasePolicy.RequiresApproval || env.RequiresApproval
		risk := bindingUsageRisk(env, requiresApproval, len(binding.Targets))
		summary.Bindings = append(summary.Bindings, domaincatalog.TemplateUsageBinding{
			ID:               binding.ID,
			ApplicationID:    binding.ApplicationID,
			EnvironmentID:    binding.EnvironmentID,
			EnvironmentKey:   firstNonEmpty(strings.TrimSpace(binding.EnvironmentKey), env.Key),
			RequiresApproval: requiresApproval,
			TargetCount:      len(binding.Targets),
			RiskLevel:        risk,
			Application:      templateUsageApplication(app),
			Environment:      templateUsageEnvironment(env),
		})
		summary.UsageCount++
		summary.TargetCount += len(binding.Targets)
		if binding.ApplicationID != "" {
			applicationIDs[binding.ApplicationID] = struct{}{}
		}
		if binding.EnvironmentID != "" {
			environmentIDs[binding.EnvironmentID] = struct{}{}
		}
		if env.IsProduction {
			summary.ProductionEnvironmentCount++
		}
		if requiresApproval {
			summary.ApprovalBindingCount++
		}
	}
	summary.ApplicationCount = len(applicationIDs)
	summary.EnvironmentCount = len(environmentIDs)
	finalizeTemplateUsageSummary(&summary)
	summary.LastExecutionSummary = s.workflowTemplateRuntimeSummary(ctx, principal, summary.TemplateID, summary.Bindings)
	return summary, nil
}

func (s *Service) buildTemplateUsage(ctx context.Context, principal domainidentity.Principal, templateID string) (domaincatalog.TemplateUsageSummary, error) {
	if templateID == "" {
		return domaincatalog.TemplateUsageSummary{}, fmt.Errorf("%w: buildTemplateID is required", apperrors.ErrInvalidArgument)
	}
	bindings, err := s.repo.ListApplicationEnvironments(ctx)
	if err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	environments, err := s.repo.ListEnvironments(ctx)
	if err != nil {
		return domaincatalog.TemplateUsageSummary{}, err
	}
	apps := s.listApplicationsForUsage(ctx)
	bindingsByApp := mapBindingsByApplication(bindings)
	envByID := mapEnvironmentsByID(environments)
	summary := domaincatalog.TemplateUsageSummary{
		TemplateKind:         domaincatalog.TemplateUsageKindBuild,
		TemplateID:           templateID,
		Bindings:             []domaincatalog.TemplateUsageBinding{},
		BuildSources:         []domaincatalog.TemplateUsageBuildSource{},
		LastExecutionSummary: map[string]any{},
	}
	applicationIDs := map[string]struct{}{}
	environmentIDs := map[string]struct{}{}
	for _, app := range apps {
		for _, source := range app.BuildSources {
			if strings.TrimSpace(fmt.Sprint(source.Config["buildTemplateId"])) != templateID {
				continue
			}
			sourceRisk := domaincatalog.TemplateUsageRiskLow
			sourceBindingCount := 0
			for _, binding := range bindingsByApp[app.ID] {
				env := envByID[binding.EnvironmentID]
				requiresApproval := binding.ReleasePolicy.RequiresApproval || env.RequiresApproval
				risk := bindingUsageRisk(env, requiresApproval, len(binding.Targets))
				sourceRisk = maxTemplateUsageRisk(sourceRisk, risk)
				sourceBindingCount++
				summary.Bindings = append(summary.Bindings, domaincatalog.TemplateUsageBinding{
					ID:               binding.ID,
					ApplicationID:    binding.ApplicationID,
					EnvironmentID:    binding.EnvironmentID,
					EnvironmentKey:   firstNonEmpty(strings.TrimSpace(binding.EnvironmentKey), env.Key),
					RequiresApproval: requiresApproval,
					TargetCount:      len(binding.Targets),
					RiskLevel:        risk,
					Application:      templateUsageApplication(app),
					Environment:      templateUsageEnvironment(env),
				})
				if binding.EnvironmentID != "" {
					environmentIDs[binding.EnvironmentID] = struct{}{}
				}
				if env.IsProduction {
					summary.ProductionEnvironmentCount++
				}
				if requiresApproval {
					summary.ApprovalBindingCount++
				}
				summary.TargetCount += len(binding.Targets)
			}
			summary.BuildSources = append(summary.BuildSources, domaincatalog.TemplateUsageBuildSource{
				ApplicationID:   app.ID,
				BuildSourceID:   source.ID,
				BuildSourceName: source.Name,
				Application:     templateUsageApplication(app),
				BindingCount:    sourceBindingCount,
				RiskLevel:       sourceRisk,
			})
			summary.UsageCount++
			if app.ID != "" {
				applicationIDs[app.ID] = struct{}{}
			}
		}
	}
	summary.ApplicationCount = len(applicationIDs)
	summary.EnvironmentCount = len(environmentIDs)
	finalizeTemplateUsageSummary(&summary)
	summary.LastExecutionSummary = s.buildTemplateRuntimeSummary(ctx, principal, summary.BuildSources, summary.Bindings)
	return summary, nil
}

func (s *Service) mustWorkflowTemplateUsage(ctx context.Context, principal domainidentity.Principal, templateID string) *domaincatalog.TemplateUsageSummary {
	usage, err := s.workflowTemplateUsage(ctx, principal, templateID)
	if err != nil {
		return nil
	}
	return &usage
}

func (s *Service) mustBuildTemplateUsage(ctx context.Context, principal domainidentity.Principal, templateID string) *domaincatalog.TemplateUsageSummary {
	usage, err := s.buildTemplateUsage(ctx, principal, templateID)
	if err != nil {
		return nil
	}
	return &usage
}

func (s *Service) listApplicationsForUsage(ctx context.Context) []domainapp.App {
	if s.apps == nil {
		return nil
	}
	items, err := s.apps.List(ctx, domainapp.Filter{Limit: 500})
	if err != nil {
		return nil
	}
	return items
}

func mapApplicationsByID(items []domainapp.App) map[string]domainapp.App {
	out := make(map[string]domainapp.App, len(items))
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}

func mapEnvironmentsByID(items []domaincatalog.Environment) map[string]domaincatalog.Environment {
	out := make(map[string]domaincatalog.Environment, len(items))
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}

func mapBindingsByApplication(items []domaincatalog.ApplicationEnvironment) map[string][]domaincatalog.ApplicationEnvironment {
	out := make(map[string][]domaincatalog.ApplicationEnvironment)
	for _, item := range items {
		out[item.ApplicationID] = append(out[item.ApplicationID], item)
	}
	return out
}

func templateUsageApplication(app domainapp.App) domaincatalog.TemplateUsageApplication {
	return domaincatalog.TemplateUsageApplication{
		ID:             app.ID,
		Name:           app.Name,
		Key:            app.Key,
		BusinessLineID: app.BusinessLineID,
		Group:          app.Group,
	}
}

func templateUsageEnvironment(env domaincatalog.Environment) domaincatalog.TemplateUsageEnvironment {
	return domaincatalog.TemplateUsageEnvironment{
		ID:               env.ID,
		Key:              env.Key,
		Name:             env.Name,
		IsProduction:     env.IsProduction,
		RequiresApproval: env.RequiresApproval,
	}
}

func bindingUsageRisk(env domaincatalog.Environment, requiresApproval bool, targetCount int) domaincatalog.TemplateUsageRiskLevel {
	if env.IsProduction || requiresApproval {
		return domaincatalog.TemplateUsageRiskHigh
	}
	if targetCount > 1 {
		return domaincatalog.TemplateUsageRiskMedium
	}
	return domaincatalog.TemplateUsageRiskLow
}

func maxTemplateUsageRisk(current, next domaincatalog.TemplateUsageRiskLevel) domaincatalog.TemplateUsageRiskLevel {
	if templateRiskWeight(next) > templateRiskWeight(current) {
		return next
	}
	return current
}

func templateRiskWeight(risk domaincatalog.TemplateUsageRiskLevel) int {
	switch risk {
	case domaincatalog.TemplateUsageRiskHigh:
		return 3
	case domaincatalog.TemplateUsageRiskMedium:
		return 2
	case domaincatalog.TemplateUsageRiskLow:
		return 1
	default:
		return 0
	}
}

func finalizeTemplateUsageSummary(summary *domaincatalog.TemplateUsageSummary) {
	risk := domaincatalog.TemplateUsageRiskLow
	for _, binding := range summary.Bindings {
		risk = maxTemplateUsageRisk(risk, binding.RiskLevel)
	}
	for _, source := range summary.BuildSources {
		risk = maxTemplateUsageRisk(risk, source.RiskLevel)
	}
	if summary.UsageCount == 0 {
		risk = domaincatalog.TemplateUsageRiskLow
	}
	summary.RiskLevel = risk
	reasons := make([]string, 0)
	if summary.ProductionEnvironmentCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d production environment bindings", summary.ProductionEnvironmentCount))
	}
	if summary.ApprovalBindingCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d approval-gated bindings", summary.ApprovalBindingCount))
	}
	if summary.TargetCount > summary.UsageCount && summary.TargetCount > 1 {
		reasons = append(reasons, fmt.Sprintf("%d release targets", summary.TargetCount))
	}
	if summary.UsageCount == 0 {
		reasons = append(reasons, "no observed bindings")
	}
	summary.RiskReasons = reasons
	switch risk {
	case domaincatalog.TemplateUsageRiskHigh:
		summary.RecommendedAction = "copy_template_before_editing"
	case domaincatalog.TemplateUsageRiskMedium:
		summary.RecommendedAction = "review_impact_before_saving"
	default:
		summary.RecommendedAction = "save_with_standard_review"
	}
	summary.LastExecutionSummary = staticTemplateUsageRuntimeSummary("application_environment_bindings", "execution status can be correlated from release board and execution task APIs")
}

func staticTemplateUsageRuntimeSummary(source, note string) map[string]any {
	return map[string]any{
		"source":       source,
		"note":         note,
		"items":        []map[string]any{},
		"statusCounts": map[string]int{},
		"stateCounts":  map[string]int{},
	}
}

type templateUsageRuntimeCollector struct {
	source       string
	items        []map[string]any
	statusCounts map[string]int
	stateCounts  map[string]int
	kindCounts   map[string]int
	latest       map[string]any
	latestAt     time.Time
}

func newTemplateUsageRuntimeCollector(source string) *templateUsageRuntimeCollector {
	return &templateUsageRuntimeCollector{
		source:       source,
		items:        []map[string]any{},
		statusCounts: map[string]int{},
		stateCounts:  map[string]int{},
		kindCounts:   map[string]int{},
	}
}

func (c *templateUsageRuntimeCollector) add(kind, id, status string, eventAt time.Time, fields map[string]any) {
	if c == nil || strings.TrimSpace(id) == "" {
		return
	}
	item := map[string]any{
		"kind":   strings.TrimSpace(kind),
		"id":     strings.TrimSpace(id),
		"status": strings.TrimSpace(status),
	}
	if !eventAt.IsZero() {
		item["observedAt"] = eventAt.Format(time.RFC3339)
	}
	for key, value := range fields {
		if value == nil {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			continue
		}
		item[key] = value
	}
	statusKey := firstNonEmpty(strings.TrimSpace(status), "unknown")
	stateKey := templateUsageRuntimeState(statusKey)
	c.statusCounts[statusKey]++
	c.stateCounts[stateKey]++
	c.kindCounts[firstNonEmpty(strings.TrimSpace(kind), "unknown")]++
	if len(c.items) < 20 {
		c.items = append(c.items, item)
	}
	if c.latest == nil || (!eventAt.IsZero() && (c.latestAt.IsZero() || eventAt.After(c.latestAt))) {
		c.latest = item
		c.latestAt = eventAt
	}
}

func (c *templateUsageRuntimeCollector) summary(note string) map[string]any {
	if c == nil {
		return nil
	}
	out := map[string]any{
		"source":       c.source,
		"items":        c.items,
		"statusCounts": c.statusCounts,
		"stateCounts":  c.stateCounts,
		"kindCounts":   c.kindCounts,
	}
	if strings.TrimSpace(note) != "" {
		out["note"] = strings.TrimSpace(note)
	}
	if c.latest != nil {
		out["latest"] = c.latest
	}
	if !c.latestAt.IsZero() {
		out["latestAt"] = c.latestAt.Format(time.RFC3339)
	}
	return out
}

func templateUsageRuntimeState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "succeeded", "success", "ready", "deployed":
		return "succeeded"
	case "failed", "failure", "error", "errored", "canceled", "cancelled", "callback_timeout", "timeout", "timed_out":
		return "failed"
	case "running", "dispatching", "building", "releasing", "deploying", "in_progress", "processing":
		return "running"
	case "queued", "pending", "waiting", "waiting_approval", "blocked":
		return "pending"
	default:
		if strings.TrimSpace(status) == "" {
			return "unknown"
		}
		return "unknown"
	}
}

func (s *Service) workflowTemplateRuntimeSummary(ctx context.Context, principal domainidentity.Principal, templateID string, bindings []domaincatalog.TemplateUsageBinding) map[string]any {
	if len(bindings) == 0 {
		return staticTemplateUsageRuntimeSummary("workflow_template_runtime", "no application environment bindings use this workflow template")
	}
	if s.runtime.Workflows == nil && s.runtime.Releases == nil && s.runtime.Delivery == nil {
		return staticTemplateUsageRuntimeSummary("application_environment_bindings", "runtime readers are not configured for workflow template usage")
	}
	collector := newTemplateUsageRuntimeCollector("workflow_template_runtime")
	seenApps := map[string]struct{}{}
	for _, binding := range bindings {
		appID := strings.TrimSpace(binding.ApplicationID)
		if appID == "" {
			continue
		}
		if _, ok := seenApps[appID]; ok {
			continue
		}
		seenApps[appID] = struct{}{}
		if s.runtime.Workflows != nil {
			if workflows, err := s.runtime.Workflows.List(ctx, principal, appID, 20); err == nil {
				for _, run := range workflows {
					if !workflowRunMatchesUsageBindings(run, bindings, templateID) {
						continue
					}
					collector.add("workflow", run.ID, run.Status, parseTemplateUsageTime(run.UpdatedAt, run.CreatedAt), map[string]any{
						"applicationId":            run.ApplicationID,
						"applicationEnvironmentId": metadataString(run.Metadata, "bindingId"),
						"workflowName":             run.WorkflowName,
						"workflowTemplateId":       metadataString(run.Metadata, "workflowTemplateId"),
					})
				}
			}
		}
		if s.runtime.Releases != nil {
			if releases, err := s.runtime.Releases.List(ctx, principal, domainrelease.Filter{ApplicationID: appID, Limit: 20}); err == nil {
				for _, release := range releases {
					if !metadataMatchesAnyBindingID(release.Metadata, bindings) && !releaseMatchesAnyUsageBindingTarget(release, bindings) {
						continue
					}
					collector.add("release", release.ID, release.Status, release.CreatedAt, map[string]any{
						"applicationId":            release.ApplicationID,
						"applicationEnvironmentId": metadataString(release.Metadata, "applicationEnvironmentId"),
						"releaseBundleId":          metadataString(release.Metadata, "releaseBundleId"),
						"executionTaskId":          metadataString(release.Metadata, "executionTaskId"),
						"clusterId":                release.ClusterID,
						"namespace":                release.Namespace,
						"deploymentName":           release.DeploymentName,
					})
				}
			}
		}
		if s.runtime.Delivery != nil {
			if tasks, err := s.runtime.Delivery.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: appID, Limit: 20}); err == nil {
				tasks = domaindelivery.WithOperationStates(tasks, time.Now().UTC())
				for _, task := range tasks {
					if !taskMatchesAnyUsageBinding(task, bindings) {
						continue
					}
					collector.add("execution_task", task.ID, task.Status, task.UpdatedAt, map[string]any{
						"applicationId":            task.ApplicationID,
						"applicationEnvironmentId": task.ApplicationEnvironmentID,
						"releaseBundleId":          task.ReleaseBundleID,
						"taskKind":                 task.TaskKind,
						"providerKind":             task.ProviderKind,
						"operationState":           task.OperationState,
					})
				}
			}
		}
	}
	return collector.summary("latest workflow, release, and execution task evidence for matched bindings")
}

func (s *Service) buildTemplateRuntimeSummary(ctx context.Context, principal domainidentity.Principal, sources []domaincatalog.TemplateUsageBuildSource, bindings []domaincatalog.TemplateUsageBinding) map[string]any {
	if len(sources) == 0 {
		return staticTemplateUsageRuntimeSummary("build_template_runtime", "no application build sources use this build template")
	}
	if s.runtime.Builds == nil && s.runtime.Delivery == nil {
		return staticTemplateUsageRuntimeSummary("application_build_sources", "runtime readers are not configured for build template usage")
	}
	collector := newTemplateUsageRuntimeCollector("build_template_runtime")
	seenApps := map[string]struct{}{}
	for _, source := range sources {
		appID := strings.TrimSpace(source.ApplicationID)
		if appID == "" {
			continue
		}
		if _, ok := seenApps[appID]; ok {
			continue
		}
		seenApps[appID] = struct{}{}
		if s.runtime.Builds != nil {
			if builds, err := s.runtime.Builds.List(ctx, principal, domainbuild.Filter{ApplicationID: appID, Limit: 20}); err == nil {
				for _, build := range builds {
					if !buildRecordMatchesBuildSources(build, sources) {
						continue
					}
					collector.add("build", build.ID, build.Status, build.CreatedAt, map[string]any{
						"applicationId":            build.ApplicationID,
						"applicationEnvironmentId": metadataString(build.Metadata, "applicationEnvironmentId"),
						"buildSourceId":            metadataString(build.Metadata, "buildSourceId"),
						"releaseBundleId":          metadataString(build.Metadata, "releaseBundleId"),
						"executionTaskId":          metadataString(build.Metadata, "executionTaskId"),
						"sourceSystem":             build.SourceSystem,
					})
				}
			}
		}
		if s.runtime.Delivery != nil {
			if bundles, err := s.runtime.Delivery.ListReleaseBundles(ctx, domaindelivery.ReleaseBundleFilter{ApplicationID: appID, Limit: 20}); err == nil {
				for _, bundle := range bundles {
					if !bundleMatchesUsageBindings(bundle, bindings) && !bundleMatchesBuildSources(bundle, sources) {
						continue
					}
					collector.add("release_bundle", bundle.ID, bundle.Status, bundle.UpdatedAt, map[string]any{
						"applicationId":            bundle.ApplicationID,
						"applicationEnvironmentId": bundle.ApplicationEnvironmentID,
						"version":                  bundle.Version,
						"sourceType":               bundle.SourceType,
						"artifactRef":              bundle.ArtifactRef,
					})
				}
			}
			if tasks, err := s.runtime.Delivery.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: appID, Limit: 20}); err == nil {
				tasks = domaindelivery.WithOperationStates(tasks, time.Now().UTC())
				for _, task := range tasks {
					if !taskMatchesAnyUsageBinding(task, bindings) && !taskMatchesBuildSources(task, sources) {
						continue
					}
					collector.add("execution_task", task.ID, task.Status, task.UpdatedAt, map[string]any{
						"applicationId":            task.ApplicationID,
						"applicationEnvironmentId": task.ApplicationEnvironmentID,
						"releaseBundleId":          task.ReleaseBundleID,
						"taskKind":                 task.TaskKind,
						"providerKind":             task.ProviderKind,
						"operationState":           task.OperationState,
					})
				}
			}
		}
	}
	return collector.summary("latest build, release bundle, and execution task evidence for matched build sources")
}

func workflowRunMatchesUsageBindings(run domainworkflow.Run, bindings []domaincatalog.TemplateUsageBinding, templateID string) bool {
	bindingID := metadataString(run.Metadata, "bindingId")
	runTemplateID := metadataString(run.Metadata, "workflowTemplateId")
	for _, binding := range bindings {
		if strings.TrimSpace(run.ApplicationID) != strings.TrimSpace(binding.ApplicationID) {
			continue
		}
		if bindingID != "" && strings.TrimSpace(binding.ID) == bindingID {
			return true
		}
		if runTemplateID != "" && templateID != "" && runTemplateID == templateID {
			return true
		}
	}
	return false
}

func releaseMatchesAnyUsageBindingTarget(record domainrelease.Record, bindings []domaincatalog.TemplateUsageBinding) bool {
	for _, binding := range bindings {
		if strings.TrimSpace(record.ApplicationID) != strings.TrimSpace(binding.ApplicationID) {
			continue
		}
		if strings.TrimSpace(record.ClusterID) == "" || strings.TrimSpace(record.Namespace) == "" || strings.TrimSpace(record.DeploymentName) == "" {
			continue
		}
		return true
	}
	return false
}

func metadataMatchesAnyBindingID(metadata map[string]any, bindings []domaincatalog.TemplateUsageBinding) bool {
	bindingID := metadataString(metadata, "applicationEnvironmentId")
	if bindingID == "" {
		bindingID = metadataString(metadata, "bindingId")
	}
	if bindingID == "" {
		return false
	}
	for _, binding := range bindings {
		if strings.TrimSpace(binding.ID) == bindingID {
			return true
		}
	}
	return false
}

func buildRecordMatchesBuildSources(record domainbuild.Record, sources []domaincatalog.TemplateUsageBuildSource) bool {
	buildSourceID := metadataString(record.Metadata, "buildSourceId")
	for _, source := range sources {
		if strings.TrimSpace(record.ApplicationID) != strings.TrimSpace(source.ApplicationID) {
			continue
		}
		if buildSourceID == "" || buildSourceID == strings.TrimSpace(source.BuildSourceID) {
			return true
		}
	}
	return false
}

func bundleMatchesUsageBindings(bundle domaindelivery.ReleaseBundle, bindings []domaincatalog.TemplateUsageBinding) bool {
	for _, binding := range bindings {
		if strings.TrimSpace(bundle.ApplicationID) != strings.TrimSpace(binding.ApplicationID) {
			continue
		}
		if strings.TrimSpace(bundle.ApplicationEnvironmentID) == "" || strings.TrimSpace(bundle.ApplicationEnvironmentID) == strings.TrimSpace(binding.ID) {
			return true
		}
	}
	return false
}

func bundleMatchesBuildSources(bundle domaindelivery.ReleaseBundle, sources []domaincatalog.TemplateUsageBuildSource) bool {
	buildSourceID := metadataString(bundle.Metadata, "buildSourceId")
	if buildSourceID == "" {
		return false
	}
	for _, source := range sources {
		if strings.TrimSpace(bundle.ApplicationID) == strings.TrimSpace(source.ApplicationID) && buildSourceID == strings.TrimSpace(source.BuildSourceID) {
			return true
		}
	}
	return false
}

func taskMatchesAnyUsageBinding(task domaindelivery.ExecutionTask, bindings []domaincatalog.TemplateUsageBinding) bool {
	for _, binding := range bindings {
		if strings.TrimSpace(task.ApplicationID) != strings.TrimSpace(binding.ApplicationID) {
			continue
		}
		if strings.TrimSpace(task.ApplicationEnvironmentID) == "" || strings.TrimSpace(task.ApplicationEnvironmentID) == strings.TrimSpace(binding.ID) {
			return true
		}
	}
	return false
}

func taskMatchesBuildSources(task domaindelivery.ExecutionTask, sources []domaincatalog.TemplateUsageBuildSource) bool {
	buildSourceID := metadataString(task.Payload, "buildSourceId")
	if buildSourceID == "" {
		buildSourceID = metadataString(task.Result, "buildSourceId")
	}
	if buildSourceID == "" {
		return false
	}
	for _, source := range sources {
		if strings.TrimSpace(task.ApplicationID) == strings.TrimSpace(source.ApplicationID) && buildSourceID == strings.TrimSpace(source.BuildSourceID) {
			return true
		}
	}
	return false
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func parseTemplateUsageTime(values ...string) time.Time {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func templateUsageAuditSnapshot(usage domaincatalog.TemplateUsageSummary) map[string]any {
	return map[string]any{
		"templateKind":               usage.TemplateKind,
		"templateId":                 usage.TemplateID,
		"usageCount":                 usage.UsageCount,
		"applicationCount":           usage.ApplicationCount,
		"environmentCount":           usage.EnvironmentCount,
		"productionEnvironmentCount": usage.ProductionEnvironmentCount,
		"approvalBindingCount":       usage.ApprovalBindingCount,
		"targetCount":                usage.TargetCount,
		"riskLevel":                  usage.RiskLevel,
		"riskReasons":                usage.RiskReasons,
		"recommendedAction":          usage.RecommendedAction,
		"lastExecutionSummary":       usage.LastExecutionSummary,
	}
}

func templateUsageAuditChangeSnapshot(before, after *domaincatalog.TemplateUsageSummary) map[string]any {
	if after == nil && before == nil {
		return nil
	}
	if after == nil {
		return templateUsageAuditSnapshot(*before)
	}
	snapshot := templateUsageAuditSnapshot(*after)
	if before != nil {
		snapshot["before"] = templateUsageAuditSnapshot(*before)
		snapshot["after"] = templateUsageAuditSnapshot(*after)
	}
	return snapshot
}

func (s *Service) authorizeApplicationEnvironment(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, item domaincatalog.ApplicationEnvironment) error {
	if s.authorizer == nil {
		return nil
	}
	return s.authorizeDelivery(ctx, principal, action, "ApplicationEnvironment", item.ID, item.BusinessLineID, item.ApplicationGroup, item.EnvironmentKey, item.ApplicationID)
}

func (s *Service) authorizeApplicationEnvironmentInput(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, input domaincatalog.ApplicationEnvironmentInput) error {
	if s.authorizer == nil {
		return nil
	}
	businessLineID, applicationGroup, err := s.lookupApplicationScope(ctx, input.ApplicationID)
	if err != nil {
		return err
	}
	resourceName := strings.TrimSpace(input.ID)
	if resourceName == "" {
		resourceName = fmt.Sprintf("%s:%s", strings.TrimSpace(input.ApplicationID), strings.TrimSpace(input.EnvironmentID))
	}
	return s.authorizeDelivery(ctx, principal, action, "ApplicationEnvironment", resourceName, businessLineID, applicationGroup, input.EnvironmentID, input.ApplicationID)
}

func (s *Service) lookupApplicationScope(ctx context.Context, applicationID string) (string, string, error) {
	if s.apps != nil {
		app, err := s.apps.Get(ctx, applicationID)
		if err == nil {
			return app.BusinessLineID, app.Group, nil
		}
	}
	repo, ok := s.repo.(catalogLookupRepository)
	if !ok {
		return "", "", nil
	}
	items, err := repo.ListApplicationEnvironments(ctx)
	if err != nil {
		return "", "", fmt.Errorf("list application environments: %w", err)
	}
	for _, item := range items {
		if item.ApplicationID == strings.TrimSpace(applicationID) {
			return item.BusinessLineID, item.ApplicationGroup, nil
		}
	}
	return "", "", nil
}

func (s *Service) authorizeDelivery(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, resourceKind, resourceName, businessLineID, applicationGroup, environmentKey, applicationID string) error {
	decision, err := s.authorizer.Authorize(ctx, domainaccess.Request{
		Principal: principal,
		Action:    action,
		Subject: domainaccess.SubjectAttributes{
			UserID:   principal.UserID,
			Roles:    principal.Roles,
			Teams:    principal.Teams,
			Projects: principal.Projects,
			Tags:     principal.Tags,
		},
		Resource: domainaccess.ResourceAttributes{
			Kind: resourceKind,
			Name: resourceName,
		},
		Delivery: domainaccess.DeliveryAttributes{
			BusinessLineID:   strings.TrimSpace(businessLineID),
			ApplicationGroup: strings.TrimSpace(applicationGroup),
			EnvironmentKey:   strings.TrimSpace(environmentKey),
			ApplicationID:    strings.TrimSpace(applicationID),
		},
		Context: domainaccess.ContextAttributes{
			Source:     requestctx.FromContext(ctx).Source,
			OccurredAt: time.Now().UTC(),
		},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return nil
}

func validateWorkflowTemplateGraph(mode string, nodes []any, rawEdges any) error {
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
		if mode == "delivery_dag" {
			if err := validateDeliveryDAGNode(node); err != nil {
				return err
			}
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
		"restart_workload":     {},
		"scale_workload":       {},
		"delete_pod":           {},
		"evict_pod":            {},
		"http_callback":        {},
		"create_silence":       {},
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

func validateDeliveryDAGNode(node map[string]any) error {
	for _, key := range []string{"inputs", "outputs"} {
		if value, ok := node[key]; ok {
			items, ok := toSliceAny(value)
			if !ok {
				return fmt.Errorf("%w: delivery_dag node.%s must be an array", apperrors.ErrInvalidArgument, key)
			}
			for _, item := range items {
				if strings.TrimSpace(fmt.Sprint(item)) == "" {
					return fmt.Errorf("%w: delivery_dag node.%s cannot contain empty values", apperrors.ErrInvalidArgument, key)
				}
			}
		}
	}
	for _, key := range []string{"serviceSelector", "environmentSelector", "targetSelector", "observability"} {
		if value, ok := node[key]; ok && value != nil {
			if _, ok := value.(map[string]any); !ok {
				return fmt.Errorf("%w: delivery_dag node.%s must be an object", apperrors.ErrInvalidArgument, key)
			}
		}
	}
	if rawOutputs, ok := node["artifactOutputs"]; ok {
		outputs, ok := toSliceAny(rawOutputs)
		if !ok {
			return fmt.Errorf("%w: delivery_dag node.artifactOutputs must be an array", apperrors.ErrInvalidArgument)
		}
		for _, rawOutput := range outputs {
			output, ok := rawOutput.(map[string]any)
			if !ok {
				return fmt.Errorf("%w: delivery_dag node.artifactOutputs must contain objects", apperrors.ErrInvalidArgument)
			}
			if strings.TrimSpace(fmt.Sprint(output["name"])) == "" {
				return fmt.Errorf("%w: delivery_dag artifact output requires name", apperrors.ErrInvalidArgument)
			}
			switch strings.TrimSpace(fmt.Sprint(output["kind"])) {
			case "image", "test_report", "scan_report", "sbom":
			default:
				return fmt.Errorf("%w: unsupported delivery_dag artifact output kind %s", apperrors.ErrInvalidArgument, output["kind"])
			}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
