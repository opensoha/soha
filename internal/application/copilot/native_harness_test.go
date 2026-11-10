package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestNativeTaskIntentValidationAndIsolation(t *testing.T) {
	zero := 0
	intent := &domaincopilot.TaskIntent{Capabilities: []sohaapi.WorkbenchTaskIntentCapabilities{"trace"}, MinDurationMs: &zero}
	if err := validateNativeTaskIntent(intent); err != nil {
		t.Fatalf("explicit zero rejected: %v", err)
	}
	input := nativeLegacyIntent(domaincopilot.SessionMetadata{PinnedContext: map[string]any{"inspectionRunId": "owned"}}, domaincopilot.WorkbenchSendMessageInput{TaskIntent: intent})
	if len(intent.InspectionRunIDs) != 0 || len(input.TaskIntent.InspectionRunIDs) != 1 {
		t.Fatal("request intent mutated instead of frozen")
	}
	negative := -1
	for _, invalid := range []*domaincopilot.TaskIntent{
		{Capabilities: []sohaapi.WorkbenchTaskIntentCapabilities{"unknown"}},
		{Capabilities: []sohaapi.WorkbenchTaskIntentCapabilities{"trace", "trace"}},
		{MetricKey: strings.Repeat("中", 257)}, {MinDurationMs: &negative},
		{PerformanceDataSourceID: strings.Repeat("中", 257)},
		{TraceDataSourceID: strings.Repeat("中", 257)},
		{RootCauseProfileID: strings.Repeat("中", 257)},
		{InspectionRunIDs: []string{strings.Repeat("中", 129)}},
		{InspectionRunIDs: []string{"one", "one"}},
	} {
		if !errors.Is(validateNativeTaskIntent(invalid), apperrors.ErrInvalidArgument) {
			t.Fatalf("invalid intent accepted: %+v", invalid)
		}
	}
}

func TestProfessionalArgumentsPinEachCapabilityInput(t *testing.T) {
	run := domaincopilot.AgentRun{SessionID: "owned", Scope: domaincopilot.SessionScope{ClusterID: "cluster", TimeRangeMinutes: 60}, Input: map[string]any{"locale": "en-US", "taskIntent": map[string]any{"dataSourceId": "legacy", "performanceDataSourceId": "metrics", "traceDataSourceId": "traces", "rootCauseProfileId": "root-profile", "metricKey": "http.request.duration", "traceId": "trace", "minDurationMs": 0, "inspectionRunIds": []string{"inspection"}}}}
	for _, item := range []struct{ tool, source, key, value string }{
		{"copilot.performance.analyze", "metrics", "metricKey", "http.request.duration"},
		{"copilot.trace.analyze", "traces", "traceId", "trace"},
		{"copilot.root_cause.analyze", "legacy", "rootCauseProfileId", "root-profile"},
	} {
		args, err := professionalChatArguments(run, item.tool, map[string]any{"question": "Analyze receipts"})
		if err != nil {
			t.Fatal(err)
		}
		intent := mapValue(args["intent"])
		if intent["dataSourceId"] != item.source || intent[item.key] != item.value || args["locale"] != "en-US" || mapValue(args["scope"])["clusterId"] != "cluster" {
			t.Fatalf("wrong capability input: %s %+v", item.tool, args)
		}
		if item.tool == "copilot.trace.analyze" && intent["minDurationMs"] != float64(0) {
			t.Fatal("explicit zero trace threshold was lost")
		}
	}
	for _, input := range []map[string]any{
		{"sessionId": "foreign"}, {"scope": map[string]any{"clusterId": "other"}},
		{"intent": map[string]any{"dataSourceId": "other"}},
		{"intent": map[string]any{"performanceDataSourceId": "other"}},
		{"intent": map[string]any{"metricKey": "other"}},
	} {
		if _, err := professionalChatArguments(run, "copilot.performance.analyze", input); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("changed frozen input accepted: %+v %v", input, err)
		}
	}
	legacy := domaincopilot.AgentRun{Input: map[string]any{"taskIntent": map[string]any{"dataSourceId": "legacy"}}}
	if _, err := nativeProfessionalIntent(legacy, "copilot.trace.analyze", map[string]any{"intent": map[string]any{"traceDataSourceId": "other"}}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("capability-specific field bypassed legacy source constraint")
	}
}

