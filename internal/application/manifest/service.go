package manifest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
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

type ApplicationReader interface {
	List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error)
	Get(context.Context, domainidentity.Principal, string) (domainapp.App, error)
}

type EnvironmentReader interface {
	GetApplicationEnvironment(context.Context, domainidentity.Principal, string) (domaincatalog.ApplicationEnvironment, error)
}

type ClusterReader interface {
	GetConnection(context.Context, string) (domaincluster.Connection, error)
}

type Service struct {
	repository   domainmanifest.Repository
	applications ApplicationReader
	environments EnvironmentReader
	clusters     ClusterReader
	authorizer   domainaccess.Authorizer
	permissions  *appaccess.PermissionResolver
	audit        AuditRecorder
	operations   OperationRecorder
}

func New(repository domainmanifest.Repository, applications ApplicationReader, environments EnvironmentReader, clusters ClusterReader, authorizer domainaccess.Authorizer, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder) *Service {
	return &Service{repository: repository, applications: applications, environments: environments, clusters: clusters, authorizer: authorizer, permissions: permissions, audit: audit, operations: operations}
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, filter domainmanifest.Filter) (domainmanifest.Page, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domainmanifest.Page{}, err
	}
	filter.Page, filter.PageSize = normalizePagination(filter.Page, filter.PageSize, filter.Limit)
	cluster, err := s.loadCluster(ctx, filter.ClusterID)
	if err != nil {
		return domainmanifest.Page{}, err
	}
	applicationIDs, err := s.authorizedApplicationIDs(ctx, principal, filter.ApplicationID, cluster, filter.Namespace)
	if err != nil {
		return domainmanifest.Page{}, err
	}
	filter.ApplicationIDs = applicationIDs
	return s.repository.List(ctx, filter)
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, packageID string) (domainmanifest.Package, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domainmanifest.Package{}, err
	}
	item, err := s.get(ctx, packageID)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	if _, err := s.authorizePackage(ctx, principal, domainaccess.ActionView, item); err != nil {
		return domainmanifest.Package{}, err
	}
	return item, nil
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, input domainmanifest.Input) (domainmanifest.Package, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domainmanifest.Package{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	app, err := s.authorizeInput(ctx, principal, domainaccess.ActionCreate, item)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	item.BusinessLineID = app.BusinessLineID
	if err := s.validateBindings(ctx, principal, domainaccess.ActionCreate, &item, app); err != nil {
		return domainmanifest.Package{}, err
	}
	now := time.Now().UTC()
	item.ID = uuid.NewString()
	item.Status = domainmanifest.StatusDraft
	item.CreatedBy = principal.UserID
	item.UpdatedBy = principal.UserID
	item.CreatedAt = now
	item.UpdatedAt = now
	created, err := s.repository.Create(ctx, item)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	s.record(ctx, principal, "delivery.manifest.create", created, "created manifest package")
	return created, nil
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, packageID string, input domainmanifest.Input) (domainmanifest.Package, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domainmanifest.Package{}, err
	}
	existing, err := s.get(ctx, packageID)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	if _, err := s.authorizePackage(ctx, principal, domainaccess.ActionUpdate, existing); err != nil {
		return domainmanifest.Package{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	app, err := s.authorizeInput(ctx, principal, domainaccess.ActionUpdate, item)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	item.BusinessLineID = app.BusinessLineID
	if err := s.validateBindings(ctx, principal, domainaccess.ActionUpdate, &item, app); err != nil {
		return domainmanifest.Package{}, err
	}
	item.ID = existing.ID
	item.Status = domainmanifest.StatusDraft
	item.CurrentRevision = existing.CurrentRevision
	item.CreatedBy = existing.CreatedBy
	item.UpdatedBy = principal.UserID
	item.CreatedAt = existing.CreatedAt
	item.UpdatedAt = time.Now().UTC()
	updated, err := s.repository.Update(ctx, existing.ID, item)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	s.record(ctx, principal, "delivery.manifest.update", updated, "updated manifest draft")
	return updated, nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, packageID string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationsDelete); err != nil {
		return err
	}
	item, err := s.get(ctx, packageID)
	if err != nil {
		return err
	}
	if _, err := s.authorizePackage(ctx, principal, domainaccess.ActionDelete, item); err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, item.ID); err != nil {
		return err
	}
	summary := "deleted manifest draft"
	if item.CurrentRevision > 0 {
		summary = "archived published manifest package"
	}
	s.record(ctx, principal, "delivery.manifest.delete", item, summary)
	return nil
}

