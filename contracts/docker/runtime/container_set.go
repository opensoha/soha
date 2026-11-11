package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ContainerSetFromValue decodes the complete set frozen before dispatch.
// Empty must be explicit; null/missing is not a safe inventory snapshot.
func ContainerSetFromValue(value any, project ProjectBinding) ([]ContainerBinding, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 256<<10 {
		return nil, errors.New("docker original container set is invalid")
	}
	var containers []ContainerBinding
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&containers) != nil || containers == nil || len(containers) > 256 || project.Validate() != nil {
		return nil, errors.New("docker original container set is missing or exceeds its limit")
	}
	names, ids := map[string]bool{}, map[string]bool{}
	for _, item := range containers {
		if item.Validate() != nil || item.ProjectBinding != project || names[item.ServiceName] || ids[item.ContainerID] {
			return nil, errors.New("docker original container set is ambiguous")
		}
		names[item.ServiceName], ids[item.ContainerID] = true, true
	}
	return containers, nil
}

func SameContainerSet(expected, actual []ContainerBinding) bool {
	if len(expected) != len(actual) {
		return false
	}
	items := make(map[string]ContainerBinding, len(expected))
	for _, item := range expected {
		if _, duplicate := items[item.ContainerID]; duplicate {
			return false
		}
		items[item.ContainerID] = item
	}
	for _, item := range actual {
		if original, exists := items[item.ContainerID]; !exists || original != item {
			return false
		}
		delete(items, item.ContainerID)
	}
	return len(items) == 0
}
