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
		{"statefulset unobserved generation", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3},"status":{"observedGeneration":1,"replicas":3,"readyReplicas":3,"updatedReplicas":3,"currentRevision":"web-abc","updateRevision":"web-abc"}}`, "progressing"},
		{"statefulset default replicas converged", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{},"status":{"observedGeneration":2,"replicas":1,"readyReplicas":1,"updatedReplicas":1,"currentRevision":"web-abc","updateRevision":"web-abc"}}`, "healthy"},
		{"statefulset no partition converged", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":3,"updatedReplicas":3,"currentRevision":"web-abc","updateRevision":"web-abc"}}`, "healthy"},
		{"statefulset no partition missing revisions", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":3,"updatedReplicas":3}}`, "progressing"},
		{"statefulset no partition revision mismatch", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":3,"updatedReplicas":3,"currentRevision":"web-old","updateRevision":"web-new"}}`, "progressing"},
		{"statefulset no partition updated short", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":3,"updatedReplicas":2,"currentRevision":"web-abc","updateRevision":"web-abc"}}`, "progressing"},
		{"statefulset extra ready replica", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":4,"updatedReplicas":3,"currentRevision":"web-abc","updateRevision":"web-abc"}}`, "progressing"},
		{"statefulset status replicas short", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3},"status":{"observedGeneration":2,"replicas":2,"readyReplicas":3,"updatedReplicas":3,"currentRevision":"web-abc","updateRevision":"web-abc"}}`, "progressing"},
		{"statefulset status replicas extra", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3},"status":{"observedGeneration":2,"replicas":4,"readyReplicas":3,"updatedReplicas":3,"currentRevision":"web-abc","updateRevision":"web-abc"}}`, "progressing"},
		{"statefulset zero replicas confirmed", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":0},"status":{"observedGeneration":2,"replicas":0,"readyReplicas":0,"updatedReplicas":0}}`, "healthy"},
		{"statefulset scaling down to zero", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":0},"status":{"observedGeneration":2,"replicas":1,"readyReplicas":1,"updatedReplicas":1}}`, "progressing"},
		{"statefulset partition", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3,"updateStrategy":{"rollingUpdate":{"partition":2}}},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":3,"updatedReplicas":1,"currentRevision":"web-old","updateRevision":"web-new"}}`, "healthy"},
		{"statefulset partition updated short", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3,"updateStrategy":{"rollingUpdate":{"partition":2}}},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":3,"updatedReplicas":0,"currentRevision":"web-old","updateRevision":"web-new"}}`, "progressing"},
		{"statefulset partition uses replica positions with nonzero start", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3,"ordinals":{"start":2},"updateStrategy":{"rollingUpdate":{"partition":2}}},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":3,"updatedReplicas":1,"currentRevision":"web-old","updateRevision":"web-new"}}`, "healthy"},
		{"statefulset on-delete manually updated replicas", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":1,"updateStrategy":{"type":"OnDelete"}},"status":{"observedGeneration":2,"replicas":1,"readyReplicas":1,"updatedReplicas":1,"currentRevision":"web-old","updateRevision":"web-new"}}`, "healthy"},
		{"statefulset partition beyond ordinal range", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3,"ordinals":{"start":2},"updateStrategy":{"rollingUpdate":{"partition":5}}},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":3,"updatedReplicas":0,"currentRevision":"web-old","updateRevision":"web-new"}}`, "healthy"},
		{"statefulset large partition bounded", `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"generation":2},"spec":{"replicas":3,"updateStrategy":{"rollingUpdate":{"partition":9007199254740992}}},"status":{"observedGeneration":2,"replicas":3,"readyReplicas":3,"updatedReplicas":0,"currentRevision":"web-old","updateRevision":"web-new"}}`, "healthy"},
		{"stale custom ready", `{"apiVersion":"example.com/v1","kind":"Database","metadata":{"generation":2},"status":{"conditions":[{"type":"Ready","status":"True","observedGeneration":1}]}}`, "unknown"},
		{"current custom ready", `{"apiVersion":"example.com/v1","kind":"Database","metadata":{"generation":2},"status":{"conditions":[{"type":"Ready","status":"True","observedGeneration":2}]}}`, "healthy"},
		{"conflicting current ready conditions", `{"apiVersion":"example.com/v1","kind":"Database","metadata":{"generation":2},"status":{"observedGeneration":2,"conditions":[{"type":"Available","status":"True"},{"type":"Ready","status":"False"}]}}`, "progressing"},
		{"current custom failure", `{"apiVersion":"example.com/v1","kind":"Database","metadata":{"generation":2},"status":{"observedGeneration":2,"conditions":[{"type":"Ready","status":"True"},{"type":"Failed","status":"True"}]}}`, "degraded"},
		{"deleting object", `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"deletionTimestamp":"2026-09-12T00:00:00Z"}}`, "degraded"},
		{"pod succeeded", `{"apiVersion":"v1","kind":"Pod","status":{"phase":"Succeeded"}}`, "healthy"},
		{"pod failed", `{"apiVersion":"v1","kind":"Pod","status":{"phase":"Failed"}}`, "degraded"},
		{"pod ready", `{"apiVersion":"v1","kind":"Pod","status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`, "healthy"},
		{"pod not ready", `{"apiVersion":"v1","kind":"Pod","status":{"phase":"Running","conditions":[{"type":"Ready","status":"False"}]}}`, "progressing"},
		{"job active", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":1},"status":{"active":1}}`, "progressing"},
		{"job ready is not terminal", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":1},"status":{"conditions":[{"type":"Ready","status":"True"}]}}`, "progressing"},
		{"job complete", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":1},"status":{"conditions":[{"type":"Complete","status":"True"}]}}`, "healthy"},
		{"job complete ignores generation", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":2},"status":{"conditions":[{"type":"Complete","status":"True"}]}}`, "healthy"},
		{"job failed", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":1},"status":{"conditions":[{"type":"Failed","status":"True"}]}}`, "degraded"},
		{"job failure wins", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":1},"status":{"conditions":[{"type":"Complete","status":"True"},{"type":"Failed","status":"True"}]}}`, "degraded"},
		{"job failure wins regardless of order", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":1},"status":{"conditions":[{"type":"Failed","status":"True"},{"type":"Complete","status":"True"}]}}`, "degraded"},
		{"job nonterminal conditions", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":1},"status":{"conditions":[{"type":"Complete","status":"False"},{"type":"Failed","status":"False"}]}}`, "progressing"},
		{"job phase is not terminal evidence", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":1},"status":{"phase":"Succeeded"}}`, "progressing"},
		{"job failed phase is not terminal evidence", `{"apiVersion":"batch/v1","kind":"Job","metadata":{"generation":1},"status":{"phase":"Failed"}}`, "progressing"},
		{"daemonset converged", `{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"generation":2},"status":{"observedGeneration":2,"desiredNumberScheduled":3,"updatedNumberScheduled":3,"numberAvailable":3}}`, "healthy"},
		{"daemonset rolling", `{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"generation":2},"status":{"observedGeneration":2,"desiredNumberScheduled":3,"updatedNumberScheduled":2,"numberAvailable":3}}`, "progressing"},
		{"daemonset extra available", `{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"generation":2},"status":{"observedGeneration":2,"desiredNumberScheduled":3,"updatedNumberScheduled":3,"numberAvailable":4}}`, "progressing"},
		{"daemonset extra updated", `{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"generation":2},"status":{"observedGeneration":2,"desiredNumberScheduled":3,"updatedNumberScheduled":4,"numberAvailable":3}}`, "progressing"},
		{"daemonset zero desired", `{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"generation":2},"status":{"observedGeneration":2,"desiredNumberScheduled":0,"updatedNumberScheduled":0,"numberAvailable":0}}`, "healthy"},
		{"daemonset unobserved generation", `{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"generation":2},"status":{"observedGeneration":1,"desiredNumberScheduled":3,"updatedNumberScheduled":3,"numberAvailable":3}}`, "progressing"},
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
