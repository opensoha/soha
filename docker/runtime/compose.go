package runtime

import (
	"encoding/json"
	"errors"
	"strings"

	"sigs.k8s.io/yaml"
)

// BindCompose returns an execution copy. The user's stored document is retained.
// Explicit container_name is incompatible with isolated project namespaces.
func BindCompose(content string, binding ProjectBinding) (string, error) {
	if err := binding.Validate(); err != nil {
		return "", err
	}
	if len(content) == 0 || len(content) > 2<<20 {
		return "", errors.New("docker Compose document exceeds the runtime boundary")
	}
	raw, err := yaml.YAMLToJSONStrict([]byte(content))
	if err != nil {
		return "", errors.New("docker Compose document is invalid")
	}
	var document map[string]any
	if json.Unmarshal(raw, &document) != nil {
		return "", errors.New("docker Compose document must be an object")
	}
	services, ok := document["services"].(map[string]any)
	if !ok || len(services) == 0 || len(services) > 256 {
		return "", errors.New("docker Compose services are invalid")
	}
	if _, exists := document["include"]; exists {
		return "", errors.New("compose include is unsupported for identity-bound projects")
	}
	for name, value := range services {
		service, err := bindComposeService(name, value, binding)
		if err != nil {
			return "", err
		}
		services[name] = service
	}
	document["name"] = binding.Namespace
	encoded, err := yaml.Marshal(document)
	return string(encoded), err
}

func bindComposeService(name string, value any, binding ProjectBinding) (map[string]any, error) {
	service, ok := value.(map[string]any)
	if !ok || !serviceNamePattern.MatchString(name) {
		return nil, errors.New("docker Compose service is invalid")
	}
	for _, field := range []string{"container_name", "extends"} {
		if _, exists := service[field]; exists {
			return nil, errors.New("explicit container_name or extends is unsupported for identity-bound projects")
		}
	}
	if err := validateSingleContainerService(service); err != nil {
		return nil, err
	}
	labels, err := boundLabels(service["labels"], binding)
	if err != nil {
		return nil, err
	}
	service["labels"] = labels
	return service, nil
}

// Identity v1 registers one immutable CID per service. Reject dynamic or
// multi-replica input before Compose can create unregistered containers.
func validateSingleContainerService(service map[string]any) error {
	if value, present := service["scale"]; present && !singleReplica(value) {
		return errors.New("identity-bound Compose requires exactly one container per service")
	}
	if value, present := service["deploy"]; present {
		deploy, ok := value.(map[string]any)
		if !ok {
			return errors.New("identity-bound Compose deploy configuration is invalid")
		}
		if value, present := deploy["replicas"]; present && !singleReplica(value) {
			return errors.New("identity-bound Compose requires exactly one container per service")
		}
		if value, present := deploy["mode"]; present && value != "replicated" {
			return errors.New("identity-bound Compose requires replicated service mode")
		}
	}
	return nil
}

func singleReplica(value any) bool { return value == float64(1) || value == "1" }

func boundLabels(value any, binding ProjectBinding) (map[string]string, error) {
	labels := map[string]string{}
	switch input := value.(type) {
	case nil:
	case map[string]any:
		for key, raw := range input {
			text, ok := raw.(string)
			if !ok {
				return nil, errors.New("docker Compose labels must be strings")
			}
			labels[key] = text
		}
	case []any:
		for _, raw := range input {
			text, ok := raw.(string)
			if !ok {
				return nil, errors.New("docker Compose labels must be strings")
			}
			key, val, found := strings.Cut(text, "=")
			if !found || key == "" {
				return nil, errors.New("docker Compose labels require explicit values")
			}
			if _, duplicate := labels[key]; duplicate {
				return nil, errors.New("docker Compose label is duplicated")
			}
			labels[key] = val
		}
	default:
		return nil, errors.New("docker Compose labels are invalid")
	}
	for key, val := range binding.Labels() {
		if previous, exists := labels[key]; exists && previous != val {
			return nil, errors.New("docker runtime identity label is reserved")
		}
		labels[key] = val
	}
	return labels, nil
}
