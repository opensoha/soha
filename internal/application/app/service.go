package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type Repository interface {
	List(context.Context, domainapp.Filter) ([]domainapp.App, error)
	Get(context.Context, string) (domainapp.App, error)
	Create(context.Context, domainapp.UpsertInput) (domainapp.App, error)
	Update(context.Context, string, domainapp.UpsertInput) (domainapp.App, error)
	Delete(context.Context, string) error
	ListServices(context.Context, string) ([]domainapp.Service, error)
	GetService(context.Context, string, string) (domainapp.Service, error)
	CreateService(context.Context, string, domainapp.ServiceInput) (domainapp.Service, error)
	UpdateService(context.Context, string, string, domainapp.ServiceInput) (domainapp.Service, error)
	DeleteService(context.Context, string, string) error
}

type GitLabClient interface {
	ListProjects(context.Context, string, int) ([]domainapp.GitRepository, error)
	ListBranches(context.Context, string, string, int) ([]domainapp.GitReference, error)
	ListTags(context.Context, string, string, int) ([]domainapp.GitReference, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	repo        Repository
	gitlab      GitLabClient
	authorizer  domainaccess.Authorizer
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
}

func New(repo Repository, gitlab GitLabClient, authorizer domainaccess.Authorizer, audit AuditRecorder, operations OperationRecorder) *Service {
	return &Service{repo: repo, gitlab: gitlab, authorizer: authorizer, audit: audit, operations: operations}
}

func (s *Service) SetPermissionResolver(permissions *appaccess.PermissionResolver) {
	s.permissions = permissions
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, filter domainapp.Filter) ([]domainapp.App, error) {
	if err := s.authorize(ctx, principal, domainaccess.ActionList, "Application", "", "", "", "", ""); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, normalizeRepoError(err)
	}
	_ = s.recordAudit(ctx, principal, "", "Application", "", string(domainaccess.ActionList), "success", "listed applications")
	allowed := make([]domainapp.App, 0, len(items))
	for _, item := range items {
		if err := s.authorize(ctx, principal, domainaccess.ActionList, "Application", item.Name, item.Key, item.BusinessLineID, item.Group, item.ID); err != nil {
			continue
		}
		allowed = append(allowed, item)
	}
	return allowed, nil
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, applicationID string) (domainapp.App, error) {
	item, err := s.repo.Get(ctx, strings.TrimSpace(applicationID))
	if err != nil {
		return domainapp.App{}, normalizeRepoError(err)
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionView, "Application", item.Name, item.Key, item.BusinessLineID, item.Group, item.ID); err != nil {
		return domainapp.App{}, err
	}
	_ = s.recordAudit(ctx, principal, "", "Application", item.Name, string(domainaccess.ActionView), "success", "viewed application")
	return item, nil
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, input domainapp.UpsertInput) (domainapp.App, error) {
	if err := validateInput(input); err != nil {
		return domainapp.App{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionCreate, "Application", input.Name, input.Key, input.BusinessLineID, input.Group, input.ID); err != nil {
		return domainapp.App{}, err
	}
	item, err := s.repo.Create(ctx, input)
	if err != nil {
		return domainapp.App{}, normalizeRepoError(err)
	}
	_ = s.recordAudit(ctx, principal, "", "Application", item.Name, string(domainaccess.ActionCreate), "success", "created application")
	s.recordOperation(ctx, principal, "delivery.application.create", item.ID, item.Name, "created application")
	return item, nil
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, applicationID string, input domainapp.UpsertInput) (domainapp.App, error) {
	if err := validateInput(input); err != nil {
		return domainapp.App{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionUpdate, "Application", input.Name, input.Key, input.BusinessLineID, input.Group, strings.TrimSpace(applicationID)); err != nil {
		return domainapp.App{}, err
	}
	item, err := s.repo.Update(ctx, strings.TrimSpace(applicationID), input)
	if err != nil {
		return domainapp.App{}, normalizeRepoError(err)
	}
	_ = s.recordAudit(ctx, principal, "", "Application", item.Name, string(domainaccess.ActionUpdate), "success", "updated application")
	s.recordOperation(ctx, principal, "delivery.application.update", item.ID, item.Name, "updated application")
	return item, nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, applicationID string) error {
	item, err := s.repo.Get(ctx, strings.TrimSpace(applicationID))
	if err != nil {
		return normalizeRepoError(err)
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionDelete, "Application", item.Name, item.Key, item.BusinessLineID, item.Group, item.ID); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, applicationID); err != nil {
		return normalizeRepoError(err)
	}
	_ = s.recordAudit(ctx, principal, "", "Application", item.Name, string(domainaccess.ActionDelete), "success", "deleted application")
	s.recordOperation(ctx, principal, "delivery.application.delete", item.ID, item.Name, "deleted application")
	return nil
}

func (s *Service) ListServices(ctx context.Context, principal domainidentity.Principal, applicationID string) ([]domainapp.Service, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationServicesView); err != nil {
		return nil, err
	}
	app, err := s.Get(ctx, principal, applicationID)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListServices(ctx, app.ID)
	if err != nil {
		return nil, normalizeRepoError(err)
	}
	_ = s.recordAudit(ctx, principal, "", "ApplicationService", app.Name, string(domainaccess.ActionList), "success", "listed application services")
	return items, nil
}

