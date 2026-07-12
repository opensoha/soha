package resourcebackend

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestListNamespaceNamesPreservesInputOrderAndLimitsConcurrency(t *testing.T) {
	t.Parallel()

	namespaces := []domainresource.NamespaceView{{Name: "ns-a"}, {Name: "ns-b"}, {Name: "ns-c"}, {Name: "ns-d"}, {Name: "ns-e"}, {Name: "ns-f"}, {Name: "ns-g"}, {Name: "ns-h"}, {Name: "ns-i"}, {Name: "ns-j"}}
	var active, maxActive atomic.Int32
	var mu sync.Mutex
	visited := make(map[string]int)
	items, err := listNamespaceNames(context.Background(), &k8sinfra.Bundle{}, namespaces, time.Second, func(ctx context.Context, _ *k8sinfra.Bundle, namespace string) ([]string, error) {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)
		select {
		case <-time.After(time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		mu.Lock()
		visited[namespace]++
		mu.Unlock()
		return []string{namespace + "-1", namespace + "-2"}, nil
	})
	if err != nil {
		t.Fatalf("listNamespaceNames returned error: %v", err)
	}
	want := []string{"ns-a-1", "ns-a-2", "ns-b-1", "ns-b-2", "ns-c-1", "ns-c-2", "ns-d-1", "ns-d-2", "ns-e-1", "ns-e-2", "ns-f-1", "ns-f-2", "ns-g-1", "ns-g-2", "ns-h-1", "ns-h-2", "ns-i-1", "ns-i-2", "ns-j-1", "ns-j-2"}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items = %#v, want %#v", items, want)
	}
	if got := maxActive.Load(); got > namespaceListParallelism {
		t.Fatalf("max concurrent list calls = %d, want <= %d", got, namespaceListParallelism)
	}
	for _, namespace := range namespaces {
		if got := visited[namespace.Name]; got != 1 {
			t.Fatalf("visited[%s] = %d, want 1", namespace.Name, got)
		}
	}
}

func TestListAcrossNamespacesWithFallbackPrefersNamespaceAll(t *testing.T) {
	t.Parallel()

	discoveryCalled := false
	items, err := listAcrossNamespacesWithFallback(context.Background(), &k8sinfra.Bundle{}, time.Second, func(context.Context) ([]domainresource.NamespaceView, error) {
		discoveryCalled = true
		return []domainresource.NamespaceView{{Name: "ns-a"}}, nil
	}, func(_ context.Context, _ *k8sinfra.Bundle, namespace string) ([]string, error) {
		if namespace != metav1.NamespaceAll {
			t.Fatalf("namespace = %q, want NamespaceAll", namespace)
		}
		return []string{"all-a", "all-b"}, nil
	})
	if err != nil {
		t.Fatalf("listAcrossNamespacesWithFallback returned error: %v", err)
	}
	if discoveryCalled {
		t.Fatal("namespace discovery was called after successful NamespaceAll list")
	}
	if !reflect.DeepEqual(items, []string{"all-a", "all-b"}) {
		t.Fatalf("items = %#v", items)
	}
}

func TestListAcrossNamespacesWithFallbackUsesNamespaceFanout(t *testing.T) {
	t.Parallel()

	allErr := errors.New("namespace-all unsupported")
	items, err := listAcrossNamespacesWithFallback(context.Background(), &k8sinfra.Bundle{}, time.Second, func(context.Context) ([]domainresource.NamespaceView, error) {
		return []domainresource.NamespaceView{{Name: "ns-a"}, {Name: "ns-b"}}, nil
	}, func(_ context.Context, _ *k8sinfra.Bundle, namespace string) ([]string, error) {
		if namespace == metav1.NamespaceAll {
			return nil, allErr
		}
		return []string{namespace}, nil
	})
	if err != nil {
		t.Fatalf("listAcrossNamespacesWithFallback returned error: %v", err)
	}
	if !reflect.DeepEqual(items, []string{"ns-a", "ns-b"}) {
		t.Fatalf("items = %#v", items)
	}
}

func TestListAcrossNamespacesWithFallbackReturnsNamespaceAllErrorWhenDiscoveryFails(t *testing.T) {
	t.Parallel()

	allErr := errors.New("namespace-all failed")
	_, err := listAcrossNamespacesWithFallback(context.Background(), &k8sinfra.Bundle{}, time.Second, func(context.Context) ([]domainresource.NamespaceView, error) {
		return nil, errors.New("namespace discovery failed")
	}, func(_ context.Context, _ *k8sinfra.Bundle, namespace string) ([]string, error) {
		if namespace == metav1.NamespaceAll {
			return nil, allErr
		}
		return []string{namespace}, nil
	})
	if !errors.Is(err, allErr) {
		t.Fatalf("err = %v, want %v", err, allErr)
	}
}
