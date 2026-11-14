package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Validation uses a private temporary directory, so a preflight cannot replace
// the live project's Compose or .env files before approval.
func (r *Runner) validateDeliveryCompose(ctx context.Context, operation DockerOperation) ([]string, error) {
	if err := validateBoundProjectNamespace(ctx, operation); err != nil {
		return nil, err
	}
	payload, err := decodeDockerComposePayload(operation.Payload)
	if err != nil {
		return nil, err
	}
	payload.ComposeContent, err = boundComposeContent(operation, payload.ComposeContent)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "soha-compose-preflight-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(payload.ComposeContent), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(payload.EnvContent), 0o600); err != nil {
		return nil, err
	}
	logs, err := runCommand(ctx, dir, "docker", "info", "--format", "{{.ServerVersion}}")
	if err != nil {
		return logs, fmt.Errorf("docker preflight requires a reachable daemon: %w", err)
	}
	output, err := runComposeOperation(ctx, dir, operation, composeArgsForAction("validate"))
	return append(logs, output...), err
}
