package aigateway

import (
	"context"
	"fmt"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type capabilityTaskScopeKey struct{}
type capabilityTaskScope struct {
	constraints []domainaigateway.CapabilityScopeConstraint
	invalid     bool
}

func canonicalCapabilityScopeDimension(dimension string) string {
	for _, alias := range gatewayScopeAliases() {
		for _, name := range alias.aliases {
			if dimension == name {
				return alias.key
			}
		}
	}
	return dimension
}

func validateCapabilityTaskScope(ctx context.Context, tool domainaigateway.ToolCapability, input map[string]any) error {
	bound, _ := ctx.Value(capabilityTaskScopeKey{}).(capabilityTaskScope)
	if bound.invalid {
		return fmt.Errorf("%w: task scope is unavailable", apperrors.ErrAccessDenied)
	}
	for _, scope := range capabilityInvocationScopes(ctx, tool.Name, capabilityGatewayScope(tool, input)) {
		for _, constraint := range bound.constraints {
			if constraint.Value == "" || scope[canonicalCapabilityScopeDimension(constraint.Dimension)] != constraint.Value {
				return fmt.Errorf("%w: invocation exceeds frozen task scope", apperrors.ErrAccessDenied)
			}
		}
	}
	return nil
}

func validateCapabilityScopeConstraints(constraints []domainaigateway.CapabilityScopeConstraint) []domainaigateway.CapabilityPlanIssue {
	seen := map[string]bool{}
	for _, constraint := range constraints {
		name := canonicalCapabilityScopeDimension(constraint.Dimension)
		if seen[name] {
			return []domainaigateway.CapabilityPlanIssue{planIssue("", "duplicate_scope_dimension", "Task scope dimensions must be unique after alias resolution")}
		}
		seen[name] = true
	}
	return nil
}
