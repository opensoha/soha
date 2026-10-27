package resource

const (
	PodLogsMaxContentBytes = 256 * 1024
	PodExecMaxOutputBytes  = 128 * 1024
	StorageRelationLimit   = 200
)

type ResourceScopeMode string

const (
	ResourceScopeModeCluster   ResourceScopeMode = "cluster"
	ResourceScopeModeNamespace ResourceScopeMode = "namespace"
)

type ResourceRef struct {
	ClusterID       string            `json:"clusterId"`
	APIVersion      string            `json:"apiVersion"`
	Kind            string            `json:"kind"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace,omitempty"`
	ScopeMode       ResourceScopeMode `json:"scopeMode"`
	UID             string            `json:"uid,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
}

type ResourceSearchItem struct {
	Resource ResourceRef `json:"resource"`
	Status   string      `json:"status,omitempty"`
}

type ResourceSearchResult struct {
	Items     []ResourceSearchItem `json:"items"`
	Truncated bool                 `json:"truncated"`
}

type ResourceStreamEvent struct {
	Type            string       `json:"type"`
	ClusterID       string       `json:"clusterId"`
	ObservedAt      string       `json:"observedAt"`
	Source          string       `json:"source,omitempty"`
	Resource        *ResourceRef `json:"resource,omitempty"`
	ResourceVersion string       `json:"resourceVersion,omitempty"`
	CacheStatus     string       `json:"cacheStatus,omitempty"`
	Message         string       `json:"message,omitempty"`
	ResyncRequired  bool         `json:"resyncRequired,omitempty"`
}

type ResourceGraphNode struct {
	ID       string      `json:"id"`
	Resource ResourceRef `json:"resource"`
	Status   string      `json:"status,omitempty"`
	Health   string      `json:"health,omitempty"`
}

type ResourceGraphEdge struct {
	ID       string `json:"id"`
	SourceID string `json:"sourceId"`
	TargetID string `json:"targetId"`
	Relation string `json:"relation"`
}

type ResourceEvidence struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Summary    string `json:"summary"`
	ObservedAt string `json:"observedAt"`
	ResourceID string `json:"resourceId,omitempty"`
	SourceRef  string `json:"sourceRef,omitempty"`
}

type ResourceGraph struct {
	ClusterID   string              `json:"clusterId"`
	Namespace   string              `json:"namespace,omitempty"`
	GeneratedAt string              `json:"generatedAt"`
	RootID      string              `json:"rootId,omitempty"`
	Nodes       []ResourceGraphNode `json:"nodes"`
	Edges       []ResourceGraphEdge `json:"edges"`
	Evidence    []ResourceEvidence  `json:"evidence"`
	Warnings    []string            `json:"warnings"`
}

type ResourceSearchInput struct {
	Query     string
	Namespace string
	Kinds     []string
	Limit     int
}

type NamespaceView struct {
	Name           string            `json:"name"`
	Status         string            `json:"status"`
	Labels         map[string]string `json:"labels"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
}

type NamespaceUpsertInput struct {
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type PodView struct {
	Name                   string                   `json:"name"`
	Namespace              string                   `json:"namespace"`
	Phase                  string                   `json:"phase"`
	NodeName               string                   `json:"nodeName,omitempty"`
	PodIP                  string                   `json:"podIp,omitempty"`
	CreatedAt              string                   `json:"createdAt,omitempty"`
	CPU                    string                   `json:"cpu,omitempty"`
	Memory                 string                   `json:"memory,omitempty"`
	Requests               ResourceQuantityView     `json:"requests,omitempty"`
	Limits                 ResourceQuantityView     `json:"limits,omitempty"`
	Labels                 map[string]string        `json:"labels,omitempty"`
	PersistentVolumeClaims []string                 `json:"persistentVolumeClaims,omitempty"`
	ReadyContainers        string                   `json:"readyContainers"`
	QOSClass               string                   `json:"qosClass,omitempty"`
	ContainerStatuses      []PodContainerStatusView `json:"containerStatuses,omitempty"`
	Restarts               int32                    `json:"restarts"`
	AgeSeconds             int64                    `json:"ageSeconds"`
	AllowedActions         []string                 `json:"allowedActions,omitempty"`
}

type PodContainerStatusView struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Ready    bool   `json:"ready"`
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
	ExitCode *int32 `json:"exitCode,omitempty"`
}

type WorkloadOverviewNamespaceView struct {
	Namespace      string `json:"namespace"`
	TotalPods      int    `json:"totalPods"`
	RunningPods    int    `json:"runningPods"`
	AtRiskPods     int    `json:"atRiskPods"`
	RestartingPods int    `json:"restartingPods"`
}

type WorkloadOverviewPodView struct {
	Name            string `json:"name"`
	Namespace       string `json:"namespace"`
	Phase           string `json:"phase"`
	ReadyContainers string `json:"readyContainers"`
	Restarts        int32  `json:"restarts"`
	NodeName        string `json:"nodeName,omitempty"`
	AgeSeconds      int64  `json:"ageSeconds"`
}

type WorkloadOverviewView struct {
	ClusterID          string                          `json:"clusterId"`
	Namespace          string                          `json:"namespace,omitempty"`
	Source             string                          `json:"source"`
	GeneratedAt        string                          `json:"generatedAt"`
	TotalPods          int                             `json:"totalPods"`
	RunningPods        int                             `json:"runningPods"`
	PendingPods        int                             `json:"pendingPods"`
	SucceededPods      int                             `json:"succeededPods"`
	FailedPods         int                             `json:"failedPods"`
	UnknownPods        int                             `json:"unknownPods"`
	RestartingPods     int                             `json:"restartingPods"`
	AtRiskPods         int                             `json:"atRiskPods"`
	NamespaceBreakdown []WorkloadOverviewNamespaceView `json:"namespaceBreakdown,omitempty"`
	ProblematicPods    []WorkloadOverviewPodView       `json:"problematicPods,omitempty"`
}

type WorkloadConditionView struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

type WorkloadContainerView struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Role         string `json:"role,omitempty"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state,omitempty"`
	LastState    string `json:"lastState,omitempty"`
	ContainerID  string `json:"containerId,omitempty"`
	StartedAt    string `json:"startedAt,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
}

type PodVolumeMountView struct {
	Name        string `json:"name"`
	MountPath   string `json:"mountPath"`
	SubPath     string `json:"subPath,omitempty"`
	ReadOnly    bool   `json:"readOnly"`
	VolumeType  string `json:"volumeType,omitempty"`
	SourceName  string `json:"sourceName,omitempty"`
	Description string `json:"description,omitempty"`
}

type PodVolumeView struct {
	Name                 string               `json:"name"`
	Type                 string               `json:"type"`
	SourceName           string               `json:"sourceName,omitempty"`
	ReadOnly             bool                 `json:"readOnly"`
	Details              []string             `json:"details,omitempty"`
	VolumeMounts         []PodVolumeMountView `json:"volumeMounts,omitempty"`
	ReferencedConfigMaps []string             `json:"referencedConfigMaps,omitempty"`
}

type PodRelatedResourceView struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Namespace string   `json:"namespace,omitempty"`
	Relations []string `json:"relations,omitempty"`
	Details   []string `json:"details,omitempty"`
}

type WorkloadRelationView struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Relation  string `json:"relation,omitempty"`
}

type ResourceQuantityView struct {
	CPU              string `json:"cpu,omitempty"`
	Memory           string `json:"memory,omitempty"`
	EphemeralStorage string `json:"ephemeralStorage,omitempty"`
	Pods             string `json:"pods,omitempty"`
}

type ResourcePercentageView struct {
	CPU              float64 `json:"cpu,omitempty"`
	Memory           float64 `json:"memory,omitempty"`
	EphemeralStorage float64 `json:"ephemeralStorage,omitempty"`
	Pods             float64 `json:"pods,omitempty"`
}

type NodeResourceSummaryView struct {
	Capacity           ResourceQuantityView   `json:"capacity,omitempty"`
	Allocatable        ResourceQuantityView   `json:"allocatable,omitempty"`
	Requests           ResourceQuantityView   `json:"requests,omitempty"`
	Limits             ResourceQuantityView   `json:"limits,omitempty"`
	Usage              ResourceQuantityView   `json:"usage,omitempty"`
	RequestPercentages ResourcePercentageView `json:"requestPercentages,omitempty"`
	LimitPercentages   ResourcePercentageView `json:"limitPercentages,omitempty"`
	UsagePercentages   ResourcePercentageView `json:"usagePercentages,omitempty"`
}

type PodDetailView struct {
	Name               string                   `json:"name"`
	Namespace          string                   `json:"namespace"`
	Phase              string                   `json:"phase"`
	PodIP              string                   `json:"podIp,omitempty"`
	HostIP             string                   `json:"hostIp,omitempty"`
	NodeName           string                   `json:"nodeName,omitempty"`
	ServiceAccountName string                   `json:"serviceAccountName,omitempty"`
	QOSClass           string                   `json:"qosClass,omitempty"`
	CreatedAt          string                   `json:"createdAt,omitempty"`
	StartTime          string                   `json:"startTime,omitempty"`
	Requests           ResourceQuantityView     `json:"requests,omitempty"`
	Limits             ResourceQuantityView     `json:"limits,omitempty"`
	Labels             map[string]string        `json:"labels,omitempty"`
	Annotations        map[string]string        `json:"annotations,omitempty"`
	Containers         []WorkloadContainerView  `json:"containers,omitempty"`
	Conditions         []WorkloadConditionView  `json:"conditions,omitempty"`
	Volumes            []PodVolumeView          `json:"volumes,omitempty"`
	RelatedResources   []PodRelatedResourceView `json:"relatedResources,omitempty"`
	AllowedActions     []string                 `json:"allowedActions,omitempty"`
}

type PodLogsView struct {
	PodName      string `json:"podName"`
	Namespace    string `json:"namespace"`
	Container    string `json:"container,omitempty"`
	Content      string `json:"content"`
	ContentBytes int64  `json:"contentBytes"`
	MaxBytes     int64  `json:"maxBytes,omitempty"`
	TailLines    int64  `json:"tailLines,omitempty"`
	Previous     bool   `json:"previous,omitempty"`
	Truncated    bool   `json:"truncated"`
}

type PodExecView struct {
	PodName         string `json:"podName"`
	Namespace       string `json:"namespace"`
	Container       string `json:"container,omitempty"`
	Command         string `json:"command"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutBytes     int64  `json:"stdoutBytes"`
	StderrBytes     int64  `json:"stderrBytes"`
	MaxBytes        int64  `json:"maxBytes,omitempty"`
	StdoutTruncated bool   `json:"stdoutTruncated,omitempty"`
	StderrTruncated bool   `json:"stderrTruncated,omitempty"`
	Success         bool   `json:"success"`
	ExitMessage     string `json:"exitMessage,omitempty"`
	ExecutedAt      string `json:"executedAt"`
}

