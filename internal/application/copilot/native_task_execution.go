package copilot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Workflow remains the durable task owner; Native only submits and observes it.
type NativeCapabilityTasks interface {
	CreateNativeCapabilityTask(context.Context, domaincopilot.NativeLease, domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityTask, error)
	FindNativeCapabilityTask(context.Context, domainidentity.Principal, domaincopilot.NativeTaskParent, string, string) (domainaigateway.CapabilityTask, error)
	GetCapabilityTask(context.Context, domainidentity.Principal, string) (domainaigateway.CapabilityTask, error)
	CancelCapabilityTask(context.Context, domainidentity.Principal, string) (domainaigateway.CapabilityTask, error)
}

type nativeTaskPlanValidator interface {
	ValidateCapabilityPlan(context.Context, domainidentity.Principal, domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityPlanValidation, error)
}

type nativeTaskCheckpoint struct {
	Parent          domaincopilot.NativeTaskParent      `json:"parent"`
	Input           domainaigateway.CapabilityTaskInput `json:"input"`
	Digest          string                              `json:"digest"`
	ID              string                              `json:"id,omitempty"`
	State           string                              `json:"state"`
	Status          string                              `json:"status,omitempty"`
	VerifiedThrough time.Time                           `json:"verifiedThrough,omitempty"`
}

