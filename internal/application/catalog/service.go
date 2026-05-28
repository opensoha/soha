package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/soha/soha/internal/application/access"
	domainaccess "github.com/soha/soha/internal/domain/access"
	domainapp "github.com/soha/soha/internal/domain/application"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domaincatalog "github.com/soha/soha/internal/domain/catalog"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainoperation "github.com/soha/soha/internal/domain/operation"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/operationentry"
	"github.com/soha/soha/internal/platform/requestctx"
	catalogrepo "github.com/soha/soha/internal/repository/catalog"
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
}

type catalogLookupRepository interface {
	domaincatalog.Repository
	GetEnvironment(context.Context, string) (domaincatalog.Environment, error)
	GetApplicationEnvironment(context.Context, string) (domaincatalog.ApplicationEnvironment, error)
}

type ApplicationReader interface {
	Get(context.Context, string) (domainapp.App, error)
}

func New(repo domaincatalog.Repository, authorizer domainaccess.Authorizer, apps ApplicationReader, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder) *Service {
	return &Service{repo: repo, authorizer: authorizer, apps: apps, permissions: permissions, audit: audit, operations: operations}
}

func (s *Service) ListBusinessLines(ctx context.Context, principal domainidentity.Principal) ([]domaincatalog.BusinessLine, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBusinessLinesView); err != nil {
		return nil, err
	}
	return s.repo.ListBusinessLines(ctx)
}

func (s *Service) CreateBusinessLine(ctx context.Context, principal domainidentity.Principal, input domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBusinessLinesManage); err != nil {
		return domaincatalog.BusinessLine{}, err
	}
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.BusinessLine{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.CreateBusinessLine(ctx, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.business_line.create", "BusinessLine", item.ID, item.Name, "created business line")
	}
	return item, err
}

func (s *Service) UpdateBusinessLine(ctx context.Context, principal domainidentity.Principal, id string, input domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBusinessLinesManage); err != nil {
		return domaincatalog.BusinessLine{}, err
	}
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.BusinessLine{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.UpdateBusinessLine(ctx, id, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.business_line.update", "BusinessLine", item.ID, item.Name, "updated business line")
	}
	return item, normalizeRepoError(err)
}

func (s *Service) DeleteBusinessLine(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryBusinessLinesManage); err != nil {
		return err
	}
	if err := normalizeRepoError(s.repo.DeleteBusinessLine(ctx, id)); err != nil {
		return err
	}
	s.recordWriteLogs(ctx, principal, "delivery.business_line.delete", "BusinessLine", id, id, "deleted business line")
	return nil
}

func (s *Service) ListEnvironments(ctx context.Context, principal domainidentity.Principal) ([]domaincatalog.Environment, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryEnvironmentsView); err != nil {
		return nil, err
	}
	return s.repo.ListEnvironments(ctx)
}

func (s *Service) CreateEnvironment(ctx context.Context, principal domainidentity.Principal, input domaincatalog.EnvironmentInput) (domaincatalog.Environment, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryEnvironmentsManage); err != nil {
		return domaincatalog.Environment{}, err
	}
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.Environment{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.CreateEnvironment(ctx, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.environment.create", "Environment", item.ID, item.Name, "created environment")
	}
	return item, err
}

func (s *Service) UpdateEnvironment(ctx context.Context, principal domainidentity.Principal, id string, input domaincatalog.EnvironmentInput) (domaincatalog.Environment, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryEnvironmentsManage); err != nil {
		return domaincatalog.Environment{}, err
	}
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.Name) == "" {
		return domaincatalog.Environment{}, fmt.Errorf("%w: key and name are required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.UpdateEnvironment(ctx, id, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.environment.update", "Environment", item.ID, item.Name, "updated environment")
	}
	return item, normalizeRepoError(err)
}

func (s *Service) DeleteEnvironment(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryEnvironmentsManage); err != nil {
		return err
	}
	if err := normalizeRepoError(s.repo.DeleteEnvironment(ctx, id)); err != nil {
		return err
	}
	s.recordWriteLogs(ctx, principal, "delivery.environment.delete", "Environment", id, id, "deleted environment")
	return nil
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
	item, err := s.repo.UpdateBuildTemplate(ctx, id, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.build_template.update", "BuildTemplate", item.ID, item.Name, "updated build template")
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
	item, err := s.repo.UpdateWorkflowTemplate(ctx, id, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "delivery.workflow_template.update", "WorkflowTemplate", item.ID, item.Name, "updated workflow template")
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

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func (s *Service) recordWriteLogs(ctx context.Context, principal domainidentity.Principal, operationType, resourceKind, targetID, targetLabel, summary string) {
	meta := requestctx.FromContext(ctx)
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
			Metadata: map[string]any{
				"targetId": targetID,
				"source":   meta.Source,
			},
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
			map[string]any{
				"targetId": targetID,
			},
		))
	}
}

func (s *Service) authorizeApplicationEnvironment(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, item domaincatalog.ApplicationEnvironment) error {
	if s.authorizer == nil {
		return nil
	}
	return s.authorizeDelivery(ctx, principal, action, "ApplicationEnvironment", item.ID, item.BusinessLineID, item.EnvironmentKey, item.ApplicationID)
}

func (s *Service) authorizeApplicationEnvironmentInput(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, input domaincatalog.ApplicationEnvironmentInput) error {
	if s.authorizer == nil {
		return nil
	}
	repo, ok := s.repo.(catalogLookupRepository)
	if !ok {
		return nil
	}
	environment, err := repo.GetEnvironment(ctx, input.EnvironmentID)
	if err != nil {
		return normalizeRepoError(err)
	}
	businessLineID, err := s.lookupApplicationBusinessLineID(ctx, input.ApplicationID)
	if err != nil {
		return err
	}
	resourceName := strings.TrimSpace(input.ID)
	if resourceName == "" {
		resourceName = fmt.Sprintf("%s:%s", strings.TrimSpace(input.ApplicationID), strings.TrimSpace(input.EnvironmentID))
	}
	return s.authorizeDelivery(ctx, principal, action, "ApplicationEnvironment", resourceName, businessLineID, environment.Key, input.ApplicationID)
}

func (s *Service) lookupApplicationBusinessLineID(ctx context.Context, applicationID string) (string, error) {
	if s.apps != nil {
		app, err := s.apps.Get(ctx, applicationID)
		if err == nil {
			return app.BusinessLineID, nil
		}
	}
	repo, ok := s.repo.(catalogLookupRepository)
	if !ok {
		return "", nil
	}
	items, err := repo.ListApplicationEnvironments(ctx)
	if err != nil {
		return "", fmt.Errorf("list application environments: %w", err)
	}
	for _, item := range items {
		if item.ApplicationID == strings.TrimSpace(applicationID) {
			return item.BusinessLineID, nil
		}
	}
	return "", nil
}

func (s *Service) authorizeDelivery(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, resourceKind, resourceName, businessLineID, environmentKey, applicationID string) error {
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
			BusinessLineID: strings.TrimSpace(businessLineID),
			EnvironmentKey: strings.TrimSpace(environmentKey),
			ApplicationID:  strings.TrimSpace(applicationID),
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
