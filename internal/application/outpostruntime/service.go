package outpostruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/application/access"
	"github.com/opensoha/soha/internal/domain/audit"
	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Mutation struct {
	RequestID, Fingerprint string
	ExpectedGeneration     int64
	State                  domain.State
	Task                   *delivery.ExecutionTask
}

type Repository interface {
	Get(context.Context, string) (domain.State, error)
	Commit(context.Context, Mutation) (domain.State, error)
	RecordResult(context.Context, delivery.ExecutionTask, domain.Command) error
	TaskByToken(context.Context, string) (delivery.ExecutionTask, error)
	RecordInstance(context.Context, domain.InstanceObservation) error
	DrainStatus(context.Context, string, int64, time.Time) (domain.DrainStatus, error)
	GrantControl(context.Context, string, string, string, domain.InstanceCredential) (domain.InstanceControl, error)
}

type AuditRecorder interface {
	Record(context.Context, audit.Entry) error
}

type Service struct {
	repo        Repository
	permissions *access.PermissionResolver
	slots       map[string]domain.Slot
	audit       AuditRecorder
	executors   map[string]bool
}

func New(repo Repository, permissions *access.PermissionResolver, slots []domain.Slot, recorder AuditRecorder) (*Service, error) {
	if repo == nil {
		return nil, fmt.Errorf("outpost runtime repository is required")
	}
	s := &Service{repo: repo, permissions: permissions, slots: map[string]domain.Slot{}, executors: map[string]bool{}, audit: recorder}
	for _, slot := range slots {
		if slot.ID == "" || slot.OutpostID == "" || slot.ExecutorID == "" || slot.UID == "" || slot.Name == "" || slot.Namespace == "" || slot.TemplateDigest == "" || slot.MaxReplicas < 1 {
			return nil, fmt.Errorf("outpost runtime slot policy is incomplete")
		}
		if _, exists := s.slots[slot.ID]; exists {
			return nil, fmt.Errorf("duplicate outpost runtime slot")
		}
		s.slots[slot.ID] = slot
	}
	return s, nil
}

func (s *Service) authorize(ctx context.Context, p identity.Principal, write bool) error {
	permission := access.PermIdentityOutpostsView
	if write {
		permission = access.ManagedActionPermission(access.PermIdentityOutpostsManage, "update")
	}
	return access.AuthorizeRuntimePermission(ctx, s.permissions, p, permission)
}

func (s *Service) Get(ctx context.Context, p identity.Principal, id string) (domain.State, error) {
	if err := s.authorize(ctx, p, false); err != nil {
		return domain.State{}, err
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) Bind(ctx context.Context, p identity.Principal, id string, input domain.BindingInput) (domain.State, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return domain.State{}, err
	}
	slot, ok := s.slots[input.SlotID]
	if !ok || slot.OutpostID != id {
		return domain.State{}, fmt.Errorf("%w: slot is not approved for this Outpost", apperrors.ErrAccessDenied)
	}
	state, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.State{}, err
	}
	if state.SlotID != "" && (state.SlotID != slot.ID || state.UID != slot.UID) {
		return domain.State{}, fmt.Errorf("%w: slot identity is immutable", apperrors.ErrConflict)
	}
	if state.Owner != "soha" && input.ExplicitAdoption {
		if !slot.AllowAdoption || !slot.PreviousOwnerStopped {
			return domain.State{}, fmt.Errorf("%w: installer approval and previous owner stop evidence are required", apperrors.ErrAccessDenied)
		}
		state.Owner, state.OwnerEpoch = "soha", state.OwnerEpoch+1
	}
	state.SlotID, state.UID = slot.ID, slot.UID
	state.Generation = input.ExpectedGeneration + 1
	state.Phase = "unobserved"
	return s.commit(ctx, p, id, input.RequestID, input.ExpectedGeneration, input, state, nil)
}