type MetricPointView struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

type MetricSeriesView struct {
	Key    string            `json:"key"`
	Label  string            `json:"label"`
	Unit   string            `json:"unit"`
	Latest float64           `json:"latest"`
	Points []MetricPointView `json:"points,omitempty"`
}

type PodMetricsView struct {
	PodName        string             `json:"podName"`
	Namespace      string             `json:"namespace"`
	Configured     bool               `json:"configured"`
	Source         string             `json:"source"`
	GeneratedAt    string             `json:"generatedAt"`
	RangeMinutes   int                `json:"rangeMinutes"`
	StepSeconds    int                `json:"stepSeconds"`
	Message        string             `json:"message,omitempty"`
	GrafanaBaseURL string             `json:"grafanaBaseUrl,omitempty"`
	Series         []MetricSeriesView `json:"series,omitempty"`
}

type ResourceMetricsView struct {
	ResourceKind   string             `json:"resourceKind"`
	ResourceName   string             `json:"resourceName"`
	Namespace      string             `json:"namespace,omitempty"`
	Configured     bool               `json:"configured"`
	Source         string             `json:"source"`
	GeneratedAt    string             `json:"generatedAt"`
	RangeMinutes   int                `json:"rangeMinutes"`
	StepSeconds    int                `json:"stepSeconds"`
	Message        string             `json:"message,omitempty"`
	GrafanaBaseURL string             `json:"grafanaBaseUrl,omitempty"`
	Series         []MetricSeriesView `json:"series,omitempty"`
}

type ResourceYAMLView struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Content   string `json:"content"`
}

type ResourceUpdatePlanRequest struct {
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Content   string `json:"content"`
}

type ManagedFieldOwner struct {
	Manager    string   `json:"manager"`
	Operation  string   `json:"operation"`
	APIVersion string   `json:"apiVersion"`
	Time       string   `json:"time,omitempty"`
	Fields     []string `json:"fields"`
}

type FieldConflict struct {
	Field   string `json:"field"`
	Manager string `json:"manager,omitempty"`
	Message string `json:"message"`
}

type ResourceUpdateAnalysis struct {
	FieldManager  string              `json:"fieldManager"`
	ChangedFields []string            `json:"changedFields"`
	Owners        []ManagedFieldOwner `json:"owners"`
	Conflicts     []FieldConflict     `json:"conflicts"`
}

type SecuritySeverityCounts struct {
	Critical int64 `json:"critical"`
	High     int64 `json:"high"`
	Medium   int64 `json:"medium"`
	Low      int64 `json:"low"`
	Unknown  int64 `json:"unknown"`
}

type SecurityFinding struct {
	ID          string       `json:"id"`
	Category    string       `json:"category"`
	Severity    string       `json:"severity"`
	Title       string       `json:"title"`
	Status      string       `json:"status"`
	ControlID   string       `json:"controlId,omitempty"`
	Message     string       `json:"message,omitempty"`
	Remediation string       `json:"remediation,omitempty"`
	ObservedAt  string       `json:"observedAt,omitempty"`
	Resource    *ResourceRef `json:"resource,omitempty"`
}

type SecurityPosture struct {
	ClusterID   string                 `json:"clusterId"`
	Provider    string                 `json:"provider"`
	Status      string                 `json:"status"`
	GeneratedAt string                 `json:"generatedAt"`
	Message     string                 `json:"message,omitempty"`
	Counts      SecuritySeverityCounts `json:"counts"`
	Findings    []SecurityFinding      `json:"findings"`
	Warnings    []string               `json:"warnings"`
}

