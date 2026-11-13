package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

func TestPreflightResourceCreateUsesDiscoveryDryRunWithoutPersisting(t *testing.T) {
	client, actions := newResourceCreationTestClient(t)
	result := client.PreflightResourceCreate(context.Background(), resourceCreateRequest(configMapCreateDocument(0, "platform", "app")))

	if !result.Ready || len(result.Items) != 1 || result.Items[0].DryRun.Status != domainresource.KubernetesResourceDryRunStatusPassed {
		t.Fatalf("PreflightResourceCreate() = %#v, want ready item", result)
	}
	if len(*actions) != 1 {
		t.Fatalf("create actions = %#v, want one dry-run", *actions)
	}
	if options := resourceCreateOptions(true); len(options.DryRun) != 1 || options.DryRun[0] != metav1.DryRunAll {
		t.Fatalf("resourceCreateOptions(true) = %#v, want DryRunAll", options)
	}
}

func TestPreflightResourceCreateRejectsResolvedTargetExpansion(t *testing.T) {
	cases := []struct {
		name     string
		document domainresource.KubernetesResourceAgentCreateDocument
		wantCode string
	}{
		{name: "namespace", document: configMapCreateDocument(0, "other", "app"), wantCode: "namespace_mismatch"},
		{name: "kind", document: configMapCreateDocument(0, "platform", "app"), wantCode: "resource_kind_mismatch"},
		{name: "multiple documents", document: configMapCreateDocument(0, "platform", "app"), wantCode: "multi_document_not_allowed"},
		{name: "cluster", document: configMapCreateDocument(0, "platform", "app"), wantCode: "resource_capability_unsupported"},
		{name: "hash", document: configMapCreateDocument(0, "platform", "app"), wantCode: "resource_kind_mismatch"},
		{name: "scope", document: configMapCreateDocument(0, "platform", "app"), wantCode: "namespace_mismatch"},
	}
	cases[0].document.ResourceRef.Namespace = "platform"
	cases[1].document.Document.Kind = "Secret"
	cases[1].document.ResourceRef.Kind = "Secret"
	cases[2].document.Content += "\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: second\n  namespace: platform\n"
	cases[2].document.Document.ContentHash = contentHash(cases[2].document.Content)
	cases[3].document.ResourceRef.ClusterID = "other-cluster"
	cases[4].document.Document.ContentHash = strings.Repeat("0", 64)
	cases[5].document.Document.ScopeMode = domainresource.KubernetesResourceScopeModeCluster

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, actions := newResourceCreationTestClient(t)
			result := client.PreflightResourceCreate(context.Background(), resourceCreateRequest(tc.document))
			if result.Ready || len(result.Items) != 1 || len(result.Items[0].Errors) != 1 || string(result.Items[0].Errors[0].Code) != tc.wantCode {
				t.Fatalf("PreflightResourceCreate() = %#v, want code %q", result, tc.wantCode)
			}
			if len(*actions) != 0 {
				t.Fatalf("create actions = %#v, want zero", *actions)
			}
		})
	}
}

func TestPreflightResourceCreateStripsClusterScopedNamespace(t *testing.T) {
	client, actions := newResourceCreationTestClient(t)
	content := "apiVersion: v1\nkind: Node\nmetadata:\n  name: worker-1\n  namespace: ignored\n"
	document := domainresource.KubernetesResourceAgentCreateDocument{
		Document:    domainresource.KubernetesResourceDocument{Index: 0, APIVersion: "v1", Kind: "Node", Name: "worker-1", Namespace: "ignored", ScopeMode: domainresource.KubernetesResourceScopeModeCluster, ContentHash: contentHash(content)},
		ResourceRef: domainresource.KubernetesResourceRef{ClusterID: "agent-cluster", APIVersion: "v1", Kind: "Node", Name: "worker-1", ScopeMode: domainresource.KubernetesResourceScopeModeCluster},
		Content:     content,
	}
	result := client.PreflightResourceCreate(context.Background(), resourceCreateRequest(document))

	if !result.Ready || len(result.Items[0].Warnings) != 1 || result.Items[0].Warnings[0].Code != "cluster_scoped_namespace_ignored" {
		t.Fatalf("PreflightResourceCreate() = %#v, want cluster scope warning", result)
	}
	if len(*actions) != 1 || (*actions)[0].namespace != "" {
		t.Fatalf("create actions = %#v, want cluster-scoped request", *actions)
	}
}

