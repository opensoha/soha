package scopegrant

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
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
	repo        domainscopegrant.Repository
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
}

func New(repo domainscopegrant.Repository, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder) *Service {
	return &Service{repo: repo, permissions: permissions, audit: audit, operations: operations}
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal) ([]domainscopegrant.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAccessScopeGrantsView); err != nil {
		return nil, err
	}
	return s.repo.List(ctx)
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, input domainscopegrant.Input) (domainscopegrant.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAccessScopeGrantsManage); err != nil {
		return domainscopegrant.Record{}, err
	}
	if err := validateInput(input); err != nil {
		return domainscopegrant.Record{}, err
	}
	item, err := s.repo.Create(ctx, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "access.scope_grant.create", item.ID, input.SubjectID, "created scope grant")
	}
	return item, err
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, id string, input domainscopegrant.Input) (domainscopegrant.Record, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAccessScopeGrantsManage); err != nil {
		return domainscopegrant.Record{}, err
	}
	if err := validateInput(input); err != nil {
		return domainscopegrant.Record{}, err
	}
	item, err := s.repo.Update(ctx, id, input)
	if err == nil {
		s.recordWriteLogs(ctx, principal, "access.scope_grant.update", item.ID, input.SubjectID, "updated scope grant")
	}
	return item, normalizeRepoError(err)
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermAccessScopeGrantsManage); err != nil {
		return err
	}
	if err := normalizeRepoError(s.repo.Delete(ctx, id)); err != nil {
		return err
	}
	s.recordWriteLogs(ctx, principal, "access.scope_grant.delete", id, id, "deleted scope grant")
	return nil
}

func validateInput(input domainscopegrant.Input) error {
	if strings.TrimSpace(input.SubjectType) == "" || strings.TrimSpace(input.SubjectID) == "" {
		return fmt.Errorf("%w: subjectType and subjectId are required", apperrors.ErrInvalidArgument)
	}
	subjectType := strings.ToLower(strings.TrimSpace(input.SubjectType))
	if subjectType != "user" && subjectType != "team" {
		return fmt.Errorf("%w: subjectType must be user or team", apperrors.ErrInvalidArgument)
	}
	scopeType := strings.ToLower(strings.TrimSpace(input.ScopeType))
	if scopeType == "" {
		scopeType = domainscopegrant.ScopeTypeLegacy
	}
	if scopeType != domainscopegrant.ScopeTypeLegacy && scopeType != domainscopegrant.ScopeTypeDelivery && scopeType != domainscopegrant.ScopeTypePlatform {
		return fmt.Errorf("%w: scopeType must be legacy, delivery, or platform", apperrors.ErrInvalidArgument)
	}
	if scopeType != domainscopegrant.ScopeTypePlatform && strings.TrimSpace(input.BusinessLineID) == "" {
		return fmt.Errorf("%w: businessLineId is required", apperrors.ErrInvalidArgument)
	}
	if scopeType == domainscopegrant.ScopeTypePlatform && len(nonEmptyStrings(input.ClusterIDs)) == 0 {
		return fmt.Errorf("%w: clusterIds are required for platform scope grants", apperrors.ErrInvalidArgument)
	}
	if !domainaccess.ValidNamespaceSelector(input.NamespaceSelector) {
		return fmt.Errorf("%w: namespaceSelector is invalid", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Role) == "" {
		return fmt.Errorf("%w: role is required", apperrors.ErrInvalidArgument)
	}
	effect := strings.ToLower(strings.TrimSpace(input.Effect))
	if effect != "" && effect != "allow" && effect != "deny" {
		return fmt.Errorf("%w: effect must be allow or deny", apperrors.ErrInvalidArgument)
	}
	return nil
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
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

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func (s *Service) recordWriteLogs(ctx context.Context, principal domainidentity.Principal, operationType, targetID, targetLabel, summary string) {
	meta := requestctx.FromContext(ctx)
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{
			ActorID:       principal.UserID,
			ActorName:     principal.UserName,
			Roles:         principal.Roles,
			Teams:         principal.Teams,
			ResourceKind:  "ScopeGrant",
			ResourceName:  targetLabel,
			Action:        strings.TrimPrefix(operationType, "access."),
			Result:        "success",
			Summary:       summary,
			RequestPath:   meta.Path,
			RequestMethod: meta.Method,
			RequestID:     meta.RequestID,
			SourceIP:      meta.SourceIP,
			Metadata: map[string]any{
				"scopeGrantId": targetID,
				"source":       meta.Source,
			},
		})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(
			ctx,
			principal,
			operationType,
			map[string]any{
				"module":       "access",
				"resourceKind": "ScopeGrant",
				"targetId":     targetID,
				"targetLabel":  targetLabel,
			},
			"success",
			summary,
			map[string]any{
				"scopeGrantId": targetID,
			},
		))
	}
}
