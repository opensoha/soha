package resource

import "encoding/json"

// ManifestHealth gives Direct and Agent execution the same conservative health
// interpretation. Successful persistence alone is health evidence only for
// built-in configuration objects, never for an unknown custom resource.
func ManifestHealth(object map[string]any) string {
	if object == nil {
		return "unknown"
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return "unknown"
	}
	var item manifestHealthObject
	if err := json.Unmarshal(encoded, &item); err != nil {
		return "unknown"
	}
	if item.Metadata.DeletionTimestamp != "" {
		return "degraded"
	}
	kind := item.APIVersion + "/" + item.Kind
	switch kind {
	case "workloads.soha.io/v1alpha1/WorkloadCronJob":
		// Ready describes configuration synchronization. Its source and owned
		// CronJob must also be observed by the runtime before declaring health.
		if item.conditionHealth(true) == "degraded" {
			return "degraded"
		}
		return "unknown"
	case "v1/ConfigMap", "v1/Secret", "v1/ServiceAccount", "rbac.authorization.k8s.io/v1/Role", "rbac.authorization.k8s.io/v1/RoleBinding", "networking.k8s.io/v1/NetworkPolicy":
		return "healthy"
	case "apps/v1/Deployment", "apps/v1/StatefulSet", "apps/v1/DaemonSet":
		return item.workloadHealth()
	case "v1/PersistentVolumeClaim":
		if item.Status.Phase == "Bound" {
			return "healthy"
		}
		if item.Status.Phase == "Lost" {
			return "degraded"
		}
		return "progressing"
	case "v1/Service":
		if item.Spec.Type != "LoadBalancer" || len(item.Status.LoadBalancer.Ingress) > 0 {
			return "healthy"
		}
		return "progressing"
	case "v1/Pod", "batch/v1/Job":
		if item.Status.Phase == "Succeeded" {
			return "healthy"
		}
		if item.Status.Phase == "Failed" {
			return "degraded"
		}
		return item.conditionHealth(false)
	default:
		return item.conditionHealth(true)
	}
}

type manifestHealthObject struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Generation        int64  `json:"generation"`
		DeletionTimestamp string `json:"deletionTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Replicas       *int64 `json:"replicas"`
		Type           string `json:"type"`
		UpdateStrategy struct {
			RollingUpdate struct {
				Partition int64 `json:"partition"`
			} `json:"rollingUpdate"`
		} `json:"updateStrategy"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration     int64  `json:"observedGeneration"`
		Replicas               int64  `json:"replicas"`
		ReadyReplicas          int64  `json:"readyReplicas"`
		UpdatedReplicas        int64  `json:"updatedReplicas"`
		AvailableReplicas      int64  `json:"availableReplicas"`
		DesiredNumberScheduled int64  `json:"desiredNumberScheduled"`
		UpdatedNumberScheduled int64  `json:"updatedNumberScheduled"`
		NumberAvailable        int64  `json:"numberAvailable"`
		Phase                  string `json:"phase"`
		LoadBalancer           struct {
			Ingress []json.RawMessage `json:"ingress"`
		} `json:"loadBalancer"`
		Conditions []struct {
			Type               string `json:"type"`
			Status             string `json:"status"`
			Reason             string `json:"reason"`
			ObservedGeneration int64  `json:"observedGeneration"`
		} `json:"conditions"`
	} `json:"status"`
}

func (item manifestHealthObject) workloadHealth() string {
	if item.Metadata.Generation < 1 || item.Status.ObservedGeneration < item.Metadata.Generation {
		return "progressing"
	}
	if item.conditionHealth(false) == "degraded" {
		return "degraded"
	}
	desired := int64(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	healthy := false
	switch item.Kind {
	case "Deployment":
		healthy = item.Status.UpdatedReplicas == desired && item.Status.Replicas == desired && item.Status.AvailableReplicas >= desired
	case "StatefulSet":
		updated := max(int64(0), desired-item.Spec.UpdateStrategy.RollingUpdate.Partition)
		healthy = item.Status.ReadyReplicas >= desired && item.Status.UpdatedReplicas >= updated
	case "DaemonSet":
		healthy = item.Status.UpdatedNumberScheduled == item.Status.DesiredNumberScheduled && item.Status.NumberAvailable >= item.Status.DesiredNumberScheduled
	}
	if healthy {
		return "healthy"
	}
	return "progressing"
}

func (item manifestHealthObject) conditionHealth(requireCurrentGeneration bool) string {
	result := "progressing"
	notReady := false
	if requireCurrentGeneration {
		result = "unknown"
	}
	for _, condition := range item.Status.Conditions {
		if requireCurrentGeneration && (item.Metadata.Generation < 1 || max(condition.ObservedGeneration, item.Status.ObservedGeneration) < item.Metadata.Generation) {
			continue
		}
		if condition.Status == "True" && (condition.Type == "Failed" || condition.Type == "Degraded" || condition.Type == "ReplicaFailure") {
			return "degraded"
		}
		if condition.Type == "Progressing" && condition.Status == "False" && condition.Reason == "ProgressDeadlineExceeded" {
			return "degraded"
		}
		if condition.Type == "Ready" || condition.Type == "Available" || condition.Type == "Complete" || condition.Type == "Established" {
			if condition.Status == "True" {
				result = "healthy"
			} else {
				notReady = true
			}
		}
	}
	if notReady {
		return "progressing"
	}
	return result
}