func TestCreateResourcesPreflightsAllDocumentsBeforeWriting(t *testing.T) {
	client, actions := newResourceCreationTestClient(t)
	calls := 0
	client.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		calls++
		create := action.(ktesting.CreateAction)
		if create.GetObject().(*unstructured.Unstructured).GetName() == "denied" {
			return true, nil, errors.New("dry-run rejected")
		}
		return true, create.GetObject(), nil
	})
	request := resourceCreateRequest(
		configMapCreateDocument(0, "platform", "allowed"),
		configMapCreateDocument(1, "platform", "denied"),
	)
	result := client.CreateResources(context.Background(), request)

	if result.Status != domainresource.KubernetesResourceCreateBatchStatusFailed || result.Items[0].Status != domainresource.KubernetesResourceCreateResultStatusNotStarted || result.Items[1].Status != domainresource.KubernetesResourceCreateResultStatusFailed {
		t.Fatalf("CreateResources() = %#v, want zero writes after failed preflight", result)
	}
	if calls != 2 || len(*actions) != 0 {
		t.Fatalf("create calls = %d recorded=%#v, want only two preflight calls", calls, *actions)
	}
}

func TestCreateResourcesCreatesOnlyAfterSuccessfulPreflight(t *testing.T) {
	client, actions := newResourceCreationTestClient(t)
	result := client.CreateResources(context.Background(), resourceCreateRequest(configMapCreateDocument(0, "platform", "app")))

	if result.Status != domainresource.KubernetesResourceCreateBatchStatusSucceeded || result.Items[0].Status != domainresource.KubernetesResourceCreateResultStatusSucceeded {
		t.Fatalf("CreateResources() = %#v, want succeeded", result)
	}
	if len(*actions) != 2 {
		t.Fatalf("create actions = %#v, want dry-run then persistent create", *actions)
	}
}

func TestCreateResourcesReturnsOnlyPersistentResponseUID(t *testing.T) {
	for _, uid := range []string{"created-uid", ""} {
		t.Run("uid="+uid, func(t *testing.T) {
			client, _ := newResourceCreationTestClient(t)
			var dryRuns []bool
			dynamicClient, ok := client.dynamic.(*dynamicfake.FakeDynamicClient)
			if !ok {
				t.Fatalf("dynamic client type = %T", client.dynamic)
			}
			dynamicClient.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
				create, ok := action.(ktesting.CreateActionImpl)
				if !ok {
					t.Fatalf("create action type = %T", action)
				}
				if create.GetCreateOptions().FieldManager != resourceEditFieldManager {
					t.Fatalf("create manager = %q; YAML edits would conflict with creation", create.GetCreateOptions().FieldManager)
				}
				dryRun := len(create.GetCreateOptions().DryRun) > 0
				dryRuns = append(dryRuns, dryRun)
				original, ok := create.GetObject().(*unstructured.Unstructured)
				if !ok {
					t.Fatalf("created object type = %T", create.GetObject())
				}
				object := original.DeepCopy()
				object.SetUID(types.UID(uid))
				if dryRun {
					object.SetUID("dry-run-uid")
				}
				return true, object, nil
			})
			document := configMapCreateDocument(0, "platform", "app")
			document.ResourceRef.UID = "caller-uid"
			result := client.CreateResources(context.Background(), resourceCreateRequest(document))
			if result.Status != domainresource.KubernetesResourceCreateBatchStatusSucceeded || result.Items[0].ResourceRef == nil || result.Items[0].ResourceRef.UID != uid {
				t.Fatalf("creation receipt = %#v, want persistent response UID %q", result, uid)
			}
			if len(dryRuns) != 2 || !dryRuns[0] || dryRuns[1] {
				t.Fatalf("dry-run flags = %v, want dry-run then persistent create", dryRuns)
			}
		})
	}
}

