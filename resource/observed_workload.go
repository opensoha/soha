package resource

import "regexp"

var workloadUIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// ValidWorkloadUID validates an observed Kubernetes UID at the API boundary.
func ValidWorkloadUID(uid string) bool {
	return workloadUIDPattern.MatchString(uid)
}

// ObservedWorkloadDeleteRequest is the Agent request for UID-guarded workload deletion.
type ObservedWorkloadDeleteRequest struct {
	Namespace   string `json:"namespace"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	ExpectedUID string `json:"expectedUid"`
}