type AccessReviewSubject struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type AccessReviewCheck struct {
	Verb      string `json:"verb"`
	Group     string `json:"group,omitempty"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

type SubjectAccessReviewInput struct {
	Subject AccessReviewSubject `json:"subject"`
	Checks  []AccessReviewCheck `json:"checks"`
}

type AccessReviewDecision struct {
	Check           AccessReviewCheck `json:"check"`
	Allowed         bool              `json:"allowed"`
	Denied          bool              `json:"denied"`
	Reason          string            `json:"reason,omitempty"`
	EvaluationError string            `json:"evaluationError,omitempty"`
}

type SubjectAccessReviewResult struct {
	Subject   AccessReviewSubject    `json:"subject"`
	Decisions []AccessReviewDecision `json:"decisions"`
}

type DeploymentView struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	Labels          map[string]string `json:"labels,omitempty"`
	DesiredReplicas int32             `json:"desiredReplicas"`
	ReadyReplicas   int32             `json:"readyReplicas"`
	UpdatedReplicas int32             `json:"updatedReplicas"`
	Available       int32             `json:"available"`
	AgeSeconds      int64             `json:"ageSeconds"`
	AllowedActions  []string          `json:"allowedActions,omitempty"`
}

type DeploymentDetailView struct {
	Name               string                  `json:"name"`
	Namespace          string                  `json:"namespace"`
	DesiredReplicas    int32                   `json:"desiredReplicas"`
	ReadyReplicas      int32                   `json:"readyReplicas"`
	UpdatedReplicas    int32                   `json:"updatedReplicas"`
	AvailableReplicas  int32                   `json:"availableReplicas"`
	ObservedGeneration int64                   `json:"observedGeneration"`
	Strategy           string                  `json:"strategy"`
	CreatedAt          string                  `json:"createdAt,omitempty"`
	Labels             map[string]string       `json:"labels,omitempty"`
	Annotations        map[string]string       `json:"annotations,omitempty"`
	Selector           map[string]string       `json:"selector,omitempty"`
	Containers         []WorkloadContainerView `json:"containers,omitempty"`
	Conditions         []WorkloadConditionView `json:"conditions,omitempty"`
	Pods               []PodView               `json:"pods,omitempty"`
	RelatedResources   []WorkloadRelationView  `json:"relatedResources,omitempty"`
	AllowedActions     []string                `json:"allowedActions,omitempty"`
}

type RolloutHistoryView struct {
	Name          string   `json:"name"`
	Namespace     string   `json:"namespace"`
	Revision      string   `json:"revision"`
	Images        []string `json:"images,omitempty"`
	Replicas      int32    `json:"replicas"`
	ReadyReplicas int32    `json:"readyReplicas"`
	CreatedAt     string   `json:"createdAt,omitempty"`
}

type DeploymentRolloutStatusView struct {
	Name               string                  `json:"name"`
	Namespace          string                  `json:"namespace"`
	Revision           string                  `json:"revision"`
	Status             string                  `json:"status"`
	Message            string                  `json:"message"`
	DesiredReplicas    int32                   `json:"desiredReplicas"`
	UpdatedReplicas    int32                   `json:"updatedReplicas"`
	ReadyReplicas      int32                   `json:"readyReplicas"`
	AvailableReplicas  int32                   `json:"availableReplicas"`
	ObservedGeneration int64                   `json:"observedGeneration"`
	Conditions         []WorkloadConditionView `json:"conditions,omitempty"`
}

type DeploymentRollbackView struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	TargetRevision string `json:"targetRevision"`
	Message        string `json:"message"`
	RequestedAt    string `json:"requestedAt"`
}

type StatefulSetView struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	ServiceName     string   `json:"serviceName,omitempty"`
	DesiredReplicas int32    `json:"desiredReplicas"`
	ReadyReplicas   int32    `json:"readyReplicas"`
	CurrentReplicas int32    `json:"currentReplicas"`
	AgeSeconds      int64    `json:"ageSeconds"`
	AllowedActions  []string `json:"allowedActions,omitempty"`
}

type StatefulSetDetailView struct {
	Name             string                 `json:"name"`
	Namespace        string                 `json:"namespace"`
	ServiceName      string                 `json:"serviceName,omitempty"`
	DesiredReplicas  int32                  `json:"desiredReplicas"`
	ReadyReplicas    int32                  `json:"readyReplicas"`
	CurrentReplicas  int32                  `json:"currentReplicas"`
	UpdateStrategy   string                 `json:"updateStrategy,omitempty"`
	CurrentRevision  string                 `json:"currentRevision,omitempty"`
	UpdateRevision   string                 `json:"updateRevision,omitempty"`
	CreatedAt        string                 `json:"createdAt,omitempty"`
	Labels           map[string]string      `json:"labels,omitempty"`
	Annotations      map[string]string      `json:"annotations,omitempty"`
	Selector         map[string]string      `json:"selector,omitempty"`
	Pods             []PodView              `json:"pods,omitempty"`
	RelatedResources []WorkloadRelationView `json:"relatedResources,omitempty"`
	AllowedActions   []string               `json:"allowedActions,omitempty"`
}

type DaemonSetView struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	DesiredNumber   int32    `json:"desiredNumber"`
	CurrentNumber   int32    `json:"currentNumber"`
	ReadyNumber     int32    `json:"readyNumber"`
	AvailableNumber int32    `json:"availableNumber"`
	UpdatedNumber   int32    `json:"updatedNumber"`
	AgeSeconds      int64    `json:"ageSeconds"`
	AllowedActions  []string `json:"allowedActions,omitempty"`
}

type DaemonSetDetailView struct {
	Name             string                 `json:"name"`
	Namespace        string                 `json:"namespace"`
	DesiredNumber    int32                  `json:"desiredNumber"`
	CurrentNumber    int32                  `json:"currentNumber"`
	ReadyNumber      int32                  `json:"readyNumber"`
	AvailableNumber  int32                  `json:"availableNumber"`
	UpdatedNumber    int32                  `json:"updatedNumber"`
	UpdateStrategy   string                 `json:"updateStrategy,omitempty"`
	CreatedAt        string                 `json:"createdAt,omitempty"`
	Labels           map[string]string      `json:"labels,omitempty"`
	Annotations      map[string]string      `json:"annotations,omitempty"`
	Selector         map[string]string      `json:"selector,omitempty"`
	Pods             []PodView              `json:"pods,omitempty"`
	RelatedResources []WorkloadRelationView `json:"relatedResources,omitempty"`
	AllowedActions   []string               `json:"allowedActions,omitempty"`
}

type JobView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	Completions    int32    `json:"completions"`
	Succeeded      int32    `json:"succeeded"`
	Failed         int32    `json:"failed"`
	Active         int32    `json:"active"`
	CompletionMode string   `json:"completionMode,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type JobDetailView struct {
	Name             string                 `json:"name"`
	Namespace        string                 `json:"namespace"`
	Completions      int32                  `json:"completions"`
	Parallelism      int32                  `json:"parallelism"`
	Succeeded        int32                  `json:"succeeded"`
	Failed           int32                  `json:"failed"`
	Active           int32                  `json:"active"`
	CompletionMode   string                 `json:"completionMode,omitempty"`
	CreatedAt        string                 `json:"createdAt,omitempty"`
	StartTime        string                 `json:"startTime,omitempty"`
	CompletionTime   string                 `json:"completionTime,omitempty"`
	Labels           map[string]string      `json:"labels,omitempty"`
	Annotations      map[string]string      `json:"annotations,omitempty"`
	Pods             []PodView              `json:"pods,omitempty"`
	RelatedResources []WorkloadRelationView `json:"relatedResources,omitempty"`
	AllowedActions   []string               `json:"allowedActions,omitempty"`
}

type CronJobView struct {
	Name             string   `json:"name"`
	Namespace        string   `json:"namespace"`
	Schedule         string   `json:"schedule"`
	Suspend          bool     `json:"suspend"`
	ActiveJobs       int32    `json:"activeJobs"`
	LastScheduleTime string   `json:"lastScheduleTime,omitempty"`
	AgeSeconds       int64    `json:"ageSeconds"`
	AllowedActions   []string `json:"allowedActions,omitempty"`
}

type CronJobDetailView struct {
	Name              string                 `json:"name"`
	Namespace         string                 `json:"namespace"`
	Schedule          string                 `json:"schedule"`
	Suspend           bool                   `json:"suspend"`
	ActiveJobs        int32                  `json:"activeJobs"`
	LastScheduleTime  string                 `json:"lastScheduleTime,omitempty"`
	ConcurrencyPolicy string                 `json:"concurrencyPolicy,omitempty"`
	TimeZone          string                 `json:"timeZone,omitempty"`
	CreatedAt         string                 `json:"createdAt,omitempty"`
	Labels            map[string]string      `json:"labels,omitempty"`
	Annotations       map[string]string      `json:"annotations,omitempty"`
	Jobs              []JobView              `json:"jobs,omitempty"`
	RelatedResources  []WorkloadRelationView `json:"relatedResources,omitempty"`
	AllowedActions    []string               `json:"allowedActions,omitempty"`
}

type ServicePortView struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	TargetPort string `json:"targetPort"`
	Port       int32  `json:"port"`
	NodePort   int32  `json:"nodePort,omitempty"`
}

