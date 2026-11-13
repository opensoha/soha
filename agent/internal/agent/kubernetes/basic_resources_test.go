package kubernetes

import (
	"context"
	"encoding/base64"
	"errors"
	contractresource "github.com/opensoha/soha-contracts/resource"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func TestConfigurationDataEncodingAndImmutableProtection(t *testing.T) {
	ctx := context.Background()
	typed := fake.NewClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "team"}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "team"}, Type: corev1.SecretTypeOpaque})
	c := &Client{typed: typed}
	binary := base64.StdEncoding.EncodeToString([]byte{0, 255, 1})
	cm, err := c.UpdateConfigMapData(ctx, "team", "cfg", map[string]string{"text": "你好"}, map[string]string{"binary": binary})
	if err != nil || cm.Data["text"] != "你好" || cm.BinaryData["binary"] != binary {
		t.Fatalf("ConfigMap roundtrip: %+v %v", cm, err)
	}
	secret, err := c.UpdateSecretData(ctx, "team", "auth", map[string]string{"text": "你好"})
	if err != nil || secret.Data["text"] != base64.StdEncoding.EncodeToString([]byte("你好")) {
		t.Fatalf("Secret encoding: %+v %v", secret, err)
	}
	stored, _ := typed.CoreV1().Secrets("team").Get(ctx, "auth", metav1.GetOptions{})
	if string(stored.Data["text"]) != "你好" || len(stored.StringData) != 0 {
		t.Fatal("Secret plaintext input was double encoded")
	}
	typed.ClearActions()
	_, err = c.UpdateConfigMapData(ctx, "team", "cfg", nil, map[string]string{"binary": "not-base64!"})
	if !apierrors.IsBadRequest(err) {
		t.Fatalf("invalid encoding: %v", err)
	}
	assertNoMutation(t, typed.Actions())
	immutable := true
	stored.Immutable = &immutable
	if _, err = typed.CoreV1().Secrets("team").Update(ctx, stored, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	typed.ClearActions()
	_, err = c.UpdateSecretData(ctx, "team", "auth", nil)
	if !apierrors.IsBadRequest(err) {
		t.Fatalf("immutable: %v", err)
	}
	assertNoMutation(t, typed.Actions())
}

func assertNoMutation(t *testing.T, actions []k8stesting.Action) {
	t.Helper()
	for _, a := range actions {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			t.Fatalf("unexpected mutation %s %s", a.GetVerb(), a.GetResource())
		}
	}
}

func TestBasicResourceMutationsRejectExternalOwnerBeforeWriting(t *testing.T) {
	ctx := context.Background()
	meta := metav1.ObjectMeta{Name: "app", Namespace: "team", UID: "uid-app", Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "external"}}
	nodeMeta := meta
	nodeMeta.Namespace = ""
	for _, test := range []struct {
		name string
		run  func(*Client) error
	}{
		{"ConfigMap", func(c *Client) error { _, err := c.UpdateConfigMapData(ctx, "team", "app", nil, nil); return err }},
		{"Secret", func(c *Client) error { _, err := c.UpdateSecretData(ctx, "team", "app", nil); return err }},
		{"Namespace update", func(c *Client) error {
			_, err := c.UpdateNamespace(ctx, "app", domainresource.NamespaceUpsertInput{})
			return err
		}},
		{"Namespace delete", func(c *Client) error { return c.DeleteNamespace(ctx, "app") }},
		{"Node update", func(c *Client) error {
			_, err := c.UpdateNode(ctx, "app", domainresource.NodeUpdateInput{})
			return err
		}},
		{"Node cordon", func(c *Client) error { return c.SetNodeUnschedulable(ctx, "app", true) }},
		{"Node drain", func(c *Client) error { return c.DrainNode(ctx, "app", domainresource.NodeDrainInput{}) }},
		{"CronJob", func(c *Client) error { _, err := c.SetCronJobSuspend(ctx, "team", "app", true); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			typed := fake.NewClientset(&corev1.ConfigMap{ObjectMeta: meta}, &corev1.Secret{ObjectMeta: meta}, &corev1.Namespace{ObjectMeta: nodeMeta}, &corev1.Node{ObjectMeta: nodeMeta}, &batchv1.CronJob{ObjectMeta: meta})
			err := test.run(&Client{typed: typed})
			if err == nil || !strings.Contains(err.Error(), "external delivery owner") {
				t.Fatalf("owner rejection: %v", err)
			}
			assertNoMutation(t, typed.Actions())
		})
	}
}

