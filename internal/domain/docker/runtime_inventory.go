package docker

import (
	"errors"

	dockerruntime "github.com/opensoha/soha/contracts/docker/runtime"
)

func RuntimeContainerSnapshot(binding dockerruntime.ProjectBinding, hostID, projectID string, services []Service) ([]dockerruntime.ContainerBinding, error) {
	if len(services) > 256 {
		return nil, errors.New("docker runtime inventory exceeds its limit")
	}
	items := make([]dockerruntime.ContainerBinding, 0, len(services))
	for _, service := range services {
		if service.HostID != hostID || service.ProjectID != projectID {
			return nil, errors.New("docker runtime inventory belongs to another project")
		}
		if service.ContainerID == "" {
			continue
		}
		items = append(items, dockerruntime.ContainerBinding{ProjectBinding: binding, ServiceName: service.Name, ContainerID: service.ContainerID})
	}
	return dockerruntime.ContainerSetFromValue(items, binding)
}
