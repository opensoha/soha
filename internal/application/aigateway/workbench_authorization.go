package aigateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type workbenchAuthorizationCeilingKey struct{}
type workbenchAuthorizationStateKey struct{}
type nativeWorkbenchInvocationKey struct{}

func WithNativeWorkbenchInvocation(ctx context.Context) context.Context {
	return context.WithValue(ctx, nativeWorkbenchInvocationKey{}, true)
}

func nativeUnownedApproval(ctx context.Context) bool {
	native, _ := ctx.Value(nativeWorkbenchInvocationKey{}).(bool)
	if !native {
		return false
	}
	execution, bound := capabilityExecutionFrom(ctx)
	return !bound || execution.Scope != domainworkflow.ScopeCapabilityTask || execution.RunID == ""
}

type workbenchAuthorizationState struct {
	Grants   []domainaigateway.ToolGrant
	Policies []domainaigateway.AccessPolicy
	Bindings []domainaigateway.SkillBinding
}

func WithWorkbenchAuthorizationCeiling(ctx context.Context, revision string) context.Context {
	ctx = WithNativeWorkbenchInvocation(ctx)
	return context.WithValue(ctx, workbenchAuthorizationCeilingKey{}, revision)
}

func (s *Service) WorkbenchAuthorizationRevision(ctx context.Context, principal domainidentity.Principal) (string, error) {
	_, revision, err := s.workbenchAuthorizationState(ctx, principal)
	return revision, err
}

func (s *Service) workbenchAuthorizationState(ctx context.Context, principal domainidentity.Principal) (*workbenchAuthorizationState, string, error) {
	ctx = context.WithValue(ctx, workbenchAuthorizationCeilingKey{}, nil)
	ctx = context.WithValue(ctx, workbenchAuthorizationStateKey{}, nil)
	state := &workbenchAuthorizationState{}
	var err error
	if state.Grants, err = s.activeToolGrants(ctx, principal, ""); err != nil {
		return nil, "", err
	}
	if state.Policies, err = s.activeAccessPolicies(ctx, principal, ""); err != nil {
		return nil, "", err
	}
	if state.Bindings, err = s.activeSkillBindings(ctx, principal, ""); err != nil {
		return nil, "", err
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > 1048576 {
		return nil, "", fmt.Errorf("%w: Gateway authorization ceiling exceeds its bounded snapshot", apperrors.ErrInvalidArgument)
	}
	if err = json.Unmarshal(data, state); err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(data)
	return state, hex.EncodeToString(hash[:]), nil
}

func (s *Service) workbenchPolicyState(ctx context.Context, principal domainidentity.Principal, aiClientID string) (*workbenchAuthorizationState, error) {
	if state, ok := ctx.Value(workbenchAuthorizationStateKey{}).(*workbenchAuthorizationState); ok {
		return state, nil
	}
	expected, guarded := ctx.Value(workbenchAuthorizationCeilingKey{}).(string)
	if !guarded {
		return nil, nil
	}
	if aiClientID != "" {
		return nil, apperrors.ErrAccessDenied
	}
	state, current, err := s.workbenchAuthorizationState(ctx, principal)
	if err != nil {
		return nil, err
	}
	if expected == "" || expected != current {
		return nil, apperrors.ErrAccessDenied
	}
	return state, nil
}

func (s *Service) withWorkbenchAuthorization(ctx context.Context, principal domainidentity.Principal, input domainaigateway.ToolInvocationRequest) (context.Context, error) {
	expected, guarded := ctx.Value(workbenchAuthorizationCeilingKey{}).(string)
	if !guarded {
		return ctx, nil
	}
	if input.AIClientID != "" || input.SkillID != "" {
		return ctx, apperrors.ErrAccessDenied
	}
	// Do not reuse a previous invocation's policy reads on nested or repeated calls.
	ctx = context.WithValue(ctx, workbenchAuthorizationStateKey{}, nil)
	state, current, err := s.workbenchAuthorizationState(ctx, principal)
	if err != nil {
		return ctx, err
	}
	if expected == "" || current != expected {
		return ctx, fmt.Errorf("%w: original Gateway tool scope changed", apperrors.ErrAccessDenied)
	}
	return context.WithValue(ctx, workbenchAuthorizationStateKey{}, state), nil
}
