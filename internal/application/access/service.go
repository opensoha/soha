package access

import (
	"context"
	"fmt"
	"slices"
	"strings"

	domainaccess "github.com/soha/soha/internal/domain/access"
	domaincatalog "github.com/soha/soha/internal/domain/catalog"
	domainscopegrant "github.com/soha/soha/internal/domain/scopegrant"
)

type Repository interface {
	ListPolicies(context.Context) ([]domainaccess.Policy, error)
	ListRoleCapabilities(context.Context) (map[string][]domainaccess.Action, error)
}

type ScopeGrantReader interface {
	List(context.Context) ([]domainscopegrant.Record, error)
}

type EnvironmentReader interface {
	ListEnvironments(context.Context) ([]domaincatalog.Environment, error)
	ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error)
}

type Service struct {
	policyEngine domainaccess.PolicyEngine
	repo         Repository
	grants       ScopeGrantReader
	catalog      EnvironmentReader
}

func New(policyEngine domainaccess.PolicyEngine, repo Repository, grants ScopeGrantReader, catalog EnvironmentReader) *Service {
	return &Service{policyEngine: policyEngine, repo: repo, grants: grants, catalog: catalog}
}

func (s *Service) Authorize(ctx context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	roleMatrix, err := s.loadRoleMatrix(ctx)
	if err != nil {
		return domainaccess.Decision{}, err
	}
	policies, err := s.loadPolicies(ctx)
	if err != nil {
		return domainaccess.Decision{}, err
	}

	baseline := roleActions(roleMatrix, request.Principal.Roles)
	if !slices.Contains(baseline, request.Action) {
		return domainaccess.Decision{Allowed: false, Reason: fmt.Sprintf("requested action %s is not granted by RBAC roles", request.Action)}, nil
	}

	decision, err := s.policyEngine.Evaluate(ctx, request, policies)
	if err != nil {
		return domainaccess.Decision{}, err
	}
	if !decision.Allowed && decision.Reason != "" {
		return decision, nil
	}
	if decision.Reason == "" && isClusterlessDeliveryRequest(request) {
		decision = domainaccess.Decision{
			Allowed:        true,
			Reason:         "delivery RBAC baseline matched without cluster-scoped ABAC policy",
			AllowedActions: append([]domainaccess.Action(nil), baseline...),
		}
	}
	if decision.Reason == "" {
		return domainaccess.Decision{Allowed: false, Reason: "no ABAC policy matched request scope"}, nil
	}

	decision.AllowedActions = intersectActions(baseline, decision.AllowedActions)
	if len(decision.AllowedActions) == 0 {
		decision.Allowed = false
		decision.Reason = "ABAC scope matched but no effective actions remain after RBAC intersection"
		return decision, nil
	}
	decision.Allowed = slices.Contains(decision.AllowedActions, request.Action)
	if !decision.Allowed {
		decision.Reason = fmt.Sprintf("action %s filtered out by ABAC policy", request.Action)
	}
	return s.applyScopeGrantConstraint(ctx, request, decision, roleMatrix)
}

func (s *Service) loadPolicies(ctx context.Context) ([]domainaccess.Policy, error) {
	if s.repo == nil {
		return DefaultPolicies(), nil
	}
	policies, err := s.repo.ListPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("load policies: %w", err)
	}
	if len(policies) == 0 {
		return DefaultPolicies(), nil
	}
	return policies, nil
}

func (s *Service) loadRoleMatrix(ctx context.Context) (map[string][]domainaccess.Action, error) {
	if s.repo == nil {
		return RoleMatrix(), nil
	}
	matrix, err := s.repo.ListRoleCapabilities(ctx)
	if err != nil {
		return nil, fmt.Errorf("load role capabilities: %w", err)
	}
	if len(matrix) == 0 {
		return RoleMatrix(), nil
	}
	return matrix, nil
}

func (s *Service) loadScopeGrants(ctx context.Context) ([]domainscopegrant.Record, error) {
	if s.grants == nil {
		return nil, nil
	}
	items, err := s.grants.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("load scope grants: %w", err)
	}
	return items, nil
}

func (s *Service) loadEnvironmentKeyMap(ctx context.Context) (map[string]string, error) {
	if s.catalog == nil {
		return map[string]string{}, nil
	}
	items, err := s.catalog.ListEnvironments(ctx)
	if err != nil {
		return nil, fmt.Errorf("load environments: %w", err)
	}
	result := make(map[string]string, len(items))
	for _, item := range items {
		result[item.ID] = item.Key
	}
	return result, nil
}

func (s *Service) loadApplicationEnvironments(ctx context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	if s.catalog == nil {
		return nil, nil
	}
	items, err := s.catalog.ListApplicationEnvironments(ctx)
	if err != nil {
		return nil, fmt.Errorf("load application environments: %w", err)
	}
	return items, nil
}

