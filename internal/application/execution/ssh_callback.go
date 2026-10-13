package execution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	delivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func normalizeSSHCallback(task delivery.ExecutionTask, input delivery.ExecutionCallbackInput) (delivery.ExecutionCallbackInput, error) {
	if task.TaskKind != "vm_ssh_command" {
		return input, nil
	}
	raw, err := json.Marshal(input.Payload["sshCommand"])
	var result sohaapi.AgentSSHCommandResult
	if err != nil || len(raw) > 3<<20 {
		return input, apperrors.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.SessionID != task.ID || result.AgentID != task.ClaimedByAgentID || result.CommandDigest != task.Payload["commandDigest"] || len(result.Stdout) > 1<<20 || len(result.Stderr) > 1<<20 {
		return input, apperrors.ErrConflict
	}
	if err := validateSSHResult(input.Status, result); err != nil {
		return input, err
	}
	input.Payload = map[string]any{"sshCommand": result}
	return input, nil
}

func (s *Service) expireSSHCommand(ctx context.Context, task delivery.ExecutionTask, now time.Time, dispatched bool) error {
	effect, status := "not_started", "failed"
	if dispatched {
		effect, status = "unknown", "callback_timeout"
	}
	task.Status, task.FinishedAt, task.UpdatedAt = status, &now, now
	task.Result = map[string]any{"sshCommand": sohaapi.AgentSSHCommandResult{SessionID: task.ID, AgentID: task.ClaimedByAgentID, CommandDigest: valueAsString(task.Payload["commandDigest"]), Effect: sohaapi.AgentSSHCommandResultEffect(effect), Stopped: true}}
	updated, err := s.updateExecutionTask(ctx, task)
	if err != nil {
		return err
	}
	return s.notifyExecutionTaskSinks(ctx, updated)
}

func validateSSHResult(status string, result sohaapi.AgentSSHCommandResult) error {
	if isStrictTerminalTaskStatus(status) && !result.Stopped {
		return apperrors.ErrInvalidArgument
	}
	if status == "running" && (result.Effect != "unknown" || result.Stopped || result.ExitCode != nil) {
		return apperrors.ErrConflict
	}
	if result.ExitCode != nil && (*result.ExitCode < 0 || *result.ExitCode > 255) {
		return apperrors.ErrInvalidArgument
	}
	if err := validateSSHExit(status, result); err != nil {
		return err
	}
	switch status {
	case "running", "completed", "failed", "callback_timeout":
		return nil
	default:
		return apperrors.ErrInvalidArgument
	}
}
func validateSSHExit(status string, result sohaapi.AgentSSHCommandResult) error {
	if result.Effect == "exited" {
		sum := sha256.Sum256([]byte(result.Stdout + "\x00" + result.Stderr))
		if result.ExitCode == nil || result.OutputDigest != "sha256:"+hex.EncodeToString(sum[:]) {
			return apperrors.ErrConflict
		}
		if status == "completed" && *result.ExitCode != 0 || status == "failed" && *result.ExitCode == 0 {
			return apperrors.ErrConflict
		}
	} else if status == "completed" || result.ExitCode != nil || !strings.Contains("|not_started|unknown|", "|"+string(result.Effect)+"|") {
		return apperrors.ErrConflict
	}
	return nil
}
