package copilot

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DSHRuntime interface {
	Lookup(context.Context, domaincopilot.DSHIdentity, string) (domaincopilot.DSHStatus, error)
	Start(context.Context, domaincopilot.DSHStart) (domaincopilot.DSHReceipt, error)
	Status(context.Context, domaincopilot.DSHSubmission) (domaincopilot.DSHStatus, error)
	Cancel(context.Context, domaincopilot.DSHSubmission, string) (domaincopilot.DSHStatus, error)
	Events(context.Context, domaincopilot.DSHIdentity, int64) (domaincopilot.DSHEventPage, error)
}

// ErrDSHStartRejected identifies a request the runtime did not admit.
var ErrDSHStartRejected = errors.New("dsh runtime rejected start before admission")

type DSHRunStore interface {
	ClaimDSHRun(context.Context, string) (domaincopilot.NativeLease, error)
}
type DSHModelCall struct {
	domaincopilot.DSHIdentity
	CallID, ContextRevision, ToolCatalogRevision, InputHash string
	RawBody                                                 json.RawMessage
	Request                                                 appaigateway.WorkbenchRelayRequest
}
type dshCheckpoint struct {
	nativeCheckpoint
	Start                   *domaincopilot.DSHStart                        `json:"dshStart,omitempty"`
	AssemblyDecision        *domaincopilot.DSHAssemblyDecision             `json:"dshAssemblyDecision,omitempty"`
	PersonalStdioDispatches map[string]bool                                `json:"dshPersonalStdioDispatches,omitempty"`
	PluginDispatches        map[string]string                              `json:"dshPluginDispatches,omitempty"`
	HookObservations        []map[string]any                               `json:"dshHookObservations,omitempty"`
	Receipt                 *domaincopilot.DSHReceipt                      `json:"dshReceipt,omitempty"`
	ModelPriorUsageKnown    bool                                           `json:"dshModelPriorUsageKnown,omitempty"`
	RuntimeSettled          bool                                           `json:"dshRuntimeSettled,omitempty"`
	CancelSubmitted         bool                                           `json:"dshCancelSubmitted,omitempty"`
	Submitted               bool                                           `json:"dshSubmitted"`
	ModelHashes             map[string]string                              `json:"dshModelHashes,omitempty"`
	ModelResponses          map[string]appaigateway.WorkbenchRelayResponse `json:"dshModelResponses,omitempty"`
	RuntimeSequence         int64                                          `json:"dshRuntimeSequence,omitempty"`
	ToolNames               map[string]string                              `json:"dshToolNames,omitempty"`
	ToolHashes              map[string]string                              `json:"dshToolHashes,omitempty"`
	ToolResults             map[string]domaincopilot.DSHToolResult         `json:"dshToolResults,omitempty"`
}

func (s *Service) ConfigureDSHRuntime(client DSHRuntime, store DSHRunStore) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.running || s.dshRuntime != nil {
		return apperrors.ErrConflict
	}
	if isNilDependency(client) || isNilDependency(store) {
		return apperrors.ErrInvalidArgument
	}
	s.dshRuntime, s.dshRuns = client, store
	return nil
}
func dshHash(v any) string {
	raw, _ := json.Marshal(v)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func readDSHCheckpoint(lease domaincopilot.NativeLease) (dshCheckpoint, error) {
	cp := dshCheckpoint{nativeCheckpoint: initialNativeCheckpoint(lease.Run)}
	if len(lease.Checkpoint) > 2 {
		if err := json.Unmarshal(lease.Checkpoint, &cp); err != nil {
			return cp, err
		}
	}
	// Empty maps are omitted from durable JSON; explicit null is also valid in
	// historical checkpoints. Every successful read restores writable maps.
	if cp.ModelHashes == nil {
		cp.ModelHashes = map[string]string{}
	}
	if cp.ModelResponses == nil {
		cp.ModelResponses = map[string]appaigateway.WorkbenchRelayResponse{}
	}
	if cp.ToolHashes == nil {
		cp.ToolHashes = map[string]string{}
	}
	if cp.ToolNames == nil {
		cp.ToolNames = map[string]string{}
	}
	if cp.ToolResults == nil {
		cp.ToolResults = map[string]domaincopilot.DSHToolResult{}
	}
	return cp, nil
}
func (s *Service) saveDSH(ctx context.Context, lease domaincopilot.NativeLease, cp dshCheckpoint, phase string, events []domaincopilot.WorkbenchStreamEvent) (domaincopilot.NativeLease, error) {
	raw, err := json.Marshal(cp)
	if err != nil {
		return lease, err
	}
	return s.nativeRuns.AdvanceNativeRun(ctx, lease, domaincopilot.NativeAdvance{Run: nativePublicRun(lease.Run), Checkpoint: raw, Phase: phase, StopReason: cp.StopReason, Events: events})
}
func (s *Service) runDSHWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	worker := "dsh-core:" + uuid.NewString()
	slots := make(chan struct{}, 4)
	var workers sync.WaitGroup
	defer workers.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case slots <- struct{}{}:
			default:
				continue
			}
			lease, err := s.dshRuns.ClaimDSHRun(ctx, worker)
			if err != nil {
				<-slots
				continue
			}
			workers.Add(1)
			go func(lease domaincopilot.NativeLease) {
				defer workers.Done()
				defer func() { <-slots }()
				_ = s.processDSHRun(ctx, lease)
			}(lease)
		}
	}
}
func (s *Service) prepareDSH(ctx context.Context, lease domaincopilot.NativeLease, cp *dshCheckpoint) (domaincopilot.NativeLease, error) {
	principal, err := s.nativePrincipal(ctx, lease.Run)
	if err != nil {
		return lease, err
	}
	ctx, err = s.nativeAuthorizationContext(ctx, principal, lease.Run, true)
	if err != nil {
		return lease, err
	}
	input, err := s.nativeModelContext(ctx, lease, &cp.nativeCheckpoint, principal)
	if err != nil {
		return lease, err
	}
	tools, err := s.nativeModelTools(ctx, principal, lease.Run)
	if err != nil {
		return lease, err
	}
	start := domaincopilot.DSHStart{DSHIdentity: domaincopilot.DSHIdentity{RunID: lease.Run.ID, Attempt: 1, Fence: lease.Fence}, OwnerKey: dshHash(lease.Run.CreatedBy), SessionID: lease.Run.SessionID, RequestID: lease.Run.ID, InputHash: lease.RequestHash, ContextRevision: dshSourceRevision(input), Profile: "chat", Model: "soha-workbench", Prompt: stringValue(lease.Run.Input["question"]), ToolGrant: uuid.NewString(), Deadline: cp.Budget.Deadline.Format(time.RFC3339Nano), MaxOutputTokens: 4096}
	session, err := s.ownedSession(ctx, principal, lease.Run.SessionID)
	if err != nil {
		return lease, err
	}
	if isPersonalSession(session) {
		start.Profile = "heya"
	}
	if err := s.attachDSHAssembly(ctx, lease.Run, &start); err != nil {
		return lease, err
	}
	for _, message := range input.Messages {

		role := message.Role
		// B2 context is quoted user/assistant data, never executable system roles.
		if role != "assistant" {
			role = "user"
		}
		content := message.Content
		if content == "" {
			content = "[Authorized image attachment]"
		}
		start.Context = append(start.Context, domaincopilot.DSHContextMessage{Role: role, Content: content})
	}
	for _, tool := range tools {
		if !isPersonalSession(session) && (strings.HasPrefix(tool.Name, "personal_")) {
			continue
		}
		raw, _ := json.Marshal(tool.Parameters)
		start.Tools = append(start.Tools, domaincopilot.DSHTool{Name: tool.Name, Description: tool.Description, InputSchema: raw, ReadOnly: !isPersonalSession(session)})
	}
	start.ToolCatalogRevision = dshHash(start.Tools)
	cp.Start = &start
	lease.Run.Output["contextSnapshot"] = input.Snapshot
	return s.saveDSH(ctx, lease, *cp, "preparing", nil)
}

