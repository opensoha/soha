package copilot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type dshTestRuntime struct {
	starts, lookups, cancels int
	receipt                  domaincopilot.DSHReceipt
	status                   domaincopilot.DSHStatus
	startErr, lookupErr      error
}

func (f *dshTestRuntime) Start(context.Context, domaincopilot.DSHStart) (domaincopilot.DSHReceipt, error) {
	f.starts++
	return f.receipt, f.startErr
}
func (f *dshTestRuntime) Lookup(context.Context, domaincopilot.DSHIdentity, string) (domaincopilot.DSHStatus, error) {
	f.lookups++
	return f.status, f.lookupErr
}
func (f *dshTestRuntime) Status(context.Context, domaincopilot.DSHSubmission) (domaincopilot.DSHStatus, error) {
	return f.status, nil
}
func (f *dshTestRuntime) Cancel(context.Context, domaincopilot.DSHSubmission, string) (domaincopilot.DSHStatus, error) {
	f.cancels++
	return f.status, nil
}
func (f *dshTestRuntime) Events(context.Context, domaincopilot.DSHIdentity, int64) (domaincopilot.DSHEventPage, error) {
	return domaincopilot.DSHEventPage{}, nil
}
func dshFixture(t *testing.T) (*Service, *nativeLoopStore, dshCheckpoint) {
	s, store, _, _ := executionGovernanceFixture(t)
	store.lease.Run.ProviderKind = "dsh"
	store.lease.Run.Input["_sohaExecutionOwner"] = "dsh-core"
	until := time.Now().Add(time.Minute)
	store.lease.LeaseUntil = &until
	cp := dshCheckpoint{nativeCheckpoint: initialNativeCheckpoint(store.lease.Run), Start: &domaincopilot.DSHStart{DSHIdentity: domaincopilot.DSHIdentity{RunID: store.lease.Run.ID, Attempt: 1, Fence: 1}, ToolGrant: "secret", InputHash: strings.Repeat("c", 64)}, Submitted: true}
	return s, store, cp
}
func TestDSHCallbackRejectsCanceledStaleGrantAndFence(t *testing.T) {
	for _, kind := range []string{"canceled", "grant", "fence", "owner", "terminal"} {
		t.Run(kind, func(t *testing.T) {
			s, store, cp := dshFixture(t)
			grant := "secret"
			id := cp.Start.DSHIdentity
			switch kind {
			case "canceled":
				store.lease.CancelRequested = true
			case "grant":
				grant = "wrong"
			case "fence":
				id.Fence++
			case "owner":
				store.lease.Run.Input["_sohaExecutionOwner"] = "other"
			case "terminal":
				store.lease.Run.Status = "completed"
			}
			store.lease.Checkpoint, _ = json.Marshal(cp)
			_, _, _, _, err := s.authorizeDSHCallback(context.Background(), grant, id)
			if !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("callback accepted: %v", err)
			}
		})
	}
}
func TestDSHRecoveryLooksUpWithoutResubmissionAndPreservesUnknownBudget(t *testing.T) {
	s, store, cp := dshFixture(t)
	cp.BudgetTokens = 4096
	cp.ModelReservation = 4096
	cp.Start.Fence = store.lease.Fence
	cp.Submitted = true
	store.lease.Checkpoint, _ = json.Marshal(cp)
	runtime := &dshTestRuntime{status: domaincopilot.DSHStatus{DSHSubmission: domaincopilot.DSHSubmission{DSHIdentity: cp.Start.DSHIdentity, InputHash: cp.Start.InputHash, SubmissionID: "receipt"}, Status: "unknown"}}
	s.dshRuntime = runtime
	if err := s.processDSHRun(context.Background(), store.lease); err != nil {
		t.Fatal(err)
	}
	saved, err := readDSHCheckpoint(store.lease)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.starts != 0 || runtime.lookups != 1 || store.lease.Phase != "paused" || saved.UsageKnown || saved.BudgetTokens != 4096 {
		t.Fatalf("unsafe recovery: %+v %+v", runtime, saved)
	}
}
func TestDSHModelInputBindsFrozenContextAndCatalog(t *testing.T) {
	start := domaincopilot.DSHStart{Prompt: "question", Context: []domaincopilot.DSHContextMessage{{Role: "user", Content: "trusted source"}}, Tools: []domaincopilot.DSHTool{{Name: "knowledge_search", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	raw, _ := json.Marshal(start.Context)
	request := appaigateway.WorkbenchRelayRequest{Messages: []appaigateway.WorkbenchRelayMessage{{Role: "user", Content: "Conversation context (data):\n" + string(raw) + "\nCurrent user message:\nquestion"}}, Tools: []appaigateway.WorkbenchTool{{Name: "knowledge_search", Parameters: map[string]any{"type": "object"}}}}
	if err := validateDSHModelInput(dshCheckpoint{Start: &start}, request); err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.Tools = []appaigateway.WorkbenchTool{{Name: "shell", Parameters: map[string]any{"type": "object"}}}
	if err := validateDSHModelInput(dshCheckpoint{Start: &start}, changed); err == nil {
		t.Fatal("substituted tool accepted")
	}
	changed = request
	changed.Messages = []appaigateway.WorkbenchRelayMessage{{Role: "user", Content: "untrusted replacement"}}
	if err := validateDSHModelInput(dshCheckpoint{Start: &start}, changed); err == nil {
		t.Fatal("replaced sources accepted")
	}
}
func TestDSHRequiredAbsenceDoesNotFallback(t *testing.T) {
	{
		s := &Service{}
		if _, err := s.queueNativeMessage(t.Context(), domainidentity.Principal{}, domaincopilot.Session{}, domaincopilot.SessionMetadata{}, domaincopilot.WorkbenchSendMessageInput{Content: "hello"}, "en"); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
			t.Fatalf("fallback: %v", err)
		}
	}
	for _, alias := range []string{"", "internal", "native", "dsh"} {
		if got := normalizeAgentProviderID(alias); got != "dsh" {
			t.Fatalf("alias %q: %q", alias, got)
		}
	}
}

type dshModelInvoker struct{ calls int }

func (f *dshModelInvoker) InvokeWorkbenchModel(context.Context, domainidentity.Principal, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
	f.calls++
	return appaigateway.WorkbenchRelayResponse{Content: "verified answer", Usage: map[string]float64{"totalTokens": 10}}, nil
}
func (f *dshModelInvoker) InvokeWorkbenchModelStream(ctx context.Context, p domainidentity.Principal, r appaigateway.WorkbenchRelayRequest, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	if !emit(appaigateway.WorkbenchRelayStreamDelta{ContentDelta: "verified "}) || !emit(appaigateway.WorkbenchRelayStreamDelta{ContentDelta: "answer"}) {
		return appaigateway.WorkbenchRelayResponse{}, appaigateway.ErrWorkbenchRelayStreamStopped
	}
	return f.InvokeWorkbenchModel(ctx, p, r)
}
func TestDSHModelProxyUsesGatewayOnceAndRejectsRevocation(t *testing.T) {
	s, store, cp := dshFixture(t)
	invoker := &dshModelInvoker{}
	s.runtime.Model = invoker
	s.runtime.Stream = invoker
	cp.Start = nil
	cp.Submitted = false
	var err error
	store.lease, err = s.prepareDSH(context.Background(), store.lease, &cp)
	if err != nil {
		t.Fatal(err)
	}
	cp, err = readDSHCheckpoint(store.lease)
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
	call := DSHModelCall{DSHIdentity: cp.Start.DSHIdentity, CallID: "first", ContextRevision: cp.Start.ContextRevision, ToolCatalogRevision: cp.Start.ToolCatalogRevision, InputHash: hex.EncodeToString(sum[:]), RawBody: raw, Request: request}
	deltas := 0
	for i := 0; i < 2; i++ {
		var response appaigateway.WorkbenchRelayResponse
		var err error
		if i == 0 {
			response, err = s.InvokeDSHModelStream(context.Background(), cp.Start.ToolGrant, call, func(d appaigateway.WorkbenchRelayStreamDelta) bool { deltas++; return true })
		} else {
			response, err = s.InvokeDSHModel(context.Background(), cp.Start.ToolGrant, call)
		}
		if err != nil || response.Content != "verified answer" {
			t.Fatalf("model: %+v %v", response, err)
		}
	}
	if invoker.calls != 1 || deltas != 2 {
		t.Fatalf("duplicate charged: %d", invoker.calls)
	}
	s.nativeAuthorization = &nativeAuthorizationFixture{revision: strings.Repeat("d", 64)}
	if _, err := s.InvokeDSHModel(context.Background(), cp.Start.ToolGrant, call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked replay accepted: %v", err)
	}
}

func TestDSHToolWaitsForOriginalApprovalWithoutRedispatch(t *testing.T) {
	s, store, _, owner := nativeGovernedTaskFixture(t)
	store.lease.Run.ProviderKind = "dsh"
	store.lease.Run.Input["_sohaExecutionOwner"] = "dsh-core"
	until := time.Now().Add(time.Minute)
	store.lease.LeaseUntil = &until
	raw := nativeTaskFixtureArguments(t)
	cp := dshCheckpoint{nativeCheckpoint: initialNativeCheckpoint(store.lease.Run), Start: &domaincopilot.DSHStart{DSHIdentity: domaincopilot.DSHIdentity{RunID: store.lease.Run.ID, Attempt: 1, Fence: store.lease.Fence}, ToolGrant: "grant", ToolCatalogRevision: "catalog"}, ToolHashes: map[string]string{}, ToolResults: map[string]domaincopilot.DSHToolResult{}}
	cp.Pending = []nativeCall{{Call: appaigateway.WorkbenchToolCall{ID: "request", Name: "harness_request_task", Arguments: raw}, ToolName: "harness.request_task", State: "prepared"}}
	var semantic any
	_ = json.Unmarshal(raw, &semantic)
	cp.ToolHashes["request"] = dshHash(semantic)
	store.lease.Checkpoint, _ = json.Marshal(cp)
	hash := sha256.Sum256(raw)
	call := domaincopilot.DSHToolCall{SchemaVersion: "opensoha.dev/dsh-runtime/v1", Kind: "tool_call", RunID: store.lease.Run.ID, Attempt: 1, Fence: store.lease.Fence, CallID: "request", ToolName: "harness_request_task", ToolCatalogRevision: "catalog", InputHash: hex.EncodeToString(hash[:]), Input: raw}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		result, err := s.InvokeDSHTool(ctx, "grant", call)
		cancel()
		if err == nil || result.Status == "completed" {
			t.Fatal("pending approval returned success")
		}
	}
	if owner.creates != 1 || owner.finds != 1 || store.lease.Phase != "waiting_approval" {
		t.Fatalf("approval redispatched: creates=%d finds=%d phase=%s", owner.creates, owner.finds, store.lease.Phase)
	}
	store.lease.CancelRequested = true
	if _, err := s.InvokeDSHTool(context.Background(), "grant", call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("canceled approval accepted: %v", err)
	}
	if owner.creates != 1 {
		t.Fatal("cancellation dispatched again")
	}
}

// The runtime worker and callback share this synchronized in-memory state machine.
// Reads and advances remain distinct so the test catches stale completion CASes.
type dshConcurrentStore struct {
	NativeRunStore
	mu   sync.Mutex
	loop *nativeLoopStore
}

func (s *dshConcurrentStore) ReadNativeLease(ctx context.Context, user, id string) (domaincopilot.NativeLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.NativeRunStore.ReadNativeLease(ctx, user, id)
}
func (s *dshConcurrentStore) AdvanceNativeRun(ctx context.Context, lease domaincopilot.NativeLease, next domaincopilot.NativeAdvance) (domaincopilot.NativeLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.NativeRunStore.AdvanceNativeRun(ctx, lease, next)
}
func (s *dshConcurrentStore) RenewNativeLease(ctx context.Context, lease domaincopilot.NativeLease) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.NativeRunStore.RenewNativeLease(ctx, lease)
}

