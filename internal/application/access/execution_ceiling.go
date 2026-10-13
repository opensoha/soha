package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type executionCeilingKey struct{}

// Only a server-owned durable task passes this ceiling; it grants no permissions.
func WithExecutionAuthorizationCeiling(ctx context.Context, revision string) context.Context {
	return context.WithValue(ctx, executionCeilingKey{}, revision)
}

type executionAuthorizationState struct {
	Subject                 domainaccess.SubjectAttributes
	TokenID                 string
	Policies                []domainaccess.Policy
	Grants                  []domainscopegrant.Record
	RoleActions             map[string][]domainaccess.Action
	RolePermissions         map[string][]string
	Environments            []domaincatalog.Environment
	ApplicationEnvironments []domaincatalog.ApplicationEnvironment
}

func (s *Service) ExecutionAuthorizationRevision(ctx context.Context, principal domainidentity.Principal) (string, error) {
	_, revision, err := s.executionAuthorizationState(ctx, principal)
	return revision, err
}

func (s *Service) executionAuthorizationState(ctx context.Context, principal domainidentity.Principal) (*executionAuthorizationState, string, error) {
	if s == nil || s.permissions == nil || s.policyEngine == nil {
		return nil, "", apperrors.ErrUnsupportedOperation
	}
	state := &executionAuthorizationState{Subject: domainaccess.SubjectAttributes{UserID: principal.UserID, Roles: sortedExecutionValues(principal.Roles), Teams: sortedExecutionValues(principal.Teams), Projects: sortedExecutionValues(principal.Projects), Tags: sortedExecutionValues(principal.Tags)}, TokenID: principal.AccessTokenID}
	var err error
	if state.Policies, err = s.loadPolicies(ctx); err != nil {
		return nil, "", err
	}
	if state.Grants, err = s.loadScopeGrants(ctx); err != nil {
		return nil, "", err
	}
	if state.RoleActions, err = s.loadRoleMatrix(ctx); err != nil {
		return nil, "", err
	}
	if state.RolePermissions, err = s.permissions.RolePermissionMatrix(ctx); err != nil {
		return nil, "", err
	}
	if s.catalog != nil {
		if state.Environments, err = s.catalog.ListEnvironments(ctx); err != nil {
			return nil, "", err
		}
		if state.ApplicationEnvironments, err = s.catalog.ListApplicationEnvironments(ctx); err != nil {
			return nil, "", err
		}
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > 1048576 {
		return nil, "", fmt.Errorf("%w: authorization ceiling exceeds its bounded snapshot", apperrors.ErrInvalidArgument)
	}
	// Freeze reader-owned slices before sorting or evaluating, including in-memory adapters.
	if err = json.Unmarshal(data, state); err != nil {
		return nil, "", err
	}
	// Preserve repository order: equal-priority policy order can affect its decision.
	hash := sha256.Sum256(data)
	return state, hex.EncodeToString(hash[:]), nil
}

func sortedExecutionValues(values []string) []string {
	if len(values) == 0 {
		return nil // JWT claims and database readers represent an empty membership differently.
	}
	result := slices.Clone(values)
	slices.Sort(result)
	return slices.Compact(result)
}

func (s *Service) authorizeExecutionCeiling(ctx context.Context, request domainaccess.Request, revision string) (domainaccess.Decision, error) {
	state, current, err := s.executionAuthorizationState(ctx, request.Principal)
	if err != nil {
		return domainaccess.Decision{}, err
	}
	// ponytail: configuration changes stop the task. Use per-target frozen scopes
	// if restarting after unrelated policy edits becomes a measured product issue.
	if revision == "" || revision != current {
		return finalizeDecision(request, domainaccess.Decision{Reason: "task authorization scope changed", ReasonCode: "authorization_ceiling_changed"}), nil
	}
	request.Subject = state.Subject
	reader := *s
	reader.repo, reader.grants, reader.catalog = state, state, state
	reader.permissions = NewPermissionResolver(state)
	ctx = context.WithValue(ctx, executionCeilingKey{}, nil)
	return reader.Authorize(ctx, request)
}

func (s *executionAuthorizationState) ListPolicies(context.Context) ([]domainaccess.Policy, error) {
	return s.Policies, nil
}
func (s *executionAuthorizationState) ListRoleCapabilities(context.Context) (map[string][]domainaccess.Action, error) {
	return s.RoleActions, nil
}
func (s *executionAuthorizationState) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.RolePermissions, nil
}
func (s *executionAuthorizationState) List(context.Context) ([]domainscopegrant.Record, error) {
	return s.Grants, nil
}
func (s *executionAuthorizationState) ListEnvironments(context.Context) ([]domaincatalog.Environment, error) {
	return s.Environments, nil
}
func (s *executionAuthorizationState) ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	return s.ApplicationEnvironments, nil
}