type ServiceView struct {
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	Type           string            `json:"type"`
	ClusterIP      string            `json:"clusterIp,omitempty"`
	Ports          []string          `json:"ports,omitempty"`
	PortMappings   []ServicePortView `json:"portMappings,omitempty"`
	Selector       map[string]string `json:"selector,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
}

type ServiceEndpointView struct {
	Address     string `json:"address"`
	Ready       *bool  `json:"ready,omitempty"`
	Serving     *bool  `json:"serving,omitempty"`
	Terminating *bool  `json:"terminating,omitempty"`
	TargetRef   string `json:"targetRef,omitempty"`
	NodeName    string `json:"nodeName,omitempty"`
	Zone        string `json:"zone,omitempty"`
}

type ServiceDetailView struct {
	Name           string                `json:"name"`
	Namespace      string                `json:"namespace"`
	Type           string                `json:"type"`
	ClusterIP      string                `json:"clusterIp,omitempty"`
	Ports          []string              `json:"ports,omitempty"`
	PortMappings   []ServicePortView     `json:"portMappings,omitempty"`
	Selector       map[string]string     `json:"selector,omitempty"`
	Labels         map[string]string     `json:"labels,omitempty"`
	Annotations    map[string]string     `json:"annotations,omitempty"`
	Endpoints      []ServiceEndpointView `json:"endpoints,omitempty"`
	BackendPods    []PodView             `json:"backendPods,omitempty"`
	AgeSeconds     int64                 `json:"ageSeconds"`
	AllowedActions []string              `json:"allowedActions,omitempty"`
}

type IngressView struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	ClassName       string   `json:"className,omitempty"`
	Hosts           []string `json:"hosts,omitempty"`
	Address         string   `json:"address,omitempty"`
	BackendServices []string `json:"backendServices,omitempty"`
	AgeSeconds      int64    `json:"ageSeconds"`
	AllowedActions  []string `json:"allowedActions,omitempty"`
}

type IngressRouteView struct {
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	PathType    string `json:"pathType,omitempty"`
	TLS         bool   `json:"tls"`
	ServiceName string `json:"serviceName"`
	ServicePort string `json:"servicePort,omitempty"`
}

type NetworkRelatedPodView struct {
	PodView
	Workloads []PodRelatedResourceView `json:"workloads,omitempty"`
}

type IngressBackendView struct {
	ServiceName string                  `json:"serviceName"`
	Endpoints   []ServiceEndpointView   `json:"endpoints,omitempty"`
	Pods        []NetworkRelatedPodView `json:"pods,omitempty"`
}

type IngressDetailView struct {
	Name           string               `json:"name"`
	Namespace      string               `json:"namespace"`
	ClassName      string               `json:"className,omitempty"`
	Address        string               `json:"address,omitempty"`
	Labels         map[string]string    `json:"labels,omitempty"`
	Annotations    map[string]string    `json:"annotations,omitempty"`
	Routes         []IngressRouteView   `json:"routes,omitempty"`
	Backends       []IngressBackendView `json:"backends,omitempty"`
	AgeSeconds     int64                `json:"ageSeconds"`
	AllowedActions []string             `json:"allowedActions,omitempty"`
}

type GatewayView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	GatewayClass   string   `json:"gatewayClass,omitempty"`
	Addresses      []string `json:"addresses,omitempty"`
	ListenerCount  int32    `json:"listenerCount"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type GatewayClassView struct {
	Name           string   `json:"name"`
	ControllerName string   `json:"controllerName"`
	Accepted       string   `json:"accepted,omitempty"`
	ParametersRef  string   `json:"parametersRef,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type GatewayClassDetailView struct {
	GatewayClassView
	Labels      map[string]string       `json:"labels,omitempty"`
	Annotations map[string]string       `json:"annotations,omitempty"`
	Conditions  []WorkloadConditionView `json:"conditions,omitempty"`
	Gateways    []GatewayView           `json:"gateways,omitempty"`
}

type GatewayListenerView struct {
	Name              string                  `json:"name"`
	Protocol          string                  `json:"protocol"`
	Port              int32                   `json:"port"`
	Hostname          string                  `json:"hostname,omitempty"`
	TLSMode           string                  `json:"tlsMode,omitempty"`
	CertificateRefs   []string                `json:"certificateRefs,omitempty"`
	AllowedRouteKinds []string                `json:"allowedRouteKinds,omitempty"`
	AttachedRoutes    int32                   `json:"attachedRoutes"`
	Conditions        []WorkloadConditionView `json:"conditions,omitempty"`
}

type GatewayRouteReferenceView struct {
	Kind      string   `json:"kind"`
	Namespace string   `json:"namespace,omitempty"`
	Name      string   `json:"name"`
	Hostnames []string `json:"hostnames,omitempty"`
	Accepted  string   `json:"accepted,omitempty"`
}

type GatewayDetailView struct {
	GatewayView
	Labels      map[string]string           `json:"labels,omitempty"`
	Annotations map[string]string           `json:"annotations,omitempty"`
	Conditions  []WorkloadConditionView     `json:"conditions,omitempty"`
	Listeners   []GatewayListenerView       `json:"listeners,omitempty"`
	Routes      []GatewayRouteReferenceView `json:"routes,omitempty"`
}

type GatewayRouteBackendView struct {
	Kind        string                `json:"kind,omitempty"`
	Namespace   string                `json:"namespace,omitempty"`
	Name        string                `json:"name"`
	Port        int32                 `json:"port,omitempty"`
	Weight      int32                 `json:"weight,omitempty"`
	Endpoints   []ServiceEndpointView `json:"endpoints,omitempty"`
	BackendPods []PodView             `json:"backendPods,omitempty"`
}

type GatewayRouteRuleView struct {
	Matches  []string                  `json:"matches,omitempty"`
	Filters  []string                  `json:"filters,omitempty"`
	Backends []GatewayRouteBackendView `json:"backends,omitempty"`
}

type GatewayRouteParentStatusView struct {
	ParentRef      string                  `json:"parentRef"`
	ControllerName string                  `json:"controllerName,omitempty"`
	Conditions     []WorkloadConditionView `json:"conditions,omitempty"`
}

type HTTPRouteView struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	Hostnames       []string `json:"hostnames,omitempty"`
	ParentRefs      []string `json:"parentRefs,omitempty"`
	BackendServices []string `json:"backendServices,omitempty"`
	AgeSeconds      int64    `json:"ageSeconds"`
	AllowedActions  []string `json:"allowedActions,omitempty"`
}

type HTTPRouteDetailView struct {
	HTTPRouteView
	Labels         map[string]string              `json:"labels,omitempty"`
	Annotations    map[string]string              `json:"annotations,omitempty"`
	Conditions     []WorkloadConditionView        `json:"conditions,omitempty"`
	ParentStatuses []GatewayRouteParentStatusView `json:"parentStatuses,omitempty"`
	Rules          []GatewayRouteRuleView         `json:"rules,omitempty"`
}

type BackendTLSPolicyView struct {
	Name                    string   `json:"name"`
	Namespace               string   `json:"namespace"`
	TargetRefs              []string `json:"targetRefs,omitempty"`
	Hostname                string   `json:"hostname,omitempty"`
	CACertificateRefs       []string `json:"caCertificateRefs,omitempty"`
	WellKnownCACertificates string   `json:"wellKnownCACertificates,omitempty"`
	AgeSeconds              int64    `json:"ageSeconds"`
	AllowedActions          []string `json:"allowedActions,omitempty"`
}

type BackendTLSPolicyDetailView struct {
	BackendTLSPolicyView
	Labels      map[string]string       `json:"labels,omitempty"`
	Annotations map[string]string       `json:"annotations,omitempty"`
	Conditions  []WorkloadConditionView `json:"conditions,omitempty"`
}

type GRPCRouteView struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	Hostnames       []string `json:"hostnames,omitempty"`
	ParentRefs      []string `json:"parentRefs,omitempty"`
	BackendServices []string `json:"backendServices,omitempty"`
	RuleCount       int32    `json:"ruleCount"`
	AgeSeconds      int64    `json:"ageSeconds"`
	AllowedActions  []string `json:"allowedActions,omitempty"`
}

type GRPCRouteDetailView struct {
	GRPCRouteView
	Labels         map[string]string              `json:"labels,omitempty"`
	Annotations    map[string]string              `json:"annotations,omitempty"`
	Conditions     []WorkloadConditionView        `json:"conditions,omitempty"`
	ParentStatuses []GatewayRouteParentStatusView `json:"parentStatuses,omitempty"`
	Rules          []GatewayRouteRuleView         `json:"rules,omitempty"`
}

type ReferenceGrantView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	From           []string `json:"from,omitempty"`
	To             []string `json:"to,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type ReferenceGrantFromView struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
}

type ReferenceGrantToView struct {
	Group string `json:"group"`
	Kind  string `json:"kind"`
	Name  string `json:"name,omitempty"`
}

type ReferenceGrantDetailView struct {
	ReferenceGrantView
	Labels      map[string]string        `json:"labels,omitempty"`
	Annotations map[string]string        `json:"annotations,omitempty"`
	FromRefs    []ReferenceGrantFromView `json:"fromRefs,omitempty"`
	ToRefs      []ReferenceGrantToView   `json:"toRefs,omitempty"`
}

type NetworkTopologySummaryView struct {
	EntryCount          int `json:"entryCount"`
	RouteCount          int `json:"routeCount"`
	ServiceCount        int `json:"serviceCount"`
	MissingServiceCount int `json:"missingServiceCount"`
	BackendPodCount     int `json:"backendPodCount"`
	PendingRouteCount   int `json:"pendingRouteCount"`
}

type NetworkTopologyNodeView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	State        string `json:"state"`
	Namespace    string `json:"namespace,omitempty"`
	ResourceName string `json:"resourceName,omitempty"`
	Subtitle     string `json:"subtitle,omitempty"`
	Badge        string `json:"badge,omitempty"`
}

type NetworkTopologyTraceView struct {
	ID          string                    `json:"id"`
	SourceType  string                    `json:"sourceType"`
	State       string                    `json:"state"`
	Entry       NetworkTopologyNodeView   `json:"entry"`
	Route       NetworkTopologyNodeView   `json:"route"`
	Service     *NetworkTopologyNodeView  `json:"service,omitempty"`
	BackendPods []NetworkTopologyNodeView `json:"backendPods,omitempty"`
	Note        string                    `json:"note,omitempty"`
}