type dshHeldStream struct {
	started, release chan struct{}
	calls            atomic.Int32
	callErr          error
}

func (f *dshHeldStream) InvokeWorkbenchModelStream(ctx context.Context, _ domainidentity.Principal, _ appaigateway.WorkbenchRelayRequest, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	f.calls.Add(1)
	if !emit(appaigateway.WorkbenchRelayStreamDelta{ContentDelta: "early"}) {
		return appaigateway.WorkbenchRelayResponse{}, appaigateway.ErrWorkbenchRelayStreamStopped
	}
	close(f.started)
	select {
	case <-ctx.Done():
		return appaigateway.WorkbenchRelayResponse{}, ctx.Err()
	case <-f.release:
	}
	if f.callErr != nil {
		return appaigateway.WorkbenchRelayResponse{}, f.callErr
	}
	return appaigateway.WorkbenchRelayResponse{Content: "early answer", Usage: map[string]float64{"totalTokens": 10}}, nil
}

type dshDeltaRuntime struct{ dshTestRuntime }

func (f *dshDeltaRuntime) Events(_ context.Context, id domaincopilot.DSHIdentity, after int64) (domaincopilot.DSHEventPage, error) {
	if after > 0 {
		return domaincopilot.DSHEventPage{NextSequence: after}, nil
	}
	return domaincopilot.DSHEventPage{Items: []domaincopilot.DSHEvent{{DSHIdentity: id, Sequence: 1, Type: "message_delta", Delta: "early"}}, NextSequence: 1}, nil
}
func dshHeldModelFixture(t *testing.T) (*Service, *dshConcurrentStore, *dshHeldStream, DSHModelCall, string) {
	t.Helper()
	s, loop, cp := dshFixture(t)
	cp.Start = nil
	lease, err := s.prepareDSH(t.Context(), loop.lease, &cp)
	if err != nil {
		t.Fatal(err)
	}
	loop.lease = lease
	store := &dshConcurrentStore{NativeRunStore: loop, loop: loop}
	s.nativeRuns = store
	stream := &dshHeldStream{started: make(chan struct{}), release: make(chan struct{})}
	s.runtime.Stream = stream
	s.dshRuntime = &dshDeltaRuntime{}
	request := appaigateway.WorkbenchRelayRequest{Messages: []appaigateway.WorkbenchRelayMessage{{Role: "user", Content: dshFrozenUser(*cp.Start)}}}
	for _, tool := range cp.Start.Tools {
		var schema map[string]any
		_ = json.Unmarshal(tool.InputSchema, &schema)
		request.Tools = append(request.Tools, appaigateway.WorkbenchTool{Name: tool.Name, Parameters: schema})
	}
	raw := json.RawMessage(`{"model":"soha-workbench"}`)
	hash := sha256.Sum256(raw)
	call := DSHModelCall{DSHIdentity: cp.Start.DSHIdentity, CallID: "held", ContextRevision: cp.Start.ContextRevision, ToolCatalogRevision: cp.Start.ToolCatalogRevision, InputHash: hex.EncodeToString(hash[:]), RawBody: raw, Request: request}
	return s, store, stream, call, cp.Start.ToolGrant
}
func TestDSHStreamingPersistsDeltaBeforeModelCompletionAndMergesCursor(t *testing.T) {
	s, store, stream, call, grant := dshHeldModelFixture(t)
	done := make(chan error, 1)
	go func() {
		_, err := s.InvokeDSHModelStream(t.Context(), grant, call, func(appaigateway.WorkbenchRelayStreamDelta) bool { return true })
		done <- err
	}()
	<-stream.started
	released := false
	defer func() {
		if !released {
			close(stream.release)
		}
	}()
	for _, id := range []string{call.CallID, "different-call"} {
		duplicate := call
		duplicate.CallID = id
		duplicateDone := make(chan error, 1)
		go func() { _, err := s.InvokeDSHModel(t.Context(), grant, duplicate); duplicateDone <- err }()
		select {
		case err := <-duplicateDone:
			if !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("in-flight call dispatched: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("in-flight duplicate waits behind the model stream")
		}
	}
	lease, err := store.ReadNativeLease(t.Context(), "", call.RunID)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := readDSHCheckpoint(lease)
	if err != nil {
		t.Fatal(err)
	}
	if cp.ModelReservation <= 0 {
		t.Fatal("model intent lacks durable reservation")
	}
	persisted := make(chan error, 1)
	go func() { persisted <- s.persistDSHEvents(t.Context(), lease, cp) }()
	select {
	case err := <-persisted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("model stream still holds event-persistence lock")
	}
	store.mu.Lock()
	early := len(store.loop.events) == 1 && store.loop.events[0].Type == "message.delta" && store.loop.events[0].ContentDelta == "early"
	store.mu.Unlock()
	if !early {
		t.Fatal("Core has no durable delta before model finishes")
	}
	select {
	case err := <-done:
		t.Fatalf("model already finished before release: %v", err)
	default:
	}
	close(stream.release)
	released = true
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	lease, err = store.ReadNativeLease(t.Context(), "", call.RunID)
	if err != nil {
		t.Fatal(err)
	}
	cp, err = readDSHCheckpoint(lease)
	if err != nil {
		t.Fatal(err)
	}
	if cp.RuntimeSequence != 1 || cp.ModelReservation != 0 || len(cp.ModelResponses) != 1 || stream.calls.Load() != 1 {
		t.Fatalf("completion overwrote concurrent state: %+v calls=%d", cp, stream.calls.Load())
	}
}
func TestDSHStreamingRejectsCanceledRevokedAndTransferredCompletionWithoutRefund(t *testing.T) {
	for _, scenario := range []string{"cancel", "fence", "revoke"} {
		t.Run(scenario, func(t *testing.T) {
			s, store, stream, call, grant := dshHeldModelFixture(t)
			done := make(chan error, 1)
			go func() {
				_, err := s.InvokeDSHModelStream(t.Context(), grant, call, func(appaigateway.WorkbenchRelayStreamDelta) bool { return true })
				done <- err
			}()
			<-stream.started
			lease, err := store.ReadNativeLease(t.Context(), "", call.RunID)
			if err != nil {
				t.Fatal(err)
			}
			before, err := readDSHCheckpoint(lease)
			if err != nil {
				t.Fatal(err)
			}
			store.mu.Lock()
			switch scenario {
			case "cancel":
				store.loop.lease.CancelRequested = true
			case "fence":
				store.loop.lease.Fence++
			case "revoke":
				s.nativeAuthorization = &nativeAuthorizationFixture{revision: strings.Repeat("d", 64)}
			}
			store.mu.Unlock()
			if scenario == "fence" {
				latest, e := store.ReadNativeLease(t.Context(), "", call.RunID)
				if e != nil {
					t.Fatal(e)
				}
				latestCP, e := readDSHCheckpoint(latest)
				if e != nil {
					t.Fatal(e)
				}
				if err := s.persistDSHEvents(t.Context(), latest, latestCP); !errors.Is(err, apperrors.ErrConflict) {
					t.Fatalf("stale-fence event accepted: %v", err)
				}
			}
			close(stream.release)
			if err := <-done; !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("stale completion accepted: %v", err)
			}
			lease, err = store.ReadNativeLease(t.Context(), "", call.RunID)
			if err != nil {
				t.Fatal(err)
			}
			after, err := readDSHCheckpoint(lease)
			if err != nil {
				t.Fatal(err)
			}
			if after.BudgetTokens != before.BudgetTokens || after.ModelReservation != before.ModelReservation || after.ModelReservation <= 0 || after.UsageKnown || len(after.ModelResponses) != 0 || len(after.Pending) != 0 || stream.calls.Load() != 1 {
				t.Fatalf("unsafe completion settlement: before=%+v after=%+v", before, after)
			}
		})
	}
}