func (s *Service) GetService(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID string) (domainapp.Service, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationServicesView); err != nil {
		return domainapp.Service{}, err
	}
	app, err := s.Get(ctx, principal, applicationID)
	if err != nil {
		return domainapp.Service{}, err
	}
	item, err := s.repo.GetService(ctx, app.ID, strings.TrimSpace(serviceID))
	if err != nil {
		return domainapp.Service{}, normalizeRepoError(err)
	}
	_ = s.recordAudit(ctx, principal, "", "ApplicationService", item.Name, string(domainaccess.ActionView), "success", "viewed application service")
	return item, nil
}

func (s *Service) CreateService(ctx context.Context, principal domainidentity.Principal, applicationID string, input domainapp.ServiceInput) (domainapp.Service, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationServicesManage); err != nil {
		return domainapp.Service{}, err
	}
	app, err := s.repo.Get(ctx, strings.TrimSpace(applicationID))
	if err != nil {
		return domainapp.Service{}, normalizeRepoError(err)
	}
	if err := validateServiceInput(input); err != nil {
		return domainapp.Service{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionUpdate, "ApplicationService", input.Name, input.Key, app.BusinessLineID, app.Group, app.ID); err != nil {
		return domainapp.Service{}, err
	}
	item, err := s.repo.CreateService(ctx, app.ID, input)
	if err != nil {
		return domainapp.Service{}, normalizeRepoError(err)
	}
	_ = s.recordAudit(ctx, principal, "", "ApplicationService", item.Name, string(domainaccess.ActionUpdate), "success", "created application service")
	s.recordOperation(ctx, principal, "delivery.application-service.create", item.ID, item.Name, "created application service")
	return item, nil
}

func (s *Service) UpdateService(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID string, input domainapp.ServiceInput) (domainapp.Service, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationServicesManage); err != nil {
		return domainapp.Service{}, err
	}
	app, err := s.repo.Get(ctx, strings.TrimSpace(applicationID))
	if err != nil {
		return domainapp.Service{}, normalizeRepoError(err)
	}
	if err := validateServiceInput(input); err != nil {
		return domainapp.Service{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionUpdate, "ApplicationService", input.Name, input.Key, app.BusinessLineID, app.Group, app.ID); err != nil {
		return domainapp.Service{}, err
	}
	item, err := s.repo.UpdateService(ctx, app.ID, strings.TrimSpace(serviceID), input)
	if err != nil {
		return domainapp.Service{}, normalizeRepoError(err)
	}
	_ = s.recordAudit(ctx, principal, "", "ApplicationService", item.Name, string(domainaccess.ActionUpdate), "success", "updated application service")
	s.recordOperation(ctx, principal, "delivery.application-service.update", item.ID, item.Name, "updated application service")
	return item, nil
}

func (s *Service) DeleteService(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationServicesManage); err != nil {
		return err
	}
	app, err := s.repo.Get(ctx, strings.TrimSpace(applicationID))
	if err != nil {
		return normalizeRepoError(err)
	}
	item, err := s.repo.GetService(ctx, app.ID, strings.TrimSpace(serviceID))
	if err != nil {
		return normalizeRepoError(err)
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionDelete, "ApplicationService", item.Name, item.Key, app.BusinessLineID, app.Group, app.ID); err != nil {
		return err
	}
	if err := s.repo.DeleteService(ctx, app.ID, item.ID); err != nil {
		return normalizeRepoError(err)
	}
	_ = s.recordAudit(ctx, principal, "", "ApplicationService", item.Name, string(domainaccess.ActionDelete), "success", "deleted application service")
	s.recordOperation(ctx, principal, "delivery.application-service.delete", item.ID, item.Name, "deleted application service")
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

