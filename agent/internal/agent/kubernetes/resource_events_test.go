package kubernetes

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResourceEventStreamPublishesFilteredPodEvents(t *testing.T) {
	client := fake.NewSimpleClientset()
	stream := newResourceEventStream("cluster-a", client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream.Start(ctx)
	waitForAgentResourceEvents(t, 3*time.Second, stream.Ready)
	events, unsubscribe := stream.Subscribe("team-a", []string{"Pod"})
	defer unsubscribe()
	if _, err := client.CoreV1().Pods("team-a").Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a"}}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	select {
	case event := <-events:
		if event.Type != "added" || event.Resource == nil || event.Resource.Name != "api" || event.Resource.Kind != "Pod" {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pod event")
	}
}

func waitForAgentResourceEvents(t *testing.T, timeout time.Duration, condition func() bool) {
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