func (s *Service) Publish(ctx context.Context, principal domainidentity.Principal, packageID, note string) (domainmanifest.Package, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryReleasesTrigger); err != nil {
		return domainmanifest.Package{}, err
	}
	item, err := s.get(ctx, packageID)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	app, err := s.authorizePackage(ctx, principal, domainaccess.ActionTrigger, item)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	if err := s.validateBindings(ctx, principal, domainaccess.ActionTrigger, &item, app); err != nil {
		return domainmanifest.Package{}, err
	}
	if err := validateRenderableFiles(item); err != nil {
		return domainmanifest.Package{}, err
	}
	payload, err := json.Marshal(struct {
		Files    []domainmanifest.File    `json:"files"`
		Bindings []domainmanifest.Binding `json:"bindings"`
	}{item.Files, item.Bindings})
	if err != nil {
		return domainmanifest.Package{}, fmt.Errorf("encode manifest revision: %w", err)
	}
	sum := sha256.Sum256(payload)
	revision := domainmanifest.Revision{
		ID: uuid.NewString(), PackageID: item.ID, Version: item.CurrentRevision + 1,
		Digest: hex.EncodeToString(sum[:]), Note: strings.TrimSpace(note), Files: item.Files,
		Bindings: item.Bindings, CreatedBy: principal.UserID, CreatedAt: time.Now().UTC(),
	}
	item.Status = domainmanifest.StatusPublished
	item.CurrentRevision = revision.Version
	item.UpdatedBy = principal.UserID
	item.UpdatedAt = revision.CreatedAt
	published, err := s.repository.Publish(ctx, item, revision)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	s.record(ctx, principal, "delivery.manifest.publish", published, fmt.Sprintf("published manifest revision v%d", revision.Version))
	return published, nil
}

func (s *Service) ListRevisions(ctx context.Context, principal domainidentity.Principal, packageID string) ([]domainmanifest.Revision, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return nil, err
	}
	item, err := s.get(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorizePackage(ctx, principal, domainaccess.ActionView, item); err != nil {
		return nil, err
	}
	return s.repository.ListRevisions(ctx, strings.TrimSpace(packageID))
}

func normalizePagination(page, pageSize, legacyLimit int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = legacyLimit
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func (s *Service) authorizedApplicationIDs(ctx context.Context, principal domainidentity.Principal, applicationID string, cluster *domaincluster.Connection, namespace string) ([]string, error) {
	applicationID = strings.TrimSpace(applicationID)
	if applicationID != "" {
		app, err := s.applications.Get(ctx, principal, applicationID)
		if err != nil {
			return nil, err
		}
		if err := s.authorizeManifest(ctx, principal, domainaccess.ActionList, domainmanifest.Package{ApplicationID: app.ID}, app, cluster, "", namespace); err != nil {
			return nil, err
		}
		return []string{app.ID}, nil
	}
	apps, err := s.applications.List(ctx, principal, domainapp.Filter{Limit: 10000})
	if err != nil {
		return nil, err
	}
	allowed := make([]string, 0, len(apps))
	for _, app := range apps {
		if err := s.authorizeManifest(ctx, principal, domainaccess.ActionList, domainmanifest.Package{ApplicationID: app.ID}, app, cluster, "", namespace); err == nil {
			allowed = append(allowed, app.ID)
		}
	}
	return allowed, nil
}

func (s *Service) authorizeInput(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, item domainmanifest.Package) (domainapp.App, error) {
	app, err := s.applications.Get(ctx, principal, item.ApplicationID)
	if err != nil {
		return domainapp.App{}, err
	}
	if err := s.authorizeManifest(ctx, principal, action, item, app, nil, "", ""); err != nil {
		return domainapp.App{}, err
	}
	return app, nil
}

func (s *Service) authorizePackage(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, item domainmanifest.Package) (domainapp.App, error) {
	return s.authorizeInput(ctx, principal, action, item)
}

func (s *Service) validateBindings(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, item *domainmanifest.Package, app domainapp.App) error {
	for index := range item.Bindings {
		binding := &item.Bindings[index]
		environment, err := s.environments.GetApplicationEnvironment(ctx, principal, binding.ApplicationEnvironmentID)
		if err != nil {
			return fmt.Errorf("validate binding %d environment: %w", index+1, err)
		}
		if environment.ApplicationID != app.ID {
			return fmt.Errorf("%w: binding %d environment does not belong to application %s", apperrors.ErrInvalidArgument, index+1, app.ID)
		}
		cluster, err := s.clusters.GetConnection(ctx, binding.ClusterID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, apperrors.ErrNotFound) {
				return fmt.Errorf("%w: binding %d references unknown cluster %s", apperrors.ErrInvalidArgument, index+1, binding.ClusterID)
			}
			return fmt.Errorf("validate binding %d cluster: %w", index+1, err)
		}
		binding.EnvironmentKey = environment.EnvironmentKey
		if binding.EnvironmentKey == "" {
			binding.EnvironmentKey = environment.EnvironmentID
		}
		if err := s.authorizeManifest(ctx, principal, action, *item, app, &cluster, binding.EnvironmentKey, binding.Namespace); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) loadCluster(ctx context.Context, clusterID string) (*domaincluster.Connection, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return nil, nil
	}
	cluster, err := s.clusters.GetConnection(ctx, clusterID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("%w: cluster not found", apperrors.ErrNotFound)
		}
		return nil, err
	}
	return &cluster, nil
}