func TestCreateResourcesRejectsMissingCreateResponse(t *testing.T) {
	client, _ := newResourceCreationTestClient(t)
	dynamicClient, ok := client.dynamic.(*dynamicfake.FakeDynamicClient)
	if !ok {
		t.Fatalf("dynamic client type = %T", client.dynamic)
	}
	dynamicClient.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		create, ok := action.(ktesting.CreateActionImpl)
		if !ok {
			t.Fatalf("create action type = %T", action)
		}
		if len(create.GetCreateOptions().DryRun) > 0 {
			return true, create.GetObject(), nil
		}
		return true, nil, nil
	})
	result := client.CreateResources(context.Background(), resourceCreateRequest(configMapCreateDocument(0, "platform", "app")))
	if result.Status != domainresource.KubernetesResourceCreateBatchStatusFailed || result.Items[0].ResourceRef != nil {
		t.Fatalf("creation receipt = %#v, want failed without resource evidence", result)
	}
}

type recordedResourceCreate struct {
	namespace string
}

func newResourceCreationTestClient(t *testing.T) (*Client, *[]recordedResourceCreate) {
	t.Helper()
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	actions := make([]recordedResourceCreate, 0)
	dynamicClient.PrependReactor("create", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		create := action.(ktesting.CreateAction)
		object := create.GetObject().(*unstructured.Unstructured).DeepCopy()
		actions = append(actions, recordedResourceCreate{namespace: action.GetNamespace()})
		return true, object, nil
	})
	discoveryClient := &fakediscovery.FakeDiscovery{Fake: &ktesting.Fake{}}
	discoveryClient.Resources = []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
			{Name: "configmaps", SingularName: "configmap", Namespaced: true, Kind: "ConfigMap"},
			{Name: "nodes", SingularName: "node", Namespaced: false, Kind: "Node"},
		},
	}}
	return &Client{cfg: cfgpkg.KubernetesConfig{ID: "agent-cluster"}, dynamic: dynamicClient, discovery: discoveryClient}, &actions
}

func resourceCreateRequest(documents ...domainresource.KubernetesResourceAgentCreateDocument) domainresource.KubernetesResourceAgentCreateRequest {
	return domainresource.KubernetesResourceAgentCreateRequest{OperationID: "operation-1", Documents: documents}
}

func configMapCreateDocument(index int, namespace, name string) domainresource.KubernetesResourceAgentCreateDocument {
	content := strings.Join([]string{"apiVersion: v1", "kind: ConfigMap", "metadata:", "  name: " + name, "  namespace: " + namespace, ""}, "\n")
	return domainresource.KubernetesResourceAgentCreateDocument{
		Document: domainresource.KubernetesResourceDocument{
			Index: index, APIVersion: "v1", Kind: "ConfigMap", Namespace: namespace, Name: name,
			ScopeMode: domainresource.KubernetesResourceScopeModeNamespace, ContentHash: contentHash(content),
		},
		ResourceRef: domainresource.KubernetesResourceRef{
			ClusterID: "agent-cluster", APIVersion: "v1", Kind: "ConfigMap", Namespace: namespace, Name: name,
			ScopeMode: domainresource.KubernetesResourceScopeModeNamespace,
		},
		Content: content,
	}
}

func generatedConfigMapDocument(index int, prefix string) domainresource.KubernetesResourceAgentCreateDocument {
	document := configMapCreateDocument(index, "platform", prefix)
	document.Content = strings.Replace(document.Content, "  name: ", "  generateName: ", 1)
	document.Document.ContentHash = contentHash(document.Content)
	return document
}

func TestResourceCreateGenerateNameBindsPrefixAndReturnsPersistentName(t *testing.T) {
	client, _ := newResourceCreationTestClient(t)
	var dryRuns []bool
	client.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		create := action.(ktesting.CreateActionImpl)
		dryRun := len(create.GetCreateOptions().DryRun) > 0
		dryRuns = append(dryRuns, dryRun)
		object := create.GetObject().(*unstructured.Unstructured).DeepCopy()
		if object.GetName() != "" || object.GetGenerateName() != "app-" {
			t.Fatalf("request identity=%+v", object.Object)
		}
		suffix := "persisted"
		if dryRun {
			suffix = "dryrun"
		}
		object.SetName(object.GetGenerateName() + suffix)
		object.SetUID(types.UID(suffix))
		return true, object, nil
	})
	result := client.CreateResources(context.Background(), resourceCreateRequest(generatedConfigMapDocument(0, "app-")))
	if result.Status != domainresource.KubernetesResourceCreateBatchStatusSucceeded || result.Items[0].ResourceRef == nil {
		t.Fatalf("result=%+v", result)
	}
	ref := result.Items[0].ResourceRef
	if ref.Name != "app-persisted" || ref.UID != "persisted" || result.Items[0].Document.Name != "app-" {
		t.Fatalf("wrong resource evidence: %+v", result.Items[0])
	}
	if len(dryRuns) != 2 || !dryRuns[0] || dryRuns[1] {
		t.Fatalf("dryruns=%v", dryRuns)
	}
}

