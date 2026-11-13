package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type sshCommandOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (output *sshCommandOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	count := len(data)
	remaining := (1 << 20) - len(output.data)
	if len(data) > remaining {
		data = data[:remaining]
		output.truncated = true
	}
	output.data = append(output.data, data...)
	return count, nil
}
func (output *sshCommandOutput) snapshot() (string, bool) {
	output.mu.Lock()
	defer output.mu.Unlock()
	text := strings.ToValidUTF8(string(output.data), "�")
	if len(text) > 1<<20 {
		text = strings.ToValidUTF8(text[:1<<20], "")
		return text, true
	}
	return text, output.truncated
}

func (r *Runner) checkSSHCommandAuthorization(ctx context.Context, input sohaapi.AgentSSHSessionRequest) error {
	body, _ := json.Marshal(sohaapi.VMSSHCredentialRedemptionInput{ClusterID: input.ExecutionClusterID, AgentID: input.AgentID, LeaseID: input.Lease.ID, Token: input.Lease.Token})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/api/v1/virtualization/ssh-sessions/"+url.PathEscape(input.SessionID)+"/execution-status", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+r.cfg.BearerToken)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("SSH command authorization unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("SSH command authorization revoked")
	}
	return nil
}

func (r *Runner) executeSSHCommand(ctx context.Context, task ExecutionTask) {
	r.metrics.markStarted(metricScopeExecution)
	var input sohaapi.AgentSSHCommandRequest
	raw, _ := json.Marshal(task.Payload["sshCommand"])
	valid := json.Unmarshal(raw, &input) == nil && r.sshClusterID != "" && input.Session.ExecutionClusterID == r.sshClusterID && task.ProviderKind == "vm_ssh."+r.sshClusterID && input.Session.SessionID == task.ID && input.TimeoutSeconds == task.TimeoutSeconds && input.TimeoutSeconds >= 1 && input.TimeoutSeconds <= 300 && len(input.Command) > 0 && len(input.Command) <= 16384 && !strings.ContainsRune(input.Command, 0) && ValidateSSHRequest(r.cfg, input.Session) == nil
	sum := sha256.Sum256([]byte(input.Command))
	result := sohaapi.AgentSSHCommandResult{SessionID: task.ID, AgentID: r.cfg.AgentID, CommandDigest: "sha256:" + hex.EncodeToString(sum[:]), Effect: "not_started", Stopped: true}
	stage := "validation"
	finish := func(status string) {
		if result.Effect == "not_started" {
			r.logger.Warn("SSH command preparation failed", zap.String("task_id", task.ID), zap.String("stage", stage))
		}
		r.finalCallback(ctx, task, status, map[string]any{"sshCommand": result})
		r.metrics.markOutcome(metricScopeExecution, status)
	}
	if !valid {
		finish("failed")
		return
	}
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()
	r.registerActiveTask(task, cancel)
	defer r.unregisterActiveTask(task.ID)
	stage = "authorization"
	if r.checkSSHCommandAuthorization(taskCtx, input.Session) != nil {
		finish("failed")
		return
	}
	stage = "credential"
	auth, err := redeemSSHAuth(taskCtx, r.cfg, input.Session)
	if err != nil {
		finish("failed")
		return
	}
	stage = "connection"
	client, err := dialSSH(taskCtx, input.Session, auth)
	if err != nil {
		finish("failed")
		return
	}
	defer func() { _ = client.Close() }()
	stopConnection := context.AfterFunc(taskCtx, func() { _ = client.Close() })
	defer stopConnection()
	stage = "session"
	session, err := client.NewSession()
	if err != nil {
		finish("failed")
		return
	}
	defer func() { _ = session.Close() }()
	stopConnection()
	stopCommand := stopSSHCommandOnCancel(taskCtx, client, session)
	defer stopCommand()
	var stdout, stderr sshCommandOutput
	session.Stdout, session.Stderr = &stdout, &stderr
	stage = "authorization"
	if r.checkSSHCommandAuthorization(taskCtx, input.Session) != nil {
		finish("failed")
		return
	}
	// Start can lose its acknowledgement after the remote process starts.
	result.Effect = "unknown"
	if err = session.Start(input.Command); err != nil {
		finish("failed")
		return
	}
	result.Stopped = false
	heartbeatCtx, stopHeartbeat := context.WithTimeout(taskCtx, 3*time.Second)
	r.callback(heartbeatCtx, task, "running", map[string]any{"sshCommand": result})
	stopHeartbeat()
	wait := make(chan error, 1)
	go func() { wait <- session.Wait() }()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	status := "callback_timeout"
	for {
		select {
		case waitErr := <-wait:
			if taskCtx.Err() == nil {
				result.ExitCode = SSHExitCode(waitErr)
				if result.ExitCode != nil {
					result.Effect = "exited"
					status = "failed"
					if *result.ExitCode == 0 {
						status = "completed"
					}
				}
			}
			goto done
		case <-taskCtx.Done():
			goto done
		case <-ticker.C:
			if r.checkSSHCommandAuthorization(taskCtx, input.Session) != nil {
				cancel()
			}
		}
	}
done:
	stopCommand()
	_ = session.Close()
	_ = client.Close()
	result.Stdout, result.OutputTruncated = stdout.snapshot()
	var stderrTruncated bool
	result.Stderr, stderrTruncated = stderr.snapshot()
	result.OutputTruncated = result.OutputTruncated || stderrTruncated
	sum = sha256.Sum256([]byte(result.Stdout + "\x00" + result.Stderr))
	result.OutputDigest = "sha256:" + hex.EncodeToString(sum[:])
	result.Stopped = true
	finish(status)
}

func stopSSHCommandOnCancel(ctx context.Context, client *ssh.Client, session *ssh.Session) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		deadline := time.AfterFunc(2*time.Second, func() { _ = client.Close() })
		// Closing a non-PTY channel can leave its remote process running.
		_, _ = session.SendRequest("signal", true, ssh.Marshal(struct{ Signal string }{Signal: "KILL"}))
		deadline.Stop()
		_ = client.Close()
	})
	var once sync.Once
	return func() {
		once.Do(func() {
			if !stop() {
				<-done
			}
		})
	}
}
