package copilot

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	AgentRunStatusQueued          = "queued"
	AgentRunStatusRunning         = "running"
	AgentRunStatusCompleted       = "completed"
	AgentRunStatusFailed          = "failed"
	AgentRunStatusCanceled        = "canceled"
	AgentRunStatusCallbackTimeout = "callback_timeout"
)

type AgentProvider struct {
	ID               string                      `json:"id"`
	Kind             string                      `json:"kind"`
	Name             string                      `json:"name"`
	Description      string                      `json:"description,omitempty"`
	Enabled          bool                        `json:"enabled"`
	Default          bool                        `json:"default,omitempty"`
	Capabilities     []string                    `json:"capabilities,omitempty"`
	SupportedModes   []string                    `json:"supportedModes,omitempty"`
	SupportsAsync    bool                        `json:"supportsAsync"`
	SupportsSkills   bool                        `json:"supportsSkills"`
	SupportsToolsets bool                        `json:"supportsToolsets"`
	Config           map[string]any              `json:"config,omitempty"`
	RuntimeStatus    *AgentProviderRuntimeStatus `json:"runtimeStatus,omitempty"`
}

type AgentProviderRuntimeStatus struct {
	State           string     `json:"state"`
	Reason          string     `json:"reason,omitempty"`
	QueuedRuns      int        `json:"queuedRuns"`
	RunningRuns     int        `json:"runningRuns"`
	RecentFailures  int        `json:"recentFailures"`
	LastRunID       string     `json:"lastRunId,omitempty"`
	LastRunStatus   string     `json:"lastRunStatus,omitempty"`
	LastAgentID     string     `json:"lastAgentId,omitempty"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastCompletedAt *time.Time `json:"lastCompletedAt,omitempty"`
	ObservedAt      time.Time  `json:"observedAt"`
}

type AgentToolBinding struct {
	ID            string         `json:"id"`
	CapabilityID  string         `json:"capabilityId"`
	ProviderID    string         `json:"providerId,omitempty"`
	ProviderKind  string         `json:"providerKind,omitempty"`
	ToolKind      string         `json:"toolKind"`
	AdapterID     string         `json:"adapterId,omitempty"`
	ToolName      string         `json:"toolName,omitempty"`
	PermissionKey string         `json:"permissionKey,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
}

type AgentSkillBinding struct {
	ID               string         `json:"id"`
	SkillID          string         `json:"skillId"`
	ProviderID       string         `json:"providerId,omitempty"`
	ProviderKind     string         `json:"providerKind,omitempty"`
	ProviderSkillRef string         `json:"providerSkillRef,omitempty"`
	CapabilityRefs   []string       `json:"capabilityRefs,omitempty"`
	PromptTemplateID string         `json:"promptTemplateId,omitempty"`
	Config           map[string]any `json:"config,omitempty"`
}

type AgentCapability struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Category       string              `json:"category,omitempty"`
	Description    string              `json:"description,omitempty"`
	AnalysisKinds  []string            `json:"analysisKinds,omitempty"`
	RequiredScopes []string            `json:"requiredScopes,omitempty"`
	ToolRefs       []string            `json:"toolRefs,omitempty"`
	ToolBindings   []AgentToolBinding  `json:"toolBindings,omitempty"`
	SkillBindings  []AgentSkillBinding `json:"skillBindings,omitempty"`
}