type NetworkTopologyView struct {
	GraphVersion string                     `json:"graphVersion,omitempty"`
	Nodes        []NetworkTopologyGraphNode `json:"nodes,omitempty"`
	Edges        []NetworkTopologyGraphEdge `json:"edges,omitempty"`
	Coverage     []NetworkTopologyCoverage  `json:"coverage,omitempty"`
	ObservedAt   string                     `json:"observedAt,omitempty"`
	Truncated    bool                       `json:"truncated,omitempty"`
	ClusterID    string                     `json:"clusterId"`
	Namespace    string                     `json:"namespace,omitempty"`
	Source       string                     `json:"source"`
	GeneratedAt  string                     `json:"generatedAt"`
	Summary      NetworkTopologySummaryView `json:"summary"`
	Traces       []NetworkTopologyTraceView `json:"traces,omitempty"`
	Warnings     []string                   `json:"warnings,omitempty"`
}

type NodeView struct {
	Name           string                  `json:"name"`
	Status         string                  `json:"status"`
	Unschedulable  bool                    `json:"unschedulable"`
	Taints         []NodeTaintView         `json:"taints"`
	Conditions     []WorkloadConditionView `json:"conditions"`
	Roles          []string                `json:"roles,omitempty"`
	Version        string                  `json:"version,omitempty"`
	InternalIP     string                  `json:"internalIp,omitempty"`
	PodCount       int                     `json:"podCount"`
	AgeSeconds     int64                   `json:"ageSeconds"`
	Resources      NodeResourceSummaryView `json:"resources,omitempty"`
	AllowedActions []string                `json:"allowedActions,omitempty"`
}

type NodePodView struct {
	Name            string               `json:"name"`
	Namespace       string               `json:"namespace"`
	Phase           string               `json:"phase"`
	PodIP           string               `json:"podIp,omitempty"`
	ReadyContainers string               `json:"readyContainers"`
	Restarts        int32                `json:"restarts"`
	CPU             string               `json:"cpu,omitempty"`
	Memory          string               `json:"memory,omitempty"`
	Labels          map[string]string    `json:"labels,omitempty"`
	Requests        ResourceQuantityView `json:"requests,omitempty"`
	Limits          ResourceQuantityView `json:"limits,omitempty"`
	AgeSeconds      int64                `json:"ageSeconds"`
}

type NodeDetailView struct {
	Name              string                  `json:"name"`
	Status            string                  `json:"status"`
	Unschedulable     bool                    `json:"unschedulable"`
	Roles             []string                `json:"roles,omitempty"`
	Version           string                  `json:"version,omitempty"`
	InternalIP        string                  `json:"internalIp,omitempty"`
	PodCount          int                     `json:"podCount"`
	AgeSeconds        int64                   `json:"ageSeconds"`
	Labels            map[string]string       `json:"labels,omitempty"`
	Annotations       map[string]string       `json:"annotations,omitempty"`
	Taints            []NodeTaintView         `json:"taints,omitempty"`
	Conditions        []WorkloadConditionView `json:"conditions,omitempty"`
	Resources         NodeResourceSummaryView `json:"resources,omitempty"`
	MetricsConfigured bool                    `json:"metricsConfigured"`
	MetricsMessage    string                  `json:"metricsMessage,omitempty"`
	Pods              []NodePodView           `json:"pods,omitempty"`
	AllowedActions    []string                `json:"allowedActions,omitempty"`
}

type NodeTaintView struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Effect string `json:"effect"`
}

type NodeUpdateInput struct {
	Labels map[string]string `json:"labels,omitempty"`
	Taints []NodeTaintView   `json:"taints,omitempty"`
}

type NodeDrainInput struct {
	Force              bool  `json:"force"`
	DeleteEmptyDirData bool  `json:"deleteEmptyDirData"`
	TimeoutSeconds     int64 `json:"timeoutSeconds"`
}

type ClusterEventView struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace,omitempty"`
	Type          string `json:"type"`
	Reason        string `json:"reason"`
	InvolvedKind  string `json:"involvedKind,omitempty"`
	InvolvedName  string `json:"involvedName,omitempty"`
	Message       string `json:"message"`
	Count         int32  `json:"count"`
	LastTimestamp string `json:"lastTimestamp,omitempty"`
	AgeSeconds    int64  `json:"ageSeconds"`
}

type PersistentVolumeClaimView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	Status         string   `json:"status"`
	VolumeName     string   `json:"volumeName,omitempty"`
	StorageClass   string   `json:"storageClass,omitempty"`
	AccessModes    []string `json:"accessModes,omitempty"`
	Requested      string   `json:"requested,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type PersistentVolumeView struct {
	Name           string   `json:"name"`
	Status         string   `json:"status"`
	StorageClass   string   `json:"storageClass,omitempty"`
	ClaimRef       string   `json:"claimRef,omitempty"`
	ClaimNamespace string   `json:"claimNamespace,omitempty"`
	ClaimName      string   `json:"claimName,omitempty"`
	AccessModes    []string `json:"accessModes,omitempty"`
	Capacity       string   `json:"capacity,omitempty"`
	ReclaimPolicy  string   `json:"reclaimPolicy,omitempty"`
	VolumeMode     string   `json:"volumeMode,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type StorageClassView struct {
	Name                 string   `json:"name"`
	Provisioner          string   `json:"provisioner"`
	ReclaimPolicy        string   `json:"reclaimPolicy,omitempty"`
	VolumeBindingMode    string   `json:"volumeBindingMode,omitempty"`
	AllowVolumeExpansion bool     `json:"allowVolumeExpansion"`
	AgeSeconds           int64    `json:"ageSeconds"`
	AllowedActions       []string `json:"allowedActions,omitempty"`
}

type StoragePodReferenceView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase,omitempty"`
	NodeName  string `json:"nodeName,omitempty"`
}

type PersistentVolumeClaimDetailView struct {
	Name           string                    `json:"name"`
	Namespace      string                    `json:"namespace"`
	Status         string                    `json:"status"`
	VolumeName     string                    `json:"volumeName,omitempty"`
	StorageClass   string                    `json:"storageClass,omitempty"`
	AccessModes    []string                  `json:"accessModes,omitempty"`
	Requested      string                    `json:"requested,omitempty"`
	VolumeMode     string                    `json:"volumeMode,omitempty"`
	Capacity       string                    `json:"capacity,omitempty"`
	Labels         map[string]string         `json:"labels,omitempty"`
	Annotations    map[string]string         `json:"annotations,omitempty"`
	CreatedAt      string                    `json:"createdAt,omitempty"`
	AgeSeconds     int64                     `json:"ageSeconds"`
	Pods           []StoragePodReferenceView `json:"pods,omitempty"`
	PodsTruncated  bool                      `json:"podsTruncated,omitempty"`
	AllowedActions []string                  `json:"allowedActions,omitempty"`
}

type PersistentVolumeDetailView struct {
	Name           string            `json:"name"`
	Status         string            `json:"status"`
	StorageClass   string            `json:"storageClass,omitempty"`
	ClaimRef       string            `json:"claimRef,omitempty"`
	ClaimNamespace string            `json:"claimNamespace,omitempty"`
	ClaimName      string            `json:"claimName,omitempty"`
	AccessModes    []string          `json:"accessModes,omitempty"`
	Capacity       string            `json:"capacity,omitempty"`
	ReclaimPolicy  string            `json:"reclaimPolicy,omitempty"`
	VolumeMode     string            `json:"volumeMode,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	CreatedAt      string            `json:"createdAt,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
}

type StorageClassDetailView struct {
	Name                 string                      `json:"name"`
	Provisioner          string                      `json:"provisioner"`
	ReclaimPolicy        string                      `json:"reclaimPolicy,omitempty"`
	VolumeBindingMode    string                      `json:"volumeBindingMode,omitempty"`
	AllowVolumeExpansion bool                        `json:"allowVolumeExpansion"`
	Parameters           map[string]string           `json:"parameters,omitempty"`
	Labels               map[string]string           `json:"labels,omitempty"`
	Annotations          map[string]string           `json:"annotations,omitempty"`
	CreatedAt            string                      `json:"createdAt,omitempty"`
	AgeSeconds           int64                       `json:"ageSeconds"`
	Volumes              []PersistentVolumeView      `json:"volumes,omitempty"`
	Claims               []PersistentVolumeClaimView `json:"claims,omitempty"`
	VolumesTruncated     bool                        `json:"volumesTruncated,omitempty"`
	ClaimsTruncated      bool                        `json:"claimsTruncated,omitempty"`
	AllowedActions       []string                    `json:"allowedActions,omitempty"`
}