func TestResourceCreateGenerateNameRejectsIdentityExpansion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*domainresource.KubernetesResourceAgentCreateDocument)
	}{
		{name: "ref prefix", mutate: func(d *domainresource.KubernetesResourceAgentCreateDocument) { d.ResourceRef.Name = "other-" }},
		{name: "document prefix", mutate: func(d *domainresource.KubernetesResourceAgentCreateDocument) { d.Document.Name = "other-" }},
		{name: "content prefix", mutate: func(d *domainresource.KubernetesResourceAgentCreateDocument) {
			d.Content = strings.Replace(d.Content, "app-", "other-", 1)
			d.Document.ContentHash = contentHash(d.Content)
		}},
		{name: "explicit name with prefix envelope", mutate: func(d *domainresource.KubernetesResourceAgentCreateDocument) {
			d.Content += "  name: victim\n"
			d.Document.ContentHash = contentHash(d.Content)
		}},
		{name: "missing both names", mutate: func(d *domainresource.KubernetesResourceAgentCreateDocument) {
			d.Content = strings.Replace(d.Content, "  generateName: app-\n", "", 1)
			d.Document.ContentHash = contentHash(d.Content)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, actions := newResourceCreationTestClient(t)
			document := generatedConfigMapDocument(0, "app-")
			tc.mutate(&document)
			result := client.PreflightResourceCreate(context.Background(), resourceCreateRequest(document))
			if result.Ready || len(result.Items[0].Errors) != 1 || result.Items[0].Errors[0].Code != domainresource.KubernetesResourceCreateErrorCodeResourceKindMismatch || len(*actions) != 0 {
				t.Fatalf("result=%+v actions=%v", result, *actions)
			}
		})
	}
}

func TestResourceCreateExplicitNameTakesPrecedenceOverGenerateName(t *testing.T) {
	client, _ := newResourceCreationTestClient(t)
	document := configMapCreateDocument(0, "platform", "fixed")
	document.Content += "  generateName: ignored-\n"
	document.Document.ContentHash = contentHash(document.Content)
	result := client.CreateResources(context.Background(), resourceCreateRequest(document))
	if result.Status != domainresource.KubernetesResourceCreateBatchStatusSucceeded || result.Items[0].ResourceRef == nil || result.Items[0].ResourceRef.Name != "fixed" {
		t.Fatalf("result=%+v", result)
	}
}

func TestResourceCreateGeneratedPartialBatchRetainsActualFirstName(t *testing.T) {
	client, _ := newResourceCreationTestClient(t)
	persistent := 0
	client.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		create := action.(ktesting.CreateActionImpl)
		object := create.GetObject().(*unstructured.Unstructured).DeepCopy()
		if len(create.GetCreateOptions().DryRun) == 0 {
			persistent++
			if object.GetGenerateName() == "second-" {
				return true, nil, errors.New("create failed")
			}
		}
		object.SetName(object.GetGenerateName() + "generated")
		return true, object, nil
	})
	result := client.CreateResources(context.Background(), resourceCreateRequest(generatedConfigMapDocument(0, "first-"), generatedConfigMapDocument(1, "second-"), generatedConfigMapDocument(2, "third-")))
	if result.Status != domainresource.KubernetesResourceCreateBatchStatusPartial || persistent != 2 || result.Items[0].ResourceRef == nil || result.Items[0].ResourceRef.Name != "first-generated" || result.Items[1].ResourceRef != nil || result.Items[1].Status != domainresource.KubernetesResourceCreateResultStatusFailed || result.Items[2].Status != domainresource.KubernetesResourceCreateResultStatusNotStarted {
		t.Fatalf("result=%+v persistent=%d", result, persistent)
	}
}