// Keep the claim identity across fresh reads. Reading a successor's version
// does not give the original goroutine its fence or permission to act.
func (s *Service) readDSHWorkerLease(ctx context.Context, claim domaincopilot.NativeLease) (domaincopilot.NativeLease, error) {
	current, err := s.nativeRuns.ReadNativeLease(ctx, "", claim.Run.ID)
	if err != nil {
		return claim, err
	}
	if current.Fence != claim.Fence || current.Run.ClaimedByAgentID != claim.Run.ClaimedByAgentID ||
		current.LeaseUntil == nil || !current.LeaseUntil.After(time.Now()) || agentRunStatusTerminal(current.Run.Status) {
		return claim, fmt.Errorf("%w: dsh claim expired or transferred", apperrors.ErrConflict)
	}
	return current, nil
}

func (s *Service) processDSHRun(ctx context.Context, claim domaincopilot.NativeLease) error {
	if claim.Run.ProviderKind != "dsh" || legacyExecutionReadOnly(claim.Run) {
		return apperrors.ErrAccessDenied
	}
	lease, cp, firstSubmission, done, err := s.prepareDSHSubmission(ctx, claim)
	if err != nil || done {
		return err
	}
	if cp.Receipt == nil {
		if done, err := s.receiveDSHSubmission(ctx, lease, cp, firstSubmission); err != nil || done {
			return err
		}
	}
	return s.pollDSHRun(ctx, claim)
}

func (s *Service) prepareDSHSubmission(ctx context.Context, claim domaincopilot.NativeLease) (domaincopilot.NativeLease, dshCheckpoint, bool, bool, error) {
	lock := s.dshLock(claim.Run.ID)
	lock.Lock()
	defer lock.Unlock()
	lease, err := s.readDSHWorkerLease(ctx, claim)
	if err != nil {
		return lease, dshCheckpoint{}, false, true, err
	}
	cp, err := readDSHCheckpoint(lease)
	if err != nil {
		return lease, cp, false, true, err
	}
	if cp.StopStatus != "" && (cp.RuntimeSettled || !cp.Submitted) {
		return lease, cp, false, true, s.finishDSHRun(ctx, lease, &cp, cp.StopStatus, cp.StopReason, cp.StopContent)
	}
	if cp.Submitted {
		return lease, cp, false, false, nil
	}
	if lease.CancelRequested {
		return lease, cp, false, true, s.finishDSHRun(ctx, lease, &cp, "canceled", "user_cancelled", "已取消本次任务。")
	}
	if cp.Start == nil || cp.Start.Fence != lease.Fence {
		lease, err = s.prepareDSH(ctx, lease, &cp)
		if err != nil {
			rejectUnconfirmedDSHAssembly(&lease.Run, cp)
			return lease, cp, false, true, s.nativeStepError(ctx, dshGovernedStore{NativeRunStore: s.nativeRuns, cp: &cp}, lease, cp.nativeCheckpoint, err, "policy_denied", "任务授权或来源检查失败。")
		}
	}
	cp.Submitted = true
	lease, err = s.saveDSH(ctx, lease, cp, "submitting", nil)
	return lease, cp, true, false, err
}

func (s *Service) receiveDSHSubmission(ctx context.Context, claim domaincopilot.NativeLease, cp dshCheckpoint, firstSubmission bool) (bool, error) {
	current, err := s.readDSHWorkerLease(ctx, claim)
	if err != nil {
		return true, err
	}
	if cp.Start == nil {
		return true, s.pauseDSH(ctx, claim, "unknown_outcome")
	}
	var receipt domaincopilot.DSHReceipt
	if firstSubmission {
		if current.CancelRequested {
			return true, s.finishRejectedDSHStart(ctx, claim, "user_cancelled")
		}
		// Renew the original claim immediately before the only allowed Start.
		var ok bool
		ok, err = s.nativeRuns.RenewNativeLease(ctx, claim)
		if err != nil || !ok {
			// No request left Core. Cancellation under this same claim is
			// conclusive even though the canceled lease could not renew.
			latest, readErr := s.readDSHWorkerLease(ctx, claim)
			if readErr == nil && latest.CancelRequested {
				return true, s.finishRejectedDSHStart(ctx, claim, "user_cancelled")
			}
			if err != nil {
				return true, err
			}
			return true, apperrors.ErrConflict
		}
		startCtx, cancel := context.WithCancel(ctx)
		heartbeat := s.nativeHeartbeat(startCtx, cancel, s.nativeRuns, claim)
		receipt, err = s.dshRuntime.Start(startCtx, *cp.Start)
		cancel()
		<-heartbeat
		if errors.Is(err, ErrDSHStartRejected) {
			return true, s.finishRejectedDSHStart(ctx, claim, "runtime_start_rejected")
		}
	}
	// A missing receipt after a possible submission can only be looked up.
	if !firstSubmission || err != nil {
		if _, err := s.readDSHWorkerLease(ctx, claim); err != nil {
			return true, err
		}
		status, lookupErr := s.dshRuntime.Lookup(ctx, cp.Start.DSHIdentity, cp.Start.InputHash)
		if lookupErr != nil {
			return true, s.pauseDSH(ctx, claim, "unknown_outcome")
		}
		receipt = domaincopilot.DSHReceipt{DSHSubmission: status.DSHSubmission, Status: status.Status, Sequence: status.Sequence}
	}
	if receipt.DSHIdentity != cp.Start.DSHIdentity || receipt.InputHash != cp.Start.InputHash || receipt.SubmissionID == "" {
		return true, s.pauseDSH(ctx, claim, "unknown_outcome")
	}
	lock := s.dshLock(claim.Run.ID)
	lock.Lock()
	defer lock.Unlock()
	lease, err := s.readDSHWorkerLease(ctx, claim)
	if err != nil {
		return true, err
	}
	cp, err = readDSHCheckpoint(lease)
	if err != nil {
		return true, err
	}
	cp.Receipt = &receipt
	_, err = s.saveDSH(ctx, lease, cp, lease.Phase, nil)
	return false, err
}

