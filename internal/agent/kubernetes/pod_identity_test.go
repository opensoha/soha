package kubernetes

import (
	"bytes"
	"context"
	"io"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestPodSessionRejectsReplacementBeforeOpeningSubresource(t *testing.T) {
	for _, uid := range []string{"new-uid", ""} {
		for _, operation := range []string{"snapshot", "logs", "terminal"} {
			t.Run(operation+"/"+uid, func(t *testing.T) {
				typed := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "test", UID: types.UID(uid)}})
				client := &Client{typed: typed}
				var err error
				switch operation {
				case "snapshot":
					_, err = client.GetPodLogs(t.Context(), "test", "api-0", "main", 10, 0, false, "old-uid")
				case "logs":
					err = client.StreamPodLogs(t.Context(), "test", "api-0", "main", 10, 0, io.Discard, "old-uid")
				case "terminal":
					err = client.StreamPodTerminal(t.Context(), "test", "api-0", "main", "/bin/sh", bytes.NewReader(nil), io.Discard, io.Discard, nil, "old-uid")
				}
				if !apierrors.IsConflict(err) {
					t.Fatalf("error = %v, want identity conflict", err)
				}
				for _, action := range typed.Actions() {
					if action.GetVerb() != "get" || action.GetSubresource() != "" {
						t.Fatalf("unexpected upstream action: %#v", action)
					}
				}
			})
		}
	}
}

func TestPodUIDLiveReadAndMissingPod(t *testing.T) {
	typed := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "test", UID: "uid-a"}})
	client := &Client{typed: typed}
	if err := client.CheckPodUID(t.Context(), "test", "api-0", "uid-a"); err != nil {
		t.Fatal(err)
	}
	if err := typed.CoreV1().Pods("test").Delete(t.Context(), "api-0", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckPodUID(t.Context(), "test", "api-0", "uid-a"); !apierrors.IsNotFound(err) {
		t.Fatalf("error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.CheckPodUID(ctx, "test", "api-0", ""); err != nil {
		t.Fatal(err)
	}
}

func TestPodViewsRetainUID(t *testing.T) {
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "test", UID: "uid-original"}}
	client := &Client{typed: fake.NewSimpleClientset(&pod)}
	if got := mapPod(pod).UID; got != "uid-original" {
		t.Fatalf("list UID = %q", got)
	}
	detail, err := client.GetPodDetail(t.Context(), "test", "api")
	if err != nil {
		t.Fatal(err)
	}
	if detail.UID != "uid-original" {
		t.Fatalf("detail UID = %q", detail.UID)
	}
}
