package copilot

import (
	"context"
	"time"
)

type SessionScope struct {
	ClusterID        string `json:"clusterId,omitempty"`
	Namespace        string `json:"namespace,omitempty"`
	Workload         string `json:"workload,omitempty"`
	Service          string `json:"service,omitempty"`
	Pod              string `json:"pod,omitempty"`
	Node             string `json:"node,omitempty"`
	AlertID          string `json:"alertId,omitempty"`
	TimeRangeMinutes int    `json:"timeRangeMinutes,omitempty"`
}

type WorkbenchLaunchContext struct {
	SourceWorkbench            string         `json:"sourceWorkbench,omitempty"`
	SourceRoute                string         `json:"sourceRoute,omitempty"`
	SourceTitle                string         `json:"sourceTitle,omitempty"`
	EntityKind                 string         `json:"entityKind,omitempty"`
	EntityName                 string         `json:"entityName,omitempty"`
	ClusterID                  string         `json:"clusterId,omitempty"`
	Namespace                  string         `json:"namespace,omitempty"`
	Workload                   string         `json:"workload,omitempty"`
	Service                    string         `json:"service,omitempty"`
	Pod                        string         `json:"pod,omitempty"`
	Node                       string         `json:"node,omitempty"`
	AlertID                    string         `json:"alertId,omitempty"`
	ApplicationID              string         `json:"applicationId,omitempty"`
	ReleaseBundleID            string         `json:"releaseBundleId,omitempty"`
	DockerHostID               string         `json:"dockerHostId,omitempty"`
	DockerServiceID            string         `json:"dockerServiceId,omitempty"`
	VirtualizationConnectionID string         `json:"virtualizationConnectionId,omitempty"`
	VMID                       string         `json:"vmId,omitempty"`
	TimeRangeMinutes           int            `json:"timeRangeMinutes,omitempty"`
	VisibleFilters             map[string]any `json:"visibleFilters,omitempty"`
	PinnedData                 map[string]any `json:"pinnedData,omitempty"`
}

type WorkbenchSelectionContext struct {
	Text               string `json:"text,omitempty"`
	Kind               string `json:"kind,omitempty"`
	SourceElementLabel string `json:"sourceElementLabel,omitempty"`
}

type SessionToolset struct {
	EnabledAdapterIDs []string       `json:"enabledAdapterIds,omitempty"`
	EnabledSkillIDs   []string       `json:"enabledSkillIds,omitempty"`
	DisabledToolNames []string       `json:"disabledToolNames,omitempty"`
	BudgetOverrides   map[string]any `json:"budgetOverrides,omitempty"`
	ScopeOverrides    map[string]any `json:"scopeOverrides,omitempty"`
}

type AnalysisRunRef struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type SessionMetadata struct {
	Mode            string           `json:"mode,omitempty"`
	Status          string           `json:"status,omitempty"`
	AgentProviderID string           `json:"agentProviderId,omitempty"`
	Scope           SessionScope     `json:"scope,omitempty"`
	PinnedContext   map[string]any   `json:"pinnedContext,omitempty"`
	Toolset         SessionToolset   `json:"toolset,omitempty"`
	AnalysisRunRefs []AnalysisRunRef `json:"analysisRunRefs,omitempty"`
	Summary         string           `json:"summary,omitempty"`
	Tags            []string         `json:"tags,omitempty"`
	ArchivedAt      string           `json:"archivedAt,omitempty"`
	Source          string           `json:"source,omitempty"`
}

type Session struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	CreatedBy string         `json:"createdBy"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type Message struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type ToolExecution struct {
	ID          string         `json:"id"`
	AdapterID   string         `json:"adapterId"`
	ToolName    string         `json:"toolName"`
	Status      string         `json:"status"`
	Summary     string         `json:"summary,omitempty"`
	Input       map[string]any `json:"input,omitempty"`
	Output      map[string]any `json:"output,omitempty"`
	StartedAt   time.Time      `json:"startedAt"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
}

