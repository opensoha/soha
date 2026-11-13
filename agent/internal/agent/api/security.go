package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
	"go.uber.org/zap"
)

const (
	actionPlatformNodesDrain              = "platform.nodes.drain"
	actionPlatformPodsExec                = "platform.pods.exec"
	actionPlatformDeploymentRestart       = "platform.deployments.restart"
	actionPlatformDeploymentScale         = "platform.deployments.scale"
	actionPlatformDeploymentImage         = "platform.deployments.image"
	actionPlatformDeploymentRollback      = "platform.deployments.rollback"
	actionPlatformStatefulSetRestart      = "platform.statefulsets.restart"
	actionPlatformStatefulSetScale        = "platform.statefulsets.scale"
	actionPlatformDaemonSetRestart        = "platform.daemonsets.restart"
	actionPlatformResourcesApply          = "platform.resources.apply"
	actionPlatformResourcesCreate         = "platform.resources.create"
	actionPlatformResourcesDelete         = "platform.resources.delete"
	actionPlatformCRDsDelete              = "platform.crds.delete"
	actionPlatformCustomResourcesList     = "platform.custom_resources.list"
	actionPlatformCustomResourcesCreate   = "platform.custom_resources.create"
	actionPlatformCustomResourcesApply    = "platform.custom_resources.apply"
	actionPlatformCustomResourcesDelete   = "platform.custom_resources.delete"
	actionPlatformPortForwardsCreate      = "platform.port_forwards.create"
	actionPlatformPortForwardsTunnel      = "platform.port_forwards.tunnel"
	actionPlatformPortForwardsDelete      = "platform.port_forwards.delete"
	actionPlatformHelmReleaseInstall      = "platform.helm_releases.install"
	actionPlatformHelmReleaseValuesUpdate = "platform.helm_releases.values_update"
	actionPlatformHelmReleaseDelete       = "platform.helm_releases.delete"
	actionPlatformHelmReleaseRollback     = "platform.helm_releases.rollback"
	actionRuntimeExecutionTaskCancel      = "runtime.execution_tasks.cancel"
	actionDockerRuntimeTerminal           = "docker.runtime.terminal"
	actionDockerNativeTerminal            = "docker.runtime.terminal.v1"
	actionDockerRuntimeLogs               = "docker.runtime.logs"
	actionDockerRuntimeFiles              = "docker.runtime.files"
	actionDockerVersionedLogs             = "docker.runtime.logs.v1"
	actionDockerVersionedFiles            = "docker.runtime.files.v1"
	actionVMSSHConnect                    = "runtime.vm_ssh.connect"
	actionVMSSHExecute                    = "runtime.vm_ssh.execute"
	actionPersonalEnvironmentAcquire      = "runtime.personal_environments.acquire"
	actionPersonalEnvironmentPreflight    = "runtime.personal_environments.preflight"
	actionPersonalEnvironmentInspect      = "runtime.personal_environments.inspect"
	actionPersonalEnvironmentExecute      = "runtime.personal_environments.execute"
	actionPersonalEnvironmentRelease      = "runtime.personal_environments.release"
	actionPersonalEnvironmentDeleteData   = "runtime.personal_environments.delete_data"
	actionPersonalEnvironmentExportData   = "runtime.personal_environments.export_data"
)

type actionPolicy struct {
	allowed map[string]struct{}
	logger  *zap.Logger
	sink    actionAuditSink
}

type actionAuditSink interface {
	WriteActionAudit(actionAuditRecord) error
}

type actionAuditRecord struct {
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	RequestID string `json:"requestId,omitempty"`
	ClientIP  string `json:"clientIp,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
}

type fileActionAuditSink struct {
	path string
	mu   sync.Mutex
}

func newActionPolicy(cfg cfgpkg.SecurityConfig, logger *zap.Logger, audit actionAuditSink) actionPolicy {
	if logger == nil {
		logger = zap.NewNop()
	}
	allowed := map[string]struct{}{}
	for _, action := range cfg.AllowedActions {
		normalized := normalizeAction(action)
		if normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	return actionPolicy{allowed: allowed, logger: logger, sink: audit}
}

func newActionAuditSink(cfg cfgpkg.AuditConfig, logger *zap.Logger) actionAuditSink {
	path := strings.TrimSpace(cfg.FilePath)
	if path == "" {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			logger.Warn("agent action audit sink disabled",
				zap.String("file_path", path),
				zap.String("reason", err.Error()),
			)
			return nil
		}
	}
	return &fileActionAuditSink{path: path}
}

func (s *fileActionAuditSink) WriteActionAudit(record actionAuditRecord) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
}

func (p actionPolicy) Require(action string) gin.HandlerFunc {
	normalized := normalizeAction(action)
	return func(c *gin.Context) {
		allowed := p.allows(normalized)
		reason := "matched allowlist"
		if !allowed {
			reason = "action is not allowlisted"
		}
		p.audit(c, normalized, allowed, reason)
		if !allowed {
			apiresponse.Error(c, http.StatusForbidden, "action_not_allowed", reason)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (p actionPolicy) allows(action string) bool {
	if action == "" {
		return false
	}
	if _, ok := p.allowed["*"]; ok {
		return true
	}
	_, ok := p.allowed[action]
	return ok
}

func (p actionPolicy) audit(c *gin.Context, action string, allowed bool, reason string) {
	path := c.FullPath()
	if path == "" && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	record := actionAuditRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Action:    action,
		Allowed:   allowed,
		Reason:    reason,
		Method:    c.Request.Method,
		Path:      path,
		RequestID: c.GetString("request_id"),
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
	fields := []zap.Field{
		zap.String("action", record.Action),
		zap.Bool("allowed", record.Allowed),
		zap.String("reason", record.Reason),
		zap.String("method", record.Method),
		zap.String("path", record.Path),
		zap.String("request_id", record.RequestID),
		zap.String("client_ip", record.ClientIP),
	}
	if allowed {
		p.logger.Info("agent action audit", fields...)
	} else {
		p.logger.Warn("agent action audit", fields...)
	}
	if p.sink != nil {
		if err := p.sink.WriteActionAudit(record); err != nil {
			p.logger.Warn("agent action audit sink write failed",
				zap.String("action", record.Action),
				zap.String("request_id", record.RequestID),
				zap.String("reason", err.Error()),
			)
		}
	}
}

func normalizeAction(action string) string {
	return strings.ToLower(strings.TrimSpace(action))
}