type AgentRun struct {
	ID                string              `json:"id"`
	ProviderID        string              `json:"providerId"`
	ProviderKind      string              `json:"providerKind"`
	CapabilityID      string              `json:"capabilityId"`
	SkillIDs          []string            `json:"skillIds,omitempty"`
	SessionID         string              `json:"sessionId,omitempty"`
	RootCauseRunID    string              `json:"rootCauseRunId,omitempty"`
	CreatedBy         string              `json:"createdBy"`
	Status            string              `json:"status"`
	Scope             SessionScope        `json:"scope,omitempty"`
	Toolset           SessionToolset      `json:"toolset,omitempty"`
	ToolBindings      []AgentToolBinding  `json:"toolBindings,omitempty"`
	SkillBindings     []AgentSkillBinding `json:"skillBindings,omitempty"`
	Input             map[string]any      `json:"input,omitempty"`
	Output            map[string]any      `json:"output,omitempty"`
	ToolExecutions    []ToolExecution     `json:"toolExecutions,omitempty"`
	AnalysisArtifacts []AnalysisArtifact  `json:"analysisArtifacts,omitempty"`
	OperationState    *OperationState     `json:"operationState,omitempty" gorm:"-"`
	CallbackToken     string              `json:"callbackToken,omitempty"`
	ClaimedByAgentID  string              `json:"claimedByAgentId,omitempty"`
	ExternalRunID     string              `json:"externalRunId,omitempty"`
	ErrorMessage      string              `json:"errorMessage,omitempty"`
	TimeoutSeconds    int                 `json:"timeoutSeconds"`
	QueuedAt          time.Time           `json:"queuedAt"`
	StartedAt         *time.Time          `json:"startedAt,omitempty"`
	LastHeartbeatAt   *time.Time          `json:"lastHeartbeatAt,omitempty"`
	CompletedAt       *time.Time          `json:"completedAt,omitempty"`
	CreatedAt         time.Time           `json:"createdAt"`
	UpdatedAt         time.Time           `json:"updatedAt"`
}

type OperationState struct {
	Phase                 string            `json:"phase"`
	Status                string            `json:"status"`
	Terminal              bool              `json:"terminal"`
	Cancelable            bool              `json:"cancelable"`
	Retryable             bool              `json:"retryable"`
	RunnerClaimRequired   bool              `json:"runnerClaimRequired"`
	HeartbeatRequired     bool              `json:"heartbeatRequired"`
	TimeoutSeconds        int               `json:"timeoutSeconds"`
	TimeoutStale          bool              `json:"timeoutStale"`
	HeartbeatStale        bool              `json:"heartbeatStale"`
	LastHeartbeatAt       time.Time         `json:"lastHeartbeatAt,omitempty"`
	NextHeartbeatDeadline time.Time         `json:"nextHeartbeatDeadline,omitempty"`
	NextTimeoutDeadline   time.Time         `json:"nextTimeoutDeadline,omitempty"`
	FailureReason         string            `json:"failureReason,omitempty"`
	FailureMessage        string            `json:"failureMessage,omitempty"`
	FailureEvidence       []FailureEvidence `json:"failureEvidence,omitempty"`
	FinalStateRecordedAt  time.Time         `json:"finalStateRecordedAt,omitempty"`
	ClaimedByAgentID      string            `json:"claimedByAgentId,omitempty"`
	ExternalRunID         string            `json:"externalRunId,omitempty"`
	ArtifactCount         int               `json:"artifactCount,omitempty"`
	ToolExecutionCount    int               `json:"toolExecutionCount,omitempty"`
	RecommendedNextAction string            `json:"recommendedNextAction,omitempty"`
}

