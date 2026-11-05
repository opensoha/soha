package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	policyv1 "k8s.io/api/policy/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	ktesting "k8s.io/client-go/testing"
)

func graphConfigurationRoot(kind, name string) cr.ResourceRef {
	root := graphTestRoot(kind, name)
	root.APIVersion = graphBuiltinAPIs[kind]
	if graphClusterConfigurationKind(kind) {
		root.Namespace = ""
		root.ScopeMode = cr.ResourceScopeModeCluster
	}
	return root
}

func TestResourceGraphConfigurationRootScopes(t *testing.T) {
	for _, kind := range []string{"ConfigMap", "Secret", "ResourceQuota", "LimitRange", "HorizontalPodAutoscaler", "PodDisruptionBudget", "PriorityClass", "RuntimeClass", "Lease", "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration"} {
		t.Run(kind, func(t *testing.T) {
			root := graphConfigurationRoot(kind, "root")
			if _, err := NormalizeResourceGraphRoot(root); err != nil {
				t.Fatal(err)
			}
			if root.Namespace == "" {
				root.Namespace = "forbidden"
			} else {
				root.Namespace = ""
			}
			if _, err := NormalizeResourceGraphRoot(root); !errors.Is(err, ErrInvalidResourceGraphRoot) {
				t.Fatalf("invalid scope accepted: %v", err)
			}
		})
	}
}

func TestResourceGraphConfigurationUsersBothDirections(t *testing.T) {
	for _, kind := range []string{"ConfigMap", "Secret"} {
		for _, source := range []string{"env", "envFrom", "volume", "projected", "init", "ephemeral", "csi", "imagePull"} {
			if kind == "ConfigMap" && (source == "csi" || source == "imagePull") {
				continue
			}
			t.Run(kind+"/"+source, func(t *testing.T) {
				config := graphTestObject("v1", kind, "config", "config-uid")
				pod := graphTestObject("v1", "Pod", "user", "pod-uid")
				spec := map[string]interface{}{}
				container := map[string]interface{}{}
				refKind, relation := "configMap", "uses-config"
				if kind == "Secret" {
					refKind, relation = "secret", "uses-secret"
				}
				switch source {
				case "env", "init", "ephemeral":
					container["env"] = []interface{}{map[string]interface{}{"valueFrom": map[string]interface{}{refKind + "KeyRef": map[string]interface{}{"name": "config"}}}}
				case "envFrom":
					container["envFrom"] = []interface{}{map[string]interface{}{refKind + "Ref": map[string]interface{}{"name": "config"}}}
				case "volume":
					nameField := "name"
					if kind == "Secret" {
						nameField = "secretName"
					}
					spec["volumes"] = []interface{}{map[string]interface{}{refKind: map[string]interface{}{nameField: "config"}}}
				case "projected":
					spec["volumes"] = []interface{}{map[string]interface{}{"projected": map[string]interface{}{"sources": []interface{}{map[string]interface{}{refKind: map[string]interface{}{"name": "config"}}}}}}
				case "csi":
					spec["volumes"] = []interface{}{map[string]interface{}{"csi": map[string]interface{}{"nodePublishSecretRef": map[string]interface{}{"name": "config"}}}}
				case "imagePull":
					spec["imagePullSecrets"] = []interface{}{map[string]interface{}{"name": "config"}}
				}
				field := "containers"
				switch source {
				case "init":
					field = "initContainers"
				case "ephemeral":
					field = "ephemeralContainers"
				}
				spec[field] = []interface{}{container}
				pod.Object["spec"] = spec
				otherNamespace := pod.DeepCopy()
				otherNamespace.SetName("other-namespace")
				otherNamespace.SetNamespace("other")
				for _, root := range []cr.ResourceRef{graphConfigurationRoot(kind, "config"), graphTestRoot("Pod", "user")} {
					graph, err := BuildResourceGraphFromObjects(root, []*unstructured.Unstructured{config, pod, otherNamespace})
					if err != nil {
						t.Fatal(err)
					}
					if !graphTestEdge(graph, relation, "user", "config") || graphTestHas(graph, "other-namespace") {
						t.Fatalf("reference lost or namespace crossed: %+v", graph)
					}
				}
			})
		}
	}
}