func (s *Service) Control(ctx context.Context, p identity.Principal, id string, input domain.ControlInput) (domain.State, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return domain.State{}, err
	}
	state, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.State{}, err
	}
	slot, ok := s.slots[state.SlotID]
	if !ok || slot.OutpostID != id || slot.UID != state.UID || state.Owner != "soha" {
		return domain.State{}, fmt.Errorf("%w: Outpost lifecycle is externally controlled", apperrors.ErrAccessDenied)
	}
	if !s.executors[slot.ExecutorID] {
		return domain.State{}, fmt.Errorf("%w: Outpost executor is not installed", apperrors.ErrUnsupportedOperation)
	}
	replicas, drain, err := controlTarget(input, slot.MaxReplicas)
	if err != nil {
		return domain.State{}, err
	}
	if input.Action == "reconcile" {
		if state.LatestTaskID == "" {
			return domain.State{}, fmt.Errorf("%w: reconciliation requires a previous runtime operation", apperrors.ErrConflict)
		}
		replicas = state.DesiredReplicas
		state.DrainHeld = state.RequiresDrain()
	} else {
		state.DrainHeld = false
	}
	now := time.Now().UTC()
	command := domain.Command{OutpostID: id, Slot: slot, Generation: input.ExpectedGeneration + 1, OwnerEpoch: state.OwnerEpoch, Action: input.Action, Replicas: replicas, DrainTimeoutSeconds: drain, Deadline: now.Add(90 * time.Second)}
	state.Generation, state.DesiredReplicas, state.Phase, state.Reason = command.Generation, replicas, "queued", ""
	task := delivery.ExecutionTask{ID: uuid.NewString(), TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), TargetKind: "outpost_slot", Status: "queued", QueueKey: slot.ID, LockKey: "outpost-slot/" + slot.ID, TimeoutSeconds: 120, CallbackToken: uuid.NewString(), Payload: map[string]any{"outpostRuntime": command}, CreatedAt: now, UpdatedAt: now}
	state.LatestTaskID = task.ID
	return s.commit(ctx, p, id, input.RequestID, input.ExpectedGeneration, input, state, &task)
}

// ControlForInstance is called only after the identity owner authenticates the
// instance. It does not issue or renew an identity/configuration lease.
func (s *Service) ControlForInstance(ctx context.Context, outpostID, instanceID, processEpoch string, credential domain.InstanceCredential) (domain.InstanceControl, error) {
	if instanceID == "" || len(instanceID) > 128 || processEpoch == "" || len(processEpoch) > 128 || credential.TokenHash == "" || credential.ConfigurationVersion < 1 || !credential.ExpiresAt.After(time.Now().UTC()) {
		return domain.InstanceControl{}, apperrors.ErrInvalidArgument
	}
	return s.repo.GrantControl(ctx, outpostID, instanceID, processEpoch, credential)
}

// AuthorizeAccess is checked by Core for every authentication subrequest. A
// stopped slot remains closed even if a terminating process still answers HTTP.
func (s *Service) AuthorizeAccess(ctx context.Context, outpostID string) error {
	state, err := s.repo.Get(ctx, outpostID)
	if err != nil {
		return err
	}
	if state.RequiresDrain() {
		return fmt.Errorf("%w: Outpost runtime is draining or stopped", apperrors.ErrAccessDenied)
	}
	return nil
}

// RecordInstanceObservation accepts only trusted, server-timestamped heartbeat
// observations after the identity owner has verified the current instance lease.
func (s *Service) RecordInstanceObservation(ctx context.Context, observation domain.InstanceObservation) error {
	now := time.Now().UTC()
	if observation.InstanceID == "" || len(observation.InstanceID) > 128 || observation.ProcessEpoch == "" || len(observation.ProcessEpoch) > 128 || observation.CredentialHash == "" || observation.ConfigurationVersion < 1 || observation.Generation < 0 || observation.ActiveChecks < 0 || observation.ObservedAt.IsZero() || observation.ObservedAt.After(now.Add(time.Second)) || observation.ObservedAt.Before(now.Add(-10*time.Second)) || !observation.LeaseExpiresAt.After(observation.ObservedAt) {
		return apperrors.ErrInvalidArgument
	}
	observation.ObservedAt = now
	return s.repo.RecordInstance(ctx, observation)
}