type FailureEvidence struct {
	Kind      string         `json:"kind"`
	Source    string         `json:"source"`
	Title     string         `json:"title,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Severity  string         `json:"severity,omitempty"`
	Reference string         `json:"reference,omitempty"`
	Timestamp time.Time      `json:"timestamp,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

const defaultAgentRunOperationTimeoutSeconds = 600

var agentRunEvidenceSensitiveValuePattern = regexp.MustCompile(`(?i)(token|password|passwd|secret|api[_-]?key|authorization|credential|kubeconfig)(\s*[:=]\s*)([^\s,;]+)`)

func WithOperationState(run AgentRun, now time.Time) AgentRun {
	run.OperationState = BuildOperationState(run, now)
	return run
}

func WithOperationStates(runs []AgentRun, now time.Time) []AgentRun {
	out := make([]AgentRun, len(runs))
	for index, run := range runs {
		out[index] = WithOperationState(run, now)
	}
	return out
}

func BuildOperationState(run AgentRun, now time.Time) *OperationState {
	status := strings.TrimSpace(run.Status)
	timeoutSeconds := run.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultAgentRunOperationTimeoutSeconds
	}
	terminal := operationStatusTerminal(status)
	claimRequired := status == AgentRunStatusQueued
	heartbeatRequired := status == AgentRunStatusRunning
	timeoutReference := operationTimeoutReference(run)
	nextTimeoutDeadline := time.Time{}
	timeoutStale := false
	if !terminal && !timeoutReference.IsZero() {
		nextTimeoutDeadline = timeoutReference.Add(time.Duration(timeoutSeconds) * time.Second)
		timeoutStale = now.After(nextTimeoutDeadline)
	}
	nextHeartbeatDeadline := time.Time{}
	heartbeatStale := false
	if heartbeatRequired {
		nextHeartbeatDeadline = operationHeartbeatReference(run).Add(time.Duration(timeoutSeconds) * time.Second)
		heartbeatStale = now.After(nextHeartbeatDeadline)
	}
	state := &OperationState{
		Phase:                 operationPhase(status),
		Status:                status,
		Terminal:              terminal,
		Cancelable:            status == AgentRunStatusQueued || status == AgentRunStatusRunning,
		Retryable:             status == AgentRunStatusFailed || status == AgentRunStatusCallbackTimeout || status == AgentRunStatusCanceled,
		RunnerClaimRequired:   claimRequired,
		HeartbeatRequired:     heartbeatRequired,
		TimeoutSeconds:        timeoutSeconds,
		TimeoutStale:          timeoutStale,
		HeartbeatStale:        heartbeatStale,
		ClaimedByAgentID:      strings.TrimSpace(run.ClaimedByAgentID),
		ExternalRunID:         strings.TrimSpace(run.ExternalRunID),
		ArtifactCount:         len(run.AnalysisArtifacts),
		ToolExecutionCount:    len(run.ToolExecutions),
		RecommendedNextAction: operationRecommendedNextAction(status, timeoutStale, heartbeatStale),
	}
	if run.LastHeartbeatAt != nil && !run.LastHeartbeatAt.IsZero() {
		state.LastHeartbeatAt = run.LastHeartbeatAt.UTC()
	}
	if !nextHeartbeatDeadline.IsZero() {
		state.NextHeartbeatDeadline = nextHeartbeatDeadline.UTC()
	}
	if !nextTimeoutDeadline.IsZero() {
		state.NextTimeoutDeadline = nextTimeoutDeadline.UTC()
	}
	if run.CompletedAt != nil && !run.CompletedAt.IsZero() {
		state.FinalStateRecordedAt = run.CompletedAt.UTC()
	}
	if terminal && status != AgentRunStatusCompleted {
		state.FailureReason = firstNonEmptyAgentRunResultString(run.Output, "failureReason", "reason", "errorCode", "agentRunStatus")
		if state.FailureReason == "" && strings.TrimSpace(run.ErrorMessage) != "" {
			state.FailureReason = status
		}
		if state.FailureReason == "" {
			state.FailureReason = status
		}
		state.FailureReason = redactAgentRunEvidenceText(state.FailureReason)
		state.FailureMessage = firstNonEmptyAgentRunResultString(run.Output, "error", "message", "cancelReason")
		if state.FailureMessage == "" {
			state.FailureMessage = strings.TrimSpace(run.ErrorMessage)
		}
		state.FailureMessage = redactAgentRunEvidenceText(state.FailureMessage)
		state.FailureEvidence = BuildFailureEvidence(run)
	}
	return state
}

