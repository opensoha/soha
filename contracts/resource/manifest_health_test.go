package resource

import (
	"encoding/json"
	"testing"
)

func TestManifestHealth(t *testing.T) {
	for _, test := range []struct{ name, object, want string }{
		{"configuration", `{"apiVersion":"v1","kind":"ConfigMap"}`, "healthy"},
		{"unknown CR", `{"apiVersion":"example.com/v1","kind":"Database","metadata":{"generation":2}}`, "unknown"},
		{"custom kind cannot impersonate built-in", `{"apiVersion":"example.com/v1","kind":"ConfigMap"}`, "unknown"},
		{"unobserved generation", `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"generation":2},"spec":{"replicas":1},"status":{"observedGeneration":1,"replicas":1,"updatedReplicas":1,"availableReplicas":1,"conditions":[{"type":"Available","status":"True"}]}}`, "progressing"},
		{"old replicas still available", `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"generation":2},"spec":{"replicas":1},"status":{"observedGeneration":2,"replicas":2,"updatedReplicas":1,"availableReplicas":2}}`, "progressing"},
		{"available but not updated", `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"generation":2},"spec":{"replicas":1},"status":{"observedGeneration":2,"replicas":1,"updatedReplicas":0,"availableReplicas":1}}`, "progressing"},
		{"deployment converged", `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"generation":2},"spec":{"replicas":1},"status":{"observedGeneration":2,"replicas":1,"updatedReplicas":1,"availableReplicas":1}}`, "healthy"},
		{"deployment failed after available", `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"generation":2},"status":{"observedGeneration":2,"replicas":1,"updatedReplicas":1,"availableReplicas":1,"conditions":[{"type":"Available","status":"True"},{"type":"Progressing","status":"False","reason":"ProgressDeadlineExceeded"}]}}`, "degraded"},
		{"stale custom ready", `{"apiVersion":"example.com/v1","kind":"Database","metadata":{"generation":2},"status":{"conditions":[{"type":"Ready","status":"True","observedGeneration":1}]}}`, "unknown"},
		{"current custom ready", `{"apiVersion":"example.com/v1","kind":"Database","metadata":{"generation":2},"status":{"conditions":[{"type":"Ready","status":"True","observedGeneration":2}]}}`, "healthy"},
		{"conflicting current ready conditions", `{"apiVersion":"example.com/v1","kind":"Database","metadata":{"generation":2},"status":{"observedGeneration":2,"conditions":[{"type":"Available","status":"True"},{"type":"Ready","status":"False"}]}}`, "progressing"},
		{"current custom failure", `{"apiVersion":"example.com/v1","kind":"Database","metadata":{"generation":2},"status":{"observedGeneration":2,"conditions":[{"type":"Ready","status":"True"},{"type":"Failed","status":"True"}]}}`, "degraded"},
		{"deleting object", `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"deletionTimestamp":"2026-09-12T00:00:00Z"}}`, "degraded"},
		{"pod not ready", `{"apiVersion":"v1","kind":"Pod","status":{"phase":"Running","conditions":[{"type":"Ready","status":"False"}]}}`, "progressing"},
		{"statefulset partition", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3,"updateStrategy":{"rollingUpdate":{"partition":2}}},"status":{"observedGeneration":2,"readyReplicas":3,"updatedReplicas":1}}`, "healthy"},
		{"daemonset rolling", `{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"generation":2},"status":{"observedGeneration":2,"desiredNumberScheduled":3,"updatedNumberScheduled":2,"numberAvailable":3}}`, "progressing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var object map[string]any
			if err := json.Unmarshal([]byte(test.object), &object); err != nil {
				t.Fatal(err)
			}
			if got := ManifestHealth(object); got != test.want {
				t.Fatalf("health = %s, want %s", got, test.want)
			}
		})
	}
}