func TestResourceGraphConfigurationClassesAndScope(t *testing.T) {
	for _, test := range []struct{ kind, field, relation string }{{"PriorityClass", "priorityClassName", ResourceGraphRelationUsesPriorityClass}, {"RuntimeClass", "runtimeClassName", ResourceGraphRelationUsesRuntimeClass}} {
		t.Run(test.kind, func(t *testing.T) {
			class := graphTestObject(graphBuiltinAPIs[test.kind], test.kind, "class", "class-uid")
			class.SetNamespace("")
			objects := []*unstructured.Unstructured{class}
			for _, namespace := range []string{"ns", "other"} {
				pod := graphTestObject("v1", "Pod", namespace+"-pod", namespace+"-uid")
				pod.SetNamespace(namespace)
				pod.Object["spec"] = map[string]interface{}{test.field: "class"}
				objects = append(objects, pod)
			}
			custom := graphTestObject("custom.example/v1", "Pod", "custom", "custom-uid")
			custom.Object["spec"] = map[string]interface{}{test.field: "class"}
			objects = append(objects, custom)
			graph, err := BuildResourceGraphFromObjects(graphConfigurationRoot(test.kind, "class"), objects)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"ns-pod", "other-pod"} {
				if !graphTestEdge(graph, test.relation, name, "class") {
					t.Fatalf("class user %s missing: %+v", name, graph)
				}
			}
			if graphTestHas(graph, "custom") {
				t.Fatal("custom Pod kind was interpreted as built-in")
			}
		})
	}
	for _, kind := range []string{"ResourceQuota", "LimitRange"} {
		t.Run(kind, func(t *testing.T) {
			config := graphTestObject("v1", kind, "scope", "scope-uid")
			namespace := graphTestObject("v1", "Namespace", "ns", "namespace-uid")
			namespace.SetNamespace("")
			pod := graphTestObject("v1", "Pod", "unowned", "pod-uid")
			graph, err := BuildResourceGraphFromObjects(graphConfigurationRoot(kind, "scope"), []*unstructured.Unstructured{config, namespace, pod})
			if err != nil {
				t.Fatal(err)
			}
			if len(graph.Edges) != 1 || !graphTestEdge(graph, ResourceGraphRelationAppliesIn, "scope", "ns") || graphTestHas(graph, "unowned") {
				t.Fatalf("scope became ownership: %+v", graph)
			}
		})
	}
}

func TestResourceGraphPDBSelectorSemantics(t *testing.T) {
	pod := graphTestObject("v1", "Pod", "pod", "pod-uid")
	pod.SetLabels(map[string]string{"app": "a"})
	for _, test := range []struct {
		name     string
		selector interface{}
		match    bool
	}{{"nil", nil, false}, {"empty", map[string]interface{}{}, true}, {"expression", map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "app", "operator": "In", "values": []interface{}{"a"}}}}, true}, {"different", map[string]interface{}{"matchLabels": map[string]interface{}{"app": "b"}}, false}, {"invalid", map[string]interface{}{"matchExpressions": []interface{}{map[string]interface{}{"key": "app", "operator": "unsupported"}}}, false}} {
		t.Run(test.name, func(t *testing.T) {
			pdb := graphTestObject("policy/v1", "PodDisruptionBudget", "pdb", "pdb-uid")
			pdb.Object["spec"] = map[string]interface{}{"selector": test.selector}
			graph, err := BuildResourceGraphFromObjects(graphConfigurationRoot("PodDisruptionBudget", "pdb"), []*unstructured.Unstructured{pdb, pod})
			if err != nil {
				t.Fatal(err)
			}
			if got := graphTestEdge(graph, "protects", "pdb", "pod"); got != test.match {
				t.Fatalf("match=%v want %v", got, test.match)
			}
		})
	}
	service := graphTestObject("v1", "Service", "svc", "svc-uid")
	service.Object["spec"] = map[string]interface{}{"selector": map[string]interface{}{}}
	graph, err := BuildResourceGraphFromObjects(graphTestRoot("Service", "svc"), []*unstructured.Unstructured{service, pod})
	if err != nil || len(graph.Edges) != 0 {
		t.Fatalf("empty Service selector matched pods: %v %+v", err, graph)
	}
}

