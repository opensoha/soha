package kubernetes

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestWarmListsUseInformerCacheWithoutAPICalls(t *testing.T) {
	typed := fake.NewClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", UID: "pod-uid", Labels: map[string]string{"app": "api"}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "team-b"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a", UID: "deployment-uid"}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "logs", Namespace: "team-a"}, Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 1}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "team-a", Annotations: map[string]string{"kubectl.kubernetes.io/last-applied-configuration": "sensitive"}}, Data: map[string]string{"key": "value"}, BinaryData: map[string][]byte{"file": []byte("data")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "team-a", Annotations: map[string]string{"private": "sensitive"}}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"password": []byte("private")}},
	)
	client := &Client{typed: typed, resourceEvents: newResourceEventStream("cluster-a", typed)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartResourceEvents(ctx)
	waitForAgentResourceEvents(t, 3*time.Second, func() bool {
		for _, spec := range client.resourceEvents.informers {
			if !spec.informer.HasSynced() {
				return false
			}
		}
		return true
	})
	// fake RESTClient is nil: the four Table paths would also fail if cache reads
	// accidentally fell back to a fresh Kubernetes request.
	typed.ClearActions()
	for i := 0; i < 100; i++ {
		pods, err := client.ListPods(ctx, "team-a")
		if err != nil || len(pods) != 1 || pods[0].Name != "api" || pods[0].UID != "pod-uid" {
			t.Fatalf("pods = %#v, %v", pods, err)
		}
		pods[0].Labels["app"] = "mutated"
		deployments, err := client.ListDeployments(ctx, "team-a")
		if err != nil || len(deployments) != 1 || deployments[0].UID != "deployment-uid" || deployments[0].DesiredReplicas != 1 || deployments[0].Available != 1 {
			t.Fatalf("deployments = %#v, %v", deployments, err)
		}
		daemons, err := client.ListDaemonSets(ctx, "team-a")
		if err != nil || len(daemons) != 1 || daemons[0].DesiredNumber != 2 || daemons[0].ReadyNumber != 1 {
			t.Fatalf("daemonsets = %#v, %v", daemons, err)
		}
		configs, err := client.ListConfigMaps(ctx, "team-a")
		if err != nil || len(configs) != 1 || configs[0].DataEntries != 2 {
			t.Fatalf("configmaps = %#v, %v", configs, err)
		}
		secrets, err := client.ListSecrets(ctx, "team-a")
		if err != nil || len(secrets) != 1 || secrets[0].DataEntries != 1 || secrets[0].Type != "Opaque" {
			t.Fatalf("secrets = %#v, %v", secrets, err)
		}
		if _, err := client.ListNodes(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if actions := typed.Actions(); len(actions) != 0 {
		t.Fatalf("warm reads made %d Kubernetes requests", len(actions))
	}
	allPods, ok := cachedResourceItems[corev1.Pod](ctx, client, "Pod", "")
	if !ok || len(allPods) != 2 || allPods[0].Labels["app"] != "api" {
		t.Fatalf("all namespaces/deep copy = %#v, %v", allPods, ok)
	}
	for _, spec := range client.resourceEvents.informers {
		for _, object := range spec.informer.GetStore().List() {
			switch item := object.(type) {
			case *corev1.ConfigMap:
				if len(item.Data)+len(item.BinaryData) != 0 || len(item.Annotations) != 1 {
					t.Fatal("ConfigMap contents retained in cache")
				}
			case *corev1.Secret:
				if len(item.Data)+len(item.StringData) != 0 || len(item.Annotations) != 1 {
					t.Fatal("Secret contents retained in cache")
				}
			}
		}
	}
	// Detail keeps its live GET and returns the actual configuration.
	detail, err := client.GetConfigMapDetail(ctx, "team-a", "config")
	if err != nil || detail.Data["key"] != "value" {
		t.Fatalf("live detail = %#v, %v", detail, err)
	}
	updated, _ := typed.CoreV1().Pods("team-a").Get(ctx, "api", metav1.GetOptions{})
	updated.ResourceVersion = "2"
	updated.Labels["app"] = "updated"
	if _, err := typed.CoreV1().Pods("team-a").Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitForAgentResourceEvents(t, 3*time.Second, func() bool {
		items, ok := cachedResourceItems[corev1.Pod](ctx, client, "Pod", "team-a")
		return ok && len(items) == 1 && items[0].Labels["app"] == "updated"
	})
	for _, spec := range client.resourceEvents.informers {
		if spec.kind == "Pod" {
			spec.healthy.Store(false)
		}
	}
	typed.ClearActions()
	if _, err := client.ListPods(ctx, "team-a"); err != nil || len(typed.Actions()) != 1 || typed.Actions()[0].GetVerb() != "list" {
		t.Fatalf("unhealthy cache did not use live fallback: %v, %#v", err, typed.Actions())
	}
	cancel()
	waitForAgentResourceEvents(t, time.Second, client.resourceEvents.stopped.Load)
	if _, cached := cachedResourceItems[corev1.ConfigMap](context.Background(), client, "ConfigMap", "team-a"); cached {
		t.Fatal("stopped cache was used")
	}
}

func TestUnsyncedOrCanceledListDoesNotUseCache(t *testing.T) {
	typed := fake.NewClientset()
	client := &Client{typed: typed, resourceEvents: newResourceEventStream("cluster-a", typed)}
	if _, err := client.ListPods(context.Background(), "team-a"); err != nil || len(typed.Actions()) != 1 {
		t.Fatalf("unsynced fallback = %v, %#v", err, typed.Actions())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, cached := cachedResourceItems[corev1.Pod](ctx, client, "Pod", "team-a"); cached {
		t.Fatal("canceled request used cache")
	}
}

func TestInformerWatchErrorDisablesListCache(t *testing.T) {
	typed := fake.NewClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a"}})
	watches := make(chan *watch.RaceFreeFakeWatcher, 1)
	var failWatch atomic.Bool
	typed.PrependWatchReactor("pods", func(clienttesting.Action) (bool, watch.Interface, error) {
		if failWatch.Load() {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("watch revoked"))
		}
		w := watch.NewRaceFreeFake()
		select {
		case watches <- w:
		default:
		}
		return true, w, nil
	})
	client := &Client{typed: typed, resourceEvents: newResourceEventStream("cluster-a", typed)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartResourceEvents(ctx)
	var podSpec agentEventInformer
	for _, spec := range client.resourceEvents.informers {
		if spec.kind == "Pod" {
			podSpec = spec
		}
	}
	waitForAgentResourceEvents(t, 3*time.Second, podSpec.informer.HasSynced)
	var w *watch.RaceFreeFakeWatcher
	select {
	case w = <-watches:
	case <-time.After(3 * time.Second):
		t.Fatal("Pod watch did not start")
	}
	failWatch.Store(true)
	w.Stop()
	waitForAgentResourceEvents(t, 3*time.Second, func() bool { return !podSpec.healthy.Load() })
	typed.ClearActions()
	if _, err := client.ListPods(ctx, "team-a"); err != nil {
		t.Fatal(err)
	}
	foundList := false
	for _, action := range typed.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == "pods" {
			foundList = true
		}
	}
	if !foundList {
		t.Fatal("watch error did not cause a live Pod list")
	}
}
