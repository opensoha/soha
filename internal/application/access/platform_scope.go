package access

import (
	"fmt"
	"slices"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
)

type platformGrantProjection struct {
	grant        domainscopegrant.Record
	namespaces   []string
	selectors    []string
	unrestricted bool
}

type platformScopeAccumulator struct {
	allowedNamespaces map[string]struct{}
	deniedNamespaces  map[string]struct{}
	allowedSelectors  []string
	deniedSelectors   []string
	allowedGroups     []string
	allowedKinds      []string
	matchedActions    []domainaccess.Action
	allowUnrestricted bool
	namespaceActions  bool
}

func evaluatePlatformScopeGrants(
	request domainaccess.Request,
	decision domainaccess.Decision,
	roleMatrix map[string][]domainaccess.Action,
	grants []domainscopegrant.Record,
	envKeyMap map[string]string,
	applicationEnvironments []domaincatalog.ApplicationEnvironment,
) domainaccess.Decision {
	projections := projectPlatformGrants(request, grants, envKeyMap, applicationEnvironments)
	if len(projections) == 0 {
		return denyScopeGrantDecision(decision, "scope grant does not allow this cluster or resource")
	}

	accumulator := newPlatformScopeAccumulator()
	for _, projection := range projections {
		if reason := accumulator.consume(projection, request, roleMatrix); reason != "" {
			return denyScopeGrantDecision(decision, reason)
		}
	}

	if len(accumulator.matchedActions) == 0 {
		return denyScopeGrantDecision(decision, "scope grant does not allow this namespace or action")
	}
	if accumulator.selectorOnlyListCannotBeEnforced(request) {
		return denyScopeGrantDecision(decision, "namespace selector scope requires an explicit namespace for this resource list")
	}

	decision.AllowedActions = intersectActions(decision.AllowedActions, accumulator.matchedActions)
	if isNamespaceDiscovery(request) && !accumulator.namespaceActions {
		decision.AllowedActions = intersectActions(decision.AllowedActions, []domainaccess.Action{
			domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch,
		})
	}
	if len(decision.AllowedActions) == 0 {
		return denyScopeGrantDecision(decision, "scope grant filtered out effective platform actions")
	}
	decision.Allowed = slices.Contains(decision.AllowedActions, request.Action)
	if !decision.Allowed {
		decision.Reason = fmt.Sprintf("action %s filtered out by platform scope grant", request.Action)
		return decision
	}

	if request.Namespace.Namespace != "" {
		accumulator.allowedNamespaces = map[string]struct{}{request.Namespace.Namespace: {}}
		accumulator.allowUnrestricted = false
	}
	scope := &domainaccess.ResourceScope{
		Clusters:                   []string{request.Cluster.ClusterID},
		ExcludedNamespaces:         sortedKeys(accumulator.deniedNamespaces),
		NamespaceSelectors:         uniqueSortedStrings(accumulator.allowedSelectors),
		ExcludedNamespaceSelectors: uniqueSortedStrings(accumulator.deniedSelectors),
		ResourceGroups:             uniqueSortedStrings(accumulator.allowedGroups),
		ResourceKinds:              uniqueSortedStrings(accumulator.allowedKinds),
	}
	if len(scope.NamespaceSelectors) == 1 {
		scope.LabelSelector = scope.NamespaceSelectors[0]
	}
	if !accumulator.allowUnrestricted {
		scope.Namespaces = sortedKeys(accumulator.allowedNamespaces)
	}
	decision.ResourceScope = mergeResourceScopes(decision.ResourceScope, scope)
	return decision
}

func newPlatformScopeAccumulator() *platformScopeAccumulator {
	return &platformScopeAccumulator{
		allowedNamespaces: make(map[string]struct{}),
		deniedNamespaces:  make(map[string]struct{}),
	}
}

func (a *platformScopeAccumulator) consume(projection platformGrantProjection, request domainaccess.Request, roleMatrix map[string][]domainaccess.Action) string {
	matchesNamespace := projectionMatchesNamespace(projection, request.Namespace)
	if normalizedEffect(projection.grant.Effect) == "deny" {
		return a.consumeDeny(projection, request, matchesNamespace)
	}
	if request.Namespace.Namespace != "" && !matchesNamespace {
		return ""
	}
	a.matchedActions = unionActions(a.matchedActions, roleMatrix[projection.grant.Role])
	a.allowUnrestricted = a.allowUnrestricted || projection.unrestricted
	addStrings(a.allowedNamespaces, projection.namespaces)
	a.allowedSelectors = append(a.allowedSelectors, projection.selectors...)
	a.allowedGroups = unionTextValues(a.allowedGroups, projection.grant.ResourceGroups)
	a.allowedKinds = unionTextValues(a.allowedKinds, projection.grant.ResourceKinds)
	a.namespaceActions = a.namespaceActions || grantAllowsNamespaceActions(projection.grant)
	return ""
}

func grantAllowsNamespaceActions(grant domainscopegrant.Record) bool {
	if normalizedScopeType(grant.ScopeType) == domainscopegrant.ScopeTypeLegacy {
		return true
	}
	if len(grant.ResourceGroups) == 0 && len(grant.ResourceKinds) == 0 {
		return true
	}
	return containsFold(grant.ResourceGroups, "inventory") || containsFold(grant.ResourceKinds, "Namespace")
}