type AnalysisGraphNode struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Title       string         `json:"title"`
	Subtitle    string         `json:"subtitle,omitempty"`
	Severity    string         `json:"severity,omitempty"`
	EvidenceIDs []string       `json:"evidenceIds,omitempty"`
	SourceRefs  []string       `json:"sourceRefs,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
}

type AnalysisGraphEdge struct {
	ID          string         `json:"id"`
	Source      string         `json:"source"`
	Target      string         `json:"target"`
	Relation    string         `json:"relation"`
	Severity    string         `json:"severity,omitempty"`
	EvidenceIDs []string       `json:"evidenceIds,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
}

type AnalysisGraph struct {
	Layout      string              `json:"layout,omitempty"`
	FocusNodeID string              `json:"focusNodeId,omitempty"`
	Nodes       []AnalysisGraphNode `json:"nodes,omitempty"`
	Edges       []AnalysisGraphEdge `json:"edges,omitempty"`
}

type AnalysisArtifact struct {
	Kind               string                `json:"kind"`
	RunID              string                `json:"runId"`
	Title              string                `json:"title,omitempty"`
	Summary            string                `json:"summary"`
	Scope              SessionScope          `json:"scope,omitempty"`
	Evidence           []RootCauseEvidence   `json:"evidence,omitempty"`
	Hypotheses         []RootCauseHypothesis `json:"hypotheses,omitempty"`
	Recommendations    []string              `json:"recommendations,omitempty"`
	ToolExecutions     []ToolExecution       `json:"toolExecutions,omitempty"`
	Graph              *AnalysisGraph        `json:"graph,omitempty"`
	DataSourceSnapshot map[string]any        `json:"dataSourceSnapshot,omitempty"`
}

type SessionMessageEnvelope struct {
	Messages          []Message          `json:"messages"`
	ToolCalls         []ToolExecution    `json:"toolCalls,omitempty"`
	AnalysisArtifacts []AnalysisArtifact `json:"analysisArtifacts,omitempty"`
	SessionPatch      map[string]any     `json:"sessionPatch,omitempty"`
}

type WorkbenchSendMessageInput struct {
	Content          string                     `json:"content"`
	Mode             string                     `json:"mode,omitempty"`
	AgentProviderID  string                     `json:"agentProviderId,omitempty"`
	Toolset          SessionToolset             `json:"toolset,omitempty"`
	ScopeOverrides   map[string]any             `json:"scopeOverrides,omitempty"`
	Source           string                     `json:"source,omitempty"`
	LaunchContext    *WorkbenchLaunchContext    `json:"launchContext,omitempty"`
	SelectionContext *WorkbenchSelectionContext `json:"selectionContext,omitempty"`
	PinnedContext    map[string]any             `json:"pinnedContext,omitempty"`
	EventSink        WorkbenchStreamEventSink   `json:"-"`
}

type WorkbenchStreamEventSink func(WorkbenchStreamEvent) bool

type WorkbenchStreamResult struct {
	Envelope SessionMessageEnvelope `json:"envelope"`
	Events   []WorkbenchStreamEvent `json:"events,omitempty"`
}

type WorkbenchGlobalAssistantEventInput struct {
	Action           string                     `json:"action"`
	LaunchContext    *WorkbenchLaunchContext    `json:"launchContext,omitempty"`
	SelectionContext *WorkbenchSelectionContext `json:"selectionContext,omitempty"`
	Prompt           string                     `json:"prompt,omitempty"`
	SessionID        string                     `json:"sessionId,omitempty"`
	Source           string                     `json:"source,omitempty"`
}