type CRDView struct {
	UID            string   `json:"uid,omitempty"`
	DeletingAt     string   `json:"deletingAt,omitempty"`
	Name           string   `json:"name"`
	Group          string   `json:"group"`
	Scope          string   `json:"scope"`
	Kind           string   `json:"kind"`
	Plural         string   `json:"plural"`
	Version        string   `json:"version,omitempty"`
	Versions       []string `json:"versions,omitempty"`
	CreatedAt      string   `json:"createdAt,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type CustomResourceView struct {
	UID            string            `json:"uid,omitempty"`
	Generation     int64             `json:"generation,omitempty"`
	DeletingAt     string            `json:"deletingAt,omitempty"`
	Finalizers     []string          `json:"finalizers,omitempty"`
	APIVersion     string            `json:"apiVersion,omitempty"`
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	CreatedAt      string            `json:"createdAt,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
}

// CustomResourceYAMLRequest is the Core/Agent custom-resource transport.
// ExpectedUID fences deletion against replacement after a user viewed the object.
type CustomResourceYAMLRequest struct {
	Definition  CRDResourceDefinition `json:"definition"`
	Namespace   string                `json:"namespace"`
	Name        string                `json:"name,omitempty"`
	Content     string                `json:"content,omitempty"`
	ExpectedUID string                `json:"expectedUid,omitempty"`
}

type HelmReleaseView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	Revision       string   `json:"revision,omitempty"`
	Status         string   `json:"status,omitempty"`
	Chart          string   `json:"chart,omitempty"`
	AppVersion     string   `json:"appVersion,omitempty"`
	StorageDriver  string   `json:"storageDriver,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type HelmReleaseDetailView struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Revision          string            `json:"revision,omitempty"`
	Status            string            `json:"status,omitempty"`
	Chart             string            `json:"chart,omitempty"`
	ChartName         string            `json:"chartName,omitempty"`
	ChartVersion      string            `json:"chartVersion,omitempty"`
	AppVersion        string            `json:"appVersion,omitempty"`
	StorageDriver     string            `json:"storageDriver,omitempty"`
	Description       string            `json:"description,omitempty"`
	CreatedAt         string            `json:"createdAt,omitempty"`
	UpdatedAt         string            `json:"updatedAt,omitempty"`
	FirstDeployedAt   string            `json:"firstDeployedAt,omitempty"`
	LastDeployedAt    string            `json:"lastDeployedAt,omitempty"`
	Notes             string            `json:"notes,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	AgeSeconds        int64             `json:"ageSeconds"`
	AllowedActions    []string          `json:"allowedActions,omitempty"`
	ValuesEditable    bool              `json:"valuesEditable"`
	ValuesDiffEnabled bool              `json:"valuesDiffEnabled"`
}

type HelmReleaseHistoryView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	Revision       string   `json:"revision"`
	Status         string   `json:"status,omitempty"`
	Chart          string   `json:"chart,omitempty"`
	ChartVersion   string   `json:"chartVersion,omitempty"`
	AppVersion     string   `json:"appVersion,omitempty"`
	Description    string   `json:"description,omitempty"`
	UpdatedAt      string   `json:"updatedAt,omitempty"`
	CreatedAt      string   `json:"createdAt,omitempty"`
	ManifestDigest string   `json:"manifestDigest,omitempty"`
	ValuesDigest   string   `json:"valuesDigest,omitempty"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type HelmReleaseManifestView struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Revision  string `json:"revision"`
	Content   string `json:"content"`
	Digest    string `json:"digest"`
}

type HelmReleaseRollbackInput struct {
	Revision       int  `json:"revision"`
	Wait           bool `json:"wait"`
	TimeoutSeconds int  `json:"timeoutSeconds"`
}

type HelmValuesView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	Revision       string   `json:"revision,omitempty"`
	Content        string   `json:"content"`
	Original       string   `json:"original,omitempty"`
	Editable       bool     `json:"editable"`
	DiffEnabled    bool     `json:"diffEnabled"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type HelmChartRepositoryView struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	DisplayName             string `json:"displayName,omitempty"`
	URL                     string `json:"url"`
	IndexURL                string `json:"indexUrl,omitempty"`
	OrganizationName        string `json:"organizationName,omitempty"`
	OrganizationDisplayName string `json:"organizationDisplayName,omitempty"`
	Official                bool   `json:"official,omitempty"`
	VerifiedPublisher       bool   `json:"verifiedPublisher,omitempty"`
}

type HelmChartMaintainerView struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

type HelmChartView struct {
	PackageID         string                    `json:"packageId,omitempty"`
	Name              string                    `json:"name"`
	NormalizedName    string                    `json:"normalizedName,omitempty"`
	RepositoryName    string                    `json:"repositoryName,omitempty"`
	RepositoryURL     string                    `json:"repositoryUrl,omitempty"`
	RepositoryDisplay string                    `json:"repositoryDisplay,omitempty"`
	LatestVersion     string                    `json:"latestVersion,omitempty"`
	AppVersion        string                    `json:"appVersion,omitempty"`
	Description       string                    `json:"description,omitempty"`
	Type              string                    `json:"type,omitempty"`
	Category          string                    `json:"category,omitempty"`
	Deprecated        bool                      `json:"deprecated,omitempty"`
	Home              string                    `json:"home,omitempty"`
	HomeURL           string                    `json:"homeUrl,omitempty"`
	Icon              string                    `json:"icon,omitempty"`
	LogoImageID       string                    `json:"logoImageId,omitempty"`
	LogoImageURL      string                    `json:"logoImageUrl,omitempty"`
	ArtifactHubURL    string                    `json:"artifactHubUrl,omitempty"`
	KubeVersion       string                    `json:"kubeVersion,omitempty"`
	CreatedAt         string                    `json:"createdAt,omitempty"`
	UpdatedAt         string                    `json:"updatedAt,omitempty"`
	Digest            string                    `json:"digest,omitempty"`
	URLs              []string                  `json:"urls,omitempty"`
	Sources           []string                  `json:"sources,omitempty"`
	Keywords          []string                  `json:"keywords,omitempty"`
	Maintainers       []HelmChartMaintainerView `json:"maintainers,omitempty"`
	Versions          []string                  `json:"versions,omitempty"`
	VersionCount      int                       `json:"versionCount"`
	Stars             int                       `json:"stars,omitempty"`
	Official          bool                      `json:"official,omitempty"`
	CNCF              bool                      `json:"cncf,omitempty"`
	Signed            bool                      `json:"signed,omitempty"`
	HasValuesSchema   bool                      `json:"hasValuesSchema,omitempty"`
	VerifiedPublisher bool                      `json:"verifiedPublisher,omitempty"`
	SecurityCritical  int                       `json:"securityCritical,omitempty"`
	SecurityHigh      int                       `json:"securityHigh,omitempty"`
	SecurityMedium    int                       `json:"securityMedium,omitempty"`
	SecurityLow       int                       `json:"securityLow,omitempty"`
	SecurityUnknown   int                       `json:"securityUnknown,omitempty"`
	AllowedActions    []string                  `json:"allowedActions,omitempty"`
}

type HelmChartLinkView struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

type HelmChartVersionView struct {
	Version                 string `json:"version"`
	AppVersion              string `json:"appVersion,omitempty"`
	CreatedAt               string `json:"createdAt,omitempty"`
	Prerelease              bool   `json:"prerelease,omitempty"`
	ContainsSecurityUpdates bool   `json:"containsSecurityUpdates,omitempty"`
}

type HelmChartDetailView struct {
	HelmChartView
	Readme            string                 `json:"readme,omitempty"`
	ContentURL        string                 `json:"contentUrl,omitempty"`
	Links             []HelmChartLinkView    `json:"links,omitempty"`
	AvailableVersions []HelmChartVersionView `json:"availableVersions,omitempty"`
}

type HelmChartValuesTemplateView struct {
	PackageID string `json:"packageId"`
	Name      string `json:"name,omitempty"`
	Version   string `json:"version"`
	Content   string `json:"content"`
}

type HelmChartInstallInput struct {
	RepositoryName  string `json:"repositoryName,omitempty"`
	RepositoryURL   string `json:"repositoryUrl"`
	ChartName       string `json:"chartName"`
	Version         string `json:"version"`
	ReleaseName     string `json:"releaseName"`
	Namespace       string `json:"namespace"`
	ValuesYAML      string `json:"valuesYaml,omitempty"`
	CreateNamespace bool   `json:"createNamespace"`
	Wait            bool   `json:"wait"`
	TimeoutSeconds  int    `json:"timeoutSeconds,omitempty"`
}