func (s *Service) authorizeManifest(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, item domainmanifest.Package, app domainapp.App, cluster *domaincluster.Connection, environmentKey, namespace string) error {
	if s.authorizer == nil {
		return nil
	}
	request := domainaccess.Request{
		Principal: principal,
		Action:    action,
		Subject: domainaccess.SubjectAttributes{
			UserID: principal.UserID, Roles: principal.Roles, Teams: principal.Teams,
			Projects: principal.Projects, Tags: principal.Tags,
		},
		Resource:  domainaccess.ResourceAttributes{Kind: "ManifestPackage", Name: item.Name, Owner: app.Key},
		Namespace: domainaccess.NamespaceAttributes{Namespace: strings.TrimSpace(namespace)},
		Delivery: domainaccess.DeliveryAttributes{
			BusinessLineID: app.BusinessLineID, ApplicationGroup: app.Group,
			EnvironmentKey: strings.TrimSpace(environmentKey), ApplicationID: app.ID,
		},
		Context: domainaccess.ContextAttributes{Source: requestctx.FromContext(ctx).Source, OccurredAt: time.Now().UTC()},
	}
	if cluster != nil {
		request.Cluster = domainaccess.ClusterAttributes{
			ClusterID: cluster.Summary.ID, Region: cluster.Summary.Region,
			Environment: cluster.Summary.Environment, Labels: cluster.Summary.Labels,
		}
	}
	decision, err := s.authorizer.Authorize(ctx, request)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return nil
}

func normalizeInput(input domainmanifest.Input) (domainmanifest.Package, error) {
	name := strings.TrimSpace(input.Name)
	applicationID := strings.TrimSpace(input.ApplicationID)
	if name == "" || applicationID == "" {
		return domainmanifest.Package{}, fmt.Errorf("%w: name and applicationId are required", apperrors.ErrInvalidArgument)
	}
	renderer := strings.TrimSpace(input.Renderer)
	if renderer == "" {
		renderer = domainmanifest.RendererRaw
	}
	if renderer != domainmanifest.RendererRaw && renderer != domainmanifest.RendererKustomize {
		return domainmanifest.Package{}, fmt.Errorf("%w: unsupported renderer %s", apperrors.ErrInvalidArgument, renderer)
	}
	files, err := normalizeFiles(input.Files)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	bindings, err := normalizeBindings(input.Bindings)
	if err != nil {
		return domainmanifest.Package{}, err
	}
	return domainmanifest.Package{Name: name, Description: strings.TrimSpace(input.Description), ApplicationID: applicationID, BusinessLineID: strings.TrimSpace(input.BusinessLineID), Renderer: renderer, Files: files, Bindings: bindings}, nil
}

func (s *Service) get(ctx context.Context, packageID string) (domainmanifest.Package, error) {
	packageID = strings.TrimSpace(packageID)
	if packageID == "" {
		return domainmanifest.Package{}, fmt.Errorf("%w: manifest package id is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repository.Get(ctx, packageID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return domainmanifest.Package{}, fmt.Errorf("%w: manifest package not found", apperrors.ErrNotFound)
	}
	return item, err
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permission string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permission)
}

func (s *Service) record(ctx context.Context, principal domainidentity.Principal, operationType string, item domainmanifest.Package, summary string) {
	meta := requestctx.FromContext(ctx)
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams, ResourceKind: "ManifestPackage", ResourceName: item.Name, Action: strings.TrimPrefix(operationType, "delivery.manifest."), Result: "success", Summary: summary, RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: map[string]any{"manifestPackageId": item.ID, "applicationId": item.ApplicationID}})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(ctx, principal, operationType, map[string]any{"module": "delivery", "resourceKind": "ManifestPackage", "targetId": item.ID, "targetLabel": item.Name}, "success", summary, map[string]any{"manifestPackageId": item.ID, "applicationId": item.ApplicationID}))
	}
}
