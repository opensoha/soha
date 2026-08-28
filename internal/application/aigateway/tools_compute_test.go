package aigateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appcompute "github.com/opensoha/soha/internal/application/compute"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type computeReadFake struct{}

func (computeReadFake) Overview(context.Context, domainidentity.Principal) (sohaapi.ComputeOverview, error) {
	return sohaapi.ComputeOverview{Attention: []sohaapi.ComputeAttention{}, ProviderHealth: []sohaapi.ComputeProviderHealth{}, Warnings: []sohaapi.ComputeWarning{}}, nil
}

func (computeReadFake) GetResource(_ context.Context, _ domainidentity.Principal, domain, kind, id string) (map[string]any, error) {
	return map[string]any{"domain": domain, "kind": kind, "id": id}, nil
}

func (computeReadFake) ListResourceRelations(context.Context, domainidentity.Principal, string, string, string, string, int) (sohaapi.ComputeResourceRelations, error) {
	return sohaapi.ComputeResourceRelations{Relations: []sohaapi.ComputeResourceRelation{}}, nil
}

func (computeReadFake) ListTasks(context.Context, domainidentity.Principal, appcompute.TaskFilter) (sohaapi.ComputeTaskListEnvelope, error) {
	return sohaapi.ComputeTaskListEnvelope{Items: []sohaapi.ComputeTaskView{}}, nil
}

func (computeReadFake) GetTask(_ context.Context, _ domainidentity.Principal, domain, id string) (sohaapi.ComputeTaskView, error) {
	return sohaapi.ComputeTaskView{ID: id, Domain: sohaapi.ComputeTaskDomain(domain), SourceType: "virtualization_task", SourceID: id, Kind: "vm_action", Category: sohaapi.ComputeTaskCategoryLifecycle, NormalizedStatus: sohaapi.ComputeTaskStatusRunning, RawStatus: "running", Resources: []sohaapi.ComputeResourceRef{}, AvailableActions: []sohaapi.ComputeTaskAction{}, CreatedAt: time.Now().UTC()}, nil
}

func (computeReadFake) ListTaskLogs(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskLogListEnvelope, error) {
	return sohaapi.ComputeTaskLogListEnvelope{Items: []sohaapi.ComputeTaskLog{{ID: "log-1", TaskID: "task-1", LogLevel: "info", Message: "Authorization=Bearer backend-secret", Payload: `{"token":"payload-secret","step":1}`, CreatedAt: time.Now().UTC()}}}, nil
}

func TestComputeToolsExposeReadEvidenceAndRedactLogs(t *testing.T) {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"developer": {appaccess.PermAIGatewayInvoke},
	}}), nil)
	service.SetComputeService(computeReadFake{})

	overview, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "compute.overview.read", Input: map[string]any{}})
	if err != nil || overview.Result != "success" {
		t.Fatalf("overview result=%#v err=%v", overview, err)
	}
	logs, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "compute.task_logs.list", Input: map[string]any{"domain": "virtualization", "taskId": "task-1"}})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(toJSONString(logs.Output), "\\u003c", "<"), "\\u003e", ">"))
	if strings.Contains(text, "backend-secret") || strings.Contains(text, "payload-secret") || !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("compute logs were not redacted: %s", text)
	}
	if logs.RelatedIDs["taskId"] != "task-1" {
		t.Fatalf("related ids = %#v", logs.RelatedIDs)
	}
}

func toJSONString(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
