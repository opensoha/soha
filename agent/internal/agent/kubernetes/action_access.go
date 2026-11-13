package kubernetes

import (
	"context"
	"strings"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ActionAccess checks this Agent's identity over the cluster, not a Core user's grants.
// Generic resources, custom resources and Helm need a concrete target to prove RBAC.
func (c *Client) ActionAccess(ctx context.Context) []contractresource.AgentActionCheck {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	decisions := map[authorizationv1.ResourceAttributes]string{}
	checks := make([]contractresource.AgentActionCheck, 0, 24)
	for _, action := range contractresource.KubernetesAgentActions() {
		check := contractresource.AgentActionCheck{Action: action, Implementation: "supported", Configured: "unknown", KubernetesRBAC: "unknown", CheckedAt: checkedAt, Scope: "cluster", ReasonCode: "target_required"}
		requirements := actionAccessRequirements(action)
		if len(requirements) > 0 {
			check.KubernetesRBAC = c.checkActionRequirements(ctx, requirements, decisions)
			check.ReasonCode = "self_subject_access_review"
		}
		checks = append(checks, check)
	}
	return checks
}

func (c *Client) checkActionRequirements(ctx context.Context, requirements []authorizationv1.ResourceAttributes, decisions map[authorizationv1.ResourceAttributes]string) string {
	result := "allowed"
	for _, attributes := range requirements {
		decision, found := decisions[attributes]
		if !found {
			decision = c.checkActionRequirement(ctx, attributes)
			decisions[attributes] = decision
		}
		if decision == "denied" {
			return "denied"
		}
		if decision == "unknown" {
			result = "unknown"
		}
	}
	return result
}

func (c *Client) checkActionRequirement(ctx context.Context, attributes authorizationv1.ResourceAttributes) string {
	if c == nil || c.typed == nil || ctx.Err() != nil {
		return "unknown"
	}
	review, err := c.typed.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &attributes}}, metav1.CreateOptions{})
	if err != nil || review.Status.EvaluationError != "" {
		return "unknown"
	}
	if review.Status.Denied || !review.Status.Allowed {
		return "denied"
	}
	return "allowed"
}

func actionAccessRequirements(action string) []authorizationv1.ResourceAttributes {
	if strings.HasPrefix(action, "platform.deployments.") {
		checks := resourceAccessRequirements("apps", "deployments", "get", "update")
		if action == "platform.deployments.rollback" {
			checks = append(checks, resourceAccessRequirements("apps", "replicasets", "list")...)
		}
		return checks
	}
	if strings.HasPrefix(action, "platform.statefulsets.") {
		return resourceAccessRequirements("apps", "statefulsets", "get", "update")
	}
	if action == "platform.daemonsets.restart" {
		return resourceAccessRequirements("apps", "daemonsets", "get", "update")
	}
	switch action {
	case "platform.nodes.drain":
		checks := resourceAccessRequirements("", "nodes", "get", "update")
		checks = append(checks, resourceAccessRequirements("", "pods", "list")...)
		return append(checks, resourceAccessRequirements("", "pods/eviction", "create")...)
	case "platform.pods.exec":
		return resourceAccessRequirements("", "pods/exec", "create")
	case "platform.port_forwards.tunnel":
		return nil
	case "platform.crds.delete":
		return resourceAccessRequirements("apiextensions.k8s.io", "customresourcedefinitions", "get", "delete")
	default:
		return nil
	}
}

func resourceAccessRequirements(group, resource string, verbs ...string) []authorizationv1.ResourceAttributes {
	checks := make([]authorizationv1.ResourceAttributes, 0, len(verbs))
	name, subresource, _ := strings.Cut(resource, "/")
	for _, verb := range verbs {
		checks = append(checks, authorizationv1.ResourceAttributes{Group: group, Resource: name, Subresource: subresource, Verb: verb})
	}
	return checks
}
