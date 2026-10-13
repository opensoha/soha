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
	appmemory "github.com/opensoha/soha/internal/application/memory"
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
	service.agentRuns = capture
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
	if s.lease.CancelRequested && input.Run.Status != "canceled" && (input.Phase != "cancelling" || input.Run.Status != "running") {
		return lease, apperrors.ErrConflict
	}
	s.lease.Run, s.lease.Phase, s.lease.Checkpoint = input.Run, input.Phase, input.Checkpoint
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

func (g *nativeLoopGateway) InvokeWorkbenchModel(ctx context.Context, _ domainidentity.Principal, input appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
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

func nativeLoopFixture(t *testing.T) (*Service, *nativeLoopStore, *nativeLoopGateway, map[string][]string) {
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
	run := domaincopilot.AgentRun{ID: "native:loop", SessionID: "session", CreatedBy: "reader", ProviderKind: "native", ProviderID: "internal", CapabilityID: "general", Status: "running", ClaimedByAgentID: "worker", QueuedAt: now, TimeoutSeconds: 600, Output: map[string]any{}, Input: map[string]any{"question": "Read evidence", "_sohaKnowledgeBaseIds": []string{"base"}, "_sohaPrincipal": map[string]any{"userId": "reader", "roles": []string{"reader"}, "permissionKeys": keys}}, ToolBindings: []domaincopilot.AgentToolBinding{{ToolName: "knowledge.search", PermissionKey: appaccess.PermAIKnowledgeView}}}
	store := &nativeLoopStore{lease: domaincopilot.NativeLease{Run: run, Version: 1, Fence: 1, Phase: "preparing", RequestHash: strings.Repeat("c", 64)}}
	store.lease.Run.Input["_sohaAuthorizationCeiling"] = strings.Repeat("a", 64)
	store.lease.Run.Input["_sohaGatewayAuthorizationCeiling"] = strings.Repeat("b", 64)
	gateway := &nativeLoopGateway{}
	service.workbenchInvoker = gateway
	return service, store, gateway, roles
}

func TestNativeLoopUsesToolResultsAcrossThreeModelSteps(t *testing.T) {
	service, store, gateway, _ := nativeLoopFixture(t)
	steps := 0
	gateway.model = func(_ context.Context, input appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		steps++
		tools := 0
		for _, m := range input.Messages {
			if m.Role == "tool" {
				tools++
				if m.Content == "" {
					t.Fatal("empty tool observation")
				}
			}
		}
		if tools != steps-1 {
			t.Fatalf("step %d received %d tool results", steps, tools)
		}
		response := appaigateway.WorkbenchRelayResponse{ContextWindowTokens: 128000, Usage: map[string]float64{"inputTokens": 8, "outputTokens": 2, "totalTokens": 10}}
		calls := map[int]appaigateway.WorkbenchToolCall{1: {ID: "a", Name: "knowledge_search", Arguments: json.RawMessage(`{"query":"lease"}`)}, 2: {ID: "b", Name: "knowledge_search", Arguments: json.RawMessage(`{"query":"cancel"}`)}}
		if call, found := calls[steps]; found {
			response.ToolCalls = []appaigateway.WorkbenchToolCall{call}
		} else {
			response.Content = "The two observed receipts explain the result."
		}
		return response, nil
	}
	if err := service.processNativeRun(context.Background(), store, store.lease); err != nil {
		t.Fatal(err)
	}
	if steps != 3 || gateway.toolCalls != 2 || store.lease.Run.Status != "completed" || len(store.final) != 1 || !containsStreamEvent(store.events, "message.done") {
		t.Fatalf("steps=%d tools=%d lease=%+v final=%v", steps, gateway.toolCalls, store.lease, store.final)
	}
	assertNativeLatestContextUsage(t, store)
	assertNativeModelRevisionHistory(t, store.modelCheckpoints)
	if err := service.processNativeRun(context.Background(), store, store.lease); err != nil || len(store.final) != 1 {
		t.Fatal("terminal run advanced again")
	}
}

func TestNativeCancellationUsesCurrentVersionWithoutTakingAnotherWorkerFence(t *testing.T) {
	for _, transferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "same_worker", true: "transferred_worker"}[transferred], func(t *testing.T) {
			service, store, gateway, _ := nativeLoopFixture(t)
			gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
				store.lease.CancelRequested = true
				store.lease.Version++
				store.lease.Phase = "cancelling"
				if transferred {
					store.lease.Fence++
				}
				return appaigateway.WorkbenchRelayResponse{}, apperrors.ErrConflict
			}
			err := service.processNativeRun(t.Context(), store, store.lease)
			if transferred {
				if !errors.Is(err, apperrors.ErrConflict) || len(store.final) != 0 || store.lease.Run.Status != "running" {
					t.Fatalf("old worker crossed the fence: final=%v status=%s err=%v", store.final, store.lease.Run.Status, err)
				}
			} else if err != nil || store.lease.Run.Status != "canceled" || len(store.final) != 1 {
				t.Fatalf("cancel waited for lease expiry: final=%v status=%s err=%v", store.final, store.lease.Run.Status, err)
			}
		})
	}
}