func TestSelectedRootCauseProfileUsesExistingOwnerAndRejectsOtherModes(t *testing.T) {
	service, repo := newInspectionAuthzTestService(map[string][]string{"root": {appaccess.PermObserveAIChatUse, appaccess.PermObserveAIRootCauseRun}})
	repo.session = domaincopilot.Session{ID: "session", CreatedBy: "owner", Metadata: sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general", Scope: domaincopilot.SessionScope{ClusterID: "cluster"}})}
	principal := domainidentity.Principal{UserID: "owner", Roles: []string{"root"}}
	for _, id := range []string{"profile:inspection", "profile:root"} {
		_, err := service.RunSessionAnalysis(t.Context(), principal, "session", domaincopilot.RootCauseRunInput{Kind: "root_cause", Question: "Investigate", AnalysisProfileID: id}, "en-US")
		if id == "profile:inspection" && !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("incompatible root profile accepted: %v", err)
		}
		if id == "profile:root" && err != nil {
			t.Fatalf("selected root profile did not reach the existing owner: %v", err)
		}
	}
}

// Storage concurrency is covered by TestNativeRunWithPostgres; this fixture tests orchestration.
type nativeLoopStore struct {
	lease            domaincopilot.NativeLease
	events           []domaincopilot.WorkbenchStreamEvent
	final            []domaincopilot.Message
	modelCheckpoints []json.RawMessage
}

type nativeQueueCapture struct {
	*inspectionAuthzTestRepository
	*nativeLoopStore
	hashes []string
	keys   []string
	run    domaincopilot.AgentRun
}

func (s *nativeQueueCapture) CreateNativeRun(_ context.Context, run domaincopilot.AgentRun, _ domaincopilot.Message, key, hash string) (domaincopilot.AgentRun, error) {
	s.hashes = append(s.hashes, hash)
	s.keys = append(s.keys, key)
	s.run = run
	return run, nil
}

func TestNativeRequestDigestIncludesFrozenDefaults(t *testing.T) {
	service, loop, _, _ := nativeLoopFixture(t)
	capture := &nativeQueueCapture{inspectionAuthzTestRepository: &inspectionAuthzTestRepository{}, nativeLoopStore: loop}
	service.nativeRuns = capture
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	input := domaincopilot.WorkbenchSendMessageInput{RequestID: "stable-request", Content: "Explain this", TaskIntent: &domaincopilot.TaskIntent{}}
	metadata := domaincopilot.SessionMetadata{Scope: domaincopilot.SessionScope{ClusterID: "one"}}
	session := domaincopilot.Session{ID: "session", CreatedBy: "reader"}
	for index := 0; index < 3; index++ {
		if index == 2 {
			metadata.Scope.ClusterID = "two"
		}
		if _, err := service.queueNativeMessage(context.Background(), principal, session, metadata, input, "en-US"); err != nil {
			t.Fatal(err)
		}
	}
	if capture.hashes[0] != capture.hashes[1] || capture.hashes[1] == capture.hashes[2] {
		t.Fatalf("frozen scope omitted from idempotency: %+v", capture.hashes)
	}
	for _, key := range capture.keys {
		if key != input.RequestID {
			t.Fatal("stable request ID was replaced")
		}
	}
}

