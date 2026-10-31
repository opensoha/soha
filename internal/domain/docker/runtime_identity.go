package docker

import (
	"errors"
	"maps"

	dockerruntime "github.com/opensoha/soha-contracts/docker/runtime"
)

// BindProjectRuntimePayload is the pure consistency rule shared by normal
// enqueue and atomic container creation after database UIDs are available.
func BindProjectRuntimePayload(project Project, host Host, payload map[string]any) (map[string]any, error) {
	if project.RuntimeIdentityVersion == 0 {
		return payload, nil
	}
	if project.RuntimeIdentityVersion != 1 || project.HostID != host.ID {
		return nil, errors.New("docker runtime identity is unavailable")
	}
	binding, err := dockerruntime.NewProjectBinding(host.ResourceUID, project.ResourceUID)
	if err != nil {
		return nil, err
	}
	if value, present := payload["runtimeBinding"]; present {
		original, err := dockerruntime.ProjectBindingFromValue(value)
		if err != nil || original != binding {
			return nil, errors.New("docker original runtime identity changed")
		}
	}
	content, ok := payload["composeContent"].(string)
	if !ok {
		if _, encrypted := payload["deliveryCiphertext"].(string); !encrypted {
			return nil, errors.New("docker runtime Compose input is unavailable")
		}
		content = project.ComposeContent
	}
	_, err = dockerruntime.BindCompose(content, binding)
	if err != nil {
		return nil, err
	}
	result := maps.Clone(payload)
	result["runtimeBinding"] = binding
	if raw, present := payload["runtimeContainerId"]; present {
		id, _ := raw.(string)
		name, _ := payload["serviceName"].(string)
		target := dockerruntime.ContainerBinding{ProjectBinding: binding, ServiceName: name, ContainerID: id}
		if target.Validate() != nil {
			return nil, errors.New("docker service requires a confirmed immutable container ID")
		}
		result["runtimeContainer"] = target
		delete(result, "runtimeContainerId")
	}
	return result, nil
}
