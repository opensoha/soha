package proxydeployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/networkidentity"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/application/access"
	"github.com/opensoha/soha/internal/domain/audit"
	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
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
	GetForMember(context.Context, string, string) (domain.State, error)
	Commit(context.Context, Mutation) (domain.State, error)
	RecordResult(context.Context, delivery.ExecutionTask, domain.Command, *domain.Receipt) error
	TaskByToken(context.Context, string) (delivery.ExecutionTask, error)
	Members(context.Context, string) ([]api.NetworkProxyDeploymentMember, error)
	DrainStatus(context.Context, string, int64, time.Time) (domain.DrainStatus, error)
	GrantControl(context.Context, networkidentity.Identity, string, string, string) (domain.InstanceControl, error)
	RecordACK(context.Context, networkidentity.Identity, domain.InstanceACK) error
}

type AuditRecorder interface {
	Record(context.Context, audit.Entry) error
}

type ScopeAuthorizer interface {
	AuthorizeDeployment(context.Context, identity.Principal, string, bool) error
}
type Service struct {
	scope       ScopeAuthorizer
	repo        Repository
	permissions *access.PermissionResolver
	slots       map[string]domain.Slot
	audit       AuditRecorder
	executors   map[string]bool
}

func New(repo Repository, permissions *access.PermissionResolver, slots []domain.Slot, recorder AuditRecorder, scope ScopeAuthorizer) (*Service, error) {
	if missing(repo) || missing(scope) {
		return nil, fmt.Errorf("proxy deployment repository is required")
	}
	s := &Service{scope: scope, repo: repo, permissions: permissions, slots: map[string]domain.Slot{}, executors: map[string]bool{}, audit: recorder}
	for _, slot := range slots {
		if err := domain.ValidatePolicy(slot); err != nil {
			return nil, err
		}
		if _, exists := s.slots[slot.ID]; exists {
			return nil, fmt.Errorf("duplicate proxy slot")
		}
		slot.Members = slices.Clone(slot.Members)
		s.slots[slot.ID] = slot
	}

	return s, nil
}

func (s *Service) authorize(ctx context.Context, p identity.Principal, id string, write bool) error {
	return s.scope.AuthorizeDeployment(ctx, p, id, write)
}
func (s *Service) Get(ctx context.Context, p identity.Principal, id string) (domain.State, error) {
	if err := s.authorize(ctx, p, id, false); err != nil {
		return domain.State{}, err
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) Bind(ctx context.Context, p identity.Principal, id string, input domain.BindingInput) (domain.State, error) {
	if err := s.authorize(ctx, p, id, true); err != nil {
		return domain.State{}, err
	}
	slot, ok := s.slots[input.SlotID]
	if !ok || slot.ProxyID != id {
		return domain.State{}, fmt.Errorf("%w: slot is not approved for this Proxy", apperrors.ErrAccessDenied)
	}
	state, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.State{}, err
	}
	if state.Generation == input.ExpectedGeneration && state.OwnerEpoch != input.ExpectedOwnerEpoch {
		return domain.State{}, apperrors.ErrConflict
	}
	if state.SlotID != "" && (state.SlotID != slot.ID || state.UID != slot.UID) {
		return domain.State{}, fmt.Errorf("%w: slot identity is immutable", apperrors.ErrConflict)
	}
	if err := ValidateBindingPin(state, slot); err != nil {
		return domain.State{}, err
	}
	if state.Owner != "soha-managed" && !input.ExplicitAdoption {
		return domain.State{}, apperrors.ErrAccessDenied
	}
	if state.Owner != "soha-managed" && input.ExplicitAdoption {
		if !slot.AllowAdoption || !slot.PreviousOwnerStopped {
			return domain.State{}, fmt.Errorf("%w: installer approval and previous owner stop evidence are required", apperrors.ErrAccessDenied)
		}
		state.Owner, state.OwnerEpoch = "soha-managed", state.OwnerEpoch+1
	}
	state.SlotID, state.UID = slot.ID, slot.UID
	state.Slot = &slot
	state.MemberSetHash = slot.MemberSetHash()
	state.Members = slot.Members
	state.ControlRevision++
	state.Generation = input.ExpectedGeneration + 1
	state.Phase = "unobserved"
	return s.commit(ctx, p, id, input.RequestID, input.ExpectedGeneration, input, state, nil)
}