type dshFeedbackMessages struct {
	MessageStore
	messages map[string]domaincopilot.Message
}

func (s *dshFeedbackMessages) GetMessage(_ context.Context, session, id string) (domaincopilot.Message, error) {
	message, ok := s.messages[id]
	if !ok || message.SessionID != session {
		return domaincopilot.Message{}, apperrors.ErrNotFound
	}
	return message, nil
}
func (s *dshFeedbackMessages) UpdateMessageMetadata(ctx context.Context, session, id string, metadata map[string]any) (domaincopilot.Message, error) {
	message, err := s.GetMessage(ctx, session, id)
	if err != nil {
		return message, err
	}
	message.Metadata = metadata
	s.messages[id] = message
	return message, nil
}
func TestDSHMemoryDegradationSurvivesRefreshOnOnlyItsRequestMessage(t *testing.T) {
	messages := &dshFeedbackMessages{messages: map[string]domaincopilot.Message{
		"failed": {ID: "failed", SessionID: "session", Role: "user", Metadata: map[string]any{"images": []string{"image"}, "_sohaExplicitMemoryIds": []string{"memory"}, "mode": "general"}},
		"next":   {ID: "next", SessionID: "session", Role: "user", Metadata: map[string]any{"mode": "general"}},
	}}
	service := &Service{messages: messages}
	run := domaincopilot.AgentRun{ID: "run", SessionID: "session", Input: map[string]any{"requestMessageId": "failed"}}
	if err := service.persistQueuedMemoryDegradation(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	refreshed, err := messages.GetMessage(t.Context(), "session", "failed")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Metadata["memoryStatus"] != "degraded" || refreshed.Metadata["memoryWarning"] == "" || refreshed.Metadata["images"] == nil || refreshed.Metadata["_sohaExplicitMemoryIds"] == nil || refreshed.Metadata["mode"] != "general" {
		t.Fatalf("degradation lost original metadata: %+v", refreshed.Metadata)
	}
	next, _ := messages.GetMessage(t.Context(), "session", "next")
	if _, stale := next.Metadata["memoryStatus"]; stale {
		t.Fatal("old outage leaked to a later turn")
	}
	immediate := queuedNativeMessageResult(run, domaincopilot.WorkbenchSendMessageInput{}, errors.New("SENSITIVE_BACKEND_ERROR"))
	raw, _ := json.Marshal(immediate)
	if immediate.Envelope.Messages[0].Metadata["memoryStatus"] != "degraded" || strings.Contains(string(raw), "SENSITIVE_BACKEND_ERROR") {
		t.Fatal("instant feedback differs from persisted fact or leaks raw error")
	}
}

func TestDSHCheckpointReadRestoresWritableMaps(t *testing.T) {
	for _, raw := range []string{`{}`, `{"dshModelHashes":null,"dshModelResponses":null,"dshToolHashes":null,"dshToolNames":null,"dshToolResults":null}`} {
		cp, err := readDSHCheckpoint(domaincopilot.NativeLease{Checkpoint: json.RawMessage(raw)})
		if err != nil {
			t.Fatal(err)
		}
		cp.ModelHashes["model"] = "hash"
		cp.ModelResponses["model"] = appaigateway.WorkbenchRelayResponse{}
		cp.ToolHashes["tool"] = "hash"
		cp.ToolNames["tool"] = "knowledge_search"
		cp.ToolResults["tool"] = domaincopilot.DSHToolResult{}
	}
}
func TestDSHSuccessfulToolCallbackRoundTripCachesWithoutRedispatch(t *testing.T) {
	service, store, cp := dshFixture(t)
	cp.Start.ToolCatalogRevision = "catalog"
	gateway := service.runtime.Tools.(*nativeLoopGateway)
	raw := json.RawMessage(`{"query":"lease"}`)
	call := appaigateway.WorkbenchToolCall{ID: "read", Name: "knowledge_search", Arguments: raw}
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	if err := service.queueNativeModelToolCalls(t.Context(), principal, &cp.nativeCheckpoint, store.lease, appaigateway.WorkbenchRelayResponse{ToolCalls: []appaigateway.WorkbenchToolCall{call}}); err != nil {
		t.Fatal(err)
	}
	var semantic any
	_ = json.Unmarshal(raw, &semantic)
	cp.ToolHashes = map[string]string{call.ID: dshHash(semantic)}
	cp.ToolNames = map[string]string{call.ID: call.Name}
	cp.ToolResults = map[string]domaincopilot.DSHToolResult{}
	var err error
	store.lease, err = service.saveDSH(t.Context(), store.lease, cp, "tools", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(store.lease.Checkpoint), `"dshToolResults"`) {
		t.Fatal("test did not exercise omitted empty tool result map")
	}
	sum := sha256.Sum256(raw)
	request := domaincopilot.DSHToolCall{SchemaVersion: "opensoha.dev/dsh-runtime/v1", Kind: "tool_call", RunID: store.lease.Run.ID, Attempt: cp.Start.Attempt, Fence: cp.Start.Fence, CallID: call.ID, ToolName: call.Name, ToolCatalogRevision: cp.Start.ToolCatalogRevision, InputHash: hex.EncodeToString(sum[:]), Input: raw}
	first, err := service.InvokeDSHTool(t.Context(), cp.Start.ToolGrant, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "completed" || !json.Valid(first.Output) || gateway.toolCalls != 1 {
		t.Fatalf("tool not completed: %+v calls=%d", first, gateway.toolCalls)
	}
	persisted, err := readDSHCheckpoint(store.lease)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ToolResults[call.ID].Status != "completed" || len(persisted.Pending) != 0 || store.lease.ToolUsage.WorkCalls != 1 {
		t.Fatalf("missing durable tool receipt: %+v", persisted)
	}
	beforeVersion := store.lease.Version
	duplicate, err := service.InvokeDSHTool(t.Context(), cp.Start.ToolGrant, request)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(duplicate)
	if string(a) != string(b) || gateway.toolCalls != 1 || store.lease.ToolUsage.WorkCalls != 1 || store.lease.Version != beforeVersion {
		t.Fatalf("duplicate tool executed or changed receipt: first=%s duplicate=%s calls=%d", a, b, gateway.toolCalls)
	}
}

func TestDSHUnknownPauseRejectsLateModelCompletionWithoutRefund(t *testing.T) {
	for _, outcome := range []string{"success", "error"} {
		t.Run(outcome, func(t *testing.T) {
			s, store, stream, call, grant := dshHeldModelFixture(t)
			if outcome == "error" {
				stream.callErr = errors.New("upstream result arrived after runtime loss")
			}
			done := make(chan error, 1)
			go func() {
				_, err := s.InvokeDSHModelStream(t.Context(), grant, call, func(appaigateway.WorkbenchRelayStreamDelta) bool { return true })
				done <- err
			}()
			<-stream.started
			released := false
			defer func() {
				if !released {
					close(stream.release)
					<-done
				}
			}()
			lease, err := store.ReadNativeLease(t.Context(), "", call.RunID)
			if err != nil {
				t.Fatal(err)
			}
			before, err := readDSHCheckpoint(lease)
			if err != nil || before.ModelReservation <= 0 {
				t.Fatal("missing durable reservation", err)
			}
			if err = s.pauseDSH(t.Context(), lease, "unknown_outcome"); err != nil {
				t.Fatal(err)
			}
			close(stream.release)
			released = true
			if err = <-done; !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("late %s accepted: %v", outcome, err)
			}
			lease, err = store.ReadNativeLease(t.Context(), "", call.RunID)
			if err != nil {
				t.Fatal(err)
			}
			after, err := readDSHCheckpoint(lease)
			if err != nil {
				t.Fatal(err)
			}
			if lease.Phase != "paused" || after.StopReason != "unknown_outcome" || after.BudgetTokens != before.BudgetTokens || after.ModelReservation != before.ModelReservation || after.UsageKnown || len(after.ModelResponses) != 0 || len(after.Pending) != 0 {
				t.Fatalf("late completion changed unknown accounting: phase=%s before=%+v after=%+v", lease.Phase, before, after)
			}
			for _, id := range []string{call.CallID, "another-model-call"} {
				replay := call
				replay.CallID = id
				if _, err = s.InvokeDSHModel(t.Context(), grant, replay); !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("unknown model replay accepted: %v", err)
				}
			}
			if _, err = s.InvokeDSHTool(t.Context(), grant, domaincopilot.DSHToolCall{RunID: call.RunID, Attempt: call.Attempt, Fence: call.Fence}); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("unknown tool callback accepted: %v", err)
			}
			if stream.calls.Load() != 1 {
				t.Fatal("unknown run redispatched model")
			}
		})
	}
}