type nativeCancelStreamGateway struct {
	*nativeLoopGateway
	store *nativeLoopStore
}

func (g *nativeCancelStreamGateway) InvokeWorkbenchModelStream(_ context.Context, _ domainidentity.Principal, _ appaigateway.WorkbenchRelayRequest, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	g.store.lease.CancelRequested = true
	g.store.lease.Version++
	g.store.lease.Phase = "cancelling"
	if emit(appaigateway.WorkbenchRelayStreamDelta{ContentDelta: "late delta"}) {
		return appaigateway.WorkbenchRelayResponse{}, errors.New("late model delta was accepted after cancellation")
	}
	return appaigateway.WorkbenchRelayResponse{}, context.Canceled
}

func TestNativeStreamingCancellationKeepsUnknownUsageAndFinishesImmediately(t *testing.T) {
	service, store, gateway, _ := nativeLoopFixture(t)
	service.workbenchInvoker = &nativeCancelStreamGateway{nativeLoopGateway: gateway, store: store}
	if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || store.lease.Run.Status != "canceled" || len(store.final) != 1 {
		t.Fatalf("stream cancellation did not finish: status=%s final=%v err=%v", store.lease.Run.Status, store.final, err)
	}
	if store.lease.Run.Output["usageKnown"] != false || containsStreamEvent(store.events, "message.delta") {
		t.Fatal("interrupted model usage became known or a late delta was persisted")
	}
}

func TestNativeLoopWaitsForInputAndResumesSameRun(t *testing.T) {
	service, store, gateway, _ := nativeLoopFixture(t)
	store.lease.Run.ToolBindings = append(store.lease.Run.ToolBindings, domaincopilot.AgentToolBinding{ToolName: "harness.request_input", PermissionKey: appaccess.PermObserveAIChatUse})
	steps := 0
	gateway.model = func(_ context.Context, input appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		steps++
		if steps == 1 {
			return appaigateway.WorkbenchRelayResponse{ToolCalls: []appaigateway.WorkbenchToolCall{{ID: "input", Name: "harness_request_input", Arguments: json.RawMessage(`{"question":"Which namespace?"}`)}}}, nil
		}
		found := false
		for _, m := range input.Messages {
			found = found || (m.Role == "tool" && m.ToolCallID == "input" && m.Content == "payments")
		}
		if !found {
			t.Fatal("resume omitted user answer or tool-call pairing")
		}
		return appaigateway.WorkbenchRelayResponse{Content: "Selected payments."}, nil
	}
	if err := service.processNativeRun(context.Background(), store, store.lease); err != nil || store.lease.Phase != "paused" || len(store.final) != 0 {
		t.Fatalf("pause: %v %+v", err, store.lease)
	}
	if _, err := store.ResumeNativeRun(context.Background(), "reader", store.lease.Run.ID, "payments"); err != nil {
		t.Fatal(err)
	}
	if err := service.processNativeRun(context.Background(), store, store.lease); err != nil || store.lease.Run.Status != "completed" || len(store.final) != 1 || steps != 2 {
		t.Fatalf("resume: %v %+v", err, store.lease)
	}
	if store.lease.Run.Output["usageKnown"] != false {
		t.Fatal("missing usage was presented as known")
	}
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

func TestNativeLoopRevocationAndCancelCannotBecomeSuccess(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "revocation", true: "cancel"}[cancel], func(t *testing.T) {
			service, store, gateway, roles := nativeLoopFixture(t)
			calls := 0
			gateway.model = func(_ context.Context, _ appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
				calls++
				if cancel {
					store.lease.CancelRequested = true
					return appaigateway.WorkbenchRelayResponse{}, context.Canceled
				}
				return appaigateway.WorkbenchRelayResponse{ToolCalls: []appaigateway.WorkbenchToolCall{{ID: "a", Name: "knowledge_search", Arguments: json.RawMessage(`{"query":"lease"}`)}}}, nil
			}
			gateway.onTool = func() { roles["reader"] = nil }
			err := service.processNativeRun(context.Background(), store, store.lease)
			if err != nil && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatal(err)
			}
			want := "failed"
			if cancel {
				want = "canceled"
			}
			if calls != 1 || store.lease.Run.Status != want || len(store.final) != 1 {
				t.Fatalf("calls=%d state=%s final=%v", calls, store.lease.Run.Status, store.final)
			}
		})
	}
}

type nativeTokenPrincipalResolver struct {
	chatToolPrincipalResolver
	err     error
	tokenID string
}

