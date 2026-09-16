package aigateway

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type contractDockerService struct {
	DockerOperationsService
	operation domaindocker.Operation
	calls     int
}

func (s *contractDockerService) DeployProject(_ context.Context, _ domainidentity.Principal, _ string, _ domaindocker.ProjectDeployInput) (domaindocker.Operation, error) {
	s.calls++
	return s.operation, nil
}

func (s *contractDockerService) GetOperation(_ context.Context, _ domainidentity.Principal, id string) (domaindocker.Operation, error) {
	s.calls++
	if id != s.operation.ID {
		return domaindocker.Operation{}, apperrors.ErrNotFound
	}
	return s.operation, nil
}

func contractTestService(repo *memoryGatewayRepository, docker *contractDockerService) *Service {
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"admin":     {appaccess.PermAIGatewayManage},
		"developer": {appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermDockerProjectsDeploy, appaccess.PermDockerOperationsView},
	}}), nil, repo)
	service.docker = docker
	return service
}

func TestVersionedCapabilityRejectsInvalidCallBeforeApproval(t *testing.T) {
	for _, scenario := range []string{"old-version", "missing-key", "blank-key", "wrong-input-type"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &memoryGatewayRepository{}
			docker := &contractDockerService{}
			service := contractTestService(repo, docker)
			request := domainaigateway.ToolInvocationRequest{ToolName: "docker.projects.deploy.trigger", CapabilityVersion: "1", Input: map[string]any{"projectId": "project-1", "idempotencyKey": "intent-1"}}
			want := apperrors.ErrInvalidArgument
			switch scenario {
			case "old-version":
				request.CapabilityVersion = "0"
				want = apperrors.ErrConflict
			case "missing-key":
				delete(request.Input, "idempotencyKey")
			case "blank-key":
				request.Input["idempotencyKey"] = "  "
			case "wrong-input-type":
				request.Input["projectId"] = 42
			}
			_, err := service.InvokeTool(context.Background(), testPrincipal("developer"), request)
			if !errors.Is(err, want) || docker.calls != 0 || len(repo.approvalRequests) != 0 {
				t.Fatalf("invalid request reached execution or approval: err=%v calls=%d approvals=%d", err, docker.calls, len(repo.approvalRequests))
			}
		})
	}
}

func TestCapabilityApprovalPinsVersionAndReturnsDomainTask(t *testing.T) {
	for _, scenario := range []string{"unchanged", "changed-version", "removed-version", "revoked", "changed-host", "missing-frozen-scope"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			repo := &memoryGatewayRepository{}
			docker := &contractDockerService{operation: domaindocker.Operation{ID: "operation-1", Status: "queued"}}
			service := contractTestService(repo, docker)
			// Old callers also pin the server's current version when an approval is created.
			held, err := service.InvokeTool(ctx, testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "docker.projects.deploy.trigger", Input: map[string]any{"projectId": "project-1", "idempotencyKey": "intent-1"}})
			if err != nil || held.CapabilityVersion != "1" || held.Task != nil || docker.calls != 0 {
				t.Fatalf("invalid held result: %+v %v", held, err)
			}
			id := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
			alterCapabilityApprovalScenario(scenario, service, repo, docker, id, held.ToolName)
			decision, err := service.ApproveApprovalRequest(ctx, testPrincipal("admin"), id, domainaigateway.ApprovalDecisionInput{})
			if scenario != "unchanged" {
				if err == nil || docker.calls != 0 || decision.Request.Status != "failed" {
					t.Fatalf("unsafe approval replay: err=%v calls=%d status=%s", err, docker.calls, decision.Request.Status)
				}
				return
			}
			if err != nil || decision.Invocation == nil || decision.Invocation.Task == nil || decision.Invocation.Task.Terminal || decision.Invocation.Task.ID != docker.operation.ID {
				t.Fatalf("missing durable task reference: %+v %v", decision, err)
			}
			verifyCapabilityContinuation(t, repo, docker, decision.Invocation.Task)
		})
	}
}