type HelmChartInstallResourceView struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

type HelmChartInstallResult struct {
	Name         string                         `json:"name"`
	Namespace    string                         `json:"namespace"`
	Revision     string                         `json:"revision,omitempty"`
	Status       string                         `json:"status,omitempty"`
	Chart        string                         `json:"chart,omitempty"`
	ChartName    string                         `json:"chartName,omitempty"`
	ChartVersion string                         `json:"chartVersion,omitempty"`
	AppVersion   string                         `json:"appVersion,omitempty"`
	Description  string                         `json:"description,omitempty"`
	Notes        string                         `json:"notes,omitempty"`
	Resources    []HelmChartInstallResourceView `json:"resources,omitempty"`
}

type HelmChartCatalogView struct {
	Repository   HelmChartRepositoryView `json:"repository"`
	Source       string                  `json:"source,omitempty"`
	Query        string                  `json:"query,omitempty"`
	Limit        int                     `json:"limit,omitempty"`
	Offset       int                     `json:"offset,omitempty"`
	GeneratedAt  string                  `json:"generatedAt,omitempty"`
	RefreshedAt  string                  `json:"refreshedAt"`
	TotalCount   int                     `json:"totalCount,omitempty"`
	LoadedCount  int                     `json:"loadedCount,omitempty"`
	ChartCount   int                     `json:"chartCount"`
	VersionCount int                     `json:"versionCount"`
	Charts       []HelmChartView         `json:"charts"`
}

type ConfigMapView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	DataEntries    int      `json:"dataEntries"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type ConfigMapDetailView struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Data        map[string]string `json:"data,omitempty"`
	BinaryData  map[string]string `json:"binaryData,omitempty"`
	Immutable   bool              `json:"immutable"`
	CreatedAt   string            `json:"createdAt,omitempty"`
	AgeSeconds  int64             `json:"ageSeconds"`
}

type SecretView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	Type           string   `json:"type"`
	DataEntries    int      `json:"dataEntries"`
	Immutable      *bool    `json:"immutable"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type SecretDetailView struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Type        string            `json:"type"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Data        map[string]string `json:"data,omitempty"`
	Immutable   bool              `json:"immutable"`
	CreatedAt   string            `json:"createdAt,omitempty"`
	AgeSeconds  int64             `json:"ageSeconds"`
}

type ServiceAccountView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type ServiceAccountDetailView struct {
	Name             string            `json:"name"`
	Namespace        string            `json:"namespace"`
	Labels           map[string]string `json:"labels,omitempty"`
	Annotations      map[string]string `json:"annotations,omitempty"`
	Secrets          []string          `json:"secrets,omitempty"`
	ImagePullSecrets []string          `json:"imagePullSecrets,omitempty"`
	AutomountSAToken bool              `json:"automountServiceAccountToken"`
	CreatedAt        string            `json:"createdAt,omitempty"`
	AgeSeconds       int64             `json:"ageSeconds"`
	AllowedActions   []string          `json:"allowedActions,omitempty"`
}

type RoleView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type RoleDetailView struct {
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	Rules          int               `json:"rules"`
	RuleSummaries  []string          `json:"ruleSummaries,omitempty"`
	CreatedAt      string            `json:"createdAt,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
}

type RoleBindingView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	RoleRef        string   `json:"roleRef"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type RoleBindingDetailView struct {
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	RoleRef        string            `json:"roleRef"`
	Subjects       []string          `json:"subjects,omitempty"`
	CreatedAt      string            `json:"createdAt,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
}

type ReplicaSetView struct {
	Name              string   `json:"name"`
	Namespace         string   `json:"namespace"`
	DesiredReplicas   int32    `json:"desiredReplicas"`
	ReadyReplicas     int32    `json:"readyReplicas"`
	AvailableReplicas int32    `json:"availableReplicas"`
	AgeSeconds        int64    `json:"ageSeconds"`
	AllowedActions    []string `json:"allowedActions,omitempty"`
}

type ReplicaSetDetailView struct {
	Name              string                 `json:"name"`
	Namespace         string                 `json:"namespace"`
	DesiredReplicas   int32                  `json:"desiredReplicas"`
	ReadyReplicas     int32                  `json:"readyReplicas"`
	AvailableReplicas int32                  `json:"availableReplicas"`
	CreatedAt         string                 `json:"createdAt,omitempty"`
	Labels            map[string]string      `json:"labels,omitempty"`
	Annotations       map[string]string      `json:"annotations,omitempty"`
	Selector          map[string]string      `json:"selector,omitempty"`
	Pods              []PodView              `json:"pods,omitempty"`
	RelatedResources  []WorkloadRelationView `json:"relatedResources,omitempty"`
	AllowedActions    []string               `json:"allowedActions,omitempty"`
}

type EndpointSliceView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	AddressType    string   `json:"addressType"`
	Endpoints      int      `json:"endpoints"`
	Ports          []string `json:"ports,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type EndpointSliceDetailView struct {
	Name           string                `json:"name"`
	Namespace      string                `json:"namespace"`
	AddressType    string                `json:"addressType"`
	ServiceName    string                `json:"serviceName,omitempty"`
	Ports          []string              `json:"ports,omitempty"`
	Labels         map[string]string     `json:"labels,omitempty"`
	Annotations    map[string]string     `json:"annotations,omitempty"`
	Endpoints      []ServiceEndpointView `json:"endpoints,omitempty"`
	AgeSeconds     int64                 `json:"ageSeconds"`
	AllowedActions []string              `json:"allowedActions,omitempty"`
}

type NetworkPolicyView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	PolicyTypes    []string `json:"policyTypes,omitempty"`
	IngressRules   int      `json:"ingressRules"`
	EgressRules    int      `json:"egressRules"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type NetworkPolicyPeerView struct {
	PodSelector       string `json:"podSelector,omitempty"`
	NamespaceSelector string `json:"namespaceSelector,omitempty"`
	IPBlock           string `json:"ipBlock,omitempty"`
}

type NetworkPolicyPortView struct {
	Protocol string `json:"protocol,omitempty"`
	Port     string `json:"port,omitempty"`
	EndPort  int32  `json:"endPort,omitempty"`
}

type NetworkPolicyRuleView struct {
	Direction string                  `json:"direction"`
	Peers     []NetworkPolicyPeerView `json:"peers,omitempty"`
	Ports     []NetworkPolicyPortView `json:"ports,omitempty"`
}

type NetworkPolicyDetailView struct {
	Name           string                  `json:"name"`
	Namespace      string                  `json:"namespace"`
	PolicyTypes    []string                `json:"policyTypes,omitempty"`
	PodSelector    string                  `json:"podSelector,omitempty"`
	Labels         map[string]string       `json:"labels,omitempty"`
	Annotations    map[string]string       `json:"annotations,omitempty"`
	Rules          []NetworkPolicyRuleView `json:"rules,omitempty"`
	MatchingPods   []PodView               `json:"matchingPods,omitempty"`
	AgeSeconds     int64                   `json:"ageSeconds"`
	AllowedActions []string                `json:"allowedActions,omitempty"`
}

type HorizontalPodAutoscalerView struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	TargetRef       string   `json:"targetRef"`
	MinReplicas     int32    `json:"minReplicas"`
	MaxReplicas     int32    `json:"maxReplicas"`
	CurrentReplicas int32    `json:"currentReplicas"`
	DesiredReplicas int32    `json:"desiredReplicas"`
	AgeSeconds      int64    `json:"ageSeconds"`
	AllowedActions  []string `json:"allowedActions,omitempty"`
}

type HorizontalPodAutoscalerMetricView struct {
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Target  string `json:"target,omitempty"`
	Current string `json:"current,omitempty"`
}

type HorizontalPodAutoscalerDetailView struct {
	HorizontalPodAutoscalerView
	Labels      map[string]string                   `json:"labels,omitempty"`
	Annotations map[string]string                   `json:"annotations,omitempty"`
	CreatedAt   string                              `json:"createdAt,omitempty"`
	Metrics     []HorizontalPodAutoscalerMetricView `json:"metrics,omitempty"`
	Conditions  []WorkloadConditionView             `json:"conditions,omitempty"`
}

type PodDisruptionBudgetView struct {
	Name               string   `json:"name"`
	Namespace          string   `json:"namespace"`
	MinAvailable       string   `json:"minAvailable,omitempty"`
	MaxUnavailable     string   `json:"maxUnavailable,omitempty"`
	CurrentHealthy     int32    `json:"currentHealthy"`
	DesiredHealthy     int32    `json:"desiredHealthy"`
	DisruptionsAllowed int32    `json:"disruptionsAllowed"`
	AgeSeconds         int64    `json:"ageSeconds"`
	AllowedActions     []string `json:"allowedActions,omitempty"`
}