func (s *Service) finishRejectedDSHStart(ctx context.Context, claim domaincopilot.NativeLease, reason string) error {
	lock := s.dshLock(claim.Run.ID)
	lock.Lock()
	defer lock.Unlock()
	lease, err := s.readDSHWorkerLease(ctx, claim)
	if err != nil {
		return err
	}
	cp, err := readDSHCheckpoint(lease)
	if err != nil {
		return err
	}
	return s.finishDSHRun(ctx, lease, &cp, "failed", reason, "运行时拒绝启动本次任务。")
}

func (s *Service) pollDSHRun(ctx context.Context, claim domaincopilot.NativeLease) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// No callback lock is needed to take a read snapshot. Mutations
			// reload under that lock and compare the same original claim.
			lease, err := s.readDSHWorkerLease(ctx, claim)
			if err != nil {
				return err
			}
			cp, err := readDSHCheckpoint(lease)
			if err != nil {
				return err
			}
			if lease.Phase == "paused" && cp.StopReason == "unknown_outcome" {
				return nil
			}
			done, err := s.reconcileDSHStatus(ctx, lease, cp)
			if err != nil || done {
				return err
			}
		}
	}
}
func (s *Service) reconcileDSHStatus(ctx context.Context, lease domaincopilot.NativeLease, cp dshCheckpoint) (bool, error) {
	if cp.Start == nil || cp.Receipt == nil {
		return true, apperrors.ErrConflict
	}
	expected := cp.Receipt.DSHSubmission
	var status domaincopilot.DSHStatus
	var err error
	cancel := lease.CancelRequested || cp.Start.Fence != lease.Fence || cp.StopStatus != "" || !cp.Budget.Deadline.After(time.Now())
	if cancel {
		status, err = s.cancelDSHRuntime(ctx, lease, cp)
	} else {
		status, err = s.dshRuntime.Status(ctx, expected)
	}
	if errors.Is(err, apperrors.ErrConflict) {
		return true, err
	}
	if err != nil || status.DSHSubmission != expected {
		return true, s.pauseDSH(ctx, lease, "unknown_outcome")
	}
	if status.Terminal() || status.Status == "unknown" {
		return true, s.finishDSHRuntimeReceipt(ctx, lease, cp, status, cancel)
	}
	if !cancel {
		if err = s.persistDSHEvents(ctx, lease, cp); err != nil {
			return true, err
		}
	}
	if cancel {
		// Cancel made one bounded stop attempt. A nonterminal receipt cannot
		// prove exit and must not trigger an unbounded series of stop requests.
		return true, s.pauseDSH(ctx, lease, "unknown_outcome")
	}
	ok, err := s.nativeRuns.RenewNativeLease(ctx, lease)
	if err != nil {
		return true, err
	}
	if !ok {
		current, err := s.readDSHWorkerLease(ctx, lease)
		if err != nil || !current.CancelRequested {
			return true, apperrors.ErrConflict
		}
	}
	return false, nil
}

func (s *Service) finishDSHRuntimeReceipt(ctx context.Context, lease domaincopilot.NativeLease, cp dshCheckpoint, status domaincopilot.DSHStatus, cancel bool) error {
	if err := s.persistDSHFinalEvents(ctx, lease, cp, status.Sequence, cancel || status.Status != "completed"); err != nil {
		return err
	}
	if status.Status == "unknown" {
		return s.pauseDSH(ctx, lease, "unknown_outcome")
	}
	return s.completeDSHRun(ctx, lease, status)
}
func (s *Service) cancelDSHRuntime(ctx context.Context, claim domaincopilot.NativeLease, cp dshCheckpoint) (domaincopilot.DSHStatus, error) {
	if cp.CancelSubmitted {
		return s.dshRuntime.Status(ctx, cp.Receipt.DSHSubmission)
	}
	lock := s.dshLock(claim.Run.ID)
	lock.Lock()
	lease, err := s.readDSHWorkerLease(ctx, claim)
	if err == nil {
		cp, err = readDSHCheckpoint(lease)
	}
	if err == nil {
		cp.CancelSubmitted = true
		_, err = s.saveDSH(ctx, lease, cp, "cancelling", nil)
	}
	lock.Unlock()
	if err != nil {
		return domaincopilot.DSHStatus{}, err
	}
	if _, err = s.readDSHWorkerLease(ctx, claim); err != nil {
		return domaincopilot.DSHStatus{}, err
	}
	return s.dshRuntime.Cancel(ctx, cp.Receipt.DSHSubmission, "Core cancellation or fence changed")
}
func (s *Service) completeDSHRun(ctx context.Context, claim domaincopilot.NativeLease, status domaincopilot.DSHStatus) error {
	lock := s.dshLock(claim.Run.ID)
	lock.Lock()
	defer lock.Unlock()
	lease, err := s.readDSHWorkerLease(ctx, claim)
	if err != nil {
		return err
	}
	cp, err := readDSHCheckpoint(lease)
	if err != nil {
		return err
	}
	if cp.Receipt == nil || cp.Receipt.DSHSubmission != status.DSHSubmission {
		return apperrors.ErrConflict
	}
	cp.RuntimeSettled = true
	cp.UsageKnown = cp.UsageKnown && status.UsageKnown
	if cp.StopStatus != "" {
		return s.finishDSHRun(ctx, lease, &cp, cp.StopStatus, cp.StopReason, cp.StopContent)
	}
	content, reaction := splitMessageReaction(status.AssistantText)
	if reaction != "" {
		cp.Reaction = reaction
	}
	return s.finishDSHRun(ctx, lease, &cp, status.Status, status.StopReason, content)
}
func (s *Service) finishDSHRun(ctx context.Context, lease domaincopilot.NativeLease, cp *dshCheckpoint, status, reason, content string) error {
	if lease.CancelRequested {
		status, reason, content = "canceled", "user_cancelled", "已取消本次任务。"
	}
	rejectUnconfirmedDSHAssembly(&lease.Run, *cp)
	if status == "completed" && cp.Start != nil && !dshAssemblyConfirmed(*cp) {
		status, reason, content = "failed", "runtime_assembly_unconfirmed", "运行配置未获确认。"
	}
	markUnsettledDSHPluginExecutions(&lease.Run, *cp)
	if dshHookObservationsPending(*cp) {
		lease.Run.Output["runtimeHookPending"] = true
	}
	return s.finishNativeRun(ctx, dshGovernedStore{NativeRunStore: s.nativeRuns, cp: cp, release: true}, lease, cp.nativeCheckpoint, status, reason, content)
}

const maxDSHHookObservations = 64

func dshHookObservationsPending(cp dshCheckpoint) bool {
	return cp.RuntimeSettled && dshHookAuditLoaded(cp) && len(cp.HookObservations) < min(len(cp.ToolNames), maxDSHHookObservations)
}

