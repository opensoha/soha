package informer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/redaction"
)

type fakeBundleManager struct {
	clusterIDs []string
	bundle     *k8sinfra.Bundle
	err        error
}

func (m fakeBundleManager) ClusterIDs() []string { return m.clusterIDs }

func (m fakeBundleManager) Bundle(context.Context, string) (*k8sinfra.Bundle, error) {
	return m.bundle, m.err
}

func TestClusterInformerLifecycleAndAllNamespaceList(t *testing.T) {
	objects := []runtime.Object{
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns-a"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "ns-b"}},
	}
	client := fake.NewSimpleClientset(objects...)
	service := New(fakeBundleManager{bundle: &k8sinfra.Bundle{Typed: client}})

	if err := service.RegisterCluster(t.Context(), "cluster-1"); err != nil {
		t.Fatalf("RegisterCluster() error = %v", err)
	}
	t.Cleanup(service.Stop)
	waitFor(t, 3*time.Second, func() bool { return service.Ready("cluster-1") })

	pods, err := service.ListPods("cluster-1", "")
	if err != nil {
		t.Fatalf("ListPods(all namespaces) error = %v", err)
	}
	if len(pods) != 2 {
		t.Fatalf("ListPods(all namespaces) count = %d, want 2", len(pods))
	}

	service.UnregisterCluster("cluster-1")
	if status := service.Status("cluster-1"); status.Status != "disabled" || status.Ready {
		t.Fatalf("Status() after unregister = %#v", status)
	}
}

func TestResourceReadinessIsIndependent(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, pod := range []*corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "ns-b"}},
	} {
		if err := indexer.Add(pod); err != nil {
			t.Fatalf("index pod: %v", err)
		}
	}
	entry := &clusterCache{
		podLister: corelisters.NewPodLister(indexer),
		stopCh:    make(chan struct{}),
		resources: newResourceStates(),
	}
	entry.resources[resourcePods].transition("ready", "", true)
	entry.resources[resourceEvents].transition("error", "events forbidden", false)
	service := New(fakeBundleManager{})
	service.caches["cluster-1"] = entry

	pods, err := service.ListPods("cluster-1", "")
	if err != nil || len(pods) != 2 {
		t.Fatalf("ListPods() = %d, %v; want 2, nil", len(pods), err)
	}
	if _, err := service.ListEvents("cluster-1", ""); !errors.Is(err, ErrCacheNotReady) {
		t.Fatalf("ListEvents() error = %v, want ErrCacheNotReady", err)
	}
	status := service.Status("cluster-1")
	if status.Status != "partial" || status.Ready {
		t.Fatalf("Status() = %#v, want partial and not fully ready", status)
	}
}

func TestRegistrationErrorIsDiagnosable(t *testing.T) {
	service := New(fakeBundleManager{err: errors.New("invalid kubeconfig token=super-secret")})
	if err := service.RegisterCluster(t.Context(), "cluster-1"); err == nil {
		t.Fatal("RegisterCluster() error = nil, want error")
	}
	status := service.Status("cluster-1")
	if status.Status != "error" || strings.Contains(status.Message, "super-secret") || !strings.Contains(status.Message, "[REDACTED]") {
		t.Fatalf("Status() = %#v", status)
	}
}

func TestWatchErrorDiagnosticIsRedacted(t *testing.T) {
	state := newResourceState()
	state.transition("ready", "", true)
	state.transition("degraded", redaction.Text("watch failed authorization=Bearer super-secret"), true)

	diagnostic := state.diagnostic(resourcePods)
	if diagnostic.Status != "degraded" || !diagnostic.Ready || strings.Contains(diagnostic.Message, "super-secret") {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
