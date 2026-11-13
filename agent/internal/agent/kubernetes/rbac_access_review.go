package kubernetes

import (
	"context"
	"fmt"
	"time"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CustomResourceActions uses SelfSubjectAccessReview so kubeconfig and in-cluster
// credentials are checked identically, without guessing a ServiceAccount name.
func (c *Client) CustomResourceActions(ctx context.Context, definition domainresource.CRDResourceDefinition, namespace, name string) ([]string, error) {
	if _, err := customResourceGVR(definition); err != nil {
		return nil, err
	}
	if !definition.Namespaced && namespace != "" {
		return nil, fmt.Errorf("cluster-scoped resource must not have a namespace")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	actions := make([]string, 0, 5)
	for _, check := range []struct{ action, verb string }{{"list", "list"}, {"view", "get"}, {"create", "create"}, {"update", "update"}, {"delete", "delete"}} {
		resourceName := name
		if check.verb == "create" || check.verb == "list" {
			resourceName = ""
		}
		review, err := c.typed.AuthorizationV1().SelfSubjectAccessReviews().Create(queryCtx, &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Group: definition.Group, Resource: definition.Resource, Namespace: namespace, Name: resourceName, Verb: check.verb}},
		}, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		if review.Status.Allowed && !review.Status.Denied && review.Status.EvaluationError == "" {
			actions = append(actions, check.action)
		}
	}
	return actions, nil
}

func (c *Client) ReviewSubjectAccess(ctx context.Context, input domainresource.SubjectAccessReviewInput) (domainresource.SubjectAccessReviewResult, error) {
	user, groups := accessReviewIdentity(input.Subject)
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := domainresource.SubjectAccessReviewResult{Subject: input.Subject, Decisions: make([]domainresource.AccessReviewDecision, 0, len(input.Checks))}
	for _, check := range input.Checks {
		review, err := c.typed.AuthorizationV1().SubjectAccessReviews().Create(queryCtx, &authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				User: user, Groups: groups,
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Verb: check.Verb, Group: check.Group, Resource: check.Resource,
					Namespace: check.Namespace, Name: check.Name,
				},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			return domainresource.SubjectAccessReviewResult{}, err
		}
		result.Decisions = append(result.Decisions, domainresource.AccessReviewDecision{
			Check: check, Allowed: review.Status.Allowed, Denied: review.Status.Denied,
			Reason: review.Status.Reason, EvaluationError: review.Status.EvaluationError,
		})
	}
	return result, nil
}

func accessReviewIdentity(subject domainresource.AccessReviewSubject) (string, []string) {
	switch subject.Kind {
	case "Group":
		return "", []string{subject.Name}
	case "ServiceAccount":
		return "system:serviceaccount:" + subject.Namespace + ":" + subject.Name, []string{
			"system:serviceaccounts", "system:serviceaccounts:" + subject.Namespace, "system:authenticated",
		}
	default:
		return subject.Name, nil
	}
}
