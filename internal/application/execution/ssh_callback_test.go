package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	delivery "github.com/opensoha/soha/internal/domain/delivery"
)

func TestSSHCompletionRequiresOriginalAgentExitAndDigest(t *testing.T) {
	task := delivery.ExecutionTask{ID: "task-one", TaskKind: "vm_ssh_command", ClaimedByAgentID: "agent-one", Payload: map[string]any{"commandDigest": "sha256:frozen"}}
	code := 0
	sum := sha256.Sum256([]byte("output\x00"))
	valid := sohaapi.AgentSSHCommandResult{SessionID: task.ID, AgentID: task.ClaimedByAgentID, CommandDigest: "sha256:frozen", Effect: "exited", Stopped: true, ExitCode: &code, Stdout: "output", OutputDigest: "sha256:" + hex.EncodeToString(sum[:])}
	if _, err := normalizeSSHCallback(task, delivery.ExecutionCallbackInput{Status: "completed", Payload: map[string]any{"sshCommand": valid}}); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeSSHCallback(task, delivery.ExecutionCallbackInput{Status: "running", Payload: map[string]any{"sshCommand": valid}}); err == nil {
		t.Fatal("running callback supplied a final exit result")
	}
	for name, mutate := range map[string]func(*sohaapi.AgentSSHCommandResult){"agent": func(v *sohaapi.AgentSSHCommandResult) { v.AgentID = "other" }, "task": func(v *sohaapi.AgentSSHCommandResult) { v.SessionID = "other" }, "command": func(v *sohaapi.AgentSSHCommandResult) { v.CommandDigest = "other" }, "unknown": func(v *sohaapi.AgentSSHCommandResult) { v.Effect = "unknown" }, "stop": func(v *sohaapi.AgentSSHCommandResult) { v.Stopped = false }, "digest": func(v *sohaapi.AgentSSHCommandResult) { v.Stdout = "changed" }, "exit": func(v *sohaapi.AgentSSHCommandResult) { v.ExitCode = nil }} {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if _, err := normalizeSSHCallback(task, delivery.ExecutionCallbackInput{Status: "completed", Payload: map[string]any{"sshCommand": value}}); err == nil {
				t.Fatal("invalid completion accepted")
			}
		})
	}
}

func TestSSHRecoveryKeepsDispatchedEffectsUnknownAndExpiresQueue(t *testing.T) {
	now := time.Now().UTC()
	repo := newExecutionRepoFake()
	for _, status := range []string{"queued", "running"} {
		task := delivery.ExecutionTask{ID: status, TaskKind: "vm_ssh_command", ProviderKind: "vm_ssh.cluster-one", Status: status, CreatedAt: now.Add(-31 * time.Minute), TimeoutSeconds: 60, Payload: map[string]any{"commandDigest": "sha256:frozen"}, Result: map[string]any{}}
		if status == "running" {
			task.AttemptCount = 1
			task.ClaimedByAgentID = "agent-one"
		}
		repo.tasks[status] = task
	}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
	if err := service.recoverStaleTasks(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	queued, active := repo.tasks["queued"], repo.tasks["running"]
	if queued.Status != "failed" || queued.AttemptCount != 0 || active.Status != "callback_timeout" {
		t.Fatal("incorrect recovery status")
	}
	queuedResult, queuedOK := queued.Result["sshCommand"].(sohaapi.AgentSSHCommandResult)
	activeResult, activeOK := active.Result["sshCommand"].(sohaapi.AgentSSHCommandResult)
	if !queuedOK || !activeOK || queuedResult.Effect != "not_started" || activeResult.Effect != "unknown" {
		t.Fatal("uncertain side effects were lost")
	}
}
