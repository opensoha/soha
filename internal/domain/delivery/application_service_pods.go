package delivery

import dr "github.com/opensoha/soha/internal/domain/resource"

type ApplicationServicePod struct {
	ClusterID    string     `json:"clusterId"`
	TargetID     string     `json:"targetId,omitempty"`
	WorkloadKind string     `json:"workloadKind"`
	WorkloadName string     `json:"workloadName"`
	Pod          dr.PodView `json:"pod"`
}

type ApplicationServicePods struct {
	Status string                  `json:"status"`
	Reason string                  `json:"reason,omitempty"`
	Items  []ApplicationServicePod `json:"items"`
}
