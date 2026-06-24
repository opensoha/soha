package handlers

type streamExitKind string

const (
	streamExitKindPodLogs        streamExitKind = "pod_logs"
	streamExitKindPodTerminal    streamExitKind = "pod_terminal"
	streamExitKindDockerLogs     streamExitKind = "docker_logs"
	streamExitKindDockerTerminal streamExitKind = "docker_terminal"
	streamExitKindTaskUpdates    streamExitKind = "task_updates"
)

func streamExitMessage(kind streamExitKind) string {
	switch kind {
	case streamExitKindPodLogs:
		return "log stream closed"
	case streamExitKindPodTerminal:
		return "terminal session closed"
	case streamExitKindDockerLogs:
		return "docker log stream closed"
	case streamExitKindDockerTerminal:
		return "docker terminal session closed"
	case streamExitKindTaskUpdates:
		return "task stream closed"
	default:
		return "stream closed"
	}
}
