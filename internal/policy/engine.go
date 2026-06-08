package policy

import (
	"context"
	"slices"
	"sort"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
)

type Engine struct{}

func NewEngine() *Engine {
	return &Engine{}
}

func (e *Engine) Evaluate(_ context.Context, request domainaccess.Request, policies []domainaccess.Policy) (domainaccess.Decision, error) {
	ordered := append([]domainaccess.Policy(nil), policies...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Priority > ordered[j].Priority
	})

	for _, policy := range ordered {
		if !matchesAction(policy.Actions, request.Action) {
			continue
		}
		if !matchesPolicy(policy, request) {
			continue
		}
		decision := buildDecision(policy, request)
		if policy.Effect == domainaccess.EffectDeny {
			decision.Allowed = false
			if decision.Reason == "" {
				decision.Reason = "matched deny policy"
			}
			return decision, nil
		}
		decision.Allowed = true
		if decision.Reason == "" {
			decision.Reason = "matched allow policy"
		}
		return decision, nil
	}

	return domainaccess.Decision{}, nil
}

func buildDecision(policy domainaccess.Policy, request domainaccess.Request) domainaccess.Decision {
	scope := &domainaccess.ResourceScope{}
	if len(policy.Clusters.IDs) > 0 {
		scope.Clusters = append(scope.Clusters, policy.Clusters.IDs...)
	} else if request.Cluster.ClusterID != "" {
		scope.Clusters = []string{request.Cluster.ClusterID}
	}
	if len(policy.Namespaces.Names) > 0 {
		scope.Namespaces = append(scope.Namespaces, policy.Namespaces.Names...)
	} else if request.Namespace.Namespace != "" {
		scope.Namespaces = []string{request.Namespace.Namespace}
	}
	return domainaccess.Decision{
		Reason:         policy.Reason,
		AllowedActions: append([]domainaccess.Action(nil), policy.Actions...),
		ResourceScope:  scope,
	}
}

func matchesPolicy(policy domainaccess.Policy, request domainaccess.Request) bool {
	if !matchAny(policy.Subjects.Users, request.Subject.UserID) {
		return false
	}
	if !matchSlice(policy.Subjects.Roles, request.Subject.Roles) {
		return false
	}
	if !matchSlice(policy.Subjects.Teams, request.Subject.Teams) {
		return false
	}
	if !matchSlice(policy.Subjects.Projects, request.Subject.Projects) {
		return false
	}
	if !matchSlice(policy.Subjects.Tags, request.Subject.Tags) {
		return false
	}
	if !matchAny(policy.Clusters.IDs, request.Cluster.ClusterID) {
		return false
	}
	if !matchAny(policy.Clusters.Regions, request.Cluster.Region) {
		return false
	}
	if !matchAny(policy.Clusters.Environments, request.Cluster.Environment) {
		return false
	}
	if !matchLabels(policy.Clusters.Labels, request.Cluster.Labels) {
		return false
	}
	if !matchAny(policy.Namespaces.Names, request.Namespace.Namespace) {
		return false
	}
	if !matchAny(policy.Namespaces.OwnerTeams, request.Namespace.OwnerTeam) {
		return false
	}
	if !matchLabels(policy.Namespaces.Labels, request.Namespace.Labels) {
		return false
	}
	if !matchAny(policy.Resources.Kinds, request.Resource.Kind) {
		return false
	}
	if !matchAny(policy.Resources.Names, request.Resource.Name) {
		return false
	}
	if !matchLabels(policy.Resources.Labels, request.Resource.Labels) {
		return false
	}
	if !matchAny(policy.Conditions.Sources, request.Context.Source) {
		return false
	}
	if !matchAny(policy.Conditions.ApprovalStates, request.Context.ApprovalState) {
		return false
	}
	return true
}

func matchesAction(actions []domainaccess.Action, action domainaccess.Action) bool {
	if len(actions) == 0 {
		return true
	}
	return slices.Contains(actions, action)
}

func matchAny(filters []string, value string) bool {
	if len(filters) == 0 {
		return true
	}
	return slices.Contains(filters, value)
}

func matchSlice(filters []string, values []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, value := range values {
		if slices.Contains(filters, value) {
			return true
		}
	}
	return false
}

func matchLabels(expected map[string][]string, actual map[string]string) bool {
	if len(expected) == 0 {
		return true
	}
	for key, values := range expected {
		actualValue, ok := actual[key]
		if !ok {
			return false
		}
		if len(values) > 0 && !slices.Contains(values, actualValue) {
			return false
		}
	}
	return true
}