func (s *Service) applyScopeGrantConstraint(ctx context.Context, request domainaccess.Request, decision domainaccess.Decision, roleMatrix map[string][]domainaccess.Action) (domainaccess.Decision, error) {
	grants, err := s.loadScopeGrants(ctx)
	if err != nil {
		return domainaccess.Decision{}, err
	}
	if len(grants) == 0 {
		return decision, nil
	}
	subjectGrants, hasScopedGrant := matchedSubjectScopeGrants(grants, request)
	if !hasScopedGrant {
		return decision, nil
	}
	envKeyMap, err := s.loadEnvironmentKeyMap(ctx)
	if err != nil {
		return domainaccess.Decision{}, err
	}

	if request.Delivery.BusinessLineID != "" || request.Delivery.EnvironmentKey != "" || request.Delivery.ApplicationID != "" {
		return s.applyDeliveryScopeGrantConstraint(request, decision, roleMatrix, subjectGrants, envKeyMap), nil
	}
	if !shouldApplyPlatformScope(request) {
		return decision, nil
	}
	applicationEnvironments, err := s.loadApplicationEnvironments(ctx)
	if err != nil {
		return domainaccess.Decision{}, err
	}
	return s.applyPlatformScopeGrantConstraint(request, decision, roleMatrix, subjectGrants, envKeyMap, applicationEnvironments), nil
}

func (s *Service) applyDeliveryScopeGrantConstraint(
	request domainaccess.Request,
	decision domainaccess.Decision,
	roleMatrix map[string][]domainaccess.Action,
	subjectGrants []domainscopegrant.Record,
	envKeyMap map[string]string,
) domainaccess.Decision {
	matchedActions := make([]domainaccess.Action, 0)
	for _, grant := range subjectGrants {
		if !grantMatchesDeliveryScope(grant, request.Delivery, envKeyMap) {
			continue
		}
		matchedActions = unionActions(matchedActions, roleMatrix[grant.Role])
	}

	if len(matchedActions) == 0 {
		decision.Allowed = false
		decision.Reason = "scope grant does not allow this delivery scope"
		decision.AllowedActions = nil
		return decision
	}
	decision.AllowedActions = intersectActions(decision.AllowedActions, matchedActions)
	if len(decision.AllowedActions) == 0 {
		decision.Allowed = false
		decision.Reason = "scope grant filtered out effective actions"
		return decision
	}
	decision.Allowed = slices.Contains(decision.AllowedActions, request.Action)
	if !decision.Allowed {
		decision.Reason = fmt.Sprintf("action %s filtered out by scope grant", request.Action)
	}
	return decision
}

func (s *Service) applyPlatformScopeGrantConstraint(
	request domainaccess.Request,
	decision domainaccess.Decision,
	roleMatrix map[string][]domainaccess.Action,
	subjectGrants []domainscopegrant.Record,
	envKeyMap map[string]string,
	applicationEnvironments []domaincatalog.ApplicationEnvironment,
) domainaccess.Decision {
	clusterNamespaces := make(map[string]map[string]struct{})
	matchedActions := make([]domainaccess.Action, 0)
	for _, binding := range applicationEnvironments {
		for _, grant := range subjectGrants {
			if !bindingMatchesScopeGrant(binding, grant, envKeyMap) {
				continue
			}
			matchedActions = unionActions(matchedActions, roleMatrix[grant.Role])
			for _, target := range binding.Targets {
				if !target.Enabled || target.ClusterID == "" {
					continue
				}
				if _, ok := clusterNamespaces[target.ClusterID]; !ok {
					clusterNamespaces[target.ClusterID] = make(map[string]struct{})
				}
				namespace := strings.TrimSpace(target.Namespace)
				if namespace != "" {
					clusterNamespaces[target.ClusterID][namespace] = struct{}{}
				}
			}
		}
	}

	allowedNamespaces, clusterAllowed := clusterNamespaces[request.Cluster.ClusterID]
	if !clusterAllowed {
		decision.Allowed = false
		decision.Reason = "scope grant does not allow this cluster"
		decision.AllowedActions = nil
		return decision
	}
	if request.Namespace.Namespace != "" {
		if _, ok := allowedNamespaces[request.Namespace.Namespace]; !ok {
			decision.Allowed = false
			decision.Reason = "scope grant does not allow this namespace"
			decision.AllowedActions = nil
			return decision
		}
	}
	decision.AllowedActions = intersectActions(decision.AllowedActions, matchedActions)
	if len(decision.AllowedActions) == 0 {
		decision.Allowed = false
		decision.Reason = "scope grant filtered out effective platform actions"
		return decision
	}
	decision.Allowed = slices.Contains(decision.AllowedActions, request.Action)
	if !decision.Allowed {
		decision.Reason = fmt.Sprintf("action %s filtered out by platform scope grant", request.Action)
		return decision
	}

	scope := &domainaccess.ResourceScope{
		Clusters: []string{request.Cluster.ClusterID},
	}
	if request.Namespace.Namespace != "" {
		scope.Namespaces = []string{request.Namespace.Namespace}
	} else {
		scope.Namespaces = sortedKeys(allowedNamespaces)
	}
	decision.ResourceScope = mergeResourceScopes(decision.ResourceScope, scope)
	return decision
}