type PodDisruptionBudgetDetailView struct {
	PodDisruptionBudgetView
	Labels      map[string]string       `json:"labels,omitempty"`
	Annotations map[string]string       `json:"annotations,omitempty"`
	CreatedAt   string                  `json:"createdAt,omitempty"`
	Selector    string                  `json:"selector,omitempty"`
	Pods        []PodView               `json:"pods,omitempty"`
	Workload    *PodRelatedResourceView `json:"workload,omitempty"`
	Conditions  []WorkloadConditionView `json:"conditions,omitempty"`
}

type IngressClassView struct {
	Name           string   `json:"name"`
	Controller     string   `json:"controller"`
	IsDefault      bool     `json:"isDefault"`
	Parameters     string   `json:"parameters,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type IngressClassDetailView struct {
	IngressClassView
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Ingresses   []IngressView     `json:"ingresses,omitempty"`
}

type PriorityClassView struct {
	Name             string   `json:"name"`
	Value            int32    `json:"value"`
	GlobalDefault    bool     `json:"globalDefault"`
	PreemptionPolicy string   `json:"preemptionPolicy,omitempty"`
	Description      string   `json:"description,omitempty"`
	AgeSeconds       int64    `json:"ageSeconds"`
	AllowedActions   []string `json:"allowedActions,omitempty"`
}

type RuntimeClassView struct {
	Name           string   `json:"name"`
	Handler        string   `json:"handler"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type ClusterRoleView struct {
	Name           string   `json:"name"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type ClusterRoleDetailView struct {
	Name             string            `json:"name"`
	Labels           map[string]string `json:"labels,omitempty"`
	Annotations      map[string]string `json:"annotations,omitempty"`
	Rules            int               `json:"rules"`
	AggregationRules int               `json:"aggregationRules"`
	RuleSummaries    []string          `json:"ruleSummaries,omitempty"`
	CreatedAt        string            `json:"createdAt,omitempty"`
	AgeSeconds       int64             `json:"ageSeconds"`
	AllowedActions   []string          `json:"allowedActions,omitempty"`
}

type ClusterRoleBindingView struct {
	Name           string   `json:"name"`
	RoleRef        string   `json:"roleRef"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type ClusterRoleBindingDetailView struct {
	Name           string            `json:"name"`
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	RoleRef        string            `json:"roleRef"`
	Subjects       []string          `json:"subjects,omitempty"`
	CreatedAt      string            `json:"createdAt,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
}

type MutatingWebhookConfigurationView struct {
	Name           string   `json:"name"`
	Webhooks       int      `json:"webhooks"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type ValidatingWebhookConfigurationView struct {
	Name           string   `json:"name"`
	Webhooks       int      `json:"webhooks"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type AdmissionWebhookRuleView struct {
	Operations  []string `json:"operations,omitempty"`
	APIGroups   []string `json:"apiGroups,omitempty"`
	APIVersions []string `json:"apiVersions,omitempty"`
	Resources   []string `json:"resources,omitempty"`
	Scope       string   `json:"scope,omitempty"`
}

type AdmissionWebhookView struct {
	Name                    string                     `json:"name"`
	ClientTarget            string                     `json:"clientTarget"`
	URL                     string                     `json:"url,omitempty"`
	ServiceName             string                     `json:"serviceName,omitempty"`
	ServiceNamespace        string                     `json:"serviceNamespace,omitempty"`
	ServicePath             string                     `json:"servicePath,omitempty"`
	ServicePort             int32                      `json:"servicePort,omitempty"`
	CABundleConfigured      bool                       `json:"caBundleConfigured"`
	FailurePolicy           string                     `json:"failurePolicy,omitempty"`
	MatchPolicy             string                     `json:"matchPolicy,omitempty"`
	SideEffects             string                     `json:"sideEffects,omitempty"`
	TimeoutSeconds          int32                      `json:"timeoutSeconds,omitempty"`
	AdmissionReviewVersions []string                   `json:"admissionReviewVersions,omitempty"`
	NamespaceSelector       string                     `json:"namespaceSelector,omitempty"`
	ObjectSelector          string                     `json:"objectSelector,omitempty"`
	Rules                   []AdmissionWebhookRuleView `json:"rules,omitempty"`
}

type AdmissionWebhookConfigurationDetailView struct {
	Name        string                 `json:"name"`
	Labels      map[string]string      `json:"labels,omitempty"`
	Annotations map[string]string      `json:"annotations,omitempty"`
	CreatedAt   string                 `json:"createdAt,omitempty"`
	AgeSeconds  int64                  `json:"ageSeconds"`
	Webhooks    []AdmissionWebhookView `json:"webhooks,omitempty"`
}

type ResourceQuotaView struct {
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	Scopes         []string          `json:"scopes,omitempty"`
	Hard           map[string]string `json:"hard,omitempty"`
	Used           map[string]string `json:"used,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
}

type ResourceQuotaDetailView struct {
	ResourceQuotaView
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	CreatedAt   string            `json:"createdAt,omitempty"`
}

type LimitRangeView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	Limits         int      `json:"limits"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type LimitRangeRuleView struct {
	Type                 string            `json:"type"`
	Min                  map[string]string `json:"min,omitempty"`
	Max                  map[string]string `json:"max,omitempty"`
	Default              map[string]string `json:"default,omitempty"`
	DefaultRequest       map[string]string `json:"defaultRequest,omitempty"`
	MaxLimitRequestRatio map[string]string `json:"maxLimitRequestRatio,omitempty"`
}

type LimitRangeDetailView struct {
	LimitRangeView
	Labels      map[string]string    `json:"labels,omitempty"`
	Annotations map[string]string    `json:"annotations,omitempty"`
	CreatedAt   string               `json:"createdAt,omitempty"`
	Rules       []LimitRangeRuleView `json:"rules,omitempty"`
}

type LeaseView struct {
	Name                 string   `json:"name"`
	Namespace            string   `json:"namespace"`
	HolderIdentity       string   `json:"holderIdentity,omitempty"`
	LeaseDurationSeconds int32    `json:"leaseDurationSeconds,omitempty"`
	AcquireTime          string   `json:"acquireTime,omitempty"`
	RenewTime            string   `json:"renewTime,omitempty"`
	AgeSeconds           int64    `json:"ageSeconds"`
	AllowedActions       []string `json:"allowedActions,omitempty"`
}

type ReplicationControllerView struct {
	Name              string   `json:"name"`
	Namespace         string   `json:"namespace"`
	DesiredReplicas   int32    `json:"desiredReplicas"`
	CurrentReplicas   int32    `json:"currentReplicas"`
	ReadyReplicas     int32    `json:"readyReplicas"`
	AvailableReplicas int32    `json:"availableReplicas"`
	AgeSeconds        int64    `json:"ageSeconds"`
	AllowedActions    []string `json:"allowedActions,omitempty"`
}

type ReplicationControllerDetailView struct {
	Name              string                 `json:"name"`
	Namespace         string                 `json:"namespace"`
	DesiredReplicas   int32                  `json:"desiredReplicas"`
	CurrentReplicas   int32                  `json:"currentReplicas"`
	ReadyReplicas     int32                  `json:"readyReplicas"`
	AvailableReplicas int32                  `json:"availableReplicas"`
	CreatedAt         string                 `json:"createdAt,omitempty"`
	Labels            map[string]string      `json:"labels,omitempty"`
	Annotations       map[string]string      `json:"annotations,omitempty"`
	Selector          map[string]string      `json:"selector,omitempty"`
	Pods              []PodView              `json:"pods,omitempty"`
	RelatedResources  []WorkloadRelationView `json:"relatedResources,omitempty"`
	AllowedActions    []string               `json:"allowedActions,omitempty"`
}

type PortForwardSessionView struct {
	SessionID  string `json:"sessionId"`
	ClusterID  string `json:"clusterId"`
	Namespace  string `json:"namespace"`
	TargetKind string `json:"targetKind"`
	TargetName string `json:"targetName"`
	LocalPort  int    `json:"localPort"`
	RemotePort int    `json:"remotePort"`
	Status     string `json:"status"`
	CreatedBy  string `json:"createdBy,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

type PortForwardRegisterInput struct {
	Namespace  string `json:"namespace"`
	TargetKind string `json:"targetKind"`
	TargetName string `json:"targetName"`
	LocalPort  int    `json:"localPort"`
	RemotePort int    `json:"remotePort"`
}
