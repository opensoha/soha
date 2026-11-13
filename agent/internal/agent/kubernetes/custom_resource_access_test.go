package kubernetes

import (
	"context"
	"slices"
	"testing"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestCustomResourceAccessUsesActualIdentityAndNamespace(t *testing.T) {
	typed := kubernetesfake.NewClientset()
	typed.PrependReactor("create", "selfsubjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		create, ok := action.(ktesting.CreateAction)
		if !ok {
			t.Fatal("unexpected action")
		}
		review, ok := create.GetObject().(*authorizationv1.SelfSubjectAccessReview)
		if !ok {
			t.Fatal("unexpected review")
		}
		attrs := review.Spec.ResourceAttributes
		if attrs.Group != "example.io" || attrs.Resource != "widgets" || attrs.Namespace != "apps" {
			t.Fatalf("wrong authorization scope: %#v", attrs)
		}
		review.Status.Allowed = attrs.Verb == "get" || attrs.Verb == "list"
		return true, review, nil
	})
	client := &Client{typed: typed}
	allowed, err := client.CustomResourceActions(context.Background(), domainresource.CRDResourceDefinition{Group: "example.io", Resource: "widgets", Kind: "Widget", Version: "v1", Namespaced: true}, "apps", "sample")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(allowed, []string{"list", "view"}) {
		t.Fatalf("writes incorrectly allowed: %v", allowed)
	}
}