func (s *Service) Control(ctx context.Context, p identity.Principal, id string, input domain.ControlInput) (domain.State, error) {
	if err := s.authorize(ctx, p, id, true); err != nil {
		return domain.State{}, err
	}
	state, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.State{}, err
	}
	slot, _, err := s.installedPin(state)
	if err != nil {
		return domain.State{}, err
	}
	if state.Owner != "soha-managed" {
		return domain.State{}, apperrors.ErrAccessDenied
	}
	if !s.executors[slot.ExecutorID] {
		return domain.State{}, fmt.Errorf("%w: Proxy executor is not installed", apperrors.ErrUnsupportedOperation)
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
		state.DrainHeld = input.Action == "stop" || (state.ActualReplicas != nil && replicas < *state.ActualReplicas)
	}
	now := time.Now().UTC()
	command := domain.Command{ProxyID: id, Slot: slot, Generation: input.ExpectedGeneration + 1, OwnerEpoch: state.OwnerEpoch, Action: input.Action, Replicas: replicas, DrainTimeoutSeconds: drain, DrainUntil: now.Add(time.Duration(drain) * time.Second), Deadline: now.Add(240 * time.Second)}
	state.DrainLeasePinned = false
	state.StopConfirmUntil = nil
	if state.DrainHeld {
		until := command.DrainUntil
		state.DrainUntil = &until
	} else {
		state.DrainUntil = nil
	}
	state.ControlRevision++
	state.Generation, state.DesiredReplicas, state.Phase, state.Reason = command.Generation, replicas, "queued", ""
	task := delivery.ExecutionTask{ID: uuid.NewString(), TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), TargetKind: "proxy_slot", Status: "queued", QueueKey: slot.ID, LockKey: "proxy-slot/" + slot.ID, TimeoutSeconds: 240, CallbackToken: uuid.NewString(), Payload: map[string]any{"proxyDeployment": command}, CreatedAt: now, UpdatedAt: now}
	state.LatestTaskID = task.ID
	return s.commit(ctx, p, id, input.RequestID, input.ExpectedGeneration, input, state, &task)
}