func (s *nativeLoopStore) CreateNativeRun(context.Context, domaincopilot.AgentRun, domaincopilot.Message, string, string) (domaincopilot.AgentRun, error) {
	return domaincopilot.AgentRun{}, apperrors.ErrUnsupportedOperation
}
func (s *nativeLoopStore) ClaimNativeRun(context.Context, string) (domaincopilot.NativeLease, error) {
	return s.lease, nil
}
func (s *nativeLoopStore) ReadNativeLease(context.Context, string, string) (domaincopilot.NativeLease, error) {
	if s.lease.ToolUsage.WorkLimit == 0 {
		s.lease.ToolUsage.WorkLimit = domaincopilot.NativeWorkToolCallLimit
		s.lease.ToolUsage.CleanupLimit = domaincopilot.NativeCleanupToolCallLimit
	}
	return s.lease, nil
}
func (s *nativeLoopStore) ReserveNativeToolCall(_ context.Context, lease domaincopilot.NativeLease, limit int) (domaincopilot.NativeToolUsage, error) {
	if lease.Version != s.lease.Version || lease.Fence != s.lease.Fence || s.lease.CancelRequested || limit <= 0 {
		return s.lease.ToolUsage, apperrors.ErrConflict
	}
	if s.lease.ToolUsage.WorkLimit == 0 {
		s.lease.ToolUsage.WorkLimit = domaincopilot.NativeWorkToolCallLimit
		s.lease.ToolUsage.CleanupLimit = domaincopilot.NativeCleanupToolCallLimit
	}
	limit = min(limit, s.lease.ToolUsage.WorkLimit)
	if s.lease.ToolUsage.WorkCalls >= limit {
		return s.lease.ToolUsage, apperrors.ErrConflict
	}
	s.lease.ToolUsage.WorkLimit = limit
	s.lease.ToolUsage.WorkCalls++
	return s.lease.ToolUsage, nil
}
func (s *nativeLoopStore) ReserveNativeChildToolCall(_ context.Context, parent domaincopilot.NativeTaskParent, cancellation bool) error {
	if parent.RunID != s.lease.Run.ID || parent.OwnerID != s.lease.Run.CreatedBy || parent.SessionID != s.lease.Run.SessionID || parent.RequestHash != s.lease.RequestHash {
		return apperrors.ErrConflict
	}
	if cancellation {
		if s.lease.ToolUsage.CleanupCalls >= domaincopilot.NativeCleanupToolCallLimit {
			return apperrors.ErrConflict
		}
		s.lease.ToolUsage.CleanupCalls++
		return nil
	}
	_, err := s.ReserveNativeToolCall(context.Background(), s.lease, domaincopilot.NativeWorkToolCallLimit)
	return err
}
func (s *nativeLoopStore) RenewNativeLease(context.Context, domaincopilot.NativeLease) (bool, error) {
	return !s.lease.CancelRequested, nil
}
func (s *nativeLoopStore) AdvanceNativeRun(_ context.Context, lease domaincopilot.NativeLease, input domaincopilot.NativeAdvance) (domaincopilot.NativeLease, error) {
	if lease.Version != s.lease.Version || lease.Fence != s.lease.Fence {
		return lease, apperrors.ErrConflict
	}
	reconciliation := input.Phase == "cancelling" && input.Run.Status == "running"
	unknownDSH := input.Run.ProviderKind == "dsh" && input.Phase == "paused" && input.StopReason == "unknown_outcome"
	if s.lease.CancelRequested && input.Run.Status != "canceled" && !reconciliation && !unknownDSH {
		return lease, apperrors.ErrConflict
	}
	s.lease.Run, s.lease.Phase, s.lease.Checkpoint = input.Run, input.Phase, input.Checkpoint
	s.lease.Run.Phase, s.lease.Run.StopReason = input.Phase, input.StopReason
	if input.Phase == "model" {
		s.modelCheckpoints = append(s.modelCheckpoints, append(json.RawMessage(nil), input.Checkpoint...))
	}
	s.lease.Version++
	s.events = append(s.events, input.Events...)
	if input.FinalMessage != nil {
		s.final = append(s.final, *input.FinalMessage)
	}
	return s.lease, nil
}
func (s *nativeLoopStore) RequestNativeCancel(context.Context, string, string) (domaincopilot.AgentRun, error) {
	s.lease.CancelRequested = true
	return s.lease.Run, nil
}
func (s *nativeLoopStore) ResumeNativeRun(_ context.Context, _, _ string, input string) (domaincopilot.AgentRun, error) {
	var cp nativeCheckpoint
	if err := json.Unmarshal(s.lease.Checkpoint, &cp); err != nil {
		return domaincopilot.AgentRun{}, err
	}
	cp.UserInput = input
	s.lease.Checkpoint, _ = json.Marshal(cp)
	s.lease.Phase = "preparing"
	return s.lease.Run, nil
}
func (s *nativeLoopStore) ListNativeEvents(context.Context, string, string, int64) (domaincopilot.NativeEventPage, error) {
	return domaincopilot.NativeEventPage{Items: s.events}, nil
}

