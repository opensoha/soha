package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// A domain resolves every target from the supplied input or its durable record.
// Resolution is read-only; caller-supplied scope hints never replace domain facts.
type ToolScopeProvider interface {
	ToolInvocationScopes(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) ([]map[string]string, error)
}

type capabilityScopeContextKey struct{}
type capabilityScopeContext struct {
	tool   string
	scopes []map[string]string
}

func (s *Service) resolveCapabilityScopeContext(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (context.Context, error) {
	ctx = context.WithValue(ctx, capabilityScopeContextKey{}, capabilityScopeContext{})
	for _, provider := range s.gatewayRegistry().providers {
		if !providerHasTool(provider, tool.Name) {
			continue
		}
		resolver, ok := provider.(ToolScopeProvider)
		if _, builtin := provider.(BuiltinCapabilityProvider); builtin && tool.Domain == "docker" {
			resolver, ok = s.docker.(ToolScopeProvider)
			if !ok {
				return ctx, apperrors.ErrUnsupportedOperation
			}
		}
		if !ok {
			return ctx, nil
		}
		scopes, err := resolver.ToolInvocationScopes(ctx, principal, tool, input)
		if err != nil {
			return ctx, err
		}
		if len(scopes) == 0 || len(scopes) > 200 {
			return ctx, fmt.Errorf("%w: domain must resolve 1..200 invocation scopes", apperrors.ErrInvalidArgument)
		}
		merged := make([]map[string]string, len(scopes))
		base := capabilityGatewayScope(tool, input)
		for i, scope := range scopes {
			merged[i], err = mergeCapabilityScope(base, scope)
			if err != nil {
				return ctx, err
			}
		}
		return context.WithValue(ctx, capabilityScopeContextKey{}, capabilityScopeContext{tool: tool.Name, scopes: merged}), nil
	}
	return ctx, nil
}

func mergeCapabilityScope(base, resolved map[string]string) (map[string]string, error) {
	if len(resolved) == 0 {
		return nil, fmt.Errorf("%w: empty domain scope", apperrors.ErrInvalidArgument)
	}
	scope := maps.Clone(base)
	if scope == nil {
		scope = map[string]string{}
	}
	for key, value := range resolved {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%w: invalid domain scope", apperrors.ErrInvalidArgument)
		}
		if supplied, exists := scope[key]; exists && supplied != value {
			return nil, fmt.Errorf("%w: input scope contradicts the owning domain", apperrors.ErrAccessDenied)
		}
		scope[key] = value
	}
	return scope, nil
}

func capabilityInvocationScopes(ctx context.Context, tool string, fallback map[string]string) []map[string]string {
	resolved, _ := ctx.Value(capabilityScopeContextKey{}).(capabilityScopeContext)
	if resolved.tool == tool && len(resolved.scopes) > 0 {
		return resolved.scopes
	}
	return []map[string]string{fallback}
}

func anyCapabilityScopeMatches(ctx context.Context, tool string, fallback map[string]string, constraint map[string]any) bool {
	for _, scope := range capabilityInvocationScopes(ctx, tool, fallback) {
		if gatewayResourceScopeMatches(constraint, scope) {
			return true
		}
	}
	return false
}

func (s *Service) evaluateCapabilityScopePolicies(ctx context.Context, tool domainaigateway.ToolCapability, policies []domainaigateway.AccessPolicy, skillID string, fallback map[string]string) (gatewayRiskDecision, error) {
	decision := gatewayRiskDecision{}
	for _, scope := range capabilityInvocationScopes(ctx, tool.Name, fallback) {
		allowed, next, reason := toolAllowedByAccessPoliciesForInvocationWithSkills(tool, policies, skillID, scope, s.gatewaySkills())
		if !allowed {
			return decision, fmt.Errorf("%w: AI Gateway access policy rejected %s: %s", apperrors.ErrAccessDenied, tool.Name, reason)
		}
		decision = mergeGatewayRiskDecision(decision, next)
	}
	return decision, nil
}

// Policy may sanitize data, but cannot retarget an already authorized call.
func (s *Service) preserveCapabilityScope(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, before, after map[string]any) error {
	if reflect.DeepEqual(before, after) {
		return nil
	}
	resolved, err := s.resolveCapabilityScopeContext(ctx, principal, tool, after)
	if err != nil {
		return err
	}
	oldScopes := capabilityInvocationScopes(ctx, tool.Name, capabilityGatewayScope(tool, before))
	newScopes := capabilityInvocationScopes(resolved, tool.Name, capabilityGatewayScope(tool, after))
	if !reflect.DeepEqual(oldScopes, newScopes) {
		return fmt.Errorf("%w: input policy cannot change authorized resource scopes", apperrors.ErrAccessDenied)
	}
	return nil
}

func resolvedCapabilityAuditScope(ctx context.Context, tool domainaigateway.ToolCapability, input, relatedIDs map[string]any) map[string]any {
	scope := gatewayAuditScope(input, relatedIDs)
	resolved, _ := ctx.Value(capabilityScopeContextKey{}).(capabilityScopeContext)
	if resolved.tool == tool.Name && len(resolved.scopes) > 0 {
		scope["invocationScopes"] = resolved.scopes
	}
	return scope
}

func matchesApprovedCapabilityScopes(ctx context.Context, toolName string, scope map[string]any) bool {
	resolved, _ := ctx.Value(capabilityScopeContextKey{}).(capabilityScopeContext)
	actual := resolved.scopes
	if resolved.tool != toolName {
		actual = nil
	}
	stored, exists := scope["invocationScopes"]
	if !exists {
		return len(actual) == 0
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return false
	}
	var approved []map[string]string
	if json.Unmarshal(encoded, &approved) != nil {
		return false
	}
	normalize := func(scopes []map[string]string) []string {
		values := make([]string, 0, len(scopes))
		for _, scope := range scopes {
			encoded, _ := json.Marshal(scope)
			values = append(values, string(encoded))
		}
		slices.Sort(values)
		return slices.Compact(values)
	}
	return slices.Equal(normalize(approved), normalize(actual))
}
