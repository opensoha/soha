package bootstrap

import (
	"context"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/infrastructure/hostedruntime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestHostedDynamicObserveRuntimeSignalsAllowlistsOwnedPods(t *testing.T) {
	rev := domain.Revision{ID: "rev_" + "0123456789abcdef0123456789abcdef", AppID: "app_" + "fedcba9876543210fedcba9876543210"}
	service := "soha-app-0123456789abcdef0123456789abcdef"
	base := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

	pods := []*corev1.Pod{
		diagnosticPod(rev, service, "running", base, corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(base)}}, ""),
		diagnosticPod(rev, service, "crash", base.Add(time.Second), corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}, ""),
		diagnosticPod(rev, service, "image", base.Add(2*time.Second), corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}, ""),
		diagnosticPod(rev, service, "oom", base.Add(3*time.Second), corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", FinishedAt: metav1.NewTime(base.Add(3 * time.Second))}}, ""),
		diagnosticPod(rev, service, "exit", base.Add(4*time.Second), corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1, FinishedAt: metav1.NewTime(base.Add(4 * time.Second))}}, ""),
		diagnosticPod(rev, service, "creating", base.Add(5*time.Second), corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}, ""),
		diagnosticPod(rev, service, "completed", base.Add(6*time.Second), corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, FinishedAt: metav1.NewTime(base.Add(6 * time.Second))}}, ""),
		diagnosticPod(rev, service, "duplicate", base.Add(500*time.Millisecond), corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(base.Add(500 * time.Millisecond))}}, ""),
	}
	pods = append(pods,
		diagnosticPod(rev, service, "foreign-managed", base.Add(7*time.Second), corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(base)}}, "app.kubernetes.io/managed-by=other"),
		diagnosticPod(rev, service, "foreign-service", base.Add(8*time.Second), corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(base)}}, "serving.knative.dev/service=other"),
		diagnosticPod(rev, service, "sidecar", base.Add(9*time.Second), corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 2}}, "container=queue-proxy"),
	)

	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	runtimeAdapter, err := hostedruntime.New(dynamicClient, "soha-apps")
	if err != nil {
		t.Fatalf("hostedruntime.New: %v", err)
	}
	hosted := &hostedDynamic{
		kube:      k8sfake.NewClientset(podObjects(pods)...),
		runtime:   runtimeAdapter,
		namespace: "soha-apps",
	}

	signals, err := hosted.ObserveRuntimeSignals(context.Background(), rev)
	if err != nil {
		t.Fatalf("ObserveRuntimeSignals: %v", err)
	}
	want := []domain.RuntimeSignal{
		{Code: runtimeSignalRunning, At: base},
		{Code: runtimeSignalCrashLoop, At: base.Add(time.Second)},
		{Code: runtimeSignalImagePull, At: base.Add(2 * time.Second)},
		{Code: runtimeSignalOOM, At: base.Add(3 * time.Second)},
		{Code: runtimeSignalExitError, At: base.Add(4 * time.Second)},
	}
	if len(signals) != len(want) {
		t.Fatalf("signals = %+v, want %+v", signals, want)
	}
	for index := range want {
		if signals[index].Code != want[index].Code || !signals[index].At.Equal(want[index].At) {
			t.Fatalf("signal %d = %+v, want %+v", index, signals[index], want[index])
		}
	}
}

func TestHostedDynamicSampleDataUsageRequiresConfiguration(t *testing.T) {
	if _, err := (&hostedDynamic{}).SampleDataUsage(context.Background(), "app_"+"fedcba9876543210fedcba9876543210"); err == nil {
		t.Fatal("SampleDataUsage without a data provisioner must fail")
	}
}

func diagnosticPod(rev domain.Revision, service, name string, created time.Time, state corev1.ContainerState, marker string) *corev1.Pod {
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "soha-app-hosting",
		"soha.io/revision-id":          rev.ID,
		"serving.knative.dev/service":  service,
	}
	containerName := "app"
	switch marker {
	case "app.kubernetes.io/managed-by=other":
		labels["app.kubernetes.io/managed-by"] = "other"
	case "serving.knative.dev/service=other":
		labels["serving.knative.dev/service"] = "other"
	case "container=queue-proxy":
		containerName = "queue-proxy"
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "soha-apps", Labels: labels, CreationTimestamp: metav1.NewTime(created)},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: containerName, Image: "app"}}},
		Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: containerName, State: state}}},
	}
}

func podObjects(pods []*corev1.Pod) []runtime.Object {
	objects := make([]runtime.Object, len(pods))
	for index, pod := range pods {
		objects[index] = pod
	}
	return objects
}