func (s *Service) ListGitRepositories(ctx context.Context, principal domainidentity.Principal, search string, limit int) ([]domainapp.GitRepository, error) {
	if s.gitlab == nil {
		return nil, fmt.Errorf("%w: gitlab client is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionList, "GitRepository", "", "", "", "", ""); err != nil {
		return nil, err
	}
	items, err := s.gitlab.ListProjects(ctx, search, limit)
	if err != nil {
		return nil, err
	}
	_ = s.recordAudit(ctx, principal, "", "GitRepository", "", string(domainaccess.ActionList), "success", "listed gitlab repositories")
	return items, nil
}

func (s *Service) ListGitBranches(ctx context.Context, principal domainidentity.Principal, projectID, search string, limit int) ([]domainapp.GitReference, error) {
	if s.gitlab == nil {
		return nil, fmt.Errorf("%w: gitlab client is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionList, "GitBranch", projectID, "", "", "", ""); err != nil {
		return nil, err
	}
	items, err := s.gitlab.ListBranches(ctx, projectID, search, limit)
	if err != nil {
		return nil, err
	}
	_ = s.recordAudit(ctx, principal, "", "GitBranch", projectID, string(domainaccess.ActionList), "success", "listed gitlab branches")
	return items, nil
}

func (s *Service) ListGitTags(ctx context.Context, principal domainidentity.Principal, projectID, search string, limit int) ([]domainapp.GitReference, error) {
	if s.gitlab == nil {
		return nil, fmt.Errorf("%w: gitlab client is not configured", apperrors.ErrInvalidArgument)
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionList, "GitTag", projectID, "", "", "", ""); err != nil {
		return nil, err
	}
	items, err := s.gitlab.ListTags(ctx, projectID, search, limit)
	if err != nil {
		return nil, err
	}
	_ = s.recordAudit(ctx, principal, "", "GitTag", projectID, string(domainaccess.ActionList), "success", "listed gitlab tags")
	return items, nil
}

func validateInput(input domainapp.UpsertInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: application name is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Key) == "" {
		return fmt.Errorf("%w: application key is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Group) == "" {
		return fmt.Errorf("%w: application group is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Language) == "" {
		return fmt.Errorf("%w: application language is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateServiceInput(input domainapp.ServiceInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: service name is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Key) == "" {
		return fmt.Errorf("%w: service key is required", apperrors.ErrInvalidArgument)
	}
	if input.ServiceKind != "" {
		switch input.ServiceKind {
		case domainapp.ServiceKindKubernetesWorkload, domainapp.ServiceKindHelmRelease, domainapp.ServiceKindExternalService, domainapp.ServiceKindJob:
		default:
			return fmt.Errorf("%w: unsupported serviceKind %s", apperrors.ErrInvalidArgument, input.ServiceKind)
		}
	}
	for _, container := range input.Containers {
		if strings.TrimSpace(container.Name) == "" {
			return fmt.Errorf("%w: container name is required", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, resourceKind, resourceName, owner, businessLineID, applicationGroup, applicationID string) error {
	if s.authorizer == nil {
		return nil
	}
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
			Kind:  resourceKind,
			Name:  resourceName,
			Owner: owner,
		},
		Delivery: domainaccess.DeliveryAttributes{
			BusinessLineID:   strings.TrimSpace(businessLineID),
			ApplicationGroup: strings.TrimSpace(applicationGroup),
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

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, namespace, resourceKind, resourceName, action, result, summary string) error {
	if s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ID:            uuid.NewString(),
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		Namespace:     namespace,
		ResourceKind:  resourceKind,
		ResourceName:  resourceName,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"source": meta.Source,
		},
	})
}

func (s *Service) recordOperation(ctx context.Context, principal domainidentity.Principal, operationType, targetID, targetLabel, summary string) {
	if s.operations == nil {
		return
	}
	_ = s.operations.Record(ctx, operationentry.New(
		ctx,
		principal,
		operationType,
		map[string]any{
			"module":       "delivery",
			"resourceKind": "Application",
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