func TestResourceGraphConfigurationLeaseAndWebhook(t *testing.T) {
	lease := graphTestObject("coordination.k8s.io/v1", "Lease", "lease", "lease-uid")
	lease.Object["spec"] = map[string]interface{}{"holderIdentity": "pod"}
	pod := graphTestObject("v1", "Pod", "pod", "pod-uid")
	graph, err := BuildResourceGraphFromObjects(graphConfigurationRoot("Lease", "lease"), []*unstructured.Unstructured{lease, pod})
	if err != nil || len(graph.Edges) != 0 || graphTestHas(graph, "pod") {
		t.Fatalf("holderIdentity was guessed: %v %+v", err, graph)
	}
	graphTestOwner(lease, "v1", "Pod", "pod", "pod-uid", false)
	graph, err = BuildResourceGraphFromObjects(graphConfigurationRoot("Lease", "lease"), []*unstructured.Unstructured{lease, pod})
	if err != nil || !graphTestEdge(graph, "owns", "pod", "lease") {
		t.Fatalf("Lease actual owner lost: %v %+v", err, graph)
	}
	for _, kind := range []string{"MutatingWebhookConfiguration", "ValidatingWebhookConfiguration"} {
		t.Run(kind, func(t *testing.T) {
			webhook := graphTestObject(graphBuiltinAPIs[kind], kind, "webhook", "webhook-uid")
			webhook.SetNamespace("")
			webhook.Object["webhooks"] = []interface{}{
				map[string]interface{}{"clientConfig": map[string]interface{}{"service": map[string]interface{}{"namespace": "other", "name": "admission"}}, "objectSelector": map[string]interface{}{}},
				map[string]interface{}{"clientConfig": map[string]interface{}{"url": "https://admission.other.svc/"}},
				map[string]interface{}{"clientConfig": map[string]interface{}{"service": map[string]interface{}{"name": "no-namespace"}}},
				map[string]interface{}{"clientConfig": map[string]interface{}{"url": "https://example.test", "service": map[string]interface{}{"namespace": "other", "name": "ambiguous"}}},
			}
			service := graphTestObject("v1", "Service", "admission", "service-uid")
			service.SetNamespace("other")
			graph, err := BuildResourceGraphFromObjects(graphConfigurationRoot(kind, "webhook"), []*unstructured.Unstructured{webhook, service, pod})
			if err != nil || len(graph.Edges) != 1 || !graphTestEdge(graph, ResourceGraphRelationWebhookService, "webhook", "admission") || graphTestHas(graph, "pod") {
				t.Fatalf("webhook relation guessed or absent: %v %+v", err, graph)
			}
		})
	}
}

func TestResourceGraphConfigurationCollectorAllRoots(t *testing.T) {
	nsmeta := metav1.ObjectMeta{Name: "root", Namespace: "ns", UID: "root-uid"}
	clmeta := nsmeta
	clmeta.Namespace = ""
	objects := []kruntime.Object{
		&corev1.ConfigMap{ObjectMeta: nsmeta}, &corev1.ResourceQuota{ObjectMeta: nsmeta}, &corev1.LimitRange{ObjectMeta: nsmeta},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: nsmeta}, &policyv1.PodDisruptionBudget{ObjectMeta: nsmeta},
		&schedulingv1.PriorityClass{ObjectMeta: clmeta}, &nodev1.RuntimeClass{ObjectMeta: clmeta}, &coordinationv1.Lease{ObjectMeta: nsmeta},
		&admissionv1.MutatingWebhookConfiguration{ObjectMeta: clmeta}, &admissionv1.ValidatingWebhookConfiguration{ObjectMeta: clmeta},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns", UID: "namespace-uid"}},
	}
	for _, kind := range []string{"ConfigMap", "ResourceQuota", "LimitRange", "HorizontalPodAutoscaler", "PodDisruptionBudget", "PriorityClass", "RuntimeClass", "Lease", "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration"} {
		t.Run(kind, func(t *testing.T) {
			client := fake.NewSimpleClientset(objects...)
			graph, err := BuildKubernetesResourceGraph(context.Background(), client, graphConfigurationRoot(kind, "root"))
			if err != nil || !graphTestHas(graph, "root") {
				t.Fatalf("root unsupported: %v %+v", err, graph)
			}
			for _, action := range client.Actions() {
				if action.GetResource().Resource == "secrets" {
					t.Fatal("typed Secret API was read")
				}
				if action.GetVerb() == "list" {
					list, ok := action.(interface{ GetListOptions() metav1.ListOptions })
					if !ok {
						t.Fatalf("list action has no list options: %+v", action)
					}
					if opts := list.GetListOptions(); opts.Limit != 500 || opts.Continue != "" {
						t.Fatalf("unbounded list: %+v", opts)
					}
					if graphClusterConfigurationKind(kind) && action.GetNamespace() != "" {
						t.Fatal("cluster root fanned out over namespaces")
					}
				}
			}
			if (kind == "ResourceQuota" || kind == "LimitRange") && !graphTestEdge(graph, ResourceGraphRelationAppliesIn, "root", "ns") {
				t.Fatal("namespace scope missing")
			}
		})
	}
}