// The durable runtime journal can gain audit observations after the business
// result is committed. Merge those observations on an owned history read.
func (s *Service) recoverDSHHookObservations(ctx context.Context, run domaincopilot.AgentRun) domaincopilot.AgentRun {
	if run.ProviderKind != "dsh" || run.CreatedBy == "" || run.Output["runtimeHookPending"] != true || s.nativeRuns == nil || s.dshRuntime == nil {
		return run
	}
	lease, err := s.nativeRuns.ReadNativeLease(ctx, run.CreatedBy, run.ID)
	if err != nil {
		return run
	}
	cp, err := readDSHCheckpoint(lease)
	if err != nil || cp.Start == nil || cp.Start.RunID != run.ID || !dshHookObservationsPending(cp) {
		return run
	}
	page, err := s.dshRuntime.Events(ctx, cp.Start.DSHIdentity, cp.RuntimeSequence)
	if err != nil || len(page.Items) == 0 {
		return run
	}
	recovered := run
	recovered.Output = maps.Clone(run.Output)
	if _, err := projectDSHEventPage(&recovered, &cp, page.Items, true); err != nil {
		return run
	}
	if !dshHookObservationsPending(cp) {
		delete(recovered.Output, "runtimeHookPending")
	}
	return recovered
}
func (s *Service) pauseDSH(ctx context.Context, claim domaincopilot.NativeLease, reason string) error {
	lock := s.dshLock(claim.Run.ID)
	lock.Lock()
	defer lock.Unlock()
	lease, err := s.readDSHWorkerLease(ctx, claim)
	if err != nil {
		return err
	}
	// A user cancel arriving during an earlier status lookup still needs its
	// own reconciliation. Do not replace the newly scheduled cancellation.
	if lease.CancelRequested && !claim.CancelRequested {
		return apperrors.ErrConflict
	}
	cp, err := readDSHCheckpoint(lease)
	if err != nil {
		return err
	}
	cp.UsageKnown = false
	markUnsettledDSHPluginExecutions(&lease.Run, cp)
	cp.StopStatus, cp.StopContent = "", ""
	cp.StopReason = reason
	lease.Run.Output["usageKnown"] = false
	lease.Run.Output["usage"] = nativeUsageProjection(cp.nativeCheckpoint)
	delete(lease.Run.Output, "pendingQuestion")
	_, err = s.saveDSH(ctx, lease, cp, "paused", []domaincopilot.WorkbenchStreamEvent{{Type: "error", Code: reason, Message: "运行结果未知，请核对任务回执。"}})
	return err
}
func (s *Service) waitDSHMessage(ctx context.Context, principal domainidentity.Principal, result domaincopilot.WorkbenchStreamResult) (domaincopilot.SessionMessageEnvelope, error) {
	id := stringValue(result.Envelope.SessionPatch["agentRunId"])
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return result.Envelope, ctx.Err()
		case <-ticker.C:
			lease, err := s.nativeRuns.ReadNativeLease(ctx, principal.UserID, id)
			if err != nil {
				return result.Envelope, err
			}
			if agentRunStatusTerminal(lease.Run.Status) {
				for _, event := range workbenchEventsFromValue(lease.Run.Output["workbenchEvents"]) {
					if event.Type == "message.done" && event.MessageID == id+":reply" {
						message := domaincopilot.Message{ID: event.MessageID, SessionID: lease.Run.SessionID, Role: event.Role, Content: event.Content, Metadata: event.Metadata, CreatedAt: event.CreatedAt}
						result.Envelope.Messages = append(result.Envelope.Messages, s.visibleKnowledgeMessage(ctx, principal, message))
						return result.Envelope, nil
					}
				}
				messages, err := s.listRecentMessages(ctx, lease.Run.SessionID, 100)
				if err != nil {
					return result.Envelope, err
				}
				for _, m := range messages {
					if m.ID == id+":reply" {
						result.Envelope.Messages = append(result.Envelope.Messages, s.visibleKnowledgeMessage(ctx, principal, m))
					}
				}
				return result.Envelope, nil
			}
			if lease.Phase == "paused" {
				return result.Envelope, nil
			}
		}
	}
}

func (s *Service) boundDSHCallback(ctx context.Context, grant string, id domaincopilot.DSHIdentity) (domaincopilot.NativeLease, dshCheckpoint, error) {
	lease, err := s.nativeRuns.ReadNativeLease(ctx, "", id.RunID)
	if err != nil {
		return lease, dshCheckpoint{}, err
	}
	cp, err := readDSHCheckpoint(lease)
	if err != nil {
		return lease, cp, err
	}
	// An unknown runtime outcome freezes its grant, including already-dispatched
	// model completions. Keep their reservation until explicit reconciliation;
	// waiting_input is a different, resumable pause.
	if cp.StopReason == "unknown_outcome" || lease.Phase == "cancelling" || legacyExecutionReadOnly(lease.Run) || cp.Start == nil || cp.Start.DSHIdentity != id || id.Fence != lease.Fence || lease.Run.ProviderKind != "dsh" || lease.Run.Input["_sohaExecutionOwner"] != "dsh-core" || lease.CancelRequested || lease.Run.Status != "running" || lease.LeaseUntil == nil || !lease.LeaseUntil.After(time.Now()) || !cp.Budget.Deadline.After(time.Now()) || subtle.ConstantTimeCompare([]byte(grant), []byte(cp.Start.ToolGrant)) != 1 {
		return lease, cp, apperrors.ErrAccessDenied
	}
	return lease, cp, nil
}

func (s *Service) authorizeDSHCallback(ctx context.Context, grant string, id domaincopilot.DSHIdentity) (domaincopilot.NativeLease, dshCheckpoint, domainidentity.Principal, context.Context, error) {
	lease, cp, err := s.boundDSHCallback(ctx, grant, id)
	if err != nil {
		return lease, cp, domainidentity.Principal{}, ctx, err
	}
	principal, err := s.nativePrincipal(ctx, lease.Run)
	if err == nil {
		err = s.authorizeDSHProfileBinding(ctx, principal, lease.Run, cp.Start)
	}
	if err == nil {
		ctx, err = s.nativeAuthorizationContext(ctx, principal, lease.Run, true)
	}
	return lease, cp, principal, ctx, err
}