func (a *platformScopeAccumulator) consumeDeny(projection platformGrantProjection, request domainaccess.Request, matchesNamespace bool) string {
	if request.Namespace.Namespace != "" {
		selectorCannotBeEvaluated := len(projection.selectors) > 0 && len(request.Namespace.Labels) == 0
		if matchesNamespace || selectorCannotBeEvaluated {
			return "scope grant explicitly denies this namespace or resource"
		}
		return ""
	}
	if projection.unrestricted {
		return "scope grant explicitly denies this cluster resource scope"
	}
	addStrings(a.deniedNamespaces, projection.namespaces)
	a.deniedSelectors = append(a.deniedSelectors, projection.selectors...)
	return ""
}

func (a *platformScopeAccumulator) selectorOnlyListCannotBeEnforced(request domainaccess.Request) bool {
	return request.Namespace.Namespace == "" && !a.allowUnrestricted && len(a.allowedNamespaces) == 0 && len(a.allowedSelectors) > 0 && !isNamespaceDiscovery(request)
}

func projectPlatformGrants(request domainaccess.Request, grants []domainscopegrant.Record, envKeyMap map[string]string, bindings []domaincatalog.ApplicationEnvironment) []platformGrantProjection {
	result := make([]platformGrantProjection, 0)
	for _, grant := range grants {
		switch normalizedScopeType(grant.ScopeType) {
		case domainscopegrant.ScopeTypeDelivery:
			continue
		case domainscopegrant.ScopeTypePlatform:
			if !platformGrantMatchesResource(grant, request) || !matchesText(grant.ClusterIDs, request.Cluster.ClusterID) {
				continue
			}
			selectors := nonEmptyText(grant.NamespaceSelector)
			result = append(result, platformGrantProjection{
				grant: grant, namespaces: uniqueSortedStrings(grant.Namespaces), selectors: selectors,
				unrestricted: len(grant.Namespaces) == 0 && len(selectors) == 0,
			})
		default:
			namespaces, clusterMatched := legacyGrantNamespaces(grant, request.Cluster.ClusterID, envKeyMap, bindings)
			if clusterMatched {
				result = append(result, platformGrantProjection{grant: grant, namespaces: namespaces})
			}
		}
	}
	return result
}

func legacyGrantNamespaces(grant domainscopegrant.Record, clusterID string, envKeyMap map[string]string, bindings []domaincatalog.ApplicationEnvironment) ([]string, bool) {
	namespaces := make(map[string]struct{})
	clusterMatched := false
	for _, binding := range bindings {
		if !bindingMatchesScopeGrant(binding, grant, envKeyMap) {
			continue
		}
		for _, target := range binding.Targets {
			if !target.Enabled || strings.TrimSpace(target.ClusterID) != clusterID {
				continue
			}
			clusterMatched = true
			if namespace := strings.TrimSpace(target.Namespace); namespace != "" {
				namespaces[namespace] = struct{}{}
			}
		}
	}
	return sortedKeys(namespaces), clusterMatched
}

func platformGrantMatchesResource(grant domainscopegrant.Record, request domainaccess.Request) bool {
	if isNamespaceDiscovery(request) {
		if normalizedEffect(grant.Effect) == "deny" && (len(grant.ResourceGroups) > 0 || len(grant.ResourceKinds) > 0) {
			return false
		}
		return true
	}
	if len(grant.ResourceGroups) > 0 && !containsFold(grant.ResourceGroups, request.Resource.Group) {
		return false
	}
	return len(grant.ResourceKinds) == 0 || containsFold(grant.ResourceKinds, request.Resource.Kind)
}

func projectionMatchesNamespace(projection platformGrantProjection, namespace domainaccess.NamespaceAttributes) bool {
	if projection.unrestricted {
		return true
	}
	if containsFold(projection.namespaces, namespace.Namespace) {
		return true
	}
	for _, selector := range projection.selectors {
		if domainaccess.MatchesNamespaceSelector(selector, namespace.Labels) {
			return true
		}
	}
	return false
}

func denyScopeGrantDecision(decision domainaccess.Decision, reason string) domainaccess.Decision {
	decision.Allowed = false
	decision.Reason = reason
	decision.AllowedActions = nil
	return decision
}

func matchesText(filters []string, value string) bool {
	return len(filters) == 0 || containsFold(filters, value) || containsFold(filters, "*")
}

func containsFold(values []string, expected string) bool {
	expected = strings.TrimSpace(expected)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}

func isNamespaceDiscovery(request domainaccess.Request) bool {
	return request.Action == domainaccess.ActionList && strings.EqualFold(request.Resource.Kind, "Namespace")
}

func addStrings(target map[string]struct{}, values []string) {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			target[value] = struct{}{}
		}
	}
}

func nonEmptyText(value string) []string {
	if value = strings.TrimSpace(value); value != "" {
		return []string{value}
	}
	return nil
}

func unionTextValues(left, right []string) []string {
	return uniqueSortedStrings(append(append([]string(nil), left...), right...))
}

func uniqueSortedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}
