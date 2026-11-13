package kubernetes

import (
	"context"
	"strings"
	"testing"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNativeActionsRejectGitOpsOwners(t *testing.T) {
	ctx := context.Background()
	for _, action := range []struct {
		name string
		run  func(*Client) error
	}{
		{"restart Deployment", func(c *Client) error { return c.RestartDeployment(ctx, "demo", "app") }},
		{"scale Deployment", func(c *Client) error { return c.ScaleDeployment(ctx, "demo", "app", 2) }},
		{"rollback Deployment", func(c *Client) error { return c.RollbackDeployment(ctx, "demo", "app", "1") }},
		{"image Deployment", func(c *Client) error {
			_, _, err := c.UpdateDeploymentImage(ctx, "demo", "app", "app", "new:v2")
			return err
		}},
		{"restart StatefulSet", func(c *Client) error { return c.RestartStatefulSet(ctx, "demo", "app") }},
		{"scale StatefulSet", func(c *Client) error { return c.ScaleStatefulSet(ctx, "demo", "app", 2) }},
		{"restart DaemonSet", func(c *Client) error { return c.RestartDaemonSet(ctx, "demo", "app") }},
		{"apply Service", func(c *Client) error {
			_, err := c.ApplyResourceYAML(ctx, "demo", "Service", "app", "apiVersion: v1\nkind: Service\nmetadata:\n  name: app\n")
			return err
		}},
		{"preview Service", func(c *Client) error {
			_, err := c.DryRunResourceYAML(ctx, "demo", "Service", "app", "apiVersion: v1\nkind: Service\nmetadata:\n  name: app\n")
			return err
		}},
		{"delete Service", func(c *Client) error { return c.DeleteResource(ctx, "demo", "Service", "app") }},
	} {
		t.Run(action.name, func(t *testing.T) {
			metadata := metav1.ObjectMeta{Name: "app", Namespace: "demo", UID: "uid-app", ResourceVersion: "7", Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "root:apps/Deployment:demo/app"}}
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			client := &Client{typed: fake.NewClientset(&appsv1.Deployment{ObjectMeta: metadata}, &appsv1.StatefulSet{ObjectMeta: metadata}, &appsv1.DaemonSet{ObjectMeta: metadata}), dynamic: dynamicfake.NewSimpleDynamicClient(scheme, &corev1.Service{ObjectMeta: metadata})}
			if err := action.run(client); err == nil || !strings.Contains(err.Error(), "external delivery owner") {
				t.Fatalf("owner rejection: %v", err)
			}
			typedClient, ok := client.typed.(*fake.Clientset)
			if !ok {
				t.Fatal("unexpected typed client")
			}
			dynamicClient, ok := client.dynamic.(*dynamicfake.FakeDynamicClient)
			if !ok {
				t.Fatal("unexpected dynamic client")
			}
			for _, call := range append(typedClient.Actions(), dynamicClient.Actions()...) {
				if call.GetVerb() != "get" {
					t.Fatalf("unexpected write: %s", call.GetVerb())
				}
			}
		})
	}
}

func TestNativeCustomCreateRejectsArgoApplication(t *testing.T) {
	client := &Client{}
	_, err := client.CreateCustomResourceYAML(context.Background(), domainresource.CRDResourceDefinition{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application", Resource: "applications", Namespaced: true}, "demo", "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: app\n")
	if err == nil || !strings.Contains(err.Error(), "frozen GitOps execution") {
		t.Fatalf("create: %v", err)
	}
}