func controlTarget(input domain.ControlInput, max int32) (int32, int, error) {
	if input.Action == "reconcile" {
		if input.Replicas != nil || input.DrainTimeoutSeconds != 0 {
			return 0, 0, fmt.Errorf("%w: reconcile accepts neither replicas nor drain timeout", apperrors.ErrInvalidArgument)
		}
		return 0, 0, nil
	}
	drain := input.DrainTimeoutSeconds
	if drain == 0 {
		drain = 30
	}
	if drain < 1 || drain > 30 {
		return 0, 0, fmt.Errorf("%w: drain timeout must be 1 to 30 seconds", apperrors.ErrInvalidArgument)
	}
	var target int32
	switch input.Action {
	case "start":
		target = 1
		if input.Replicas != nil {
			target = *input.Replicas
		}
	case "scale":
		if input.Replicas == nil {
			return 0, 0, fmt.Errorf("%w: replicas are required", apperrors.ErrInvalidArgument)
		}
		target = *input.Replicas
	case "stop":
		if input.Replicas != nil && *input.Replicas != 0 {
			return 0, 0, fmt.Errorf("%w: stop cannot specify nonzero replicas", apperrors.ErrInvalidArgument)
		}
	default:
		return 0, 0, fmt.Errorf("%w: unsupported runtime action", apperrors.ErrInvalidArgument)
	}
	if target < 0 || target > max || (input.Action == "start" && target == 0) {
		return 0, 0, fmt.Errorf("%w: replicas exceed slot policy", apperrors.ErrInvalidArgument)
	}
	return target, drain, nil
}

func (s *Service) commit(ctx context.Context, p identity.Principal, id, requestID string, expected int64, input any, state domain.State, task *delivery.ExecutionTask) (domain.State, error) {
	if strings.TrimSpace(requestID) == "" || len(requestID) > 128 || expected < 0 {
		return domain.State{}, fmt.Errorf("%w: request ID and generation are required", apperrors.ErrInvalidArgument)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return domain.State{}, err
	}
	digest := sha256.Sum256(encoded)
	result, err := s.repo.Commit(ctx, Mutation{RequestID: requestID, Fingerprint: hex.EncodeToString(digest[:]), ExpectedGeneration: expected, State: state, Task: task})
	if err == nil && s.audit != nil {
		err = s.audit.Record(ctx, audit.Entry{ActorID: p.UserID, Action: "identity.outpost.runtime.update", ResourceKind: "IdentityOutpost", ResourceName: id, RequestID: requestID, Result: "success", Metadata: map[string]any{"generation": result.Generation, "slotId": result.SlotID, "taskId": result.LatestTaskID}, CreatedAt: time.Now().UTC()})
	}
	return result, err
}

func DecodeCommand(task delivery.ExecutionTask) (domain.Command, error) {
	var command domain.Command
	encoded, err := json.Marshal(task.Payload["outpostRuntime"])
	if err == nil {
		err = json.Unmarshal(encoded, &command)
	}
	if err == nil && (task.TaskKind != domain.TaskKind || command.OutpostID == "" || command.Generation < 1 || command.Slot.UID == "" || task.ProviderKind != command.Slot.ProviderKind()) {
		err = fmt.Errorf("invalid Outpost runtime command")
	}
	return command, err
}

func (s *Service) RecordExecutionTaskResult(ctx context.Context, task delivery.ExecutionTask) error {
	if task.TaskKind != domain.TaskKind {
		return nil
	}
	command, err := DecodeCommand(task)
	if err != nil {
		return err
	}
	return s.repo.RecordResult(ctx, task, command)
}

func DecodeReceipt(result map[string]any) (*domain.Receipt, error) {
	raw, ok := result["outpostRuntime"]
	if !ok {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var receipt *domain.Receipt
	err = json.Unmarshal(encoded, &receipt)
	return receipt, err
}