func TestDSHWaitingInputPauseKeepsCallbackAdmission(t *testing.T) {
	s, store, cp := dshFixture(t)
	cp.StopReason = "waiting_input"
	store.lease.Phase = "paused"
	store.lease.Checkpoint, _ = json.Marshal(cp)
	if _, _, _, _, err := s.authorizeDSHCallback(t.Context(), cp.Start.ToolGrant, cp.Start.DSHIdentity); err != nil {
		t.Fatalf("resumable input pause denied: %v", err)
	}
}

func TestDSHStaleWorkerCannotAdoptSuccessorLease(t *testing.T) {
	s, store, cp := dshFixture(t)
	original := store.lease
	store.lease.Fence++
	store.lease.Run.ClaimedByAgentID = "dsh-core:successor"
	store.lease.Checkpoint, _ = json.Marshal(cp)
	rt := &dshTestRuntime{}
	s.dshRuntime = rt
	for _, step := range []func() error{
		func() error { return s.processDSHRun(t.Context(), original) },
		func() error { return s.pollDSHRun(t.Context(), original) },
		func() error { return s.persistDSHEvents(t.Context(), original, cp) },
		func() error { return s.pauseDSH(t.Context(), original, "unknown_outcome") },
		func() error {
			return s.completeDSHRun(t.Context(), original, domaincopilot.DSHStatus{Status: "completed"})
		},
	} {
		if err := step(); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("stale worker acted with successor lease: %v", err)
		}
	}
	if rt.starts+rt.lookups+rt.cancels != 0 || len(store.events)+len(store.final) != 0 {
		t.Fatalf("stale worker performed side effects: %+v", rt)
	}
}