func (s *Service) InvokeDSHModel(ctx context.Context, grant string, in DSHModelCall) (appaigateway.WorkbenchRelayResponse, error) {
	return s.invokeDSHModel(ctx, grant, in, nil)
}
func (s *Service) InvokeDSHModelStream(ctx context.Context, grant string, in DSHModelCall, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	return s.invokeDSHModel(ctx, grant, in, emit)
}
func (s *Service) invokeDSHModel(ctx context.Context, grant string, in DSHModelCall, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	s.dshLock(in.RunID).Lock()
	defer s.dshLock(in.RunID).Unlock()
	lease, cp, principal, ctx, err := s.authorizeDSHCallback(ctx, grant, in.DSHIdentity)
	if err != nil {
		return appaigateway.WorkbenchRelayResponse{}, err
	}
	if !dshAssemblyConfirmed(cp) {
		return appaigateway.WorkbenchRelayResponse{}, apperrors.ErrAccessDenied
	}
	hash := sha256.Sum256(in.RawBody)
	if in.CallID == "" || len(in.CallID) > 128 || len(in.RawBody) == 0 || hex.EncodeToString(hash[:]) != in.InputHash || in.ContextRevision != cp.Start.ContextRevision || in.ToolCatalogRevision != cp.Start.ToolCatalogRevision {
		return appaigateway.WorkbenchRelayResponse{}, apperrors.ErrAccessDenied
	}
	sourceCheckpoint := cp.nativeCheckpoint
	sourceCheckpoint.Messages = nil
	input, err := s.nativeModelContext(ctx, lease, &sourceCheckpoint, principal)
	if err != nil {
		return appaigateway.WorkbenchRelayResponse{}, err
	}
	if dshSourceRevision(input) != cp.Start.ContextRevision {
		return appaigateway.WorkbenchRelayResponse{}, fmt.Errorf("%w: model context sources changed", apperrors.ErrAccessDenied)
	}
	if previous, exists := cp.ModelHashes[in.CallID]; exists {
		if previous != in.InputHash {
			return appaigateway.WorkbenchRelayResponse{}, apperrors.ErrConflict
		}
		if response, ok := cp.ModelResponses[in.CallID]; ok {
			return response, nil
		}
		return appaigateway.WorkbenchRelayResponse{}, fmt.Errorf("%w: prior model outcome unknown", apperrors.ErrConflict)
	}
	if len(cp.Pending) > 0 || dshModelInFlight(cp) {
		return appaigateway.WorkbenchRelayResponse{}, apperrors.ErrConflict
	}
	if err = validateDSHModelInput(cp, in.Request); err != nil {
		return appaigateway.WorkbenchRelayResponse{}, err
	}
	return s.executeDSHModel(ctx, grant, lease, cp, principal, in, input, emit)
}
func (s *Service) executeDSHModel(ctx context.Context, grant string, lease domaincopilot.NativeLease, cp dshCheckpoint, principal domainidentity.Principal, in DSHModelCall, input nativeModelInput, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	currentTools, err := s.nativeModelTools(ctx, principal, lease.Run)
	if err != nil {
		return appaigateway.WorkbenchRelayResponse{}, err
	}
	effective := dshEffectiveStart(cp)
	for _, tool := range dshNativeTools(cp) {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			return appaigateway.WorkbenchRelayResponse{}, err
		}
		currentTools = append(currentTools, appaigateway.WorkbenchTool{Name: tool.Name, Description: tool.Description, Parameters: schema})
	}
	tools, err := dshAuthorizedTools(effective, currentTools)
	if err != nil {
		return appaigateway.WorkbenchRelayResponse{}, err
	}
	// Core rebuilds the exact upstream request from reauthorized sources and
	// durable receipts. Runtime text and images never become model authority.
	messages := append(input.Messages, cp.Messages...)
	request, err := s.nativeModelRequest(ctx, lease.Run, input.Metadata, messages, tools)
	if err != nil {
		return appaigateway.WorkbenchRelayResponse{}, err
	}
	if cp.Start.MaxOutputTokens > 0 {
		request.MaxOutputTokens = int(cp.Start.MaxOutputTokens)
		if in.Request.MaxOutputTokens > 0 {
			request.MaxOutputTokens = min(request.MaxOutputTokens, in.Request.MaxOutputTokens)
		}
	}
	if cp.Usage.Steps >= cp.Budget.MaxSteps {
		return appaigateway.WorkbenchRelayResponse{}, apperrors.ErrConflict
	}
	if err = reserveNativeModelBudget(&cp.nativeCheckpoint, &request); err != nil {
		return appaigateway.WorkbenchRelayResponse{}, err
	}
	cp.Usage.Steps++
	if err = recordNativeModelContext(&cp.nativeCheckpoint, request, input.Snapshot); err != nil {
		return appaigateway.WorkbenchRelayResponse{}, err
	}
	cp.ModelContext.Lineage = input.Lineage
	if cp.ModelHashes == nil {
		cp.ModelHashes = map[string]string{}
		cp.ModelResponses = map[string]appaigateway.WorkbenchRelayResponse{}
	}
	cp.ModelHashes[in.CallID] = in.InputHash
	cp.ModelPriorUsageKnown = cp.UsageKnown
	cp.UsageKnown = false
	lease, err = s.saveDSH(ctx, lease, cp, "model", nil)
	if err != nil {
		return appaigateway.WorkbenchRelayResponse{}, err
	}
	response, reaction, callErr := s.invokeDSHGatewayUnlocked(ctx, lease, cp, principal, request, emit)
	return s.completeDSHModelCall(ctx, grant, in, response, reaction, callErr)
}

// The submission and reservation above are durable before releasing this lock.
// Reacquire it before returning so the caller can reconcile against fresh state.
func (s *Service) invokeDSHGatewayUnlocked(ctx context.Context, lease domaincopilot.NativeLease, cp dshCheckpoint, principal domainidentity.Principal, request appaigateway.WorkbenchRelayRequest, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, string, error) {
	lock := s.dshLock(lease.Run.ID)
	lock.Unlock()
	defer lock.Lock()
	modelCtx, cancel := context.WithDeadline(ctx, cp.Budget.Deadline)
	defer cancel()
	done := s.nativeHeartbeat(modelCtx, cancel, s.nativeRuns, lease)
	defer func() { cancel(); <-done }()
	// Protected arguments may appear in free text split across arbitrary deltas.
	protected := nativeHasProtectedCapability(lease.Run)
	var response appaigateway.WorkbenchRelayResponse
	var err error
	if emit == nil {
		response, err = s.runtime.Model.InvokeWorkbenchModel(modelCtx, principal, request)
	} else {
		filter := reactionStream{send: func(content string) bool {
			return emit(appaigateway.WorkbenchRelayStreamDelta{ContentDelta: content})
		}}
		sink := func(delta appaigateway.WorkbenchRelayStreamDelta) bool {
			return modelCtx.Err() == nil && (protected || filter.push(delta.ContentDelta))
		}
		response, err = s.runtime.Stream.InvokeWorkbenchModelStream(modelCtx, principal, request, sink)
		if err == nil && !protected && !filter.flush() {
			err = appaigateway.ErrWorkbenchRelayStreamStopped
		}
	}
	if err != nil {
		return response, "", err
	}
	projection := cp.nativeCheckpoint
	for _, call := range response.ToolCalls {
		projection.Pending = append(projection.Pending, nativeCall{Call: call})
	}
	content, reaction := splitMessageReaction(response.Content)
	response.Content = nativePrivateText(projection, content)
	if protected && emit != nil && response.Content != "" && !emit(appaigateway.WorkbenchRelayStreamDelta{ContentDelta: response.Content}) {
		return response, "", appaigateway.ErrWorkbenchRelayStreamStopped
	}
	return response, reaction, nil
}