func verifyCapabilityContinuation(t *testing.T, repo *memoryGatewayRepository, docker *contractDockerService, task *domainaigateway.CapabilityTaskRef) {
	t.Helper()
	ctx := context.Background()
	// Recreate the Gateway: continuation is resolved by the domain, not process memory.
	service := contractTestService(repo, docker)
	docker.operation.Status = "completed"
	call := task.StatusCall
	result, err := service.InvokeTool(ctx, testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: call.ToolName, CapabilityVersion: call.CapabilityVersion, Input: call.Input})
	if err != nil || result.Task == nil || !result.Task.Terminal || result.Task.ID != docker.operation.ID {
		t.Fatalf("task could not be resumed through a new Gateway: %+v %v", result, err)
	}
	calls := docker.calls
	_, err = service.InvokeTool(ctx, testPrincipal("no-access"), domainaigateway.ToolInvocationRequest{ToolName: call.ToolName, Input: call.Input})
	if err == nil || docker.calls != calls {
		t.Fatal("task reference bypassed current authorization")
	}
}

func alterCapabilityApprovalScenario(scenario string, service *Service, repo *memoryGatewayRepository, docker *contractDockerService, id, toolName string) {
	if scenario == "changed-version" || scenario == "removed-version" {
		tools := defaultTools()
		for i := range tools {
			if tools[i].Name == toolName {
				tools[i].Version = "2"
				if scenario == "removed-version" {
					tools[i].Version = ""
				}
			}
		}
		service.SetCapabilityProviders(testCapabilityProvider{tools: tools})
	}
	if scenario == "revoked" {
		service.identity = approvalPrincipalFunc(func(context.Context, string) (domainidentity.Principal, error) {
			return testPrincipal("no-access"), nil
		})
	}
	if scenario == "changed-host" {
		docker.operation.HostID = "host-after-approval"
	}
	if scenario == "missing-frozen-scope" {
		for index := range repo.approvalRequests {
			if repo.approvalRequests[index].ID == id {
				delete(repo.approvalRequests[index].ResourceScope, "invocationScopes")
			}
		}
	}
}

func TestCapabilityTaskReferenceRespectsRedaction(t *testing.T) {
	tool, _ := newDefaultCapabilityRegistry().ToolByName("docker.projects.deploy.trigger")
	task := capabilityTaskRef(tool, domaindocker.Operation{ID: "operation-1", Status: "queued"})
	for _, output := range []any{nil, map[string]any{"id": "[REDACTED]", "status": "queued"}, map[string]any{"id": "operation-1"}} {
		if visibleCapabilityTaskRef(task, output) != nil {
			t.Fatal("task metadata restored redacted output")
		}
	}
}

func TestCapabilitySchemaRejectsExternalReferencesAndKeepsLegacyCalls(t *testing.T) {
	tool := domainaigateway.ToolCapability{Name: "example", Version: "1", InputSchema: map[string]any{"$ref": "file:///etc/passwd"}}
	if err := validateCapabilityInput(tool, nil); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("external schema reference accepted: %v", err)
	}
	tool.Version = ""
	if err := validateCapabilityInput(tool, nil); err != nil {
		t.Fatalf("legacy validation changed: %v", err)
	}
}

type taskCapabilityProvider struct{ testCapabilityProvider }

func (taskCapabilityProvider) TaskReference(_ domainaigateway.ToolCapability, _, visible any) *domainaigateway.CapabilityTaskRef {
	object, _ := visible.(map[string]any)
	id, _ := object["id"].(string)
	if id == "" {
		return nil
	}
	return &domainaigateway.CapabilityTaskRef{Kind: "reports.query", ID: id, Status: "running", StatusCall: domainaigateway.CapabilityCall{ToolName: "reports.query.get", CapabilityVersion: "1", Input: map[string]any{"id": id}}}
}

func TestProviderContributesTaskReferenceWithoutGatewayDomainBranch(t *testing.T) {
	service := contractTestService(&memoryGatewayRepository{}, nil)
	service.SetCapabilityProviders(taskCapabilityProvider{testCapabilityProvider{
		tools: []domainaigateway.ToolCapability{{Name: "reports.query.get", Version: "1", RiskLevel: domainaigateway.RiskLevelRead, PermissionKeys: []string{appaccess.PermAIGatewayInvoke}, InputSchema: gatewayObjectSchema(nil, map[string]any{})}},
		invoke: func(context.Context, domainidentity.Principal, domainaigateway.ToolCapability, map[string]any) (any, map[string]any, error) {
			return map[string]any{"id": "query-1"}, nil, nil
		},
	}})
	result, err := service.InvokeTool(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: "reports.query.get", CapabilityVersion: "1"})
	if err != nil || result.Task == nil || result.Task.Kind != "reports.query" || result.Task.ID != "query-1" {
		t.Fatalf("provider task projection failed: %+v %v", result, err)
	}
}