func (r *nativeTokenPrincipalResolver) CurrentExecutionPrincipal(_ context.Context, _, tokenID string) (domainidentity.Principal, error) {
	r.tokenID = tokenID
	principal := r.principal
	principal.AccessTokenID = tokenID
	return principal, r.err
}

func TestNativeTokenRevocationAndEmptyCeilingFailClosed(t *testing.T) {
	for _, tokenRevoked := range []bool{true, false} {
		t.Run(map[bool]string{true: "token_revoked", false: "empty_intersection"}[tokenRevoked], func(t *testing.T) {
			service, store, gateway, roles := nativeLoopFixture(t)
			resolver := &nativeTokenPrincipalResolver{chatToolPrincipalResolver: chatToolPrincipalResolver{principal: domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}}}
			service.agentPrincipals = resolver
			if tokenRevoked {
				mapValue(store.lease.Run.Input["_sohaPrincipal"])["accessTokenId"] = "frozen-token"
				resolver.err = apperrors.ErrUnauthorized
			} else {
				mapValue(store.lease.Run.Input["_sohaPrincipal"])["permissionKeys"] = []string{appaccess.PermAIKnowledgeView}
				roles["reader"] = []string{appaccess.PermObserveAIChatUse}
			}
			models := 0
			gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
				models++
				return appaigateway.WorkbenchRelayResponse{Content: "must not run"}, nil
			}
			if err := service.processNativeRun(t.Context(), store, store.lease); err != nil || models != 0 || store.lease.Run.Status != "failed" {
				t.Fatalf("authorization bypassed: err=%v models=%d state=%s", err, models, store.lease.Run.Status)
			}
			if tokenRevoked && resolver.tokenID != "frozen-token" {
				t.Fatal("original token was not revalidated")
			}
		})
	}
}

func TestNativeLoopRevalidatesKnowledgeACLBeforeAnotherModelCall(t *testing.T) {
	service, store, gateway, _ := nativeLoopFixture(t)
	models := 0
	gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		models++
		return appaigateway.WorkbenchRelayResponse{ToolCalls: []appaigateway.WorkbenchToolCall{{ID: "a", Name: "knowledge_search", Arguments: json.RawMessage(`{"query":"lease"}`)}}}, nil
	}
	gateway.onTool = func() { nativeLoopCitations(t, service).denied = true }
	if err := service.processNativeRun(context.Background(), store, store.lease); err != nil {
		t.Fatal(err)
	}
	if models != 1 || store.lease.Run.Status != "failed" {
		t.Fatalf("revoked knowledge was sent to model: calls=%d state=%s", models, store.lease.Run.Status)
	}
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

func TestNativeLoopRevalidatesMemoryBeforeAnotherModelCall(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "corrected", true: "deleted"}[deleted], func(t *testing.T) {
			service, store, gateway, roles := nativeLoopFixture(t)
			roles["reader"] = append(roles["reader"], appaccess.PermAIMemoryView)
			mapValue(store.lease.Run.Input["_sohaPrincipal"])["permissionKeys"] = roles["reader"]
			memory, _ := appmemory.NewService(appmemory.NewMemoryStore())
			service.SetChatMemoryReader(memory)
			policy := appmemory.Policy{ID: "personal", Version: "v1", OwnerTypes: []string{"user"}, DefaultTTL: time.Hour, MaximumTTL: time.Hour, ExplicitWriteOnly: true, Enabled: true}
			if err := memory.PutPolicy(t.Context(), policy); err != nil {
				t.Fatal(err)
			}
			record, err := memory.PutRecord(t.Context(), appmemory.Record{ID: "fact", OwnerType: "user", OwnerID: "reader", ScopeHash: "sha256:fixture", Fact: "original fact", SourceType: "explicit_user", Confidence: 1, PolicyVer: "v1"}, policy)
			if err != nil {
				t.Fatal(err)
			}
			store.lease.Run.Input["contextSelection"] = domaincopilot.ContextSelection{MemoryIDs: []string{record.ID}}
			models := 0
			gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
				models++
				return appaigateway.WorkbenchRelayResponse{ToolCalls: []appaigateway.WorkbenchToolCall{{ID: "a", Name: "knowledge_search", Arguments: json.RawMessage(`{"query":"lease"}`)}}}, nil
			}
			gateway.onTool = func() {
				if deleted {
					err = memory.DeleteRecord(t.Context(), record.ID)
				} else {
					record.Fact = "corrected fact"
					_, err = memory.PutRecord(t.Context(), record, policy)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := service.processNativeRun(t.Context(), store, store.lease); err != nil {
				t.Fatal(err)
			}
			if models != 1 || store.lease.Run.Status != "failed" {
				t.Fatalf("old memory entered a resumed model call: models=%d state=%s", models, store.lease.Run.Status)
			}
		})
	}
}