func TestResourceCreateDiscoveryValidatesRequestedServedVersion(t *testing.T) {
	for _, tc := range []struct {
		name, apiVersion, kind string
		ready                  bool
	}{
		{name: "served older builtin", apiVersion: "autoscaling/v1", kind: "HorizontalPodAutoscaler", ready: true},
		{name: "unserved builtin", apiVersion: "autoscaling/v9", kind: "HorizontalPodAutoscaler"},
		{name: "served CRD", apiVersion: "example.io/v1beta1", kind: "Widget", ready: true},
		{name: "wrong group", apiVersion: "other.io/v1beta1", kind: "Widget"},
		{name: "unserved CRD", apiVersion: "example.io/v2", kind: "Widget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, actions := newResourceCreationTestClient(t)
			discovery := client.discovery.(*fakediscovery.FakeDiscovery)
			discovery.Resources = append(discovery.Resources,
				&metav1.APIResourceList{GroupVersion: "autoscaling/v1", APIResources: []metav1.APIResource{{Name: "horizontalpodautoscalers", Kind: "HorizontalPodAutoscaler", Namespaced: true}}},
				&metav1.APIResourceList{GroupVersion: "example.io/v1beta1", APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true}}})
			document := configMapCreateDocument(0, "platform", "app")
			document.Content = strings.Replace(document.Content, "apiVersion: v1", "apiVersion: "+tc.apiVersion, 1)
			document.Content = strings.Replace(document.Content, "kind: ConfigMap", "kind: "+tc.kind, 1)
			document.Document.APIVersion = tc.apiVersion
			document.ResourceRef.APIVersion = tc.apiVersion
			document.Document.Kind = tc.kind
			document.ResourceRef.Kind = tc.kind
			document.Document.ContentHash = contentHash(document.Content)
			result := client.PreflightResourceCreate(context.Background(), resourceCreateRequest(document))
			if result.Ready != tc.ready {
				t.Fatalf("result=%+v wantReady=%v", result, tc.ready)
			}
			if !tc.ready && (len(*actions) != 0 || len(result.Items[0].Errors) != 1 || result.Items[0].Errors[0].Code != domainresource.KubernetesResourceCreateErrorCodeResourceCapabilityUnsupported) {
				t.Fatalf("invalid version reached create: %+v actions=%v", result, *actions)
			}
		})
	}
}

func TestResourceCreateKnownKindDiscoveryCannotChangePermissionTarget(t *testing.T) {
	for _, tc := range []struct {
		name, group, resource string
		namespaced            bool
		ready                 bool
	}{
		{name: "known kind different plural", group: "autoscaling", resource: "otherautoscalers", namespaced: true},
		{name: "known kind different scope", group: "autoscaling", resource: "horizontalpodautoscalers", namespaced: false},
		{name: "same spelling in custom group", group: "example.io", resource: "widgets", namespaced: true, ready: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, actions := newResourceCreationTestClient(t)
			apiVersion := tc.group + "/v1"
			discovery := client.discovery.(*fakediscovery.FakeDiscovery)
			discovery.Resources = append(discovery.Resources, &metav1.APIResourceList{GroupVersion: apiVersion, APIResources: []metav1.APIResource{{Name: tc.resource, Kind: "HorizontalPodAutoscaler", Namespaced: tc.namespaced}}})
			document := configMapCreateDocument(0, "platform", "app")
			document.Content = strings.Replace(document.Content, "apiVersion: v1", "apiVersion: "+apiVersion, 1)
			document.Content = strings.Replace(document.Content, "kind: ConfigMap", "kind: HorizontalPodAutoscaler", 1)
			document.Document.APIVersion = apiVersion
			document.ResourceRef.APIVersion = apiVersion
			document.Document.Kind = "HorizontalPodAutoscaler"
			document.ResourceRef.Kind = "HorizontalPodAutoscaler"
			document.Document.ContentHash = contentHash(document.Content)
			result := client.PreflightResourceCreate(context.Background(), resourceCreateRequest(document))
			if result.Ready != tc.ready {
				t.Fatalf("result=%+v", result)
			}
			if !tc.ready && (len(*actions) != 0 || len(result.Items[0].Errors) != 1 || result.Items[0].Errors[0].Code != domainresource.KubernetesResourceCreateErrorCodeResourceKindMismatch) {
				t.Fatalf("changed mapping reached create: %+v actions=%v", result, *actions)
			}
		})
	}
}
