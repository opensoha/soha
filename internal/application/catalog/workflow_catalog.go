package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type workflowCatalogRepository interface {
	ListWorkflowCatalogCandidates(context.Context) ([]domaincatalog.WorkflowCatalogCandidate, error)
}

func (s *Service) ListWorkflowCatalog(ctx context.Context, principal domainidentity.Principal, filter domaincatalog.WorkflowCatalogFilter) (domaincatalog.WorkflowCatalogPage, error) {
	page := domaincatalog.WorkflowCatalogPage{Items: []domaincatalog.WorkflowCatalogEntry{}, Applications: []domaincatalog.WorkflowCatalogOption{}, Environments: []domaincatalog.WorkflowCatalogOption{}}
	if filter.Limit == 0 {
		filter.Limit = 12
	}
	if !validWorkflowCatalogFilter(filter) {
		return page, fmt.Errorf("%w: invalid workflow catalog filter", apperrors.ErrInvalidArgument)
	}
	canViewBuilds, canViewWorkflows, err := s.workflowCatalogPermissions(ctx, principal)
	if err != nil {
		return page, err
	}
	repo, ok := s.repo.(workflowCatalogRepository)
	if !ok {
		return page, fmt.Errorf("workflow catalog repository is unavailable")
	}
	candidates, err := repo.ListWorkflowCatalogCandidates(ctx)
	if err != nil {
		return page, err
	}
	applications, environments := map[string]string{}, map[string]string{}
	// ponytail: authorization scans lightweight definition metadata before pagination;
	// push authorized scopes into SQL if catalog size makes this scan material.
	for _, candidate := range candidates {
		build := candidate.SourceKind == "build_source"
		if (build && !canViewBuilds) || (!build && !canViewWorkflows) {
			continue
		}
		entry, err := s.authorizeWorkflowCatalogEntry(ctx, principal, candidate)
		if errors.Is(err, apperrors.ErrAccessDenied) {
			continue
		}
		if err != nil {
			return page, err
		}
		for _, scope := range entry.Scopes {
			applications[scope.ApplicationID] = scope.ApplicationName
			if scope.EnvironmentID != "" && (filter.ApplicationID == "" || filter.ApplicationID == scope.ApplicationID) {
				environments[scope.EnvironmentID] = scope.EnvironmentName
			}
		}
		if !workflowCatalogMatches(entry, filter) {
			continue
		}
		if page.Total >= filter.Offset && len(page.Items) < filter.Limit {
			page.Items = append(page.Items, entry)
		}
		page.Total++
	}
	page.Applications = workflowCatalogOptions(applications)
	page.Environments = workflowCatalogOptions(environments)
	return page, nil
}

func validWorkflowCatalogFilter(filter domaincatalog.WorkflowCatalogFilter) bool {
	return filter.Limit >= 1 && filter.Limit <= 200 && filter.Offset >= 0 && utf8.RuneCountInString(filter.Search) <= 200 &&
		(filter.Kind == "" || filter.Kind == "all" || filter.Kind == "build" || filter.Kind == "workflow")
}

func (s *Service) workflowCatalogPermissions(ctx context.Context, principal domainidentity.Principal) (bool, bool, error) {
	buildErr := s.authorize(ctx, principal, appaccess.PermDeliveryApplicationsView)
	workflowErr := s.authorize(ctx, principal, appaccess.PermDeliveryWorkflowsView)
	for _, err := range []error{buildErr, workflowErr} {
		if err != nil && !errors.Is(err, apperrors.ErrAccessDenied) {
			return false, false, err
		}
	}
	if buildErr != nil && workflowErr != nil {
		return false, false, buildErr
	}
	return buildErr == nil, workflowErr == nil, nil
}

func (s *Service) authorizeWorkflowCatalogEntry(ctx context.Context, principal domainidentity.Principal, candidate domaincatalog.WorkflowCatalogCandidate) (domaincatalog.WorkflowCatalogEntry, error) {
	entry := candidate.WorkflowCatalogEntry
	entry.Scopes = []domaincatalog.WorkflowCatalogScope{}
	if len(candidate.AuthorizationScopes) == 0 {
		return entry, apperrors.ErrAccessDenied
	}
	for _, scope := range candidate.AuthorizationScopes {
		if !scope.Exists {
			return entry, apperrors.ErrAccessDenied
		}
		resource := domainaccess.ResourceAttributes{Kind: "Workflow", Name: scope.ServiceID, Owner: scope.ApplicationKey}
		permission := appaccess.PermDeliveryWorkflowsView
		if entry.SourceKind == "build_source" {
			resource.Kind, resource.Name = "Application", scope.ApplicationName
			permission = appaccess.PermDeliveryApplicationsView
		}
		if s.authorizer != nil {
			decision, err := s.authorizer.Authorize(ctx, domainaccess.Request{
				Principal: principal, PermissionKey: permission, Action: domainaccess.ActionView,
				Subject:  domainaccess.SubjectAttributes{UserID: principal.UserID, Roles: principal.Roles, Teams: principal.Teams, Projects: principal.Projects, Tags: principal.Tags},
				Resource: resource,
				Delivery: domainaccess.DeliveryAttributes{ApplicationID: scope.ApplicationID, BusinessLineID: scope.BusinessLineID, ApplicationGroup: scope.ApplicationGroup, EnvironmentKey: scope.EnvironmentKey},
				Context:  domainaccess.ContextAttributes{Source: requestctx.FromContext(ctx).Source, OccurredAt: time.Now().UTC()},
			})
			if err != nil {
				return entry, err
			}
			if !decision.Allowed {
				return entry, apperrors.ErrAccessDenied
			}
		}
		entry.Scopes = append(entry.Scopes, scope.WorkflowCatalogScope)
	}
	return entry, nil
}

func workflowCatalogMatches(entry domaincatalog.WorkflowCatalogEntry, filter domaincatalog.WorkflowCatalogFilter) bool {
	build := entry.SourceKind == "build_source"
	if (filter.Kind == "build" && !build) || (filter.Kind == "workflow" && build) {
		return false
	}
	matchedScope := false
	searchText := entry.Name + " " + entry.Context
	for _, scope := range entry.Scopes {
		searchText += " " + scope.ApplicationName + " " + scope.EnvironmentName
		if (filter.ApplicationID == "" || filter.ApplicationID == scope.ApplicationID) && (filter.EnvironmentID == "" || filter.EnvironmentID == scope.EnvironmentID) {
			matchedScope = true
		}
	}
	return matchedScope && strings.Contains(strings.ToLower(searchText), strings.ToLower(strings.TrimSpace(filter.Search)))
}

func workflowCatalogOptions(values map[string]string) []domaincatalog.WorkflowCatalogOption {
	result := make([]domaincatalog.WorkflowCatalogOption, 0, len(values))
	for value, label := range values {
		result = append(result, domaincatalog.WorkflowCatalogOption{Value: value, Label: label})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Label == result[j].Label {
			return result[i].Value < result[j].Value
		}
		return result[i].Label < result[j].Label
	})
	return result
}
