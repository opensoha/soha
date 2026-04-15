package resource

const (
	PodLogsMaxContentBytes = 256 * 1024
	PodExecMaxOutputBytes  = 128 * 1024
)

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
	Name                   string            `json:"name"`
	Namespace              string            `json:"namespace"`
	Phase                  string            `json:"phase"`
	NodeName               string            `json:"nodeName,omitempty"`
	PodIP                  string            `json:"podIp,omitempty"`
	CreatedAt              string            `json:"createdAt,omitempty"`
	CPU                    string            `json:"cpu,omitempty"`
	Memory                 string            `json:"memory,omitempty"`
	Labels                 map[string]string `json:"labels,omitempty"`
	PersistentVolumeClaims []string          `json:"persistentVolumeClaims,omitempty"`
	ReadyContainers        string            `json:"readyContainers"`
	Restarts               int32             `json:"restarts"`
	AgeSeconds             int64             `json:"ageSeconds"`
	AllowedActions         []string          `json:"allowedActions,omitempty"`
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
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state,omitempty"`
	LastState    string `json:"lastState,omitempty"`
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
	Name               string                  `json:"name"`
	Namespace          string                  `json:"namespace"`
	Phase              string                  `json:"phase"`
	PodIP              string                  `json:"podIp,omitempty"`
	HostIP             string                  `json:"hostIp,omitempty"`
	NodeName           string                  `json:"nodeName,omitempty"`
	ServiceAccountName string                  `json:"serviceAccountName,omitempty"`
	QOSClass           string                  `json:"qosClass,omitempty"`
	StartTime          string                  `json:"startTime,omitempty"`
	Labels             map[string]string       `json:"labels,omitempty"`
	Annotations        map[string]string       `json:"annotations,omitempty"`
	Containers         []WorkloadContainerView `json:"containers,omitempty"`
	Conditions         []WorkloadConditionView `json:"conditions,omitempty"`
	AllowedActions     []string                `json:"allowedActions,omitempty"`
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
	Labels             map[string]string       `json:"labels,omitempty"`
	Annotations        map[string]string       `json:"annotations,omitempty"`
	Selector           map[string]string       `json:"selector,omitempty"`
	Containers         []WorkloadContainerView `json:"containers,omitempty"`
	Conditions         []WorkloadConditionView `json:"conditions,omitempty"`
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
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	ServiceName     string            `json:"serviceName,omitempty"`
	DesiredReplicas int32             `json:"desiredReplicas"`
	ReadyReplicas   int32             `json:"readyReplicas"`
	CurrentReplicas int32             `json:"currentReplicas"`
	UpdateStrategy  string            `json:"updateStrategy,omitempty"`
	CurrentRevision string            `json:"currentRevision,omitempty"`
	UpdateRevision  string            `json:"updateRevision,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	Selector        map[string]string `json:"selector,omitempty"`
	AllowedActions  []string          `json:"allowedActions,omitempty"`
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
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	DesiredNumber   int32             `json:"desiredNumber"`
	CurrentNumber   int32             `json:"currentNumber"`
	ReadyNumber     int32             `json:"readyNumber"`
	AvailableNumber int32             `json:"availableNumber"`
	UpdatedNumber   int32             `json:"updatedNumber"`
	UpdateStrategy  string            `json:"updateStrategy,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	Selector        map[string]string `json:"selector,omitempty"`
	AllowedActions  []string          `json:"allowedActions,omitempty"`
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
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	Completions    int32             `json:"completions"`
	Parallelism    int32             `json:"parallelism"`
	Succeeded      int32             `json:"succeeded"`
	Failed         int32             `json:"failed"`
	Active         int32             `json:"active"`
	CompletionMode string            `json:"completionMode,omitempty"`
	StartTime      string            `json:"startTime,omitempty"`
	CompletionTime string            `json:"completionTime,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
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
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Schedule          string            `json:"schedule"`
	Suspend           bool              `json:"suspend"`
	ActiveJobs        int32             `json:"activeJobs"`
	LastScheduleTime  string            `json:"lastScheduleTime,omitempty"`
	ConcurrencyPolicy string            `json:"concurrencyPolicy,omitempty"`
	TimeZone          string            `json:"timeZone,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
	AllowedActions    []string          `json:"allowedActions,omitempty"`
}

type ServiceView struct {
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	Type           string            `json:"type"`
	ClusterIP      string            `json:"clusterIp,omitempty"`
	Ports          []string          `json:"ports,omitempty"`
	Selector       map[string]string `json:"selector,omitempty"`
	AgeSeconds     int64             `json:"ageSeconds"`
	AllowedActions []string          `json:"allowedActions,omitempty"`
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

type GatewayView struct {
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	GatewayClass   string   `json:"gatewayClass,omitempty"`
	Addresses      []string `json:"addresses,omitempty"`
	ListenerCount  int32    `json:"listenerCount"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type HTTPRouteView struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	Hostnames       []string `json:"hostnames,omitempty"`
	ParentRefs      []string `json:"parentRefs,omitempty"`
	BackendServices []string `json:"backendServices,omitempty"`
	RuleCount       int32    `json:"ruleCount"`
	AgeSeconds      int64    `json:"ageSeconds"`
	AllowedActions  []string `json:"allowedActions,omitempty"`
}

type NodeView struct {
	Name           string                  `json:"name"`
	Status         string                  `json:"status"`
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
	AccessModes    []string `json:"accessModes,omitempty"`
	Capacity       string   `json:"capacity,omitempty"`
	ReclaimPolicy  string   `json:"reclaimPolicy,omitempty"`
	VolumeMode     string   `json:"volumeMode,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
}

type StorageClassView struct {
	Name                 string            `json:"name"`
	Provisioner          string            `json:"provisioner"`
	ReclaimPolicy        string            `json:"reclaimPolicy,omitempty"`
	VolumeBindingMode    string            `json:"volumeBindingMode,omitempty"`
	AllowVolumeExpansion bool              `json:"allowVolumeExpansion"`
	Parameters           map[string]string `json:"parameters,omitempty"`
	AgeSeconds           int64             `json:"ageSeconds"`
	AllowedActions       []string          `json:"allowedActions,omitempty"`
}

type CRDView struct {
	Name           string   `json:"name"`
	Group          string   `json:"group"`
	Scope          string   `json:"scope"`
	Kind           string   `json:"kind"`
	Plural         string   `json:"plural"`
	Versions       []string `json:"versions,omitempty"`
	AgeSeconds     int64    `json:"ageSeconds"`
	AllowedActions []string `json:"allowedActions,omitempty"`
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