func TestDSHStartRejectionAndLostReplyHaveDifferentOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, status, phase, reason string
		startErr                    error
		lookups                     int
	}{
		{"rejected", "failed", "failed", "runtime_start_rejected", ErrDSHStartRejected, 0},
		{"lost", "running", "paused", "unknown_outcome", context.DeadlineExceeded, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, cp := dshFixture(t)
			cp.Submitted = false
			store.lease.Checkpoint, _ = json.Marshal(cp)
			rt := &dshTestRuntime{startErr: tc.startErr, lookupErr: apperrors.ErrNotFound}
			s.dshRuntime = rt
			if err := s.processDSHRun(t.Context(), store.lease); err != nil {
				t.Fatal(err)
			}
			run := store.lease.Run
			if rt.starts != 1 || rt.lookups != tc.lookups || run.Status != tc.status || run.Phase != tc.phase || run.StopReason != tc.reason {
				t.Fatalf("start disposition: runtime=%+v status=%s phase=%s reason=%s", rt, run.Status, run.Phase, run.StopReason)
			}
		})
	}
}

func TestDSHUnconfirmedCancelPersistsUnknownAndDoesNotRepeat(t *testing.T) {
	s, store, cp := dshFixture(t)
	submission := domaincopilot.DSHSubmission{DSHIdentity: cp.Start.DSHIdentity, InputHash: cp.Start.InputHash, SubmissionID: "receipt"}
	cp.Receipt = &domaincopilot.DSHReceipt{DSHSubmission: submission}
	cp.ModelReservation = 100
	store.lease.Checkpoint, _ = json.Marshal(cp)
	store.lease.CancelRequested = true
	rt := &dshTestRuntime{status: domaincopilot.DSHStatus{DSHSubmission: submission, Status: "running"}}
	s.dshRuntime = rt
	for range 2 {
		if err := s.processDSHRun(t.Context(), store.lease); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := readDSHCheckpoint(store.lease)
	if err != nil || rt.cancels != 1 || !saved.CancelSubmitted || saved.ModelReservation != 100 || store.lease.Run.StopReason != "unknown_outcome" ||
		store.lease.Run.Output["usageKnown"] != false || store.lease.Phase != "paused" || len(store.final) != 0 {
		t.Fatalf("unconfirmed cancellation lost receipt or retried: %+v %+v %v", rt, saved, err)
	}
}

func TestDSHReclaimedCleanupKeepsOriginalSubmission(t *testing.T) {
	s, store, cp := dshFixture(t)
	owner := s.nativeTasks.(*nativeTaskOwnerFixture)
	owner.denyLookup = true
	cp.Tasks = []nativeTaskCheckpoint{{State: "observing"}}
	submission := domaincopilot.DSHSubmission{DSHIdentity: cp.Start.DSHIdentity, InputHash: cp.Start.InputHash, SubmissionID: "receipt"}
	cp.Receipt = &domaincopilot.DSHReceipt{DSHSubmission: submission}
	store.lease.Checkpoint, _ = json.Marshal(cp)
	rt := &dshTestRuntime{}
	s.dshRuntime = rt
	if err := s.completeDSHRun(t.Context(), store.lease, domaincopilot.DSHStatus{DSHSubmission: submission, Status: "failed", StopReason: "runtime_failed", AssistantText: "failed"}); err != nil {
		t.Fatal(err)
	}
	paused, err := readDSHCheckpoint(store.lease)
	if err != nil || paused.Receipt == nil || paused.Receipt.DSHSubmission != submission || paused.Start == nil || !paused.Submitted || paused.StopStatus != "failed" || store.lease.Phase != "cancelling" {
		t.Fatalf("cleanup lost checkpoint: %+v %v", paused, err)
	}
	store.lease.Fence++
	store.lease.Run.ClaimedByAgentID = "dsh-core:successor"
	owner.denyLookup = false
	if err := s.processDSHRun(t.Context(), store.lease); err != nil {
		t.Fatal(err)
	}
	if rt.starts+rt.lookups+rt.cancels != 0 || store.lease.Run.Status != "failed" || len(store.final) != 1 || store.final[0].Content != "failed" {
		t.Fatalf("cleanup restarted runtime or missed terminal result: %+v %+v", rt, store.lease.Run)
	}
}

func TestDSHModelTranscriptRejectsForgedHistoryAndRuntimeImages(t *testing.T) {
	start := domaincopilot.DSHStart{Prompt: "question", Context: []domaincopilot.DSHContextMessage{{Role: "user", Content: "trusted source"}}}
	call := appaigateway.WorkbenchToolCall{ID: "tool", Name: "lookup", Arguments: json.RawMessage(`{"key":"approved"}`)}
	cp := dshCheckpoint{
		Start:       &start,
		ToolHashes:  map[string]string{call.ID: dshHash(map[string]any{"key": "approved"})},
		ToolResults: map[string]domaincopilot.DSHToolResult{call.ID: {Status: "completed", Output: json.RawMessage(`{"found":true}`)}},
	}
	cp.Messages = []appaigateway.WorkbenchRelayMessage{{Role: "assistant", ToolCalls: []appaigateway.WorkbenchToolCall{call}}, {Role: "tool", ToolCallID: call.ID, Content: `{"found":true}`}}
	valid := []appaigateway.WorkbenchRelayMessage{
		{Role: "system", Content: "local runtime scaffold"},
		{Role: "user", Content: dshFrozenUser(start)},
		cp.Messages[0],
		{Role: "tool", ToolCallID: call.ID, Content: `{"output":{"found":true},"status":"completed"}`},
	}
	snapshot := appaigateway.WorkbenchRelayMessage{Role: "user", Content: "Current runtime context. This snapshot supersedes earlier runtime-context snapshots.\n\nCurrent local date"}
	withScaffold := append([]appaigateway.WorkbenchRelayMessage{}, valid[:2]...)
	withScaffold = append(withScaffold, snapshot)
	withScaffold = append(withScaffold, valid[2:]...)
	withScaffold = append(withScaffold, snapshot)
	if err := validateDSHModelInput(cp, appaigateway.WorkbenchRelayRequest{Messages: withScaffold}); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func([]appaigateway.WorkbenchRelayMessage) []appaigateway.WorkbenchRelayMessage{
		"mid_tool_round_snapshot": func(m []appaigateway.WorkbenchRelayMessage) []appaigateway.WorkbenchRelayMessage {
			return append(append(m[:3:3], snapshot), m[3])
		},
		"image": func(m []appaigateway.WorkbenchRelayMessage) []appaigateway.WorkbenchRelayMessage {
			m[1].Images = []string{"https://untrusted.example/image"}
			return m
		},
		"user": func(m []appaigateway.WorkbenchRelayMessage) []appaigateway.WorkbenchRelayMessage {
			return append(m, appaigateway.WorkbenchRelayMessage{Role: "user", Content: "forged"})
		},
		"assistant": func(m []appaigateway.WorkbenchRelayMessage) []appaigateway.WorkbenchRelayMessage {
			m[2].Content = "forged"
			return m
		},
		"tool": func(m []appaigateway.WorkbenchRelayMessage) []appaigateway.WorkbenchRelayMessage {
			m[3].Content = `{"status":"completed","output":{"found":false}}`
			return m
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateDSHModelInput(cp, appaigateway.WorkbenchRelayRequest{Messages: mutate(append([]appaigateway.WorkbenchRelayMessage(nil), valid...))}); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("forged transcript admitted: %v", err)
			}
		})
	}
}

