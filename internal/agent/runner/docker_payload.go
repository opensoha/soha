package runner

import (
	"encoding/json"
	"fmt"
	"strings"
)

type dockerComposePayload struct {
	Action         string `json:"action"`
	ProjectSlug    string `json:"projectSlug"`
	ComposeContent string `json:"composeContent"`
	EnvContent     string `json:"envContent"`
	ServiceName    string `json:"serviceName"`
}

func validateDockerOperationPayload(operation DockerOperation) error {
	if _, _, err := dockerOperationBinding(operation); err != nil {
		return err
	}
	switch operation.OperationKind {
	case "container_start", "project_deploy", "service_action":
		payload, err := decodeDockerComposePayload(operation.Payload)
		if err != nil {
			return err
		}
		if strings.TrimSpace(payload.ComposeContent) == "" {
			return fmt.Errorf("composeContent is required for %s", operation.OperationKind)
		}
		if operation.OperationKind == "service_action" {
			if _, _, err := boundServiceTarget(operation); err != nil {
				return err
			}
			if strings.TrimSpace(payload.ServiceName) == "" {
				return fmt.Errorf("serviceName is required for docker service action")
			}
			if len(composeServiceArgsForAction(payload.Action, payload.ServiceName)) == 0 {
				return fmt.Errorf("unsupported docker service action %q", payload.Action)
			}
			return nil
		}
		if len(composeArgsForAction(payload.Action)) == 0 {
			return fmt.Errorf("unsupported compose action %q", payload.Action)
		}
	}
	return nil
}

func decodeDockerComposePayload(input map[string]any) (dockerComposePayload, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return dockerComposePayload{}, fmt.Errorf("encode Docker operation payload: %w", err)
	}
	var payload dockerComposePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return dockerComposePayload{}, fmt.Errorf("invalid Docker operation payload: %w", err)
	}
	return payload, nil
}