type nativeLoopGateway struct {
	model                 func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error)
	catalog               []domainaigateway.ToolCapability
	authorizationRevision string
	lastCall              domainaigateway.ToolInvocationRequest
	onTool                func()
	toolCalls             int
	toolDeadline          time.Time
}

func (g *nativeLoopGateway) WorkbenchAuthorizationRevision(context.Context, domainidentity.Principal) (string, error) {
	if g.authorizationRevision != "" {
		return g.authorizationRevision, nil
	}
	return strings.Repeat("b", 64), nil
}

func (g *nativeLoopGateway) InvokeWorkbenchModelStream(ctx context.Context, _ domainidentity.Principal, input appaigateway.WorkbenchRelayRequest, _ func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	return g.model(ctx, input)
}
func (g *nativeLoopGateway) WorkbenchCapabilities(context.Context, domainidentity.Principal, domainaigateway.ManifestRequest) (domainaigateway.Manifest, error) {
	if g.catalog != nil {
		return domainaigateway.Manifest{Tools: g.catalog}, nil
	}
	return domainaigateway.Manifest{Tools: []domainaigateway.ToolCapability{{Name: "knowledge.search", Version: "1", RiskLevel: domainaigateway.RiskLevelRead, InputSchema: map[string]any{"type": "object"}}}}, nil
}
func (g *nativeLoopGateway) InvokeTool(ctx context.Context, _ domainidentity.Principal, call domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	g.toolDeadline, _ = ctx.Deadline()
	g.lastCall = call
	g.toolCalls++
	if g.onTool != nil {
		g.onTool()
	}
	return domainaigateway.ToolInvocationResult{Result: "success", Output: domainknowledge.SearchResult{Hits: []domainknowledge.SearchHit{{Content: "Observed lease receipt"}}, Citations: []domainknowledge.Citation{{ID: "fixture", KnowledgeBaseID: "base", DocumentID: "doc", ChunkID: "fixture", ContentHash: "hash"}}}}, nil
}

type nativeCitationFixture struct{ denied bool }

type nativeAuthorizationFixture struct{ revision string }

func (f *nativeAuthorizationFixture) ExecutionAuthorizationRevision(context.Context, domainidentity.Principal) (string, error) {
	return f.revision, nil
}

