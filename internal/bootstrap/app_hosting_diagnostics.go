package bootstrap

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	runtimeSignalRunning   = "runtime_running"
	runtimeSignalCrashLoop = "runtime_crash_loop"
	runtimeSignalImagePull = "runtime_image_pull_failed"
	runtimeSignalOOM       = "runtime_oom"
	runtimeSignalExitError = "runtime_exit_error"

	hostedRuntimeAppContainer = "app"
	knativeServiceLabel       = "serving.knative.dev/service"

	maxRuntimeSignalPods = 50
	maxRuntimeSignals    = 50
)

var errRuntimeDiagnosticsUnavailable = errors.New("hosted runtime diagnostics are not configured")

// SampleDataUsage reports the measured app-data size for one hosted application.
func (h *hostedDynamic) SampleDataUsage(ctx context.Context, appID string) (int64, error) {
	if h == nil || h.data == nil {
		return 0, errRuntimeDiagnosticsUnavailable
	}
	size, err := h.data.Usage(ctx, appID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, apperrors.ErrNotFound
	}
	if err != nil {
		return 0, errors.New("hosted app data usage is unavailable")
	}
	return size, nil
}

// ObserveRuntimeSignals returns deduplicated, allowlisted container states for a revision.
func (h *hostedDynamic) ObserveRuntimeSignals(ctx context.Context, rev domain.Revision) ([]domain.RuntimeSignal, error) {
	if h == nil || h.kube == nil || h.runtime == nil {
		return nil, errRuntimeDiagnosticsUnavailable
	}
	service, err := h.runtime.ServiceName(rev.ID)
	if err != nil {
		return nil, errors.New("hosted revision identifier is invalid")
	}
	pods, err := h.kube.CoreV1().Pods(h.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=soha-app-hosting," +
			"soha.io/revision-id=" + rev.ID + "," + knativeServiceLabel + "=" + service,
		Limit: maxRuntimeSignalPods,
	})
	if err != nil {
		return nil, errors.New("hosted runtime pod state is unavailable")
	}
	signals := make([]domain.RuntimeSignal, 0, maxRuntimeSignals)
	seen := make(map[string]int)
	for index := range pods.Items {
		pod := &pods.Items[index]
		if index >= maxRuntimeSignalPods || !ownedRuntimePod(pod, rev, service) {
			continue
		}
		for _, signal := range podRuntimeSignals(pod) {
			key := signal.Code + "|" + signal.At.UTC().Truncate(time.Second).Format(time.RFC3339)
			if index, duplicate := seen[key]; duplicate {
				if signal.At.Before(signals[index].At) {
					signals[index] = signal
				}
				continue
			}
			seen[key] = len(signals)
			signals = append(signals, signal)
		}
	}
	sort.SliceStable(signals, func(i, j int) bool {
		if !signals[i].At.Equal(signals[j].At) {
			return signals[i].At.Before(signals[j].At)
		}
		return signals[i].Code < signals[j].Code
	})
	if len(signals) > maxRuntimeSignals {
		signals = signals[:maxRuntimeSignals]
	}
	return signals, nil
}

func ownedRuntimePod(pod *corev1.Pod, rev domain.Revision, service string) bool {
	labels := pod.Labels
	return labels["app.kubernetes.io/managed-by"] == "soha-app-hosting" &&
		labels["soha.io/revision-id"] == rev.ID &&
		labels[knativeServiceLabel] == service
}

func podRuntimeSignals(pod *corev1.Pod) []domain.RuntimeSignal {
	signals := make([]domain.RuntimeSignal, 0, 2)
	for _, container := range pod.Spec.Containers {
		if container.Name != hostedRuntimeAppContainer {
			continue
		}
		signals = append(signals, containerRuntimeSignals(pod, container)...)
	}
	return signals
}

func containerRuntimeSignals(pod *corev1.Pod, container corev1.Container) []domain.RuntimeSignal {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != container.Name {
			continue
		}
		return containerStatusSignals(pod, status)
	}
	return nil
}

func containerStatusSignals(pod *corev1.Pod, status corev1.ContainerStatus) []domain.RuntimeSignal {
	switch {
	case status.State.Running != nil:
		return []domain.RuntimeSignal{{Code: runtimeSignalRunning, At: firstTimestamp(status.State.Running.StartedAt.Time, pod.CreationTimestamp.Time)}}
	case status.State.Waiting != nil:
		code := ""
		switch status.State.Waiting.Reason {
		case "CrashLoopBackOff":
			code = runtimeSignalCrashLoop
		case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
			code = runtimeSignalImagePull
		}
		if code == "" {
			return nil
		}
		return []domain.RuntimeSignal{{Code: code, At: pod.CreationTimestamp.UTC()}}
	case status.State.Terminated != nil:
		terminated := status.State.Terminated
		code := runtimeSignalExitError
		if terminated.Reason == "OOMKilled" {
			code = runtimeSignalOOM
		} else if terminated.ExitCode == 0 {
			return nil
		}
		return []domain.RuntimeSignal{{Code: code, At: firstTimestamp(terminated.FinishedAt.Time, pod.CreationTimestamp.Time)}}
	default:
		return nil
	}
}

func firstTimestamp(primary, fallback time.Time) time.Time {
	if primary.IsZero() {
		return fallback.UTC()
	}
	return primary.UTC()
}
