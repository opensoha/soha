package kubernetes

import (
	"context"
	"errors"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestActionAccessSeparatesDeniedUnverifiableAndTargetDependent(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		typed := fake.NewClientset()
		calls := map[string]int{}
		typed.PrependReactor("create", "selfsubjectaccessreviews", func(a ktesting.Action) (bool, runtime.Object, error) {
			if unavailable {
				return true, nil, errors.New("review unavailable")
			}
			create, ok := a.(ktesting.CreateAction)
			if !ok {
				t.Fatalf("expected create action, got %T", a)
			}
			review, ok := create.GetObject().(*authorizationv1.SelfSubjectAccessReview)
			if !ok {
				t.Fatalf("expected access review, got %T", create.GetObject())
			}
			attrs := review.Spec.ResourceAttributes
			if attrs.Namespace != "" || attrs.Name != "" {
				t.Fatal("cluster diagnostic claimed a target scope")
			}
			key := attrs.Group + "/" + attrs.Resource + "/" + attrs.Subresource + "/" + attrs.Verb
			calls[key]++
			review.Status.Allowed = attrs.Resource != "nodes"
			return true, review, nil
		})
		checks := (&Client{typed: typed}).ActionAccess(context.Background())
		for _, check := range checks {
			if check.Configured != "unknown" || check.Implementation != "supported" || check.CheckedAt == "" {
				t.Fatalf("RBAC probe guessed runtime policy: %+v", check)
			}
			switch check.Action {
			case "platform.pods.exec":
				want := "allowed"
				if unavailable {
					want = "unknown"
				}
				if check.KubernetesRBAC != want {
					t.Fatalf("exec access: %+v", check)
				}
			case "platform.nodes.drain":
				want := "denied"
				if unavailable {
					want = "unknown"
				}
				if check.KubernetesRBAC != want {
					t.Fatalf("drain access: %+v", check)
				}
			case "platform.resources.apply", "platform.helm_releases.install", "platform.port_forwards.create":
				if check.KubernetesRBAC != "unknown" || check.ReasonCode != "target_required" {
					t.Fatalf("target-free RBAC claim: %+v", check)
				}
			}
		}
		for key, count := range calls {
			if count != 1 {
				t.Fatalf("duplicate access review %s: %d", key, count)
			}
		}
	}
}
