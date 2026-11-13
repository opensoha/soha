package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"testing"
	"time"
)

func preparedDSHCall(t *testing.T, s *Service, store *nativeLoopStore, cp *dshCheckpoint) DSHModelCall {
	t.Helper()
	cp.Start = nil
	cp.Submitted = false
	var err error
	store.lease, err = s.prepareDSH(t.Context(), store.lease, cp)
	if err != nil {
		t.Fatal(err)
	}
	*cp, err = readDSHCheckpoint(store.lease)
	if err != nil {
		t.Fatal(err)
	}
	request := appaigateway.WorkbenchRelayRequest{Messages: []appaigateway.WorkbenchRelayMessage{{Role: "user", Content: dshFrozenUser(*cp.Start)}}}
	for _, tool := range cp.Start.Tools {
		var schema map[string]any
		_ = json.Unmarshal(tool.InputSchema, &schema)
		request.Tools = append(request.Tools, appaigateway.WorkbenchTool{Name: tool.Name, Parameters: schema})
	}
	raw := json.RawMessage(`{"model":"soha-workbench"}`)
	sum := sha256.Sum256(raw)
	return DSHModelCall{DSHIdentity: cp.Start.DSHIdentity, CallID: "first", ContextRevision: cp.Start.ContextRevision, ToolCatalogRevision: cp.Start.ToolCatalogRevision, InputHash: hex.EncodeToString(sum[:]), RawBody: raw, Request: request}
}

func TestDSHProtectedModelTextIsBufferedAcrossDeltaBoundaries(t *testing.T) {
	s, store, cp := dshFixture(t)
	gateway := s.runtime.Stream.(*nativeLoopGateway)
	private := "PRIVATE_SPLIT_STREAM_MARKER"
	tool := domainai.ToolCapability{Name: "k8s.pods.exec", Version: "1", RiskLevel: "high", InputSchema: map[string]any{"type": "object"}, Execution: &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true, Checks: []domainai.CapabilityCheckReference{{ToolName: "test.assess", CapabilityVersion: "1", Purpose: "verification"}}}}
	gateway.catalog = append(gateway.catalog, tool)
	actor := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	_, catalog, err := s.freezeNativeCatalog(t.Context(), actor, "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := nativeDefinition(tool)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Tasks[tool.Name] = definition
	store.lease.Run.Input["_sohaNativeCatalog"] = catalog
	store.lease.Run.ToolBindings = appendNativeTaskBindings(nil, catalog.Tasks)
	s.runtime.Stream = &nativeProtectedStreamFixture{gateway, private}
	call := preparedDSHCall(t, s, store, &cp)
	cp.Tasks = []nativeTaskCheckpoint{{ID: "verified-task", State: "completed", VerifiedThrough: time.Now(), Input: domainai.CapabilityTaskInput{Plan: domainai.CapabilityPlan{Steps: []domainai.CapabilityPlanStep{{Call: sohaapi.CapabilityCall{ToolName: "k8s.pods.exec", Input: map[string]any{"execution": map[string]any{"argv": []string{private}}}}}}}}}}
	store.lease.Checkpoint, _ = json.Marshal(cp)
	var visible strings.Builder
	response, err := s.InvokeDSHModelStream(t.Context(), cp.Start.ToolGrant, call, func(d appaigateway.WorkbenchRelayStreamDelta) bool { visible.WriteString(d.ContentDelta); return true })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(visible.String(), "PRIVATE_") || !strings.Contains(visible.String(), "[PROTECTED]") || strings.Contains(response.Content, private) {
		t.Fatalf("protected model output leaked: %q", visible.String())
	}
	raw, _ := json.Marshal(store.lease.Run.Output)
	if strings.Contains(string(raw), private) {
		t.Fatal("protected public run leaked")
	}
}

func TestLegacyRunResumeRequiresReconciliationWithoutCheckpointMutation(t *testing.T) {
	s, store, _, _ := executionGovernanceFixture(t)
	store.lease.Run.ProviderKind = "native"
	store.lease.Checkpoint = json.RawMessage(`{"pending":[{"state":"dispatching"}],"userInput":"original"}`)
	before := string(store.lease.Checkpoint)
	_, err := s.ResumeAgentRun(t.Context(), domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}, store.lease.Run.ID, "new input")
	if !errors.Is(err, apperrors.ErrConflict) || !strings.Contains(err.Error(), "reconciliation") || string(store.lease.Checkpoint) != before {
		t.Fatalf("legacy checkpoint resumed: %v", err)
	}
}

func TestDSHModelDispatchPersistsContextAndUnknownKeepsReservation(t *testing.T) {
	s, store, cp := dshFixture(t)
	gateway := s.runtime.Stream.(*nativeLoopGateway)
	gateway.model = func(_ context.Context, request appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		saved, err := readDSHCheckpoint(store.lease)
		if err != nil || saved.ModelContext == nil || saved.ModelContext.Status != "prepared" || saved.ModelContext.RequestedPublicModel != request.PublicModel || len(saved.ModelContext.RequestHash) != 64 || saved.ModelReservation <= 0 {
			t.Fatal("model dispatched before durable context and reservation")
		}
		return appaigateway.WorkbenchRelayResponse{}, errors.New("upstream outcome unknown")
	}
	call := preparedDSHCall(t, s, store, &cp)
	_, err := s.InvokeDSHModelStream(t.Context(), cp.Start.ToolGrant, call, func(appaigateway.WorkbenchRelayStreamDelta) bool { return true })
	if err == nil {
		t.Fatal("unknown model accepted")
	}
	saved, err := readDSHCheckpoint(store.lease)
	if err != nil || saved.UsageKnown || saved.BudgetTokens <= 0 || saved.ModelContext.GatewayRequestID != "" || saved.ModelResponses[call.CallID].Content != "" {
		t.Fatal("unknown result lost reservation or invented receipt")
	}
}

func attachDSHQueueFixture(t *testing.T, service *Service, repo *inspectionAuthzTestRepository) *nativeQueueCapture {
	t.Helper()
	template, store, gateway, _ := executionGovernanceFixture(t)
	professional, err := appaigateway.NewProfessionalCapabilityProvider(service.RunProfessionalCapability)
	if err != nil {
		t.Fatal(err)
	}
	gateway.catalog = professional.Tools()
	service.runtime = template.runtime
	if service.settings == nil {
		service.settings = template.settings
	}
	service.nativeAuthorization = template.nativeAuthorization
	service.agentPrincipals = template.agentPrincipals
	service.nativeTaskParents = template.nativeTaskParents
	service.nativeTasks = template.nativeTasks
	service.dshRuntime = &dshTestRuntime{}
	service.dshRuns = store
	capture := &nativeQueueCapture{inspectionAuthzTestRepository: repo, nativeLoopStore: store}
	service.nativeRuns = capture
	return capture
}

func TestLegacyCancelDoesNotChangeFrozenRun(t *testing.T) {
	s, store, _, _ := executionGovernanceFixture(t)
	store.lease.Run.ProviderKind = "native"
	repo := s.sessions.(*inspectionAuthzTestRepository)
	repo.agentRuns = []domaincopilot.AgentRun{store.lease.Run}
	s.agentRuns = repo
	_, err := s.CancelAgentRun(t.Context(), domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}, store.lease.Run.ID)
	if !errors.Is(err, apperrors.ErrConflict) || store.lease.CancelRequested {
		t.Fatalf("legacy cancel changed frozen run: %v", err)
	}
}