type WorkbenchToolCall struct {
	ID            string     `json:"id"`
	AdapterID     string     `json:"adapterId"`
	ToolName      string     `json:"toolName"`
	SkillID       string     `json:"skillId,omitempty"`
	SkillName     string     `json:"skillName,omitempty"`
	CapabilityID  string     `json:"capabilityId,omitempty"`
	Status        string     `json:"status"`
	InputPreview  any        `json:"inputPreview,omitempty"`
	OutputPreview any        `json:"outputPreview,omitempty"`
	Summary       string     `json:"summary,omitempty"`
	EvidenceRefs  []string   `json:"evidenceRefs,omitempty"`
	ArtifactRefs  []string   `json:"artifactRefs,omitempty"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	DurationMs    int64      `json:"durationMs,omitempty"`
}

type WorkbenchSource struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type WorkbenchStreamEvent struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	SessionID    string             `json:"sessionId"`
	RunID        string             `json:"runId,omitempty"`
	MessageID    string             `json:"messageId,omitempty"`
	Sequence     int                `json:"sequence"`
	CreatedAt    time.Time          `json:"createdAt"`
	Role         string             `json:"role,omitempty"`
	ContentDelta string             `json:"contentDelta,omitempty"`
	Content      string             `json:"content,omitempty"`
	Metadata     map[string]any     `json:"metadata,omitempty"`
	TextDelta    string             `json:"textDelta,omitempty"`
	Summary      string             `json:"summary,omitempty"`
	Collapsed    bool               `json:"collapsed,omitempty"`
	ProviderID   string             `json:"providerId,omitempty"`
	ProviderKind string             `json:"providerKind,omitempty"`
	Status       string             `json:"status,omitempty"`
	ToolCall     *WorkbenchToolCall `json:"toolCall,omitempty"`
	ToolCallID   string             `json:"toolCallId,omitempty"`
	OutputDelta  string             `json:"outputDelta,omitempty"`
	LogDelta     string             `json:"logDelta,omitempty"`
	Artifact     any                `json:"artifact,omitempty"`
	Source       *WorkbenchSource   `json:"source,omitempty"`
	SurfaceID    string             `json:"surfaceId,omitempty"`
	Command      any                `json:"command,omitempty"`
	Message      string             `json:"message,omitempty"`
	Code         string             `json:"code,omitempty"`
	Retryable    *bool              `json:"retryable,omitempty"`
}

type Insight struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Severity    string   `json:"severity"`
	Actions     []string `json:"actions,omitempty"`
}

type RootCauseRun struct {
	ID                 string                `json:"id"`
	Kind               string                `json:"kind,omitempty"`
	SessionID          string                `json:"sessionId,omitempty"`
	Title              string                `json:"title"`
	CreatedBy          string                `json:"createdBy"`
	AnalysisProfileID  string                `json:"analysisProfileId,omitempty"`
	TriggerType        string                `json:"triggerType,omitempty"`
	Status             string                `json:"status"`
	Severity           string                `json:"severity"`
	Summary            string                `json:"summary"`
	ClusterID          string                `json:"clusterId,omitempty"`
	Namespace          string                `json:"namespace,omitempty"`
	WorkloadKind       string                `json:"workloadKind,omitempty"`
	WorkloadName       string                `json:"workloadName,omitempty"`
	AlertID            string                `json:"alertId,omitempty"`
	TimeRangeMinutes   int                   `json:"timeRangeMinutes"`
	Question           string                `json:"question,omitempty"`
	Evidence           []RootCauseEvidence   `json:"evidence,omitempty"`
	Hypotheses         []RootCauseHypothesis `json:"hypotheses,omitempty"`
	Recommendations    []string              `json:"recommendations,omitempty"`
	ToolExecutions     []ToolExecution       `json:"toolExecutions,omitempty"`
	DataSourceSnapshot map[string]any        `json:"dataSourceSnapshot,omitempty"`
	PlaybookResults    map[string]any        `json:"playbookResults,omitempty"`
	RemediationPlan    map[string]any        `json:"remediationPlan,omitempty"`
	DedupKey           string                `json:"dedupKey,omitempty"`
	CreatedAt          time.Time             `json:"createdAt"`
	UpdatedAt          time.Time             `json:"updatedAt"`
}

type RootCauseRunInput struct {
	Title             string `json:"title"`
	Kind              string `json:"kind,omitempty"`
	SessionID         string `json:"sessionId,omitempty"`
	AnalysisProfileID string `json:"analysisProfileId,omitempty"`
	AgentProviderID   string `json:"agentProviderId,omitempty"`
	TriggerType       string `json:"triggerType,omitempty"`
	ClusterID         string `json:"clusterId,omitempty"`
	Namespace         string `json:"namespace,omitempty"`
	WorkloadKind      string `json:"workloadKind,omitempty"`
	WorkloadName      string `json:"workloadName,omitempty"`
	AlertID           string `json:"alertId,omitempty"`
	TimeRangeMinutes  int    `json:"timeRangeMinutes"`
	Question          string `json:"question,omitempty"`
}

type RootCauseRunFilter struct {
	ClusterID      string
	AlertID        string
	TriggerType    string
	DedupKey       string
	DedupKeyPrefix string
	Limit          int
}

type RootCauseEvidence struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	Summary    string         `json:"summary"`
	Severity   string         `json:"severity,omitempty"`
	ClusterID  string         `json:"clusterId,omitempty"`
	Namespace  string         `json:"namespace,omitempty"`
	Timestamp  *time.Time     `json:"timestamp,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type RootCauseHypothesis struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Summary         string   `json:"summary"`
	Confidence      int      `json:"confidence"`
	EvidenceIDs     []string `json:"evidenceIds,omitempty"`
	Recommendations []string `json:"recommendations,omitempty"`
}