func (s *Service) RuntimeControl(ctx context.Context, id networkidentity.Identity, instance, epoch string) (domain.InstanceControl, error) {
	if id.Scope != networkidentity.ScopeNetworkControl || id.Kind != "proxy" || instance == "" || len(instance) > 128 || epoch == "" || len(epoch) > 128 {
		return domain.InstanceControl{}, apperrors.ErrUnauthorized
	}
	pin, err := s.runtimePin(ctx, id.ID, instance)
	if err != nil {
		return domain.InstanceControl{}, err
	}
	return s.repo.GrantControl(ctx, id, instance, epoch, pin)
}
func (s *Service) RuntimeACK(ctx context.Context, id networkidentity.Identity, ack domain.InstanceACK) error {
	if id.Scope != networkidentity.ScopeNetworkControl || id.Kind != "proxy" || ack.RuntimeID != id.ID || ack.InstanceID == "" || ack.ProcessEpoch == "" || ack.ActiveConnections < 0 || (!ack.ActiveCountKnown && ack.ActiveConnections != 0) {
		return apperrors.ErrInvalidArgument
	}
	if ack.ProcessStopped && (ack.AcceptingRequests || !ack.ActiveCountKnown || ack.ActiveConnections != 0) {
		return apperrors.ErrInvalidArgument
	}
	return s.repo.RecordACK(ctx, id, ack)
}
func (s *Service) ApprovedPool(ctx context.Context, id string) (domain.ApprovedPool, error) {
	state, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.ApprovedPool{}, err
	}
	if !hasAcceptedBinding(state) {
		return domain.ApprovedPool{MasterID: id, Owner: state.Owner, Phase: state.Phase, Members: []domain.Member{{InstanceID: id, RuntimeID: id}}, MaxReplicas: 1}, nil
	}
	if _, _, err := s.installedPin(state); err != nil {
		return domain.ApprovedPool{}, err
	}
	slot := state.Slot
	var servingUntil *time.Time
	if state.DrainHeld {
		servingUntil = state.DrainUntil
	}
	return domain.ApprovedPool{MasterID: id, ObservedReplicas: state.ActualReplicas, DrainHeld: state.DrainHeld, Phase: state.Phase, Owner: state.Owner, ServingUntil: servingUntil, OwnerEpoch: state.OwnerEpoch, MemberSetHash: state.MemberSetHash, Members: slices.Clone(state.Members), MaxReplicas: slot.MaxReplicas, DesiredReplicas: state.DesiredReplicas, WorkloadKind: slot.WorkloadKind, UID: slot.UID}, nil
}
func (s *Service) View(ctx context.Context, p identity.Principal, id string) (api.NetworkProxyDeployment, error) {
	state, err := s.Get(ctx, p, id)
	if err != nil {
		return api.NetworkProxyDeployment{}, err
	}
	return s.DescribeState(ctx, state)
}
func (s *Service) DescribeState(ctx context.Context, state domain.State) (api.NetworkProxyDeployment, error) {
	members := []api.NetworkProxyDeploymentMember{}
	pinMismatch := false
	if hasAcceptedBinding(state) {
		_, _, err := s.installedPin(state)
		pinMismatch = err != nil
	}
	if !pinMismatch {
		var err error
		members, err = s.repo.Members(ctx, state.ProxyID)
		if err != nil {
			return api.NetworkProxyDeployment{}, err
		}
	}
	dto := api.NetworkProxyDeployment{Revision: state.Generation, Members: members, SlotID: state.SlotID, ObjectUID: state.UID, Owner: api.NetworkProxyDeploymentOwner(state.Owner), OwnerEpoch: state.OwnerEpoch, DesiredReplicas: int64(state.DesiredReplicas), ObservedGeneration: state.ObservedGeneration, OperationID: state.LatestTaskID, ReasonCode: state.Reason, State: api.NetworkProxyDeploymentState(domain.PublicPhase(state))}
	if state.Slot != nil {
		dto.TemplateHash = state.Slot.TemplateDigest
	}
	if state.ActualReplicas != nil {
		observed := int64(*state.ActualReplicas)
		dto.ObservedReplicas = &observed
	}
	if pinMismatch {
		dto.State = "conflict"
		dto.ReasonCode = "rebind_required"
	}
	return dto, nil
}
func (s *Service) BindRequest(ctx context.Context, p identity.Principal, id string, input domain.BindingRequest) (api.NetworkProxyDeployment, error) {
	if err := s.authorize(ctx, p, id, true); err != nil {
		return api.NetworkProxyDeployment{}, err
	}
	slot, ok := s.slots[input.SlotID]
	if !ok || input.ObjectUID != slot.UID || input.TemplateHash != slot.TemplateDigest || input.Owner != "soha-managed" {
		return api.NetworkProxyDeployment{}, apperrors.ErrConflict
	}
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return api.NetworkProxyDeployment{}, err
	}
	if input.ExpectedRevision == current.Generation && input.OwnerEpoch != current.OwnerEpoch {
		return api.NetworkProxyDeployment{}, apperrors.ErrConflict
	}
	state, err := s.Bind(ctx, p, id, domain.BindingInput{SlotID: input.SlotID, ExpectedGeneration: input.ExpectedRevision, RequestID: input.RequestID, ExpectedOwnerEpoch: input.OwnerEpoch, ExplicitAdoption: input.Adopt})
	if err != nil {
		return api.NetworkProxyDeployment{}, err
	}
	return s.DescribeState(ctx, state)
}
func (s *Service) ControlRequest(ctx context.Context, p identity.Principal, id string, input domain.ControlRequest) (api.NetworkProxyDeploymentOperation, error) {
	if input.Replicas < 0 || int64(input.Replicas) > 2147483647 {
		return api.NetworkProxyDeploymentOperation{}, apperrors.ErrInvalidArgument
	}
	replicas := int32(input.Replicas)
	request := domain.ControlInput{Action: string(input.Action), ExpectedGeneration: input.ExpectedRevision, RequestID: input.RequestID, DrainTimeoutSeconds: input.DrainSeconds}
	if input.Action == "scale" || (input.Action == "start" && input.Replicas != 0) {
		request.Replicas = &replicas
	}
	if (input.Action == "stop" || input.Action == "reconcile") && input.Replicas != 0 {
		return api.NetworkProxyDeploymentOperation{}, apperrors.ErrInvalidArgument
	}
	state, err := s.Control(ctx, p, id, request)
	if err != nil {
		return api.NetworkProxyDeploymentOperation{}, err
	}
	return api.NetworkProxyDeploymentOperation{OperationID: state.LatestTaskID, RequestID: input.RequestID, DesiredRevision: state.Generation, Status: "pending"}, nil
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
		drain = 120
	}
	if drain < 1 || drain > 120 {
		return 0, 0, fmt.Errorf("%w: drain timeout must be 1 to 120 seconds", apperrors.ErrInvalidArgument)
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
		err = s.audit.Record(ctx, audit.Entry{ActorID: p.UserID, Action: "network_access.proxy_deployment.update", ResourceKind: "NetworkProxyDeployment", ResourceName: id, RequestID: requestID, Result: "success", Metadata: map[string]any{"generation": result.Generation, "slotId": result.SlotID, "taskId": result.LatestTaskID}, CreatedAt: time.Now().UTC()})
	}
	return result, err
}

func DecodeCommand(task delivery.ExecutionTask) (domain.Command, error) {
	var command domain.Command
	encoded, err := json.Marshal(task.Payload["proxyDeployment"])
	if err == nil {
		err = json.Unmarshal(encoded, &command)
	}
	if err == nil && (task.TaskKind != domain.TaskKind || command.ProxyID == "" || command.Generation < 1 || command.Slot.UID == "" || task.ProviderKind != command.Slot.ProviderKind()) {
		err = fmt.Errorf("invalid Proxy runtime command")
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
	var receipt *domain.Receipt
	if raw, ok := task.Result["proxyDeployment"]; ok {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(encoded, &receipt); err != nil {
			return err
		}
	}
	return s.repo.RecordResult(ctx, task, command, receipt)
}

func missing(v any) bool {
	if v == nil {
		return true
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return value.IsNil()
	}
	return false
}
