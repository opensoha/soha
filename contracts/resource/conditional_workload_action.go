package resource

import (
	"fmt"
	"strconv"
)

type ConditionalWorkloadActionInput struct {
	Preconditions ResourceMutationPreconditions `json:"preconditions"`
	Replicas      *int32                        `json:"replicas,omitempty"`
	Revision      string                        `json:"revision,omitempty"`
	DryRun        bool                          `json:"dryRun,omitempty"`
}

func (input ConditionalWorkloadActionInput) Validate(kind, action string) error {
	if err := input.Preconditions.Validate(); err != nil {
		return err
	}
	switch action {
	case "restart":
		if (kind != "Deployment" && kind != "StatefulSet" && kind != "DaemonSet") || input.Replicas != nil || input.Revision != "" {
			return fmt.Errorf("restart supports Deployment, StatefulSet and DaemonSet without replicas or revision")
		}
	case "scale":
		if (kind != "Deployment" && kind != "StatefulSet") || input.Replicas == nil || *input.Replicas < 0 || input.Revision != "" {
			return fmt.Errorf("scale requires Deployment or StatefulSet and nonnegative replicas without revision")
		}
	case "rollback":
		if kind != "Deployment" || input.Replicas != nil || !validWorkloadRevision(input.Revision) {
			return fmt.Errorf("rollback requires Deployment and one positive revision without replicas")
		}
	default:
		return fmt.Errorf("unsupported conditional workload action")
	}
	return nil
}

func validWorkloadRevision(value string) bool {
	if value == "" || len(value) > 19 || value[0] == '0' {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	return err == nil && revision > 0
}

// The returned UID/version belongs to the API response. Applied acknowledges
// an update, not observed rollout readiness. DryRun never changes live state.
type ConditionalWorkloadActionReceipt struct {
	Applied         bool                          `json:"applied"`
	DryRun          bool                          `json:"dryRun"`
	Kind            string                        `json:"kind"`
	Name            string                        `json:"name"`
	Namespace       string                        `json:"namespace"`
	Action          string                        `json:"action"`
	UID             string                        `json:"uid"`
	ResourceVersion string                        `json:"resourceVersion"`
	Preconditions   ResourceMutationPreconditions `json:"preconditions"`
}
