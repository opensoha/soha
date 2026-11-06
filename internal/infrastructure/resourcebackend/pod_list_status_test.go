package resourcebackend

import (
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestPodListIncludesInitStatusesAndActualQoS(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := corev1.Pod{Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "migrate"}, {Name: "proxy", RestartPolicy: &always}},
		Containers:     []corev1.Container{{Name: "api", Env: []corev1.EnvVar{{Name: "TOKEN", Value: "must-not-leak"}}}, {Name: "worker"}},
	}, Status: corev1.PodStatus{QOSClass: corev1.PodQOSGuaranteed,
		InitContainerStatuses: []corev1.ContainerStatus{{Name: "proxy", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}, {Name: "migrate", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed"}}}},
		ContainerStatuses:     []corev1.ContainerStatus{{Name: "stale", Ready: true}, {Name: "api", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
	}}
	view := mapPodView(pod)
	if view.QOSClass != "Guaranteed" || view.ReadyContainers != "1/2" {
		t.Fatalf("unexpected summary: %+v", view)
	}
	if len(view.ContainerStatuses) != 4 {
		t.Fatalf("statuses = %+v", view.ContainerStatuses)
	}
	for i, kind := range []string{"init", "sidecar", "container", "container"} {
		if view.ContainerStatuses[i].Kind != kind {
			t.Fatalf("status %d: %+v", i, view.ContainerStatuses[i])
		}
	}
	if v := view.ContainerStatuses[0]; v.ExitCode == nil || *v.ExitCode != 0 || v.State != "terminated" || v.Reason != "Completed" {
		t.Fatalf("completed init = %+v", v)
	}
	if v := view.ContainerStatuses[3]; v.State != "unknown" || v.Ready || v.ExitCode != nil {
		t.Fatalf("missing status = %+v", v)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), `"env"`) || strings.Contains(string(encoded), `"message"`) {
		t.Fatalf("list leaked detail: %s", encoded)
	}
}

func TestPodListKeepsWaitingReasonAndFailedExit(t *testing.T) {
	pod := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}, {Name: "job"}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
		{Name: "api", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
		{Name: "job", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1}}},
	}}}
	view := mapPodView(pod)
	if v := view.ContainerStatuses[0]; v.State != "waiting" || v.Reason != "CrashLoopBackOff" || v.ExitCode != nil {
		t.Fatalf("waiting = %+v", v)
	}
	if v := view.ContainerStatuses[1]; v.State != "terminated" || v.ExitCode == nil || *v.ExitCode != 1 {
		t.Fatalf("failure = %+v", v)
	}
	if view.QOSClass != "" {
		t.Fatal("must not infer QoS from resources")
	}
}
