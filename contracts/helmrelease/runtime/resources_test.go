package runtime

import (
	"io"
	"testing"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/kube"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	cliresource "k8s.io/cli-runtime/pkg/resource"
)

type deliveryResourceClient struct {
	*kubefake.PrintingKubeClient
	items kube.ResourceList
}

func (c deliveryResourceClient) Build(io.Reader, bool) (kube.ResourceList, error) {
	return c.items, nil
}

func TestHelmResourceAdmissionRejectsUntrackedOrRetainedObjects(t *testing.T) {
	for _, test := range []struct {
		name, namespace, objectName string
		scope                       meta.RESTScope
		keep, duplicate, invalid    bool
	}{
		{name: "namespaced", namespace: "dev", objectName: "app", scope: meta.RESTScopeNamespace},
		{name: "other namespace", namespace: "prod", objectName: "app", scope: meta.RESTScopeNamespace, invalid: true},
		{name: "cluster scope", objectName: "app", scope: meta.RESTScopeRoot, invalid: true},
		{name: "generated name", namespace: "dev", scope: meta.RESTScopeNamespace, invalid: true},
		{name: "retained resource", namespace: "dev", objectName: "app", scope: meta.RESTScopeNamespace, keep: true, invalid: true},
		{name: "duplicate resource", namespace: "dev", objectName: "app", scope: meta.RESTScopeNamespace, duplicate: true, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap"}}
			object.SetName(test.objectName)
			object.SetNamespace(test.namespace)
			if test.keep {
				object.SetAnnotations(map[string]string{"helm.sh/resource-policy": "keep"})
			}
			item := &cliresource.Info{Name: test.objectName, Namespace: test.namespace, Object: object, Mapping: &meta.RESTMapping{Scope: test.scope, GroupVersionKind: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}}}
			items := kube.ResourceList{item}
			if test.duplicate {
				items = append(items, item)
			}
			cfg := &action.Configuration{KubeClient: deliveryResourceClient{&kubefake.PrintingKubeClient{Out: io.Discard}, items}}
			resources, digest, err := helmDeliveryResources(cfg, "dev", "manifest", nil)
			if (err != nil) != test.invalid {
				t.Fatalf("err=%v", err)
			}
			if err == nil && (len(resources) != 1 || len(digest) != 71) {
				t.Fatal("resource identity or digest missing")
			}
		})
	}
}