func TestNamespaceLifecycleKeepsIdentityAndDeleteUID(t *testing.T) {
	ctx := context.Background()
	typed := fake.NewClientset()
	c := &Client{typed: typed}
	view, err := c.CreateNamespace(ctx, domainresource.NamespaceUpsertInput{Name: "  team  ", Labels: map[string]string{"env": "test"}})
	if err != nil || view.Name != "team" {
		t.Fatalf("create: %+v %v", view, err)
	}
	ns, _ := typed.CoreV1().Namespaces().Get(ctx, "team", metav1.GetOptions{})
	ns.UID = "team-uid"
	ns.ResourceVersion = "7"
	_, _ = typed.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
	typed.ClearActions()
	_, err = c.UpdateNamespace(ctx, "team", domainresource.NamespaceUpsertInput{Name: "other"})
	if !apierrors.IsBadRequest(err) {
		t.Fatalf("rename: %v", err)
	}
	assertNoMutation(t, typed.Actions())
	if _, err = c.UpdateNamespace(ctx, "team", domainresource.NamespaceUpsertInput{Labels: map[string]string{"env": "prod"}}); err != nil {
		t.Fatal(err)
	}
	if err = c.DeleteNamespace(ctx, "team"); err != nil {
		t.Fatal(err)
	}
	calls := typed.Actions()
	last, ok := calls[len(calls)-1].(k8stesting.DeleteAction)
	if !ok {
		t.Fatal("last action is not a delete")
	}
	if pre := last.GetDeleteOptions().Preconditions; pre == nil || pre.UID == nil || *pre.UID != "team-uid" || pre.ResourceVersion == nil || *pre.ResourceVersion != "7" {
		t.Fatal("delete lacks observed UID precondition")
	}
}

func TestCordonCronJobSuspendAndDrainCancellation(t *testing.T) {
	ctx := context.Background()
	typed := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}, &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "job", Namespace: "team"}})
	c := &Client{typed: typed}
	for _, value := range []bool{true, false} {
		if err := c.SetNodeUnschedulable(ctx, "node", value); err != nil {
			t.Fatal(err)
		}
		node, _ := typed.CoreV1().Nodes().Get(ctx, "node", metav1.GetOptions{})
		if node.Spec.Unschedulable != value {
			t.Fatal("cordon value not saved")
		}
		job, err := c.SetCronJobSuspend(ctx, "team", "job", value)
		if err != nil || job.Suspend != value {
			t.Fatalf("suspend: %+v %v", job, err)
		}
	}
	if err := c.DrainNode(ctx, "node", domainresource.NodeDrainInput{TimeoutSeconds: 1}); !apierrors.IsBadRequest(err) {
		t.Fatalf("timeout range: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	realClient, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	err = (&Client{typed: realClient}).DrainNode(deadline, "node", domainresource.NodeDrainInput{TimeoutSeconds: 30})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain cancellation: %v", err)
	}
}

