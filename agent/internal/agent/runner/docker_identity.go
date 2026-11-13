package runner

import (
	"context"
	"errors"

	engine "github.com/opensoha/soha/agent/internal/agent/dockerruntime"
	dockerruntime "github.com/opensoha/soha/contracts/docker/runtime"
)

func dockerOperationBinding(operation DockerOperation) (dockerruntime.ProjectBinding, bool, error) {
	value, present := operation.Payload["runtimeBinding"]
	if !present {
		return dockerruntime.ProjectBinding{}, false, nil
	}
	binding, err := dockerruntime.ProjectBindingFromValue(value)
	return binding, true, err
}

func boundComposeContent(operation DockerOperation, content string) (string, error) {
	binding, present, err := dockerOperationBinding(operation)
	if err != nil {
		return "", err
	}
	if !present {
		return content, nil
	}
	return dockerruntime.BindCompose(content, binding)
}

func boundComposeArgs(operation DockerOperation, args []string) ([]string, error) {
	binding, present, err := dockerOperationBinding(operation)
	if err != nil {
		return nil, err
	}
	if !present {
		return args, nil
	}
	if len(args) == 0 || args[0] != "compose" {
		return nil, errors.New("bound Docker operation requires a Compose command")
	}
	result := []string{"compose", "-p", binding.Namespace}
	return append(result, args[1:]...), nil
}

func runComposeOperation(ctx context.Context, dir string, operation DockerOperation, args []string) ([]string, error) {
	bound, err := boundComposeArgs(operation, args)
	if err != nil {
		return nil, err
	}
	return runCommand(ctx, dir, "docker", bound...)
}

func supportedDockerRuntimeProtocols() []string { return []string{dockerruntime.IdentityProtocol} }

func validateBoundProjectNamespace(ctx context.Context, operation DockerOperation) error {
	binding, present, err := dockerOperationBinding(operation)
	if err != nil || !present {
		return err
	}
	expected, err := dockerruntime.ContainerSetFromValue(operation.Payload["runtimeContainers"], binding)
	if err != nil {
		return err
	}
	items, err := engine.ListProjectContainers(ctx, binding)
	if err != nil {
		return err
	}
	actual := make([]dockerruntime.ContainerBinding, 0, len(items))
	for _, item := range items {
		actual = append(actual, dockerruntime.ContainerBinding{ProjectBinding: binding, ServiceName: item.Labels["com.docker.compose.service"], ContainerID: item.ID})
	}
	if !dockerruntime.SameContainerSet(expected, actual) {
		return errors.New("docker actual project containers changed from the original registered IDs")
	}
	return nil
}

func boundServiceTarget(operation DockerOperation) (dockerruntime.ContainerBinding, bool, error) {
	binding, present, err := dockerOperationBinding(operation)
	if err != nil || !present {
		return dockerruntime.ContainerBinding{}, present, err
	}
	target, err := dockerruntime.ContainerBindingFromValue(operation.Payload["runtimeContainer"])
	name, _ := operation.Payload["serviceName"].(string)
	if err != nil || target.ProjectBinding != binding || target.ServiceName != name {
		return dockerruntime.ContainerBinding{}, true, errors.New("docker service container identity is unavailable or changed")
	}
	return target, true, nil
}

func executeBoundServiceAction(ctx context.Context, operation DockerOperation) ([]string, error) {
	target, _, err := boundServiceTarget(operation)
	if err != nil {
		return nil, err
	}
	action, _ := operation.Payload["action"].(string)
	if action != "start" && action != "stop" && action != "restart" {
		return nil, errors.New("unsupported bound Docker service action")
	}
	if _, err := engine.InspectContainer(ctx, target); err != nil {
		return nil, err
	}
	return runCommand(ctx, "", "docker", "container", action, target.ContainerID)
}

func collectBoundServices(ctx context.Context, binding dockerruntime.ProjectBinding) ([]map[string]any, error) {
	items, err := engine.ListProjectContainers(ctx, binding)
	if err != nil {
		return nil, err
	}
	records := make([]map[string]any, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		name := item.Labels["com.docker.compose.service"]
		if seen[name] || item.Status == "" {
			return nil, errors.New("docker service inventory is ambiguous or unconfirmed")
		}
		seen[name] = true
		records = append(records, map[string]any{"name": name, "containerId": item.ID, "image": item.Image, "status": item.Status})
	}
	return records, nil
}