func TestDockerOperationScopeUsesTheActualRequestedID(t *testing.T) {
	scope := standardGatewayScope(map[string]any{"operationId": "operation-1", "dockerOperation": "operation-2"}, nil)
	if !gatewayResourceScopeMatches(map[string]any{"dockerOperation": []string{"operation-1"}}, scope) || gatewayResourceScopeMatches(map[string]any{"dockerOperation": []string{"operation-2"}}, scope) {
		t.Fatal("operation scope does not follow the executed operationId")
	}
}

func TestCapabilityVersionIsRetainedAcrossAuditSinks(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := contractTestService(repo, nil)
	audit := &captureAuditRecorder{}
	operations := &captureOperationRecorder{}
	service.audit = audit
	service.operations = operations
	request := domainaigateway.ToolInvocationRequest{CapabilityVersion: "old"}
	tool := domainaigateway.ToolCapability{Name: "reports.query", Version: "current", RiskLevel: domainaigateway.RiskLevelRead}
	if err := service.recordToolAudit(context.Background(), testPrincipal("developer"), request, tool, "denied", "version mismatch", nil); err != nil {
		t.Fatal(err)
	}
	if len(audit.entries) != 1 || len(operations.entries) != 1 || len(repo.auditLogs) != 1 {
		t.Fatal("versioned invocation did not reach each audit sink")
	}
	for _, metadata := range []map[string]any{audit.entries[0].Metadata, operations.entries[0].Metadata, repo.auditLogs[0].Metadata} {
		if metadata["capabilityVersion"] != "current" || metadata["requestedCapabilityVersion"] != "old" {
			t.Fatalf("audit lost requested or resolved capability version: %+v", metadata)
		}
	}
}

// Keys are transported by Gateway, outside domain JSON. Recovery must retain
// them just like dispatch, or a lost response can never find its durable receipt.
type recoveryDockerService struct {
	DockerOperationsService
	key, id, action string
}

func (s *recoveryDockerService) FindQuickCreateOperation(_ context.Context, _ domainidentity.Principal, input domaindocker.QuickCreateHostInput) (domaindocker.Operation, error) {
	s.key = input.IdempotencyKey
	return domaindocker.Operation{ID: "receipt", Status: "running"}, nil
}
func (s *recoveryDockerService) FindProjectDeployOperation(_ context.Context, _ domainidentity.Principal, id string, input domaindocker.ProjectDeployInput) (domaindocker.Operation, error) {
	s.key, s.id, s.action = input.IdempotencyKey, id, input.Action
	return domaindocker.Operation{ID: "receipt", Status: "running"}, nil
}
func (s *recoveryDockerService) FindServiceActionOperation(_ context.Context, _ domainidentity.Principal, id string, input domaindocker.ServiceActionInput) (domaindocker.Operation, error) {
	s.key, s.id, s.action = input.IdempotencyKey, id, input.Action
	return domaindocker.Operation{ID: "receipt", Status: "running"}, nil
}
func TestCapabilityDockerRecoveryRetainsTransportedKey(t *testing.T) {
	for _, name := range []string{"docker.hosts.quick_create.trigger", "docker.projects.deploy.trigger", "docker.services.action.trigger"} {
		t.Run(name, func(t *testing.T) {
			docker := &recoveryDockerService{}
			service := &Service{docker: docker}
			output, found, err := service.recoverDockerCapability(context.Background(), testPrincipal("developer"), domainaigateway.ToolInvocationRequest{ToolName: name, Input: map[string]any{"idempotencyKey": "frozen-intent-key", "projectId": "object", "serviceId": "object", "action": "restart"}})
			operation, ok := output.(domaindocker.Operation)
			if err != nil || !found || !ok || operation.ID != "receipt" || docker.key != "frozen-intent-key" {
				t.Fatalf("receipt recovery lost key: found=%v key=%q err=%v", found, docker.key, err)
			}
			if name != "docker.hosts.quick_create.trigger" && (docker.id != "object" || docker.action != "restart") {
				t.Fatalf("frozen action changed: %+v", docker)
			}
		})
	}
}

func (s *contractDockerService) ToolInvocationScopes(_ context.Context, _ domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) ([]map[string]string, error) {
	scope := map[string]string{"hostId": firstNonEmpty(s.operation.HostID, "host-1")}
	for _, key := range []string{"projectId", "operationId", "serviceId"} {
		if value := firstMapString(input, key); value != "" {
			scope[key] = value
		}
	}
	return []map[string]string{scope}, nil
}
