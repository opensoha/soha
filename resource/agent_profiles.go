package resource

import "fmt"

// KubernetesAgentProfileActions is an installation preset, never a runtime permission bypass.
// Legacy preserves installations generated before the workbench profile was introduced.
func KubernetesAgentProfileActions(profile string) ([]string, error) {
	switch profile {
	case "read-only":
		return []string{"platform.custom_resources.list"}, nil
	case "workbench", "legacy":
		return append([]string(nil), kubernetesWorkbenchActions[:]...), nil
	default:
		return nil, fmt.Errorf("unknown Kubernetes Agent action profile")
	}
}

// KubernetesAgentActions lists implemented platform actions, including separately opted-in mutations.
func KubernetesAgentActions() []string {
	actions := make([]string, 0, len(kubernetesWorkbenchActions))
	for _, action := range kubernetesWorkbenchActions {
		if action != "runtime.execution_tasks.cancel" {
			actions = append(actions, action)
		}
	}
	return append(actions,
		"platform.helm_releases.install",
		"platform.helm_releases.values_update",
		"platform.helm_releases.delete",
		"platform.helm_releases.rollback",
		"platform.crds.delete",
	)
}

var kubernetesWorkbenchActions = [...]string{
	"platform.nodes.drain",
	"platform.pods.exec",
	"platform.deployments.restart",
	"platform.deployments.scale",
	"platform.deployments.image",
	"platform.deployments.rollback",
	"platform.statefulsets.restart",
	"platform.statefulsets.scale",
	"platform.daemonsets.restart",
	"platform.resources.apply",
	"platform.resources.create",
	"platform.resources.delete",
	"platform.custom_resources.list",
	"platform.custom_resources.create",
	"platform.custom_resources.apply",
	"platform.custom_resources.delete",
	"platform.port_forwards.create",
	"platform.port_forwards.tunnel",
	"platform.port_forwards.delete",
	"runtime.execution_tasks.cancel",
}
