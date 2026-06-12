package monitoring

import (
	"strings"
	"testing"
	"time"

	domaingovernance "github.com/opensoha/soha/internal/domain/governance"
)

func TestGovernanceAlertEventInputBuildsAlertEvent(t *testing.T) {
	createdAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	event := governanceAlertEventInput(domaingovernance.AlertInput{
		Source:        "operation",
		EventID:       "op-1",
		ActorID:       "user-1",
		ActorName:     "Operator",
		OperationType: "platform.pod.delete",
		Result:        "failure",
		Summary:       "delete denied token=raw-token",
		ClusterID:     "cluster-a",
		Namespace:     "prod",
		ResourceKind:  "Pod",
		ResourceName:  "api-0",
		RequestID:     "req-1",
		RequestPath:   "/api/v1/pods/api-0",
		RequestMethod: "DELETE",
		Severity:      "critical",
		Labels:        map[string]string{"result": "failure"},
		Annotations:   map[string]string{"approvalRequestId": "approval-1", "authorization": "Bearer raw-bearer"},
		CreatedAt:     createdAt,
	})

	if event.ID != "governance:operation:op-1" || event.SourceType != "governance" || event.SourceSystem != domaingovernance.AlertSourceSystem {
		t.Fatalf("unexpected event identity: %#v", event)
	}
	if event.Fingerprint != "governance:operation:platform.pod.delete:req-1" {
		t.Fatalf("fingerprint = %q", event.Fingerprint)
	}
	if event.Severity != "critical" || event.Status != "firing" || event.CurrentState != "firing" {
		t.Fatalf("unexpected status fields: %#v", event)
	}
	if event.ClusterID != "cluster-a" || event.Namespace != "prod" {
		t.Fatalf("unexpected scope: %#v", event)
	}
	if event.Labels["category"] != "governance" || event.Labels["source"] != "operation" || event.Labels["result"] != "failure" {
		t.Fatalf("unexpected labels: %#v", event.Labels)
	}
	if event.Annotations["approvalRequestId"] != "approval-1" || event.Annotations["resourceKind"] != "Pod" || event.Annotations["resourceName"] != "api-0" {
		t.Fatalf("unexpected annotations: %#v", event.Annotations)
	}
	for _, leaked := range []string{"raw-token", "raw-bearer"} {
		if strings.Contains(event.Summary, leaked) || strings.Contains(event.Annotations["authorization"], leaked) {
			t.Fatalf("event leaked %q: %#v", leaked, event)
		}
	}
}