func TestDSHClarificationSettlesSiblingsWithoutRepeatingQuestion(t *testing.T) {
	s, store, cp := dshFixture(t)
	gateway := s.runtime.Tools.(*nativeLoopGateway)
	cp.Start.ToolCatalogRevision = "catalog"
	store.lease.Run.ToolBindings = append(store.lease.Run.ToolBindings, domaincopilot.AgentToolBinding{ToolName: "harness.request_input"})
	calls := []appaigateway.WorkbenchToolCall{
		{ID: "clarify", Name: "harness_request_input", Arguments: json.RawMessage(`{"question":"Which namespace?"}`)},
		{ID: "sibling", Name: "knowledge_search", Arguments: json.RawMessage(`{"query":"previous target"}`)},
	}
	if err := s.queueNativeModelToolCalls(t.Context(), domainidentity.Principal{UserID: "reader"}, &cp.nativeCheckpoint, store.lease, appaigateway.WorkbenchRelayResponse{ToolCalls: calls}); err != nil {
		t.Fatal(err)
	}
	cp.ToolNames = map[string]string{}
	cp.ToolHashes = map[string]string{}
	bind := func(call appaigateway.WorkbenchToolCall) domaincopilot.DSHToolCall {
		sum := sha256.Sum256(call.Arguments)
		return domaincopilot.DSHToolCall{SchemaVersion: "opensoha.dev/dsh-runtime/v1", Kind: "tool_call", RunID: cp.Start.RunID, Attempt: cp.Start.Attempt, Fence: cp.Start.Fence, CallID: call.ID, ToolName: call.Name, ToolCatalogRevision: cp.Start.ToolCatalogRevision, InputHash: hex.EncodeToString(sum[:]), Input: call.Arguments}
	}
	for _, call := range calls {
		var args any
		_ = json.Unmarshal(call.Arguments, &args)
		cp.ToolNames[call.ID], cp.ToolHashes[call.ID] = call.Name, dshHash(args)
	}
	store.lease.Checkpoint, _ = json.Marshal(cp)
	for range 2 {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		_, err := s.InvokeDSHTool(ctx, cp.Start.ToolGrant, bind(calls[0]))
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting question: %v", err)
		}
	}
	if len(store.events) != 2 || store.lease.Run.StopReason != "waiting_input" {
		t.Fatalf("question was repeated or missing: %+v", store.events)
	}
	cp, err := readDSHCheckpoint(store.lease)
	if err != nil {
		t.Fatal(err)
	}
	cp.UserInput = "payments"
	if _, err = s.saveDSH(t.Context(), store.lease, cp, "preparing", nil); err != nil {
		t.Fatal(err)
	}
	for i, call := range calls {
		result, err := s.InvokeDSHTool(t.Context(), cp.Start.ToolGrant, bind(call))
		if err != nil || result.Status != "completed" {
			t.Fatalf("call %s not settled: %+v %v", call.ID, result, err)
		}
		if i == 0 && string(result.Output) != `"payments"` || i == 1 && !strings.Contains(string(result.Output), "not executed") {
			t.Fatalf("call %s got another call's observation: %s", call.ID, result.Output)
		}
	}
	if gateway.toolCalls != 0 || store.lease.ToolUsage.WorkCalls != 0 {
		t.Fatal("stale sibling was executed")
	}
}

