package resource

const ResourceMonitoringSnapshotFeature = "resource.monitoring.snapshot.v1"
const ResourceMonitoringMaxNodes = 500
const ResourceMonitoringMaxPoints = 1440
const ResourceMonitoringMaxTotalPoints = 60000

type ResourceMonitoringQuery struct {
	View         string `json:"view"`
	Node         string `json:"node,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	RangeMinutes int    `json:"rangeMinutes,omitempty"`
	StepSeconds  int    `json:"stepSeconds,omitempty"`
}

type ResourceMonitoringQuantity struct {
	CPUCores    *float64 `json:"cpuCores"`
	MemoryBytes *float64 `json:"memoryBytes"`
	PodSlots    *float64 `json:"podSlots"`
}

type ResourceMonitoringNode struct {
	NodeRef              ResourceRef                `json:"nodeRef"`
	InternalIP           string                     `json:"internalIP,omitempty"`
	OS                   string                     `json:"os,omitempty"`
	CreatedAt            string                     `json:"createdAt,omitempty"`
	Ready                string                     `json:"ready"`
	Cordoned             bool                       `json:"cordoned"`
	MemoryPressure       string                     `json:"memoryPressure"`
	DiskPressure         string                     `json:"diskPressure"`
	PIDPressure          string                     `json:"pidPressure"`
	Capacity             ResourceMonitoringQuantity `json:"capacity"`
	Allocatable          ResourceMonitoringQuantity `json:"allocatable"`
	Requests             ResourceMonitoringQuantity `json:"requests"`
	Limits               ResourceMonitoringQuantity `json:"limits"`
	CPULimitsComplete    bool                       `json:"cpuLimitsComplete"`
	MemoryLimitsComplete bool                       `json:"memoryLimitsComplete"`
	BoundPods            *float64                   `json:"boundPods"`
	PendingPods          *float64                   `json:"pendingPods"`
	RemainingPodSlots    *float64                   `json:"remainingPodSlots"`
	OverflowPodSlots     *float64                   `json:"overflowPodSlots"`
	Quality              string                     `json:"quality"`
	ObservedAt           string                     `json:"observedAt,omitempty"`
}

type ResourceMonitoringInventory struct {
	Nodes                  []ResourceMonitoringNode `json:"nodes"`
	UnscheduledPendingPods *float64                 `json:"unscheduledPendingPods"`
	Quality                string                   `json:"quality"`
	ReasonCode             string                   `json:"reasonCode,omitempty"`
	ObservedAt             string                   `json:"observedAt"`
	Truncated              bool                     `json:"truncated,omitempty"`
}

type ResourceMonitoringPoint struct {
	Timestamp string   `json:"timestamp"`
	Value     *float64 `json:"value"`
}

type ResourceMonitoringSeries struct {
	NodeRef    ResourceRef               `json:"nodeRef"`
	MetricKey  string                    `json:"metricKey"`
	Unit       string                    `json:"unit"`
	Quality    string                    `json:"quality"`
	ReasonCode string                    `json:"reasonCode,omitempty"`
	Source     string                    `json:"source"`
	SampledAt  string                    `json:"sampledAt,omitempty"`
	Latest     *float64                  `json:"latest"`
	Points     []ResourceMonitoringPoint `json:"points"`
}

type ResourceMonitoringPanel struct {
	Key           string                     `json:"key"`
	Label         string                     `json:"label"`
	Unit          string                     `json:"unit"`
	Quality       string                     `json:"quality"`
	ReasonCode    string                     `json:"reasonCode,omitempty"`
	Estimated     bool                       `json:"estimated,omitempty"`
	WindowSeconds int                        `json:"windowSeconds,omitempty"`
	Series        []ResourceMonitoringSeries `json:"series"`
}

type ResourceMonitoringView struct {
	NodeOptions            []ResourceRef             `json:"nodeOptions,omitempty"`
	ClusterID              string                    `json:"clusterId"`
	View                   string                    `json:"view"`
	Node                   string                    `json:"node,omitempty"`
	From                   string                    `json:"from"`
	To                     string                    `json:"to"`
	StepSeconds            int                       `json:"stepSeconds"`
	GeneratedAt            string                    `json:"generatedAt"`
	SnapshotObservedAt     string                    `json:"snapshotObservedAt,omitempty"`
	SnapshotQuality        string                    `json:"snapshotQuality"`
	UnscheduledPendingPods *float64                  `json:"unscheduledPendingPods"`
	Nodes                  []ResourceMonitoringNode  `json:"nodes"`
	Panels                 []ResourceMonitoringPanel `json:"panels"`
	Truncated              bool                      `json:"truncated,omitempty"`
}