func (s *Service) completeDSHModelCall(ctx context.Context, grant string, in DSHModelCall, response appaigateway.WorkbenchRelayResponse, reaction string, callErr error) (appaigateway.WorkbenchRelayResponse, error) {
	lease, cp, principal, ctx, err := s.authorizeDSHCallback(ctx, grant, in.DSHIdentity)
	if err != nil {
		return response, err
	}
	if cp.ModelHashes[in.CallID] != in.InputHash {
		return response, apperrors.ErrConflict
	}
	if _, completed := cp.ModelResponses[in.CallID]; completed {
		return response, apperrors.ErrConflict
	}
	if callErr != nil {
		if cp.ModelContext != nil {
			cp.ModelContext.Status = "outcome_unknown"
		}
		cp.UsageKnown = false
		_, err = s.saveDSH(ctx, lease, cp, "running", nil)
		if err != nil {
			return response, err
		}
		return response, callErr
	}
	sourceCheckpoint := cp.nativeCheckpoint
	sourceCheckpoint.Messages = nil
	input, err := s.nativeModelContext(ctx, lease, &sourceCheckpoint, principal)
	if err != nil {
		return response, err
	}
	if dshSourceRevision(input) != cp.Start.ContextRevision {
		return response, fmt.Errorf("%w: model context sources changed", apperrors.ErrAccessDenied)
	}
	cp.UsageKnown = cp.ModelPriorUsageKnown
	if err = settleNativeModelBudget(&cp.nativeCheckpoint, response); err != nil {
		return response, err
	}
	if reaction != "" {
		cp.Reaction = reaction
	}
	cp.ModelContext.receive(response, cp.nativeCheckpoint)
	// Clarification invalidates the other arguments in this round. Put the
	// question first on the wire as well as in Core's execution queue.
	if i := slices.IndexFunc(response.ToolCalls, func(call appaigateway.WorkbenchToolCall) bool {
		return call.Name == "harness_request_input"
	}); i > 0 {
		response.ToolCalls = slices.Clone(response.ToolCalls)
		question := response.ToolCalls[i]
		copy(response.ToolCalls[1:i+1], response.ToolCalls[:i])
		response.ToolCalls[0] = question
	}
	if cp.ToolHashes == nil {
		cp.ToolNames = map[string]string{}
		cp.ToolHashes = map[string]string{}
		cp.ToolResults = map[string]domaincopilot.DSHToolResult{}
	}
	for _, call := range response.ToolCalls {
		if call.ID == "" || cp.ToolHashes[call.ID] != "" {
			return response, apperrors.ErrConflict
		}
		var arguments any
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
			return response, apperrors.ErrInvalidArgument
		}
		cp.ToolHashes[call.ID] = dshHash(arguments)
		cp.ToolNames[call.ID] = call.Name
	}
	if err = s.queueNativeModelToolCalls(ctx, principal, &cp.nativeCheckpoint, lease, response, dshNativeTools(cp)...); err != nil {
		return response, err
	}
	if cp.ModelResponses == nil {
		cp.ModelResponses = map[string]appaigateway.WorkbenchRelayResponse{}
	}
	cp.ModelResponses[in.CallID] = response
	_, err = s.saveDSH(ctx, lease, cp, "running", nil)
	return response, err
}
func dshAuthorizedTools(start domaincopilot.DSHStart, current []appaigateway.WorkbenchTool) ([]appaigateway.WorkbenchTool, error) {
	tools := make([]appaigateway.WorkbenchTool, 0, len(start.Tools))
	for _, frozen := range start.Tools {
		var schema map[string]any
		if json.Unmarshal(frozen.InputSchema, &schema) != nil {
			return nil, apperrors.ErrAccessDenied
		}
		if !slices.ContainsFunc(current, func(tool appaigateway.WorkbenchTool) bool {
			return tool.Name == frozen.Name && dshHash(tool.Parameters) == dshHash(schema)
		}) {
			return nil, apperrors.ErrAccessDenied
		}
		tools = append(tools, appaigateway.WorkbenchTool{Name: frozen.Name, Description: frozen.Description, Parameters: schema})
	}
	return tools, nil
}

// The pinned SDK projects runtime context after its user prompt and can keep
// that row in later requests. Accept it only between complete conversation
// rounds. The projection is local scaffold, never model input for Core.
func omitDSHRuntimeContext(messages []appaigateway.WorkbenchRelayMessage) []appaigateway.WorkbenchRelayMessage {
	const snapshotPrefix = "Current runtime context. This snapshot supersedes earlier runtime-context snapshots.\n\n"
	const cleared = "Current runtime context: none. Earlier runtime-context snapshots no longer apply."
	transcript := make([]appaigateway.WorkbenchRelayMessage, 0, len(messages))
	for i, message := range messages {
		if i > 0 && message.Role == "user" && message.ToolCallID == "" && len(message.ToolCalls) == 0 &&
			(strings.HasPrefix(message.Content, snapshotPrefix) || message.Content == cleared) &&
			(i == 1 && messages[0].Role == "user" || messages[i-1].Role == "tool") &&
			(i+1 == len(messages) || messages[i+1].Role == "assistant") {
			continue
		}
		transcript = append(transcript, message)
	}
	return transcript
}

func validateDSHModelInput(cp dshCheckpoint, in appaigateway.WorkbenchRelayRequest) error {
	effective := dshEffectiveStart(cp)
	if len(in.Messages) == 0 || len(in.Messages) > 128 || len(in.Tools) != len(effective.Tools) || in.MaxOutputTokens < 0 {
		return apperrors.ErrInvalidArgument
	}
	if _, err := dshAuthorizedTools(effective, in.Tools); err != nil {
		return err
	}
	messages := in.Messages
	// SDK bootstrap instructions are local runtime scaffolding. Core supplies
	// its own instructions; never forward adapter-provided system messages.
	for _, m := range messages {
		if len(m.Images) != 0 {
			return apperrors.ErrAccessDenied
		}
	}
	for len(messages) > 0 && (messages[0].Role == "system" || messages[0].Role == "developer") {
		if len(messages[0].ToolCalls) != 0 || messages[0].ToolCallID != "" {
			return apperrors.ErrAccessDenied
		}
		messages = messages[1:]
	}
	messages = omitDSHRuntimeContext(messages)
	if len(messages) != 1+len(cp.Messages) || messages[0].Role != "user" || messages[0].ToolCallID != "" ||
		len(messages[0].ToolCalls) != 0 || !dshUserMatches(messages[0].Content, *cp.Start) {
		return fmt.Errorf("%w: runtime model transcript changed", apperrors.ErrAccessDenied)
	}
	for i, expected := range cp.Messages {
		if !dshHistoryMatches(cp, expected, messages[i+1]) {
			return fmt.Errorf("%w: runtime model transcript changed", apperrors.ErrAccessDenied)
		}
	}
	return nil
}

func dshHistoryMatches(cp dshCheckpoint, expected, message appaigateway.WorkbenchRelayMessage) bool {
	if message.Role != expected.Role || message.ToolCallID != expected.ToolCallID || len(message.ToolCalls) != len(expected.ToolCalls) {
		return false
	}
	if message.Role == "tool" {
		receipt, ok := cp.ToolResults[message.ToolCallID]
		var actual, wanted any
		wire, _ := json.Marshal(struct {
			Status string          `json:"status"`
			Output json.RawMessage `json:"output,omitempty"`
		}{receipt.Status, receipt.Output})
		return ok && json.Unmarshal([]byte(message.Content), &actual) == nil && json.Unmarshal(wire, &wanted) == nil && dshHash(actual) == dshHash(wanted)
	}
	if message.Content != expected.Content && (message.Content != "" || strings.TrimSpace(expected.Content) != "") {
		return false
	}
	for i, call := range message.ToolCalls {
		var arguments any
		if call.ID != expected.ToolCalls[i].ID || call.Name != expected.ToolCalls[i].Name ||
			json.Unmarshal(call.Arguments, &arguments) != nil || cp.ToolHashes[call.ID] != dshHash(arguments) {
			return false
		}
	}
	return true
}