func matchedSubjectScopeGrants(grants []domainscopegrant.Record, request domainaccess.Request) ([]domainscopegrant.Record, bool) {
	items := make([]domainscopegrant.Record, 0)
	hasScopedGrant := false
	for _, grant := range grants {
		if !grant.Enabled {
			continue
		}
		if grant.Effect != "" && grant.Effect != "allow" {
			continue
		}
		if grant.SubjectType == "user" && grant.SubjectID != request.Subject.UserID {
			continue
		}
		if grant.SubjectType == "team" && !slices.Contains(request.Subject.Teams, grant.SubjectID) {
			continue
		}
		hasScopedGrant = true
		items = append(items, grant)
	}
	return items, hasScopedGrant
}

func grantMatchesDeliveryScope(grant domainscopegrant.Record, delivery domainaccess.DeliveryAttributes, envKeyMap map[string]string) bool {
	if grant.BusinessLineID != delivery.BusinessLineID {
		return false
	}
	if len(grant.EnvironmentIDs) > 0 {
		allowed := false
		for _, environmentID := range grant.EnvironmentIDs {
			if envKeyMap[environmentID] == delivery.EnvironmentKey || environmentID == delivery.EnvironmentKey {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	if len(grant.ApplicationIDs) > 0 && !slices.Contains(grant.ApplicationIDs, delivery.ApplicationID) {
		return false
	}
	return true
}

func bindingMatchesScopeGrant(binding domaincatalog.ApplicationEnvironment, grant domainscopegrant.Record, envKeyMap map[string]string) bool {
	if grant.BusinessLineID != binding.BusinessLineID {
		return false
	}
	if len(grant.EnvironmentIDs) > 0 {
		allowed := false
		for _, environmentID := range grant.EnvironmentIDs {
			if environmentID == binding.EnvironmentID || environmentID == binding.EnvironmentKey || envKeyMap[environmentID] == binding.EnvironmentKey {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	if len(grant.ApplicationIDs) > 0 && !slices.Contains(grant.ApplicationIDs, binding.ApplicationID) {
		return false
	}
	return true
}

func shouldApplyPlatformScope(request domainaccess.Request) bool {
	if request.Cluster.ClusterID == "" {
		return false
	}
	if request.Resource.Kind == "Cluster" {
		return request.Action == domainaccess.ActionView || request.Action == domainaccess.ActionList
	}
	return true
}

func mergeResourceScopes(left *domainaccess.ResourceScope, right *domainaccess.ResourceScope) *domainaccess.ResourceScope {
	if left == nil {
		return cloneResourceScope(right)
	}
	if right == nil {
		return cloneResourceScope(left)
	}
	return &domainaccess.ResourceScope{
		Clusters:      intersectScopeValues(left.Clusters, right.Clusters),
		Namespaces:    intersectScopeValues(left.Namespaces, right.Namespaces),
		LabelSelector: pickScopeLabelSelector(left.LabelSelector, right.LabelSelector),
	}
}

func cloneResourceScope(scope *domainaccess.ResourceScope) *domainaccess.ResourceScope {
	if scope == nil {
		return nil
	}
	return &domainaccess.ResourceScope{
		Clusters:      append([]string(nil), scope.Clusters...),
		Namespaces:    append([]string(nil), scope.Namespaces...),
		LabelSelector: scope.LabelSelector,
	}
}

func intersectScopeValues(left []string, right []string) []string {
	switch {
	case len(left) == 0:
		return append([]string(nil), right...)
	case len(right) == 0:
		return append([]string(nil), left...)
	}
	allowed := make([]string, 0, len(left))
	for _, item := range left {
		if slices.Contains(right, item) {
			allowed = append(allowed, item)
		}
	}
	return allowed
}

func pickScopeLabelSelector(left string, right string) string {
	if left != "" {
		return left
	}
	return right
}

func sortedKeys(items map[string]struct{}) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for item := range items {
		keys = append(keys, item)
	}
	slices.Sort(keys)
	return keys
}

func isClusterlessDeliveryRequest(request domainaccess.Request) bool {
	if strings.TrimSpace(request.Cluster.ClusterID) != "" {
		return false
	}
	return strings.TrimSpace(request.Delivery.BusinessLineID) != "" ||
		strings.TrimSpace(request.Delivery.EnvironmentKey) != "" ||
		strings.TrimSpace(request.Delivery.ApplicationID) != ""
}

func RoleMatrix() map[string][]domainaccess.Action {
	return map[string][]domainaccess.Action{
		"admin":     allActions(),
		"ops":       {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionLogs, domainaccess.ActionRestart, domainaccess.ActionRollback, domainaccess.ActionScale, domainaccess.ActionTrigger, domainaccess.ActionCreate, domainaccess.ActionUpdate},
		"developer": {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionLogs, domainaccess.ActionRestart, domainaccess.ActionRollback, domainaccess.ActionScale, domainaccess.ActionTrigger},
		"readonly":  {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionLogs},
		"auditor":   {domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch},
	}
}

func DefaultPolicies() []domainaccess.Policy {
	return []domainaccess.Policy{
		{
			ID:       "deny-prod-exec",
			Name:     "Deny Exec In Production",
			Effect:   domainaccess.EffectDeny,
			Priority: 200,
			Clusters: domainaccess.ClusterMatcher{Environments: []string{"production"}},
			Actions:  []domainaccess.Action{domainaccess.ActionExec},
			Reason:   "exec is denied in production by default policy",
		},
		{
			ID:       "admin-all",
			Name:     "Admin Full Access",
			Effect:   domainaccess.EffectAllow,
			Priority: 100,
			Subjects: domainaccess.Matcher{Roles: []string{"admin"}},
			Actions:  allActions(),
			Reason:   "role admin matched",
		},
		{
			ID:       "ops-access",
			Name:     "Ops Cluster Access",
			Effect:   domainaccess.EffectAllow,
			Priority: 80,
			Subjects: domainaccess.Matcher{Roles: []string{"ops"}},
			Actions:  []domainaccess.Action{domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionLogs, domainaccess.ActionRestart, domainaccess.ActionRollback, domainaccess.ActionScale, domainaccess.ActionTrigger, domainaccess.ActionCreate, domainaccess.ActionUpdate},
			Reason:   "role ops matched",
		},
		{
			ID:       "developer-nonprod",
			Name:     "Developer Non Production Access",
			Effect:   domainaccess.EffectAllow,
			Priority: 60,
			Subjects: domainaccess.Matcher{Roles: []string{"developer"}},
			Clusters: domainaccess.ClusterMatcher{Environments: []string{"development", "staging"}},
			Actions:  []domainaccess.Action{domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionLogs, domainaccess.ActionRestart, domainaccess.ActionRollback, domainaccess.ActionScale, domainaccess.ActionTrigger},
			Reason:   "role developer matched non-production scope",
		},
		{
			ID:       "readonly-view",
			Name:     "Readonly View Access",
			Effect:   domainaccess.EffectAllow,
			Priority: 50,
			Subjects: domainaccess.Matcher{Roles: []string{"readonly"}},
			Actions:  []domainaccess.Action{domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch, domainaccess.ActionLogs},
			Reason:   "role readonly matched",
		},
		{
			ID:       "auditor-view",
			Name:     "Auditor Event And Audit Access",
			Effect:   domainaccess.EffectAllow,
			Priority: 50,
			Subjects: domainaccess.Matcher{Roles: []string{"auditor"}},
			Actions:  []domainaccess.Action{domainaccess.ActionView, domainaccess.ActionList, domainaccess.ActionWatch},
			Reason:   "role auditor matched",
		},
	}
}

func roleActions(matrix map[string][]domainaccess.Action, roles []string) []domainaccess.Action {
	set := map[domainaccess.Action]struct{}{}
	for _, role := range roles {
		for _, action := range matrix[role] {
			set[action] = struct{}{}
		}
	}
	actions := make([]domainaccess.Action, 0, len(set))
	for action := range set {
		actions = append(actions, action)
	}
	return actions
}

func intersectActions(left []domainaccess.Action, right []domainaccess.Action) []domainaccess.Action {
	allowed := make([]domainaccess.Action, 0, len(left))
	for _, action := range left {
		if slices.Contains(right, action) {
			allowed = append(allowed, action)
		}
	}
	return allowed
}

func unionActions(left []domainaccess.Action, right []domainaccess.Action) []domainaccess.Action {
	out := append([]domainaccess.Action(nil), left...)
	for _, action := range right {
		if !slices.Contains(out, action) {
			out = append(out, action)
		}
	}
	return out
}

func allActions() []domainaccess.Action {
	return []domainaccess.Action{
		domainaccess.ActionView,
		domainaccess.ActionList,
		domainaccess.ActionWatch,
		domainaccess.ActionCreate,
		domainaccess.ActionUpdate,
		domainaccess.ActionDelete,
		domainaccess.ActionRestart,
		domainaccess.ActionRollback,
		domainaccess.ActionScale,
		domainaccess.ActionTrigger,
		domainaccess.ActionLogs,
		domainaccess.ActionExec,
	}
}
