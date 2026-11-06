package resourcebackend

import (
	"strings"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
)

func buildPodListContainerStatuses(pod corev1.Pod) ([]domainresource.PodContainerStatusView, int) {
	views := make([]domainresource.PodContainerStatusView, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	ready := 0
	appendStatuses := func(specs []corev1.Container, statuses []corev1.ContainerStatus, init bool) {
		byName := make(map[string]corev1.ContainerStatus, len(statuses))
		for _, status := range statuses {
			byName[status.Name] = status
		}
		for _, spec := range specs {
			status := byName[spec.Name]
			kind := "container"
			if init {
				kind = "init"
				if spec.RestartPolicy != nil && *spec.RestartPolicy == corev1.ContainerRestartPolicyAlways {
					kind = "sidecar"
				}
			} else if status.Ready {
				ready++
			}
			state, reason, _ := strings.Cut(podContainerState(status.State), ":")
			if state == "" {
				state = "unknown"
			}
			view := domainresource.PodContainerStatusView{Name: spec.Name, Kind: kind, Ready: status.Ready, State: state, Reason: reason}
			if status.State.Terminated != nil {
				code := status.State.Terminated.ExitCode
				view.ExitCode = &code
			}
			views = append(views, view)
		}
	}
	appendStatuses(pod.Spec.InitContainers, pod.Status.InitContainerStatuses, true)
	appendStatuses(pod.Spec.Containers, pod.Status.ContainerStatuses, false)
	return views, ready
}