func decodeNativeTaskArguments(raw json.RawMessage, output any) error {
	if len(raw) == 0 || len(raw) > 32768 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return apperrors.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return apperrors.ErrInvalidArgument
	}
	if decoder.Decode(new(any)) != io.EOF {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func (s *Service) prepareNativeTask(ctx context.Context, principal domainidentity.Principal, lease domaincopilot.NativeLease, cp *nativeCheckpoint, raw json.RawMessage) (nativeTaskCheckpoint, error) {
	var input struct {
		Plan domainaigateway.CapabilityPlan `json:"plan"`
	}
	if err := decodeNativeTaskArguments(raw, &input); err != nil || len(input.Plan.Steps) == 0 || len(input.Plan.Steps) > 8 || lease.RequestHash == "" {
		return nativeTaskCheckpoint{}, apperrors.ErrInvalidArgument
	}
	ctx, err := s.nativeAuthorizationContext(ctx, principal, lease.Run, true)
	if err != nil {
		return nativeTaskCheckpoint{}, err
	}
	tools, err := s.currentNativeTaskCatalog(ctx, principal, lease.Run)
	if err != nil {
		return nativeTaskCheckpoint{}, err
	}
	prepared, err := nativeTaskInput(lease.Run, input.Plan, tools)
	if err != nil {
		return nativeTaskCheckpoint{}, err
	}
	validator, ok := s.workbenchInvoker.(nativeTaskPlanValidator)
	if !ok {
		return nativeTaskCheckpoint{}, apperrors.ErrUnsupportedOperation
	}
	validation, err := validator.ValidateCapabilityPlan(ctx, principal, prepared)
	if err != nil {
		return nativeTaskCheckpoint{}, err
	}
	if !validation.Valid || validation.Digest != nativeTaskDigest(prepared) {
		return nativeTaskCheckpoint{}, fmt.Errorf("%w: governed task plan is invalid", apperrors.ErrInvalidArgument)
	}
	for _, existing := range cp.Tasks {
		if existing.Input.IdempotencyKey == prepared.IdempotencyKey && existing.Digest == validation.Digest {
			return existing, nil
		}
	}
	count := len(prepared.Plan.Steps)
	if cp.Usage.ToolCalls+count > cp.Budget.MaxToolCalls || cp.Usage.Steps+count >= cp.Budget.MaxSteps || len(cp.Tasks) >= 8 {
		return nativeTaskCheckpoint{}, fmt.Errorf("%w: governed task exceeds the remaining Native budget", apperrors.ErrInvalidArgument)
	}
	cp.Usage.Steps += count
	parent := domaincopilot.NativeTaskParent{RunID: lease.Run.ID, OwnerID: lease.Run.CreatedBy, SessionID: lease.Run.SessionID, RequestHash: lease.RequestHash, Deadline: cp.Budget.Deadline}
	return nativeTaskCheckpoint{Parent: parent, Input: prepared, Digest: validation.Digest, State: "prepared"}, nil
}

func nativeTaskInput(run domaincopilot.AgentRun, plan domainaigateway.CapabilityPlan, tools map[string]domainaigateway.ToolCapability) (domainaigateway.CapabilityTaskInput, error) {
	if plan.TimeoutSeconds < 0 || run.TimeoutSeconds <= 0 {
		return domainaigateway.CapabilityTaskInput{}, apperrors.ErrInvalidArgument
	}
	if plan.TimeoutSeconds == 0 || plan.TimeoutSeconds > run.TimeoutSeconds {
		plan.TimeoutSeconds = run.TimeoutSeconds
	}
	for i := range plan.Steps {
		step := &plan.Steps[i]
		tool, found := tools[step.Call.ToolName]
		if !found || !nativeTaskCapability(tool) || tool.Version != step.Call.CapabilityVersion {
			return domainaigateway.CapabilityTaskInput{}, apperrors.ErrAccessDenied
		}
		if step.Call.Input == nil {
			step.Call.Input = map[string]any{}
		}
		if field := tool.Execution.IdempotencyKeyField; field != "" {
			delete(step.Call.Input, field)
		}
	}
	if err := validateNativePlanChecks(plan, tools); err != nil {
		return domainaigateway.CapabilityTaskInput{}, err
	}
	input := domainaigateway.CapabilityTaskInput{Plan: plan, ScopeConstraints: nativeTaskScopeConstraints(run.Scope)}
	data, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	key := sha256.Sum256(append([]byte(run.ID+":"), data...))
	input.IdempotencyKey = "native:" + hex.EncodeToString(key[:])
	for i := range input.Plan.Steps {
		step := &input.Plan.Steps[i]
		if field := tools[step.Call.ToolName].Execution.IdempotencyKeyField; field != "" {
			key := sha256.Sum256([]byte(input.IdempotencyKey + ":" + step.ID))
			step.Call.Input[field] = "native:" + hex.EncodeToString(key[:])
		}
	}
	return input, nil
}

func nativeTaskScopeConstraints(scope domaincopilot.SessionScope) []domainaigateway.CapabilityScopeConstraint {
	constraints := []domainaigateway.CapabilityScopeConstraint{}
	for _, item := range []domainaigateway.CapabilityScopeConstraint{{Dimension: "clusterId", Value: scope.ClusterID}, {Dimension: "namespace", Value: scope.Namespace}, {Dimension: "workload", Value: scope.Workload}, {Dimension: "service", Value: scope.Service}, {Dimension: "pod", Value: scope.Pod}, {Dimension: "node", Value: scope.Node}, {Dimension: "alertId", Value: scope.AlertID}} {
		if item.Value != "" {
			constraints = append(constraints, item)
		}
	}
	return constraints
}

func nativeTaskDigest(input domainaigateway.CapabilityTaskInput) string {
	data, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (s *Service) nativeTaskToolStep(ctx context.Context, store NativeRunStore, lease domaincopilot.NativeLease, cp *nativeCheckpoint, principal domainidentity.Principal) (domaincopilot.NativeLease, error) {
	if s.nativeTasks == nil {
		return lease, apperrors.ErrUnsupportedOperation
	}
	toolCtx, cancel := context.WithDeadline(ctx, minNativeTaskDeadline(cp.Budget.Deadline))
	defer cancel()
	done := s.nativeHeartbeat(toolCtx, cancel, store, lease)
	defer func() { cancel(); <-done }()
	pending := &cp.Pending[0]
	if pending.ToolName == "harness.discover_tasks" {
		if err := reserveNativeWorkCall(ctx, store, lease, cp); err != nil {
			return lease, err
		}
		output, err := s.discoverNativeTasks(toolCtx, principal, lease.Run, pending.Call.Arguments)
		return completeNativeTool(ctx, store, lease, cp, pending.Call.ID, nativeTaskToolExecution(lease, *pending, output, err), err)
	}
	if pending.TaskKey == "" {
		if err := reserveNativeWorkCall(ctx, store, lease, cp); err != nil {
			return lease, err
		}
		child, err := s.prepareNativeTask(toolCtx, principal, lease, cp, pending.Call.Arguments)
		if err != nil {
			return lease, err
		}
		pending.TaskKey = child.Input.IdempotencyKey
		if !slices.ContainsFunc(cp.Tasks, func(existing nativeTaskCheckpoint) bool { return existing.Input.IdempotencyKey == pending.TaskKey }) {
			cp.Tasks = append(cp.Tasks, child)
		}
		lease, err = saveNativeCheckpoint(ctx, store, lease, *cp, "tools", nil)
		if err != nil {
			return lease, err
		}
	}
	index := slices.IndexFunc(cp.Tasks, func(child nativeTaskCheckpoint) bool { return child.Input.IdempotencyKey == pending.TaskKey })
	if index < 0 {
		return lease, apperrors.ErrInvalidArgument
	}
	return s.advanceNativeTask(toolCtx, store, lease, cp, principal, index)
}

func minNativeTaskDeadline(deadline time.Time) time.Time {
	if limit := time.Now().Add(20 * time.Second); limit.Before(deadline) {
		return limit
	}
	return deadline
}

func (s *Service) advanceNativeTask(ctx context.Context, store NativeRunStore, lease domaincopilot.NativeLease, cp *nativeCheckpoint, principal domainidentity.Principal, index int) (domaincopilot.NativeLease, error) {
	child := &cp.Tasks[index]
	var task domainaigateway.CapabilityTask
	var err error
	if child.State == "prepared" {
		child.State = "dispatching"
		lease, err = saveNativeCheckpoint(ctx, store, lease, *cp, "tools", []domaincopilot.WorkbenchStreamEvent{{Type: "tool.started", ToolCall: nativeWorkbenchCall(nativeTaskToolExecution(lease, cp.Pending[0], nil, nil))}})
		if err == nil {
			task, err = s.nativeTasks.CreateNativeCapabilityTask(ctx, lease, child.Input)
		}
	} else {
		task, err = s.nativeTasks.FindNativeCapabilityTask(ctx, principal, child.Parent, child.Input.IdempotencyKey, child.Digest)
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		cp.StopReason = "unknown_outcome"
		return saveNativeCheckpoint(ctx, store, lease, *cp, "paused", []domaincopilot.WorkbenchStreamEvent{{Type: "error", Code: "unknown_outcome", Message: "任务提交结果未知，须核对领域回执。"}})
	}
	if err != nil {
		return lease, err
	}
	if task.ID == "" || task.CreatedBy != child.Parent.OwnerID || (child.ID != "" && child.ID != task.ID) || task.PlanVersion != 1 {
		return lease, apperrors.ErrAccessDenied
	}
	child.ID, child.Status, child.State = task.ID, task.Status, "observing"
	lease.Run.Output["capabilityTaskIds"] = nativeTaskIDs(cp.Tasks)
	lease.Run.Output["capabilityTaskId"] = task.ID
	lease.Run.Output["capabilityTaskStatus"] = task.Status
	lease.Run.Output["approvalRequestIds"] = nativeTaskApprovalIDs(task)
	if task.Status == "completed" {
		child.VerifiedThrough, err = nativeTaskVerifiedThrough(task, time.Now())
		if err != nil {
			return lease, err
		}
		child.State = "completed"
		output := map[string]any{"capabilityTaskId": task.ID, "status": task.Status, "assessment": task.Assessment, "goalVerified": true}
		return completeNativeTool(ctx, store, lease, cp, cp.Pending[0].Call.ID, nativeTaskToolExecution(lease, cp.Pending[0], output, nil), nil)
	}
	if task.Status == "failed" || task.Status == "inconclusive" || task.Status == "canceled" || task.Status == "blocked" {
		return lease, fmt.Errorf("%w: governed task is %s; goal is unverified", apperrors.ErrInvalidArgument, task.Status)
	}
	phase := "waiting_task"
	if task.Status == "waiting_approval" {
		phase = "waiting_approval"
	}
	return saveNativeCheckpoint(ctx, store, lease, *cp, phase, []domaincopilot.WorkbenchStreamEvent{{Type: "agent.status", Status: "running", Metadata: map[string]any{"capabilityTaskId": task.ID, "capabilityTaskStatus": task.Status}}})
}

func nativeTaskApprovalIDs(task domainaigateway.CapabilityTask) []string {
	ids := []string{}
	for _, node := range task.Nodes {
		if node.ApprovalRequestID != "" && !slices.Contains(ids, node.ApprovalRequestID) {
			ids = append(ids, node.ApprovalRequestID)
		}
	}
	return ids
}

func nativeTaskIDs(tasks []nativeTaskCheckpoint) []string {
	ids := []string{}
	for _, task := range tasks {
		if task.ID != "" && !slices.Contains(ids, task.ID) {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func nativeTaskVerifiedThrough(task domainaigateway.CapabilityTask, now time.Time) (time.Time, error) {
	if task.Assessment == nil || task.Assessment.Verdict != "satisfied" || len(task.Assessment.Evidence) == 0 || !nativeTaskEffectsTerminal(task) || task.CreatedAt.IsZero() {
		return time.Time{}, fmt.Errorf("%w: task goal lacks confirmed domain verification", apperrors.ErrInvalidArgument)
	}
	through := now
	for _, evidence := range task.Assessment.Evidence {
		if evidence.Incomplete || evidence.Kind == "" || evidence.Source == "" || evidence.ObservedAt.Before(task.CreatedAt) || evidence.ObservedAt.Before(now.Add(-5*time.Minute)) || evidence.ObservedAt.After(now.Add(30*time.Second)) {
			return time.Time{}, fmt.Errorf("%w: task verification is incomplete or stale", apperrors.ErrInvalidArgument)
		}
		if evidence.ObservedAt.Before(through) {
			through = evidence.ObservedAt
		}
	}
	return through, nil
}

func nativeTaskEffectsTerminal(task domainaigateway.CapabilityTask) bool {
	for _, node := range task.Nodes {
		if node.Invocation != nil && node.Invocation.Task != nil {
			child := node.Invocation.Task
			if !child.Terminal || child.ID == "" || !slices.Contains([]string{"succeeded", "failed", "canceled"}, child.Outcome) || (task.Status == "completed" && child.Outcome != "succeeded") {
				return false
			}
		}
	}
	return task.Status == "completed" || task.Status == "canceled" || task.Status == "failed" || task.Status == "inconclusive"
}

func nativeTaskToolExecution(lease domaincopilot.NativeLease, pending nativeCall, output map[string]any, err error) domaincopilot.ToolExecution {
	status, summary := "success", ""
	if output == nil {
		status = "running"
	}
	if err != nil {
		status, summary = "failed", "Governed task discovery failed"
		output = map[string]any{"error": summary}
	}
	now := time.Now().UTC()
	return domaincopilot.ToolExecution{ID: lease.Run.ID + ":" + pending.Call.ID, ToolName: pending.ToolName, AdapterID: "harness", Status: status, StartedAt: now, CompletedAt: &now, Summary: summary, Output: output}
}

// Stop dispatch first, then reconcile through the task owner outside the parent lock.
func (s *Service) finishNativeRun(ctx context.Context, store NativeRunStore, lease domaincopilot.NativeLease, cp nativeCheckpoint, status, reason, content string) error {
	if status == "completed" {
		for _, child := range cp.Tasks {
			if child.State != "completed" || child.VerifiedThrough.Before(time.Now().Add(-5*time.Minute)) {
				status, reason, content = "failed", "verification_incomplete", "领域任务尚未通过当前验收，任务未完成。"
				break
			}
		}
	}
	if status != "completed" && len(cp.Tasks) > 0 {
		cp.StopReason, cp.StopStatus, cp.StopContent = reason, status, content
		var err error
		lease, err = saveNativeCheckpoint(ctx, store, lease, cp, "cancelling", nil)
		if err != nil {
			return err
		}
		confirmed := s.reconcileNativeTaskStop(ctx, lease, &cp)
		if !confirmed {
			_, err = saveNativeCheckpoint(ctx, store, lease, cp, "cancelling", []domaincopilot.WorkbenchStreamEvent{{Type: "agent.status", Status: "running", Metadata: map[string]any{"cancellationConfirmed": false, "stopReason": reason}}})
			return err
		}
	}
	// Without a dispatched tool or child task this run cannot own a personal lease.
	if s.personalEnvironment != nil && (cp.Usage.ToolCalls > 0 || len(cp.Tasks) > 0) {
		cp.StopReason, cp.StopStatus, cp.StopContent = reason, status, content
		var err error
		lease, err = saveNativeCheckpoint(ctx, store, lease, cp, "cancelling", nil)
		if err != nil {
			return err
		}
		if !s.cleanupNativePersonalTask(ctx, lease, cp) {
			_, err = saveNativeCheckpoint(ctx, store, lease, cp, "cancelling", []domaincopilot.WorkbenchStreamEvent{{Type: "agent.status", Status: "running", Metadata: map[string]any{"personalCleanupConfirmed": false, "stopReason": reason}}})
			return err
		}
	}
	return s.commitNativeResult(ctx, store, lease, cp, status, reason, content)
}

func (s *Service) reconcileNativeTaskStop(ctx context.Context, lease domaincopilot.NativeLease, cp *nativeCheckpoint) bool {
	if s.nativeTasks == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	principal, err := s.nativePrincipal(ctx, lease.Run)
	if err != nil {
		return false
	}
	confirmed := true
	for i := range cp.Tasks {
		child := &cp.Tasks[i]
		if child.State == "prepared" || child.State == "stopped" {
			child.State = "stopped"
			continue
		}
		task, err := s.nativeTasks.FindNativeCapabilityTask(ctx, principal, child.Parent, child.Input.IdempotencyKey, child.Digest)
		if errors.Is(err, apperrors.ErrNotFound) {
			// Lookup serializes with the stopped parent: no old worker can register now.
			child.State = "stopped"
			continue
		}
		if err == nil && !nativeTaskEffectsTerminal(task) {
			task, err = s.nativeTasks.CancelCapabilityTask(ctx, principal, task.ID)
		}
		if err != nil || !nativeTaskEffectsTerminal(task) {
			confirmed = false
			continue
		}
		child.ID, child.Status, child.State = task.ID, task.Status, "stopped"
	}
	return confirmed
}