func BuildFailureEvidence(run AgentRun) []FailureEvidence {
	items := []FailureEvidence{}
	if strings.TrimSpace(run.ErrorMessage) != "" {
		items = append(items, FailureEvidence{
			Kind:     "error_message",
			Source:   "agent_run",
			Title:    "Agent run error",
			Summary:  redactAgentRunEvidenceText(strings.TrimSpace(run.ErrorMessage)),
			Severity: "error",
		})
	}
	if reason := firstNonEmptyAgentRunResultString(run.Output, "failureReason", "reason", "errorCode", "agentRunStatus"); reason != "" {
		items = append(items, FailureEvidence{
			Kind:     "callback_payload",
			Source:   "provider_callback",
			Title:    "Provider failure reason",
			Summary:  redactAgentRunEvidenceText(reason),
			Severity: failureEvidenceSeverity(run.Status),
			Metadata: compactAgentRunEvidenceMetadata(map[string]any{
				"externalRunId": strings.TrimSpace(run.ExternalRunID),
				"status":        strings.TrimSpace(run.Status),
			}),
		})
	}
	if message := firstNonEmptyAgentRunResultString(run.Output, "error", "message", "cancelReason"); message != "" && message != strings.TrimSpace(run.ErrorMessage) {
		items = append(items, FailureEvidence{
			Kind:     "callback_message",
			Source:   "provider_callback",
			Title:    "Provider failure message",
			Summary:  redactAgentRunEvidenceText(message),
			Severity: failureEvidenceSeverity(run.Status),
		})
	}
	for _, tool := range run.ToolExecutions {
		if !toolExecutionFailed(tool) {
			continue
		}
		item := FailureEvidence{
			Kind:      "tool_execution",
			Source:    firstNonEmptyString(tool.AdapterID, "agent_tool"),
			Title:     firstNonEmptyString(tool.ToolName, tool.ID, "tool execution failed"),
			Summary:   redactAgentRunEvidenceText(firstNonEmptyString(tool.Summary, firstNonEmptyAgentRunResultString(tool.Output, "error", "message", "summary"))),
			Severity:  "error",
			Reference: tool.ID,
			Timestamp: tool.StartedAt,
			Metadata: compactAgentRunEvidenceMetadata(map[string]any{
				"adapterId": tool.AdapterID,
				"toolName":  tool.ToolName,
				"status":    tool.Status,
			}),
		}
		if tool.CompletedAt != nil && !tool.CompletedAt.IsZero() {
			item.Timestamp = tool.CompletedAt.UTC()
		}
		items = append(items, item)
	}
	for _, artifact := range run.AnalysisArtifacts {
		if strings.TrimSpace(artifact.Summary) != "" {
			items = append(items, FailureEvidence{
				Kind:      "analysis_artifact",
				Source:    firstNonEmptyString(artifact.Kind, "analysis"),
				Title:     firstNonEmptyString(artifact.Title, artifact.Kind, "analysis artifact"),
				Summary:   redactAgentRunEvidenceText(artifact.Summary),
				Severity:  "warning",
				Reference: firstNonEmptyString(artifact.RunID, run.RootCauseRunID),
			})
		}
		for _, evidence := range artifact.Evidence {
			items = append(items, FailureEvidence{
				Kind:      firstNonEmptyString(evidence.Kind, "evidence"),
				Source:    firstNonEmptyString(artifact.Kind, "analysis"),
				Title:     evidence.Title,
				Summary:   redactAgentRunEvidenceText(evidence.Summary),
				Severity:  firstNonEmptyString(evidence.Severity, "warning"),
				Reference: evidence.ID,
				Timestamp: evidenceTime(evidence),
				Metadata:  compactAgentRunEvidenceMetadata(evidence.Attributes),
			})
		}
	}
	if strings.TrimSpace(run.ClaimedByAgentID) != "" || strings.TrimSpace(run.ExternalRunID) != "" {
		items = append(items, FailureEvidence{
			Kind:     "runner_claim",
			Source:   "agent_runner",
			Title:    "Runner claim metadata",
			Severity: "info",
			Metadata: compactAgentRunEvidenceMetadata(map[string]any{
				"claimedByAgentId": strings.TrimSpace(run.ClaimedByAgentID),
				"externalRunId":    strings.TrimSpace(run.ExternalRunID),
			}),
		})
	}
	return compactFailureEvidence(items)
}

func operationTimeoutReference(run AgentRun) time.Time {
	if run.Status == AgentRunStatusRunning {
		return operationHeartbeatReference(run)
	}
	if !run.QueuedAt.IsZero() {
		return run.QueuedAt.UTC()
	}
	return run.CreatedAt.UTC()
}