type dshReactionStream struct {
	request appaigateway.WorkbenchRelayRequest
}

func (f *dshReactionStream) InvokeWorkbenchModelStream(_ context.Context, _ domainidentity.Principal, in appaigateway.WorkbenchRelayRequest, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	f.request = in
	for _, content := range []string{"[[reac", "tion:👋]]", "\nHello"} {
		if !emit(appaigateway.WorkbenchRelayStreamDelta{ContentDelta: content}) {
			return appaigateway.WorkbenchRelayResponse{}, appaigateway.ErrWorkbenchRelayStreamStopped
		}
	}
	return appaigateway.WorkbenchRelayResponse{Content: "[[reaction:👋]]\nHello", Usage: map[string]float64{"totalTokens": 10}}, nil
}

func TestDSHModelUsesCoreImagesAndRuntimeOutputCapAndPublishesReaction(t *testing.T) {
	s, store, cp := dshFixture(t)
	var imageData bytes.Buffer
	canvas := image.NewGray(image.Rect(0, 0, 256, 256))
	random := rand.New(rand.NewPCG(42, 77))
	for i := range canvas.Pix {
		canvas.Pix[i] = byte(random.Uint32())
	}
	if err := png.Encode(&imageData, canvas); err != nil {
		t.Fatal(err)
	}
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData.Bytes())
	if len(imageURL) <= 32<<10 || len(imageURL) > maxChatImageDataURLBytes {
		t.Fatalf("fixture does not exercise an allowed image above the former token reservation: %d", len(imageURL))
	}
	store.lease.Run.Input["contextSelection"] = map[string]any{"images": []map[string]any{{"id": "image", "name": "diagram.png", "dataUrl": imageURL}}}
	if _, err := s.prepareDSH(t.Context(), store.lease, &cp); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dshFrozenUser(*cp.Start), imageURL) {
		t.Fatal("image leaked to runtime context")
	}
	tools := make([]appaigateway.WorkbenchTool, 0, len(cp.Start.Tools))
	for _, tool := range cp.Start.Tools {
		var schema map[string]any
		_ = json.Unmarshal(tool.InputSchema, &schema)
		tools = append(tools, appaigateway.WorkbenchTool{Name: tool.Name, Parameters: schema})
	}
	raw := json.RawMessage(`{"model":"soha-workbench"}`)
	sum := sha256.Sum256(raw)
	call := DSHModelCall{
		DSHIdentity: cp.Start.DSHIdentity, CallID: "reaction", ContextRevision: cp.Start.ContextRevision,
		ToolCatalogRevision: cp.Start.ToolCatalogRevision, InputHash: hex.EncodeToString(sum[:]), RawBody: raw,
		Request: appaigateway.WorkbenchRelayRequest{
			MaxOutputTokens: 37, Tools: tools, Messages: []appaigateway.WorkbenchRelayMessage{
				{Role: "system", Content: "RUNTIME SYSTEM SCAFFOLD"},
				{Role: "user", Content: dshFrozenUser(*cp.Start)},
			},
		},
	}
	stream := &dshReactionStream{}
	s.runtime.Stream = stream
	var visible strings.Builder
	response, err := s.InvokeDSHModelStream(t.Context(), cp.Start.ToolGrant, call, func(delta appaigateway.WorkbenchRelayStreamDelta) bool {
		visible.WriteString(delta.ContentDelta)
		return true
	})
	if err != nil || response.Content != "Hello" || visible.String() != "Hello" || stream.request.MaxOutputTokens != 37 {
		t.Fatalf("reaction or output cap changed: %+v visible=%q limit=%d err=%v", response, visible.String(), stream.request.MaxOutputTokens, err)
	}
	foundImage := false
	for _, message := range stream.request.Messages {
		if message.Content == "RUNTIME SYSTEM SCAFFOLD" {
			t.Fatal("runtime instructions reached the gateway")
		}
		if len(message.Images) == 1 && message.Images[0] == imageURL && message.Role == "user" {
			foundImage = true
		}
	}
	if !foundImage {
		t.Fatal("Core-authorized image did not reach the gateway")
	}
	cp, err = readDSHCheckpoint(store.lease)
	if err != nil || cp.Reaction != "👋" || cp.ModelContext == nil || cp.ModelContext.ReservationBasis != "serialized_text_bytes_plus_message_overhead_and_image_estimate" {
		t.Fatalf("reaction or image reservation lost from checkpoint: %q %v", cp.Reaction, err)
	}
	submission := domaincopilot.DSHSubmission{DSHIdentity: cp.Start.DSHIdentity, InputHash: cp.Start.InputHash, SubmissionID: "receipt"}
	cp.Receipt = &domaincopilot.DSHReceipt{DSHSubmission: submission}
	if _, err = s.saveDSH(t.Context(), store.lease, cp, "running", nil); err != nil {
		t.Fatal(err)
	}
	if err = s.completeDSHRun(t.Context(), store.lease, domaincopilot.DSHStatus{DSHSubmission: submission, Status: "completed", AssistantText: "Hello", UsageKnown: true}); err != nil {
		t.Fatal(err)
	}
	if len(store.final) != 1 || store.final[0].Metadata["assistantReaction"] != "👋" || store.final[0].Content != "Hello" {
		t.Fatalf("reaction lost from final message: %+v", store.final)
	}
}

