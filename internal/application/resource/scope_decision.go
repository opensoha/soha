package resource

import (
	"context"
	"fmt"
	"slices"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *ResourceCreation) DecideCreateScope(ctx context.Context, principal domainidentity.Principal, clusterID string, input domainresource.ResourceCreateScopeDecisionRequest) (domainresource.ResourceCreateScopeDecision, error) {
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.ResourceGroup = strings.TrimSpace(input.ResourceGroup)
	input.APIVersion = strings.TrimSpace(input.APIVersion)
	input.Kind = strings.TrimSpace(input.Kind)
	if input.Kind == "" || input.ResourceGroup == "" {
		return domainresource.ResourceCreateScopeDecision{}, fmt.Errorf("%w: resourceGroup and kind are required", apperrors.ErrInvalidArgument)
	}
	connection, decision, err := s.decide(ctx, principal, clusterID, input.Namespace, input.ResourceGroup, input.Kind, domainaccess.ActionCreate)
	if err != nil {
		return domainresource.ResourceCreateScopeDecision{}, err
	}
	result := domainresource.ResourceCreateScopeDecision{
		Allowed: decision.Allowed, Reason: decision.Reason,
		AllowedActions: stringifyActions(decision.AllowedActions),
		ClusterIDs:     []string{connection.Summary.ID}, Namespaces: []string{},
		ResourceGroups: []string{input.ResourceGroup}, ResourceKinds: []string{input.Kind},
		Capability: resourceCreateCapability(connection),
	}
	if decision.ResourceScope != nil {
		result.ClusterIDs = append([]string(nil), decision.ResourceScope.Clusters...)
		result.Namespaces = append([]string(nil), decision.ResourceScope.Namespaces...)
		if len(decision.ResourceScope.ResourceGroups) > 0 {
			result.ResourceGroups = append([]string(nil), decision.ResourceScope.ResourceGroups...)
		}
		if len(decision.ResourceScope.ResourceKinds) > 0 {
			result.ResourceKinds = append([]string(nil), decision.ResourceScope.ResourceKinds...)
		}
	}
	if input.Namespace != "" && len(result.Namespaces) == 0 {
		result.Namespaces = []string{input.Namespace}
	}
	return result, nil
}

func resourceCreateCapability(connection domaincluster.Connection) domainresource.ResourceCreateCapability {
	result := domainresource.ResourceCreateCapability{Key: "resource.create", Status: "available", Mode: "direct"}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		result.Mode = "agent"
		if !slices.Contains(connection.Summary.Capabilities, "resource.creation") {
			result.Status = "unsupported"
			result.Reason = "connected agent has not published the resource.creation capability"
		}
	}
	return result
}
