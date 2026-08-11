package resource

import (
	"bytes"
	"context"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestPodOperationsProduceGovernanceEvidence(t *testing.T) {
	audit := &workloadAuditRecorder{}
	operations := &workloadOperationRecorder{}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: directGovernanceConnection()},
		Authorizer:  allowAllResourceAuthorizer{},
		Audit:       audit,
		Operations:  operations,
		DirectPods:  &stubDirectPods{},
	})
	principal := domainidentity.Principal{UserID: "user-1"}
	if err := service.Workloads().DeletePod(t.Context(), principal, "direct-cluster", "platform", "api-0"); err != nil {
		t.Fatalf("DeletePod() error = %v", err)
	}
	if _, err := service.Workloads().ExecPod(t.Context(), principal, "direct-cluster", "platform", "api-0", "api", "sensitive command", 30); err != nil {
		t.Fatalf("ExecPod() error = %v", err)
	}
	if err := service.Workloads().StreamPodTerminal(t.Context(), principal, "direct-cluster", "platform", "api-0", "api", "/bin/sh", bytes.NewReader(nil), &bytes.Buffer{}, &bytes.Buffer{}, nil); err != nil {
		t.Fatalf("StreamPodTerminal() error = %v", err)
	}

	if len(audit.entries) != 3 {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
	wantOperations := []string{"platform.pod.delete", "platform.pod.exec", "platform.pod.terminal"}
	if len(operations.entries) != len(wantOperations) {
		t.Fatalf("operation entries = %#v", operations.entries)
	}
	for index, want := range wantOperations {
		if operations.entries[index].OperationType != want {
			t.Fatalf("operation[%d] = %#v, want %q", index, operations.entries[index], want)
		}
	}
	if _, leaked := operations.entries[1].Metadata["command"]; leaked {
		t.Fatalf("exec operation metadata leaked command: %#v", operations.entries[1].Metadata)
	}
}

func TestAgentUnsupportedMutationIsAudited(t *testing.T) {
	audit := &workloadAuditRecorder{}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "agent-cluster", ConnectionMode: domaincluster.ConnectionModeAgent,
		}}},
		Authorizer: allowAllResourceAuthorizer{},
		Audit:      audit,
	})
	_, err := service.Workloads().SetCronJobSuspend(t.Context(), domainidentity.Principal{UserID: "user-1"}, "agent-cluster", "platform", "cleanup", true)
	if err == nil {
		t.Fatal("SetCronJobSuspend() error = nil, want unsupported operation")
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "failure" || audit.entries[0].Action != "suspend" {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
}

func TestCronJobSuspendProducesOperationEvidence(t *testing.T) {
	audit := &workloadAuditRecorder{}
	operations := &workloadOperationRecorder{}
	service := New(Dependencies{
		Connections:     stubConnectionResolver{connection: directGovernanceConnection()},
		Authorizer:      allowAllResourceAuthorizer{},
		Audit:           audit,
		Operations:      operations,
		DirectWorkloads: cronJobGovernanceDirect{},
	})
	if _, err := service.Workloads().SetCronJobSuspend(t.Context(), domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "platform", "cleanup", true); err != nil {
		t.Fatalf("SetCronJobSuspend() error = %v", err)
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "success" {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
	if len(operations.entries) != 1 || operations.entries[0].OperationType != "platform.cronjob.suspend" || operations.entries[0].Metadata["suspend"] != true {
		t.Fatalf("operation entries = %#v", operations.entries)
	}
}

func directGovernanceConnection() domaincluster.Connection {
	return domaincluster.Connection{Summary: domaincluster.Summary{
		ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
	}}
}

type cronJobGovernanceDirect struct{ DirectWorkloads }

func (cronJobGovernanceDirect) SetCronJobSuspend(context.Context, string, string, string, bool) (domainresource.CronJobDetailView, error) {
	return domainresource.CronJobDetailView{Name: "cleanup", Namespace: "platform"}, nil
}