func graphSecretMetadataClient() *metadatafake.FakeMetadataClient {
	client := metadatafake.NewSimpleMetadataClient(kruntime.NewScheme())
	client.PrependReactor("*", "*", func(action ktesting.Action) (bool, kruntime.Object, error) {
		get, ok := action.(ktesting.GetAction)
		if !ok || action.GetVerb() != "get" || action.GetResource() != graphSecretResource {
			return true, nil, errors.New("metadata source only permits named Secret Get")
		}
		return true, &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{
			Name: get.GetName(), Namespace: action.GetNamespace(), UID: "secret-uid",
			Annotations: map[string]string{"sensitive": "must-not-retain"}, ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "must-not-retain"}},
		}}, nil
	})
	return client
}

func TestResourceGraphSecretMetadataRootAndReferences(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "ns", UID: "pod-uid"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main", EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "config"}}}}}}}}
	for _, root := range []cr.ResourceRef{graphConfigurationRoot("Secret", "config"), graphTestRoot("Pod", "user")} {
		client := fake.NewSimpleClientset(pod)
		client.PrependReactor("*", "secrets", func(ktesting.Action) (bool, kruntime.Object, error) {
			t.Error("typed Secret Get/List is prohibited")
			return true, nil, errors.New("prohibited")
		})
		metadataClient := graphSecretMetadataClient()
		graph, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), client, metadataClient, root)
		if err != nil || !graphTestEdge(graph, "uses-secret", "user", "config") {
			t.Fatalf("metadata graph failed: %v %+v", err, graph)
		}
		for _, node := range graph.Nodes {
			if node.Resource.Kind == "Secret" && (node.Status == "unresolved" || node.Resource.UID != "secret-uid") {
				t.Fatalf("metadata not confirmed: %+v", node)
			}
		}
		if len(metadataClient.Actions()) != 1 {
			t.Fatalf("Secret was not read once by exact identity: %+v", metadataClient.Actions())
		}
		encoded, _ := json.Marshal(graph)
		if strings.Contains(string(encoded), "must-not-retain") {
			t.Fatal("sensitive metadata leaked into graph")
		}
	}
	projection, err := graphObjectProjection(&metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns", Annotations: map[string]string{"sensitive": "x"}, ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "x"}}}}, "v1", "Secret")
	if err != nil || len(projection.Object) != 3 || projection.GetAnnotations() != nil || projection.GetManagedFields() != nil {
		t.Fatalf("metadata projection retained payload: %v %+v", err, projection)
	}
}