type InspectionTask struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	ScopeType       string         `json:"scopeType"`
	ClusterID       string         `json:"clusterId,omitempty"`
	Namespace       string         `json:"namespace,omitempty"`
	Checks          []string       `json:"checks,omitempty"`
	Enabled         bool           `json:"enabled"`
	IntervalMinutes int            `json:"intervalMinutes"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedBy       string         `json:"createdBy"`
	LastRunAt       *time.Time     `json:"lastRunAt,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type InspectionTaskInput struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	ScopeType       string         `json:"scopeType"`
	ClusterID       string         `json:"clusterId,omitempty"`
	Namespace       string         `json:"namespace,omitempty"`
	Checks          []string       `json:"checks,omitempty"`
	Enabled         bool           `json:"enabled"`
	IntervalMinutes int            `json:"intervalMinutes"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type InspectionFinding struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	Severity       string         `json:"severity"`
	Summary        string         `json:"summary"`
	Recommendation string         `json:"recommendation,omitempty"`
	Source         string         `json:"source"`
	Data           map[string]any `json:"data,omitempty"`
}

type InspectionRun struct {
	ID          string              `json:"id"`
	TaskID      string              `json:"taskId"`
	TriggeredBy string              `json:"triggeredBy"`
	Status      string              `json:"status"`
	Severity    string              `json:"severity"`
	Summary     string              `json:"summary"`
	Findings    []InspectionFinding `json:"findings,omitempty"`
	Report      map[string]any      `json:"report,omitempty"`
	StartedAt   time.Time           `json:"startedAt"`
	CompletedAt *time.Time          `json:"completedAt,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
}

type InspectionRunFilter struct {
	TaskID     string
	ClusterID  string
	Namespace  string
	Check      string
	LatestOnly bool
	Limit      int
}

type Repository interface {
	ListSessions(context.Context, string, int) ([]Session, error)
	GetSession(context.Context, string, string) (Session, error)
	CreateSession(context.Context, Session) (Session, error)
	UpdateSession(context.Context, string, string, Session) (Session, error)
	DeleteSession(context.Context, string, string) error
	ListMessages(context.Context, string, int) ([]Message, error)
	CreateMessage(context.Context, Message) (Message, error)
	ListRootCauseRuns(context.Context, string, RootCauseRunFilter) ([]RootCauseRun, error)
	GetRootCauseRun(context.Context, string, string) (RootCauseRun, error)
	CreateRootCauseRun(context.Context, RootCauseRun) (RootCauseRun, error)
	UpdateRootCauseRun(context.Context, RootCauseRun) (RootCauseRun, error)
	GetAnalysisProfile(context.Context, string) (AnalysisProfile, error)
	ListInspectionTasks(context.Context, string, int) ([]InspectionTask, error)
	GetInspectionTask(context.Context, string, string) (InspectionTask, error)
	ListDueInspectionTasks(context.Context, time.Time, int) ([]InspectionTask, error)
	CreateInspectionTask(context.Context, InspectionTask) (InspectionTask, error)
	UpdateInspectionTask(context.Context, string, string, InspectionTaskInput) (InspectionTask, error)
	TouchInspectionTaskRun(context.Context, string, time.Time) error
	ListInspectionRuns(context.Context, string, InspectionRunFilter) ([]InspectionRun, error)
	CreateInspectionRun(context.Context, InspectionRun) (InspectionRun, error)
}
