package resourcebackend

import (
	"errors"
	"github.com/opensoha/soha/internal/platform/apperrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestPodUIDGuardUsesLiveIdentity(t *testing.T) {
	typed := fake.NewSimpleClientset(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "test", UID: "uid-a"}})
	pods := typed.CoreV1().Pods("test")
	if err := checkPodUID(t.Context(), pods, "api-0", "uid-a"); err != nil {
		t.Fatal(err)
	}
	pod, err := pods.Get(t.Context(), "api-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod.UID = "uid-b"
	if _, err := pods.Update(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := checkPodUID(t.Context(), pods, "api-0", "uid-a"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("error = %v", err)
	}
	if got := mapPodView(*pod).UID; got != "uid-b" {
		t.Fatalf("mapped UID = %q", got)
	}
}