// Native tool governance writes through this adapter so the dsh receipt and
// grants survive each existing workflow checkpoint without duplicating its loop.
type dshGovernedStore struct {
	NativeRunStore
	cp      *dshCheckpoint
	release bool
}

func (d dshGovernedStore) AdvanceNativeRun(ctx context.Context, lease domaincopilot.NativeLease, next domaincopilot.NativeAdvance) (domaincopilot.NativeLease, error) {
	checkpoint := *d.cp
	checkpoint.nativeCheckpoint = nativeCheckpoint{}
	if err := json.Unmarshal(next.Checkpoint, &checkpoint.nativeCheckpoint); err != nil {
		return lease, err
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return lease, err
	}
	next.Checkpoint = raw
	next.Release = d.release && next.Release && len(next.Events) > 0
	saved, err := d.NativeRunStore.AdvanceNativeRun(ctx, lease, next)
	if err == nil {
		*d.cp = checkpoint
	}
	return saved, err
}
func (s *Service) InvokeDSHTool(ctx context.Context, grant string, in domaincopilot.DSHToolCall) (domaincopilot.DSHToolResult, error) {
	s.dshLock(in.RunID).Lock()
	defer s.dshLock(in.RunID).Unlock()
	id := domaincopilot.DSHIdentity{RunID: in.RunID, Attempt: in.Attempt, Fence: in.Fence}
	lease, cp, principal, ctx, err := s.authorizeDSHCallback(ctx, grant, id)
	if err != nil {
		return domaincopilot.DSHToolResult{}, err
	}
	if !dshAssemblyConfirmed(cp) {
		return domaincopilot.DSHToolResult{}, apperrors.ErrAccessDenied
	}
	if slices.ContainsFunc(dshNativeTools(cp), func(tool domaincopilot.DSHNativeTool) bool { return tool.Name == in.ToolName }) {
		return domaincopilot.DSHToolResult{}, apperrors.ErrAccessDenied
	}
	hash := sha256.Sum256(in.Input)
	var semanticInput any
	if err := json.Unmarshal(in.Input, &semanticInput); err != nil {
		return domaincopilot.DSHToolResult{}, apperrors.ErrInvalidArgument
	}
	if in.SchemaVersion != "opensoha.dev/dsh-runtime/v1" || in.Kind != "tool_call" || in.ToolCatalogRevision != cp.Start.ToolCatalogRevision || hex.EncodeToString(hash[:]) != in.InputHash || cp.ToolHashes[in.CallID] != dshHash(semanticInput) {
		return domaincopilot.DSHToolResult{}, apperrors.ErrAccessDenied
	}
	if cp.ToolNames[in.CallID] != in.ToolName &&
		!(cp.ToolNames[in.CallID] == "" && len(cp.Pending) != 0 && cp.Pending[0].Call.ID == in.CallID && cp.Pending[0].Call.Name == in.ToolName) {
		return domaincopilot.DSHToolResult{}, apperrors.ErrAccessDenied
	}
	return s.settleDSHTool(ctx, lease, cp, principal, in)
}

func (s *Service) settleDSHTool(ctx context.Context, lease domaincopilot.NativeLease, cp dshCheckpoint, principal domainidentity.Principal, in domaincopilot.DSHToolCall) (domaincopilot.DSHToolResult, error) {
	if result, ok := cp.ToolResults[in.CallID]; ok {
		if result.InputHash != in.InputHash {
			return domaincopilot.DSHToolResult{}, apperrors.ErrConflict
		}
		return result, nil
	}
	output, settled := dshToolObservation(cp, in.CallID)
	if !settled {
		if len(cp.Pending) == 0 || cp.Pending[0].Call.ID != in.CallID || nativeFunctionName(cp.Pending[0].ToolName) != in.ToolName {
			return domaincopilot.DSHToolResult{}, apperrors.ErrConflict
		}
		// The frozen model arguments, not a fresh callback body, reach the domain tool.
		toolCtx, cancel := context.WithDeadline(ctx, cp.Budget.Deadline)
		defer cancel()
		done := s.nativeHeartbeat(toolCtx, cancel, s.nativeRuns, lease)
		defer func() { cancel(); <-done }()
		var err error
		lease, cp, err = s.waitDSHTool(toolCtx, lease, cp, principal, in.CallID)
		if err != nil {
			return domaincopilot.DSHToolResult{}, err
		}
		if result, ok := cp.ToolResults[in.CallID]; ok {
			if result.InputHash != in.InputHash {
				return domaincopilot.DSHToolResult{}, apperrors.ErrConflict
			}
			return result, nil
		}
		output, settled = dshToolObservation(cp, in.CallID)
		if !settled {
			return domaincopilot.DSHToolResult{}, apperrors.ErrConflict
		}
	}
	result := domaincopilot.DSHToolResult{SchemaVersion: in.SchemaVersion, Kind: "tool_result", RunID: in.RunID, Attempt: in.Attempt, Fence: in.Fence, CallID: in.CallID, InputHash: in.InputHash, Status: "completed", Output: output}
	cp.ToolResults[in.CallID] = result
	_, err := s.saveDSH(ctx, lease, cp, "running", nil)
	return result, err
}

// Clarification settles each sibling as skipped and stores the answer against
// its own call ID. The same lookup also recovers a lost tool-result response.
func dshToolObservation(cp dshCheckpoint, id string) (json.RawMessage, bool) {
	for _, message := range cp.Messages {
		if message.Role != "tool" || message.ToolCallID != id {
			continue
		}
		output := json.RawMessage(message.Content)
		if cp.ToolNames[id] == "harness_request_input" || !json.Valid(output) {
			output, _ = json.Marshal(message.Content)
		}
		return output, true
	}
	return nil, false
}

func (s *Service) waitDSHTool(ctx context.Context, lease domaincopilot.NativeLease, cp dshCheckpoint, principal domainidentity.Principal, callID string) (domaincopilot.NativeLease, dshCheckpoint, error) {
	claim := lease
	store := dshGovernedStore{NativeRunStore: s.nativeRuns, cp: &cp}
	for {
		if _, settled := dshToolObservation(cp, callID); settled {
			return lease, cp, nil
		}
		if len(cp.Pending) == 0 || cp.Pending[0].Call.ID != callID {
			return lease, cp, apperrors.ErrConflict
		}
		// An already-published question stays in place until Resume writes input.
		if lease.Phase != "paused" || cp.StopReason != "waiting_input" || cp.UserInput != "" {
			var err error
			lease, err = s.nativeToolStep(ctx, store, lease, &cp.nativeCheckpoint, principal)
			if err != nil || !nativeWaitingPhase(lease.Phase) {
				return lease, cp, err
			}
			if lease.Phase == "paused" && cp.StopReason != "waiting_input" {
				return lease, cp, apperrors.ErrConflict
			}
		}
		var err error
		lease, cp, principal, ctx, err = s.refreshWaitingDSHTool(ctx, claim)
		if err != nil {
			return lease, cp, err
		}
	}
}

