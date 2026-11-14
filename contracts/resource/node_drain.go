package resource

// NodeDrainError reports an incomplete drain without exposing provider error text.
type NodeDrainError struct {
	Code  string
	Cause error
}

func (e *NodeDrainError) Error() string {
	switch e.Code {
	case "node_drain_pdb_blocked":
		return "PodDisruptionBudget blocked eviction. The drain did not complete; inspect remaining pods and scheduling before retrying."
	case "node_drain_eviction_blocked":
		return "Eviction was repeatedly refused. Check disruption budgets or API throttling and inspect remaining pods before retrying."
	case "node_drain_timeout":
		return "The drain timed out before all evictable pods terminated. Inspect remaining pods and scheduling before retrying."
	case "node_drain_canceled":
		return "The drain was interrupted. Previously submitted evictions may still finish; inspect remaining pods and scheduling."
	case "node_drain_pod_selection":
		return "Evictable pods could not be selected. Check pod read access and drain options for unmanaged pods or emptyDir data."
	default:
		return "The drain did not complete. Inspect remaining pods, disruption budgets and permissions before retrying."
	}
}

func (e *NodeDrainError) Unwrap() error { return e.Cause }

func IsNodeDrainErrorCode(code string) bool {
	switch code {
	case "node_drain_pdb_blocked", "node_drain_eviction_blocked", "node_drain_timeout", "node_drain_canceled", "node_drain_pod_selection", "node_drain_failed":
		return true
	}
	return false
}