func TestConfigurationReferencePathsCoverProjectedAndPullSecrets(t *testing.T) {
	spec := map[string]any{"imagePullSecrets": []any{map[string]any{"name": "auth"}}, "volumes": []any{map[string]any{"secret": map[string]any{"secretName": "auth"}}, map[string]any{"projected": map[string]any{"sources": []any{map[string]any{"configMap": map[string]any{"name": "cfg"}}, map[string]any{"secret": map[string]any{"name": "auth"}}}}}}, "containers": []any{map[string]any{"env": []any{map[string]any{"valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "auth"}}}}, "envFrom": []any{map[string]any{"configMapRef": map[string]any{"name": "cfg"}}}}}}
	if got := configReferencePaths(spec, "auth", false); len(got) != 4 {
		t.Fatalf("secret refs: %v", got)
	}
	if got := configReferencePaths(spec, "cfg", true); len(got) != 2 {
		t.Fatalf("config refs: %v", got)
	}
	if got := configReferencePaths(spec, "other", true); len(got) != 0 {
		t.Fatalf("unrelated refs: %v", got)
	}
}

func TestDrainCordonsBeforeSelectingPods(t *testing.T) {
	typed := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
	if err := (&Client{typed: typed}).DrainNode(context.Background(), "node", domainresource.NodeDrainInput{TimeoutSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	cordon, podList := -1, -1
	for i, a := range typed.Actions() {
		if a.GetResource().Resource == "nodes" && (a.GetVerb() == "patch" || a.GetVerb() == "update") {
			cordon = i
		}
		if a.GetResource().Resource == "pods" && a.GetVerb() == "list" {
			podList = i
		}
	}
	if cordon < 0 || podList <= cordon {
		t.Fatalf("drain did not cordon before selecting pods: %v", typed.Actions())
	}
}

func TestNodeUpdateCannotIntroduceExternalOwnerLabels(t *testing.T) {
	typed := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
	_, err := (&Client{typed: typed}).UpdateNode(context.Background(), "node", domainresource.NodeUpdateInput{Labels: map[string]string{"kustomize.toolkit.fluxcd.io/name": "external", "kustomize.toolkit.fluxcd.io/namespace": "flux-system"}})
	if err == nil {
		t.Fatal("Node update introduced external ownership")
	}
	assertNoMutation(t, typed.Actions())
}

func TestDrainManagedPodsUsesEvictionAndRetainsCordonOnFailure(t *testing.T) {
	for _, outcome := range []string{"success", "forbidden", "pdb-retry", "pdb-cancel", "pdb-blocked"} {
		t.Run(outcome, func(t *testing.T) {
			controller := true
			typed := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", UID: "node-uid", ResourceVersion: "7"}}, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "app", Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "node"}})
			discovery, ok := typed.Discovery().(*fakediscovery.FakeDiscovery)
			if !ok {
				t.Fatalf("expected fake discovery, got %T", typed.Discovery())
			}
			discovery.Resources = []*metav1.APIResourceList{{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "pods/eviction", Kind: "Eviction", Group: "policy", Version: "v1"}}}}
			evictions := 0
			typed.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
				if a.GetSubresource() != "eviction" {
					return false, nil, nil
				}
				evictions++
				if outcome == "forbidden" {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods/eviction"}, "app", errors.New("denied"))
				}
				if outcome == "pdb-blocked" {
					blocked := apierrors.NewTooManyRequests("private provider detail", 1)
					blocked.ErrStatus.Details = &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: policyv1.DisruptionBudgetCause}}}
					return true, nil, blocked
				}
				if outcome == "pdb-cancel" || (outcome == "pdb-retry" && evictions == 1) {
					return true, nil, apierrors.NewTooManyRequests("disruption budget exhausted", 1)
				}
				create, ok := a.(k8stesting.CreateAction)
				if !ok {
					t.Fatalf("expected create action, got %T", a)
				}
				eviction, ok := create.GetObject().(*policyv1.Eviction)
				if !ok {
					t.Fatalf("expected eviction, got %T", create.GetObject())
				}
				err := typed.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "team", "app")
				return true, eviction, err
			})
			ctx := context.Background()
			if outcome == "pdb-cancel" || outcome == "pdb-blocked" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			err := (&Client{typed: typed}).DrainNode(ctx, "node", domainresource.NodeDrainInput{TimeoutSeconds: 30})
			if outcome == "pdb-blocked" {
				var failure *contractresource.NodeDrainError
				if !errors.As(err, &failure) || failure.Code != "node_drain_pdb_blocked" {
					t.Fatalf("lost PDB reason: %v", err)
				}
			}
			wantEvictions := 1
			if outcome == "pdb-retry" {
				wantEvictions = 2
			}
			if (err != nil) != (outcome == "forbidden" || outcome == "pdb-cancel" || outcome == "pdb-blocked") || evictions != wantEvictions {
				t.Fatalf("drain: %v; evictions=%d", err, evictions)
			}
			node, _ := typed.CoreV1().Nodes().Get(context.Background(), "node", metav1.GetOptions{})
			if !node.Spec.Unschedulable {
				t.Fatal("drain lost cordon")
			}
			for _, a := range typed.Actions() {
				if a.GetVerb() == "delete" && a.GetResource().Resource == "pods" {
					t.Fatal("eviction bypassed by Pod delete")
				}
				if a.GetVerb() == "update" && a.GetResource().Resource == "nodes" {
					update, ok := a.(k8stesting.UpdateAction)
					if !ok {
						t.Fatalf("expected update action, got %T", a)
					}
					written, ok := update.GetObject().(*corev1.Node)
					if !ok {
						t.Fatalf("expected node, got %T", update.GetObject())
					}
					if written.ResourceVersion != "7" || written.UID != "node-uid" {
						t.Fatal("cordon lost identity fence")
					}
				}
			}
		})
	}
}

func TestPodYAMLIncludesManifestIdentityAfterTypedDecode(t *testing.T) {
	typed := fake.NewClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sample", Namespace: "demo"}})
	client := &Client{typed: typed}
	view, err := client.GetPodYAML(context.Background(), "demo", "sample")
	if err != nil || !strings.Contains(view.Content, "apiVersion: v1\n") || !strings.Contains(view.Content, "kind: Pod\n") {
		t.Fatalf("Pod YAML cannot be reapplied: %+v %v", view, err)
	}
}