func operationHeartbeatReference(run AgentRun) time.Time {
	if run.LastHeartbeatAt != nil && !run.LastHeartbeatAt.IsZero() {
		return run.LastHeartbeatAt.UTC()
	}
	if run.StartedAt != nil && !run.StartedAt.IsZero() {
		return run.StartedAt.UTC()
	}
	if !run.QueuedAt.IsZero() {
		return run.QueuedAt.UTC()
	}
	return run.CreatedAt.UTC()
}

func operationStatusTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case AgentRunStatusCompleted, AgentRunStatusFailed, AgentRunStatusCanceled, AgentRunStatusCallbackTimeout:
		return true
	default:
		return false
	}
}

func operationPhase(status string) string {
	switch strings.TrimSpace(status) {
	case AgentRunStatusQueued:
		return "pending"
	case AgentRunStatusRunning:
		return "running"
	case AgentRunStatusCompleted:
		return "succeeded"
	case AgentRunStatusFailed, AgentRunStatusCallbackTimeout:
		return "failed"
	case AgentRunStatusCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

func operationRecommendedNextAction(status string, timeoutStale, heartbeatStale bool) string {
	switch strings.TrimSpace(status) {
	case AgentRunStatusQueued:
		if timeoutStale {
			return "inspect_runner_or_cancel"
		}
		return "wait_for_runner_claim"
	case AgentRunStatusRunning:
		if heartbeatStale || timeoutStale {
			return "inspect_runner_or_cancel"
		}
		return "wait_for_callback"
	case AgentRunStatusFailed, AgentRunStatusCallbackTimeout:
		return "inspect_failure_or_retry"
	case AgentRunStatusCanceled:
		return "retry_or_close"
	case AgentRunStatusCompleted:
		return "inspect_result"
	default:
		return "inspect_status"
	}
}

func firstNonEmptyAgentRunResultString(result map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := result[key]
		if !ok {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func compactFailureEvidence(items []FailureEvidence) []FailureEvidence {
	out := make([]FailureEvidence, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item.Kind = strings.TrimSpace(item.Kind)
		item.Source = strings.TrimSpace(item.Source)
		item.Title = strings.TrimSpace(item.Title)
		item.Summary = strings.TrimSpace(item.Summary)
		item.Severity = strings.TrimSpace(item.Severity)
		item.Reference = strings.TrimSpace(item.Reference)
		if item.Kind == "" || item.Source == "" {
			continue
		}
		key := item.Kind + "\x00" + item.Source + "\x00" + item.Reference + "\x00" + item.Summary
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func toolExecutionFailed(tool ToolExecution) bool {
	switch strings.ToLower(strings.TrimSpace(tool.Status)) {
	case "failed", "failure", "error", "denied", "timeout", "canceled", "cancelled":
		return true
	default:
		return false
	}
}

func evidenceTime(evidence RootCauseEvidence) time.Time {
	if evidence.Timestamp == nil || evidence.Timestamp.IsZero() {
		return time.Time{}
	}
	return evidence.Timestamp.UTC()
}

func failureEvidenceSeverity(status string) string {
	switch strings.TrimSpace(status) {
	case AgentRunStatusCanceled:
		return "warning"
	case AgentRunStatusCallbackTimeout, AgentRunStatusFailed:
		return "error"
	default:
		return "warning"
	}
}

func compactAgentRunEvidenceMetadata(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			out[key] = redactAgentRunEvidenceValue(key, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func redactAgentRunEvidenceValue(key string, value any) any {
	if agentRunEvidenceSensitiveKey(key) {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case string:
		return redactAgentRunEvidenceText(typed)
	case map[string]any:
		return compactAgentRunEvidenceMetadata(typed)
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = redactAgentRunEvidenceValue("", item)
		}
		return out
	default:
		return value
	}
}

func redactAgentRunEvidenceText(value string) string {
	return agentRunEvidenceSensitiveValuePattern.ReplaceAllString(value, "$1$2[REDACTED]")
}

func agentRunEvidenceSensitiveKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	for _, needle := range []string{"token", "password", "passwd", "secret", "credential", "apikey", "authorization", "kubeconfig"} {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type AgentRunInput struct {
	ProviderID     string              `json:"providerId,omitempty"`
	CapabilityID   string              `json:"capabilityId"`
	SkillIDs       []string            `json:"skillIds,omitempty"`
	SessionID      string              `json:"sessionId,omitempty"`
	RootCauseRunID string              `json:"rootCauseRunId,omitempty"`
	CreatedBy      string              `json:"createdBy,omitempty"`
	Scope          SessionScope        `json:"scope,omitempty"`
	Toolset        SessionToolset      `json:"toolset,omitempty"`
	ToolBindings   []AgentToolBinding  `json:"toolBindings,omitempty"`
	SkillBindings  []AgentSkillBinding `json:"skillBindings,omitempty"`
	Input          map[string]any      `json:"input,omitempty"`
	TimeoutSeconds int                 `json:"timeoutSeconds,omitempty"`
}

type GatewayAnalysisArtifactInput struct {
	CapabilityID       string                `json:"capabilityId"`
	Title              string                `json:"title,omitempty"`
	Summary            string                `json:"summary"`
	SkillIDs           []string              `json:"skillIds,omitempty"`
	Scope              SessionScope          `json:"scope,omitempty"`
	Toolset            SessionToolset        `json:"toolset,omitempty"`
	Input              map[string]any        `json:"input,omitempty"`
	Output             map[string]any        `json:"output,omitempty"`
	Evidence           []RootCauseEvidence   `json:"evidence,omitempty"`
	Hypotheses         []RootCauseHypothesis `json:"hypotheses,omitempty"`
	Recommendations    []string              `json:"recommendations,omitempty"`
	ToolExecutions     []ToolExecution       `json:"toolExecutions,omitempty"`
	Graph              *AnalysisGraph        `json:"graph,omitempty"`
	DataSourceSnapshot map[string]any        `json:"dataSourceSnapshot,omitempty"`
}

type GatewayAnalysisAgentRunInput struct {
	GatewayAnalysisArtifactInput
	AgentProviderID string `json:"agentProviderId,omitempty"`
	TimeoutSeconds  int    `json:"timeoutSeconds,omitempty"`
}

type AgentRunFilter struct {
	CreatedBy      string
	Status         string
	ProviderID     string
	CapabilityID   string
	TriggerType    string
	DedupKey       string
	DedupKeyPrefix string
	Limit          int
}

type AgentRunClaimInput struct {
	AgentID     string   `json:"agentId"`
	ProviderIDs []string `json:"providerIds,omitempty"`
	Kinds       []string `json:"kinds,omitempty"`
}

type AgentRunCallbackInput struct {
	RunID             string             `json:"runId"`
	CallbackToken     string             `json:"callbackToken"`
	AgentID           string             `json:"agentId,omitempty"`
	Status            string             `json:"status"`
	Payload           map[string]any     `json:"payload,omitempty"`
	ToolExecutions    []ToolExecution    `json:"toolExecutions,omitempty"`
	AnalysisArtifacts []AnalysisArtifact `json:"analysisArtifacts,omitempty"`
	ExternalRunID     string             `json:"externalRunId,omitempty"`
	ErrorMessage      string             `json:"errorMessage,omitempty"`
}

type AgentRunCancelInput struct {
	RunID       string `json:"runId"`
	RequestedBy string `json:"requestedBy,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type AgentToolCallInput struct {
	RunID         string         `json:"runId"`
	CallbackToken string         `json:"callbackToken"`
	AgentID       string         `json:"agentId,omitempty"`
	ToolBindingID string         `json:"toolBindingId,omitempty"`
	AdapterID     string         `json:"adapterId,omitempty"`
	ToolName      string         `json:"toolName,omitempty"`
	Input         map[string]any `json:"input,omitempty"`
}

type AgentToolCallResult struct {
	RunID         string         `json:"runId"`
	ToolExecution ToolExecution  `json:"toolExecution"`
	Output        map[string]any `json:"output,omitempty"`
}