func (s *Service) refreshWaitingDSHTool(ctx context.Context, claim domaincopilot.NativeLease) (domaincopilot.NativeLease, dshCheckpoint, domainidentity.Principal, context.Context, error) {
	lock := s.dshLock(claim.Run.ID)
	lock.Unlock()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	lock.Lock()
	if err := ctx.Err(); err != nil {
		return claim, dshCheckpoint{}, domainidentity.Principal{}, ctx, err
	}
	lease, err := s.readDSHWorkerLease(ctx, claim)
	if err != nil {
		return lease, dshCheckpoint{}, domainidentity.Principal{}, ctx, err
	}
	cp, err := readDSHCheckpoint(lease)
	if err != nil {
		return lease, cp, domainidentity.Principal{}, ctx, err
	}
	if lease.CancelRequested || cp.StopReason == "unknown_outcome" || lease.Phase == "cancelling" {
		return lease, cp, domainidentity.Principal{}, ctx, apperrors.ErrAccessDenied
	}
	principal, err := s.nativePrincipal(ctx, lease.Run)
	if err == nil {
		err = s.authorizeDSHProfileBinding(ctx, principal, lease.Run, cp.Start)
	}
	if err == nil {
		ctx, err = s.nativeAuthorizationContext(ctx, principal, lease.Run, true)
	}
	return lease, cp, principal, ctx, err
}

func (s *Service) dshLock(id string) *sync.Mutex {
	v, _ := s.dshLocks.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (s *Service) persistDSHEvents(ctx context.Context, lease domaincopilot.NativeLease, cp dshCheckpoint) error {
	if cp.Start == nil || cp.Start.Fence != lease.Fence {
		return apperrors.ErrConflict
	}
	_, err := s.persistDSHEventPage(ctx, lease, cp, false)
	return err
}

func (s *Service) persistDSHFinalEvents(ctx context.Context, lease domaincopilot.NativeLease, cp dshCheckpoint, sequence int64, observationsOnly bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for pages := 0; cp.RuntimeSequence < sequence && pages < 128; pages++ {
		next, err := s.persistDSHEventPage(ctx, lease, cp, observationsOnly || cp.Start.Fence != lease.Fence)
		if err != nil {
			return err
		}
		if next <= cp.RuntimeSequence {
			return apperrors.ErrConflict
		}
		cp.RuntimeSequence = next
	}
	if cp.RuntimeSequence < sequence {
		return apperrors.ErrConflict
	}
	return nil
}

func (s *Service) persistDSHEventPage(ctx context.Context, lease domaincopilot.NativeLease, cp dshCheckpoint, observationsOnly bool) (int64, error) {
	if cp.Start == nil {
		return cp.RuntimeSequence, apperrors.ErrConflict
	}
	id := cp.Start.DSHIdentity
	if _, err := s.readDSHWorkerLease(ctx, lease); err != nil {
		return cp.RuntimeSequence, err
	}
	page, err := s.dshRuntime.Events(ctx, id, cp.RuntimeSequence)
	if err != nil {
		return cp.RuntimeSequence, err
	}
	if len(page.Items) == 0 {
		return cp.RuntimeSequence, nil
	}
	lock := s.dshLock(lease.Run.ID)
	lock.Lock()
	defer lock.Unlock()
	lease, err = s.readDSHWorkerLease(ctx, lease)
	if err != nil {
		return cp.RuntimeSequence, err
	}
	cp, err = readDSHCheckpoint(lease)
	if err != nil {
		return cp.RuntimeSequence, err
	}
	if cp.StopReason == "unknown_outcome" && !observationsOnly && !lease.CancelRequested {
		return cp.RuntimeSequence, nil
	}
	if cp.Start == nil || cp.Start.DSHIdentity != id {
		return cp.RuntimeSequence, apperrors.ErrConflict
	}
	observationsOnly = observationsOnly || lease.CancelRequested || cp.Start.Fence != lease.Fence
	events, err := projectDSHEventPage(&lease.Run, &cp, page.Items, observationsOnly)
	if err != nil {
		return cp.RuntimeSequence, err
	}
	phase := lease.Phase
	if lease.CancelRequested {
		phase = "cancelling"
	}
	_, err = s.saveDSH(ctx, lease, cp, phase, events)
	return cp.RuntimeSequence, err
}

func projectDSHEventPage(run *domaincopilot.AgentRun, cp *dshCheckpoint, page []domaincopilot.DSHEvent, observationsOnly bool) ([]domaincopilot.WorkbenchStreamEvent, error) {
	events := []domaincopilot.WorkbenchStreamEvent{}
	for _, event := range page {
		if event.Sequence <= cp.RuntimeSequence {
			continue
		}
		if event.DSHIdentity != cp.Start.DSHIdentity {
			return nil, apperrors.ErrConflict
		}
		cp.RuntimeSequence = event.Sequence
		if event.Type == "message_delta" && !observationsOnly && !nativeHasProtectedCapability(*run) {
			events = append(events, domaincopilot.WorkbenchStreamEvent{Type: "message.delta", Role: "assistant", MessageID: run.ID + ":reply", ContentDelta: event.Delta})
		}
		if event.Type == "hook_observed" {
			if record := dshHookObservation(*cp, event); record != nil {
				if len(cp.HookObservations) >= maxDSHHookObservations || slices.ContainsFunc(cp.HookObservations, func(previous map[string]any) bool {
					return previous["callId"] == event.CallID
				}) {
					continue
				}
				cp.HookObservations = append(cp.HookObservations, record)
				run.Output["runtimeHooks"] = cp.HookObservations
				events = append(events, domaincopilot.WorkbenchStreamEvent{Type: "agent.status", RunID: run.ID, Status: run.Status, Metadata: map[string]any{"runtimeHook": record}})
			}
		}
	}
	return events, nil
}

func dshFrozenUser(start domaincopilot.DSHStart) string {
	if len(start.Context) == 0 {
		return start.Prompt
	}
	raw, _ := json.Marshal(start.Context)
	return "Conversation context (data):\n" + string(raw) + "\nCurrent user message:\n" + start.Prompt
}

func dshUserMatches(content string, start domaincopilot.DSHStart) bool {
	if len(start.Context) == 0 {
		return content == start.Prompt
	}
	const prefix = "Conversation context (data):\n"
	if !strings.HasPrefix(content, prefix) {
		return false
	}
	rest := strings.TrimPrefix(content, prefix)
	decoder := json.NewDecoder(strings.NewReader(rest))
	var messages []domaincopilot.DSHContextMessage
	if decoder.Decode(&messages) != nil || dshHash(messages) != dshHash(start.Context) {
		return false
	}
	return rest[decoder.InputOffset():] == "\nCurrent user message:\n"+start.Prompt
}

func dshSourceRevision(input nativeModelInput) string {
	return dshHash(struct {
		Messages      []appaigateway.WorkbenchRelayMessage
		MemorySharing any
	}{input.Messages, input.Snapshot["_sohaMemoryShareReceipts"]})
}

func dshModelInFlight(cp dshCheckpoint) bool {
	for id := range cp.ModelHashes {
		if _, completed := cp.ModelResponses[id]; !completed {
			return true
		}
	}
	return false
}