type dshCancelBeforeRenewStore struct{ *nativeLoopStore }

func (s dshCancelBeforeRenewStore) RenewNativeLease(context.Context, domaincopilot.NativeLease) (bool, error) {
	s.lease.CancelRequested = true
	s.lease.Phase = "cancelling"
	s.lease.Version++
	return false, nil
}

func TestDSHCancelBeforeFirstStartRenewalDoesNotBecomeUnknown(t *testing.T) {
	s, store, cp := dshFixture(t)
	cp.Submitted = false
	store.lease.Checkpoint, _ = json.Marshal(cp)
	s.nativeRuns = dshCancelBeforeRenewStore{store}
	rt := &dshTestRuntime{}
	s.dshRuntime = rt
	if err := s.processDSHRun(t.Context(), store.lease); err != nil {
		t.Fatal(err)
	}
	if rt.starts+rt.lookups+rt.cancels != 0 || store.lease.Run.Status != "canceled" || store.lease.Run.StopReason != "user_cancelled" {
		t.Fatalf("pre-Start cancellation incorrectly reconciled a runtime request: runtime=%+v run=%+v", rt, store.lease.Run)
	}
}

func TestDSHContextFreePromptMustMatchExactly(t *testing.T) {
	start := domaincopilot.DSHStart{Prompt: "hello"}
	if !dshUserMatches("hello", start) || dshUserMatches("hello from elsewhere", start) {
		t.Fatal("context-free prompt binding was not exact")
	}
}