func (*nativeCitationFixture) Search(context.Context, domainidentity.Principal, domainknowledge.SearchRequest) (domainknowledge.SearchResult, error) {
	return domainknowledge.SearchResult{}, apperrors.ErrUnsupportedOperation
}
func (f *nativeCitationFixture) ValidateCitations(context.Context, domainidentity.Principal, []domainknowledge.Citation) error {
	if f.denied {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func executionGovernanceFixture(t *testing.T) (*Service, *nativeLoopStore, *nativeLoopGateway, map[string][]string) {
	t.Helper()
	keys := []string{appaccess.PermObserveAIChatUse, appaccess.PermAIKnowledgeView, appaccess.PermAIGatewayInvoke}
	roles := map[string][]string{"reader": keys}
	service, repo := newInspectionAuthzTestService(roles)
	service.contextBuilder = NewContextBuilder(&nativeCitationFixture{}, service.permissions)
	service.settings = inspectionAuthzSettingsResolver{settings: domainsettings.AISettings{WorkbenchModel: domainsettings.AIWorkbenchModelSettings{Enabled: true, DefaultPublicModel: "fixture"}}}
	repo.session = domaincopilot.Session{ID: "session", CreatedBy: "reader"}
	service.agentPrincipals = &chatToolPrincipalResolver{principal: domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}}
	service.nativeAuthorization = &nativeAuthorizationFixture{revision: strings.Repeat("a", 64)}
	now := time.Now().UTC()
	run := domaincopilot.AgentRun{ID: "native:loop", SessionID: "session", CreatedBy: "reader", ProviderKind: "dsh", ProviderID: "dsh", CapabilityID: "general", Status: "running", ClaimedByAgentID: "worker", QueuedAt: now, TimeoutSeconds: 600, Output: map[string]any{}, Input: map[string]any{"question": "Read evidence", "_sohaKnowledgeBaseIds": []string{"base"}, "_sohaPrincipal": map[string]any{"userId": "reader", "roles": []string{"reader"}, "permissionKeys": keys}}, ToolBindings: []domaincopilot.AgentToolBinding{{ToolName: "knowledge.search", PermissionKey: appaccess.PermAIKnowledgeView}}}
	store := &nativeLoopStore{lease: domaincopilot.NativeLease{Run: run, Version: 1, Fence: 1, Phase: "preparing", RequestHash: strings.Repeat("c", 64)}}
	store.lease.Run.Input["_sohaExecutionOwner"] = "dsh-core"
	store.lease.Run.Input["_sohaAuthorizationCeiling"] = strings.Repeat("a", 64)
	store.lease.Run.Input["_sohaGatewayAuthorizationCeiling"] = strings.Repeat("b", 64)
	gateway := &nativeLoopGateway{}
	service.runtime = RuntimeDependencies{Stream: gateway, Tools: gateway, CapabilityCatalog: gateway, GatewayAuthorization: gateway, PlanValidator: gateway}
	service.nativeRuns = store
	service.dshRuntime = &dshTestRuntime{}
	service.dshRuns = store
	service.nativeTaskParents = &nativeTaskStoreFixture{nativeQueueCapture: &nativeQueueCapture{nativeLoopStore: store}}
	service.nativeTasks = &nativeTaskOwnerFixture{store: store}
	return service, store, gateway, roles
}

func TestNativeUserInputIsConsumedOnceForEachQuestion(t *testing.T) {
	_, store, _, _ := nativeLoopFixture(t)
	cp := initialNativeCheckpoint(store.lease.Run)
	cp.Pending = []nativeCall{{ToolName: "harness.request_input", Call: appaigateway.WorkbenchToolCall{ID: "first", Arguments: json.RawMessage(`{"question":"Which namespace?"}`)}}}
	cp.UserInput = "payments"
	lease, err := nativeUserInputStep(t.Context(), store, store.lease, &cp)
	if err != nil || cp.UserInput != "" || len(cp.Messages) != 1 || cp.Messages[0].Content != "payments" {
		t.Fatalf("answer was not consumed exactly once: %+v %v", cp, err)
	}
	cp.Pending = []nativeCall{{ToolName: "harness.request_input", Call: appaigateway.WorkbenchToolCall{ID: "second", Arguments: json.RawMessage(`{"question":"Which pod?"}`)}}}
	lease, err = nativeUserInputStep(t.Context(), store, lease, &cp)
	if err != nil || lease.Phase != "paused" || cp.StopReason != "waiting_input" || len(cp.Pending) != 1 || len(cp.Messages) != 1 || lease.Run.Output["pendingQuestion"] != "Which pod?" {
		t.Fatalf("prior answer silently answered a new question: %+v %v", cp, err)
	}
}

func (r *nativeTokenPrincipalResolver) CurrentExecutionPrincipal(_ context.Context, _, tokenID string) (domainidentity.Principal, error) {
	r.tokenID = tokenID
	principal := r.principal
	principal.AccessTokenID = tokenID
	return principal, r.err
}

func nativeLoopCitations(t *testing.T, service *Service) *nativeCitationFixture {
	t.Helper()
	builder, ok := service.contextBuilder.(*ContextBuilder)
	if !ok {
		t.Fatal("Native context builder fixture is unavailable")
	}
	reader, ok := builder.knowledge.(*nativeCitationFixture)
	if !ok {
		t.Fatal("Native knowledge fixture is unavailable")
	}
	return reader
}

// nativeLoopFixture remains a fixture alias for the memory unit's existing tests.
func nativeLoopFixture(t *testing.T) (*Service, *nativeLoopStore, *nativeLoopGateway, map[string][]string) {
	return executionGovernanceFixture(t)
}

type nativeTokenPrincipalResolver struct {
	chatToolPrincipalResolver
	err     error
	tokenID string
}

func (s *nativeLoopStore) ClaimDSHRun(context.Context, string) (domaincopilot.NativeLease, error) {
	return s.lease, nil
}