func TestResourceGraphSecretMetadataErrorsAndBudget(t *testing.T) {
	for _, sourceError := range []error{apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "config", errors.New("denied")), apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "config"), context.Canceled, context.DeadlineExceeded} {
		metadataClient := graphSecretMetadataClient()
		metadataClient.PrependReactor("get", "secrets", func(ktesting.Action) (bool, kruntime.Object, error) { return true, nil, sourceError })
		_, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), fake.NewSimpleClientset(), metadataClient, graphConfigurationRoot("Secret", "config"))
		if !errors.Is(err, sourceError) {
			t.Fatalf("root error swallowed: %v", err)
		}
	}
	root := graphConfigurationRoot("Secret", "config")
	root.UID = "old-uid"
	_, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), fake.NewSimpleClientset(), graphSecretMetadataClient(), root)
	if !errors.Is(err, ErrInvalidResourceGraphRoot) {
		t.Fatalf("stale root UID accepted: %v", err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "ns", UID: "pod-uid"}}
	for i := 0; i < 20; i++ {
		pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: fmt.Sprintf("secret-%02d", i)})
	}
	metadataClient := graphSecretMetadataClient()
	graph, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), fake.NewSimpleClientset(pod), metadataClient, graphTestRoot("Pod", "user"))
	if err != nil || len(metadataClient.Actions()) != ResourceGraphMetadataTargetLimit || !strings.Contains(strings.Join(graph.Warnings, " "), "Secret reference metadata truncated at 16") {
		t.Fatalf("metadata budget missing: %v %d %+v", err, len(metadataClient.Actions()), graph.Warnings)
	}
	metadataClient = graphSecretMetadataClient()
	metadataClient.PrependReactor("get", "secrets", func(ktesting.Action) (bool, kruntime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", errors.New("sensitive=must-not-leak"))
	})
	graph, err = BuildKubernetesResourceGraphWithMetadata(context.Background(), fake.NewSimpleClientset(pod), metadataClient, graphTestRoot("Pod", "user"))
	warnings := strings.Join(graph.Warnings, " ")
	if err != nil || !strings.Contains(warnings, "Secret reference metadata Forbidden") || strings.Contains(warnings, "sensitive=") {
		t.Fatalf("partial failure hidden or unsafe: %v %s", err, warnings)
	}
}

func TestResourceGraphConfigurationCollectorBoundedTargets(t *testing.T) {
	webhook := &admissionv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "root", UID: "root-uid"}}
	for i := 0; i < 20; i++ {
		webhook.Webhooks = append(webhook.Webhooks, admissionv1.MutatingWebhook{ClientConfig: admissionv1.WebhookClientConfig{Service: &admissionv1.ServiceReference{Namespace: fmt.Sprintf("namespace-%02d", i), Name: "admission"}, CABundle: []byte("must-not-retain")}})
	}
	client := fake.NewSimpleClientset(webhook)
	metadataClient := metadatafake.NewSimpleMetadataClient(kruntime.NewScheme())
	metadataClient.PrependReactor("get", "services", func(action ktesting.Action) (bool, kruntime.Object, error) {
		return true, &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "admission", Namespace: action.GetNamespace(), UID: types.UID(action.GetNamespace())}}, nil
	})
	graph, err := BuildKubernetesResourceGraphWithMetadata(context.Background(), client, metadataClient, graphConfigurationRoot("MutatingWebhookConfiguration", "root"))
	if err != nil || !strings.Contains(strings.Join(graph.Warnings, " "), "configuration reference metadata truncated at 16") {
		t.Fatalf("bounded targets failed: %v %+v", err, graph.Warnings)
	}
	gets := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "list" {
			t.Fatal("webhook root performed namespace/source scans")
		}
	}
	gets = len(metadataClient.Actions())
	if gets != ResourceGraphMetadataTargetLimit || len(graph.Edges) != 20 {
		t.Fatalf("target budget or unresolved declarations lost: gets=%d edges=%d", gets, len(graph.Edges))
	}
	projected, err := graphObjectProjection(webhook, "admissionregistration.k8s.io/v1", "MutatingWebhookConfiguration")
	encoded, _ := json.Marshal(projected)
	if err != nil || strings.Contains(string(encoded), "must-not-retain") {
		t.Fatalf("CA bundle retained: %v", err)
	}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "ns", UID: "deployment-uid"}}
	deployment.Spec.Template.Spec.PriorityClassName = "class"
	class := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "class", UID: "class-uid"}}
	client = fake.NewSimpleClientset(class, deployment)
	client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, kruntime.Object, error) {
		return true, &corev1.PodList{ListMeta: metav1.ListMeta{Continue: "another-page"}}, nil
	})
	graph, err = BuildKubernetesResourceGraph(context.Background(), client, graphConfigurationRoot("PriorityClass", "class"))
	if err != nil || !graphTestEdge(graph, ResourceGraphRelationUsesPriorityClass, "app", "class") || !strings.Contains(strings.Join(graph.Warnings, " "), "Pod source truncated") {
		t.Fatalf("class collection relation or partial lost: %v %+v", err, graph)
	}
}

func TestResourceGraphConfigurationNamespaceMetadata(t *testing.T) {
	quota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "quota", Namespace: "ns", UID: "quota-uid"}}
	for _, state := range []string{"available", "missing-client", "forbidden", "wrong-scope"} {
		t.Run(state, func(t *testing.T) {
			metadataClient := metadatafake.NewSimpleMetadataClient(kruntime.NewScheme())
			metadataClient.PrependReactor("get", "namespaces", func(action ktesting.Action) (bool, kruntime.Object, error) {
				get, ok := action.(ktesting.GetAction)
				if !ok || action.GetNamespace() != "" || get.GetName() != "ns" {
					t.Fatalf("namespace metadata identity was not exact: %+v", action)
				}
				if state == "forbidden" {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "ns", errors.New("denied"))
				}
				namespace := ""
				if state == "wrong-scope" {
					namespace = "invalid"
				}
				return true, &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "ns", Namespace: namespace, UID: "namespace-uid"}}, nil
			})
			client := fake.NewSimpleClientset(quota)
			var graph cr.ResourceGraph
			var err error
			if state == "missing-client" {
				graph, err = BuildKubernetesResourceGraph(context.Background(), client, graphConfigurationRoot("ResourceQuota", "quota"))
			} else {
				graph, err = BuildKubernetesResourceGraphWithMetadata(context.Background(), client, metadataClient, graphConfigurationRoot("ResourceQuota", "quota"))
			}
			if err != nil || !graphTestEdge(graph, ResourceGraphRelationAppliesIn, "quota", "ns") {
				t.Fatalf("scope declaration lost: %v %+v", err, graph)
			}
			for _, node := range graph.Nodes {
				if node.Resource.Kind != "Namespace" {
					continue
				}
				if node.Resource.APIVersion != "v1" || node.Resource.Namespace != "" || node.Resource.ScopeMode != cr.ResourceScopeModeCluster || (node.Status != "unresolved") != (state == "available") {
					t.Fatalf("namespace identity or observation was falsified: %+v", node)
				}
			}
			if state != "available" && len(graph.Warnings) == 0 {
				t.Fatal("missing metadata was silently treated as no relation")
			}
			for _, action := range client.Actions() {
				if action.GetResource().Resource != "resourcequotas" || action.GetVerb() != "get" {
					t.Fatalf("scope root read unrelated/full targets: %+v", action)
				}
			}
		})
	}
}

func TestResourceGraphConfigurationHPAExactTarget(t *testing.T) {
	for _, api := range []string{"apps/v1", "custom.example/v1", ""} {
		t.Run(api, func(t *testing.T) {
			hpa := graphTestObject("autoscaling/v2", "HorizontalPodAutoscaler", "hpa", "hpa-uid")
			hpa.Object["spec"] = map[string]interface{}{"scaleTargetRef": map[string]interface{}{"apiVersion": api, "kind": "Deployment", "name": "app"}}
			deployment := graphTestObject("apps/v1", "Deployment", "app", "app-uid")
			deployment.SetLabels(map[string]string{"same": "label"})
			hpa.SetLabels(deployment.GetLabels())
			graph, err := BuildResourceGraphFromObjects(graphConfigurationRoot("HorizontalPodAutoscaler", "hpa"), []*unstructured.Unstructured{hpa, deployment})
			if err != nil {
				t.Fatal(err)
			}
			if api == "" {
				if len(graph.Edges) != 0 {
					t.Fatal("invalid target guessed from kind/labels")
				}
				return
			}
			if !graphTestEdge(graph, "scales", "hpa", "app") {
				t.Fatal("declared scale target missing")
			}
			for _, node := range graph.Nodes {
				if node.Resource.Name == "app" && (node.Resource.APIVersion != api || (node.Status != "unresolved") != (api == "apps/v1")) {
					t.Fatalf("wrong API-group target confirmed: %+v", node)
				}
			}
		})
	}
}
