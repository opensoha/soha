package radiusdeployment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"time"

	"github.com/google/uuid"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appnetwork "github.com/opensoha/soha/internal/application/networkaccess"
	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Mutation struct {
	State                    domain.State
	ExpectedRevision         int64
	RequestID, Fingerprint   string
	Create                   bool
	ExpectedBindingUpdatedAt *time.Time
	CreateApprovedBinding    bool
	Task                     *delivery.ExecutionTask
}
type Repository interface {
	SiteActive(context.Context, string) (bool, error)
	Reservation(context.Context, string) (string, string, error)
	ResolveManagedNAS(context.Context, networkidentity.Identity, string, *networkprotocol.ManagedNASProof, string, bool) (access.NASBinding, error)
	List(context.Context) ([]domain.State, error)
	Get(context.Context, string) (domain.State, error)
	Binding(context.Context, string) (access.NASBinding, error)
	BindingActivity(context.Context, access.NASBinding) (bool, error)
	Commit(context.Context, Mutation) (domain.State, error)
	Members(context.Context, string) ([]domain.MemberRecord, error)
	GrantControl(context.Context, networkidentity.Identity, string, string, string) (api.NetworkRADIUSRuntimeControl, error)
	RecordACK(context.Context, networkidentity.Identity, api.NetworkRADIUSRuntimeACK, string) error
	Drain(context.Context, string, int64, time.Time) (domain.DrainStatus, error)
	RecordResult(context.Context, delivery.ExecutionTask, domain.Command, *domain.Receipt) error
	TaskByToken(context.Context, string) (delivery.ExecutionTask, error)
}
type Authority interface {
	AuthorizeRADIUS(context.Context, identity.Principal, string, string, string) error
}
type Service struct {
	repo      Repository
	authority Authority
	slots     map[string]domain.Slot
	executors map[string]bool
}

func New(repo Repository, authority Authority, slots []domain.Slot) (*Service, error) {
	if nilInterface(repo) || nilInterface(authority) {
		return nil, fmt.Errorf("RADIUS repository and scope authority are required")
	}
	s := &Service{repo: repo, authority: authority, slots: map[string]domain.Slot{}, executors: map[string]bool{}}
	seen := map[string]bool{}
	for _, slot := range slots {
		if err := domain.ValidateSlot(slot); err != nil {
			return nil, err
		}
		if _, ok := s.slots[slot.ID]; ok {
			return nil, apperrors.ErrConflict
		}
		for _, member := range slot.Members {
			if seen[member.RuntimeID] {
				return nil, apperrors.ErrConflict
			}
			seen[member.RuntimeID] = true
		}
		for _, configuration := range slot.Configurations {
			if err := appnetwork.ValidateEAPServerTrust(&configuration.EAPServerTrust); err != nil {
				return nil, err
			}
		}
		encoded, _ := json.Marshal(slot)
		var owned domain.Slot
		_ = json.Unmarshal(encoded, &owned)
		s.slots[slot.ID] = owned
	}
	return s, nil
}
func nilInterface(v any) bool {
	if v == nil {
		return true
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return value.IsNil()
	}
	return false
}
func (s *Service) pinned(state domain.State) error {
	slot, ok := s.slots[state.Pool.SlotID]
	if !ok || slot.Hash() != state.SlotHash || slot.UID != state.Pool.ObjectUID {
		return fmt.Errorf("%w: approved slot changed; same owner recovery required", apperrors.ErrConflict)
	}
	return nil
}
func (s *Service) authorized(ctx context.Context, p identity.Principal, id, action string) (domain.State, error) {
	state, err := s.repo.Get(ctx, id)
	if err == nil {
		err = s.authority.AuthorizeRADIUS(ctx, p, state.Pool.SiteID, state.Pool.NASBindingID, action)
	}
	return state, err
}

func (s *Service) List(ctx context.Context, p identity.Principal) (api.NetworkRADIUSPoolList, error) {
	states, err := s.repo.List(ctx)
	if err != nil {
		return api.NetworkRADIUSPoolList{}, err
	}
	view := api.NetworkRADIUSPoolList{Items: []api.NetworkRADIUSPool{}}
	if err := s.authority.AuthorizeRADIUS(ctx, p, "", "", "view"); err != nil {
		return view, err
	}
	for _, state := range states {
		authErr := s.authority.AuthorizeRADIUS(ctx, p, state.Pool.SiteID, state.Pool.NASBindingID, "view")
		if errors.Is(authErr, apperrors.ErrAccessDenied) {
			continue
		}
		if authErr != nil {
			return view, authErr
		}
		item, err := s.describe(ctx, state)
		if err != nil {
			return view, err
		}
		view.Items = append(view.Items, item)
	}
	return view, nil
}
func (s *Service) Get(ctx context.Context, p identity.Principal, id string) (api.NetworkRADIUSPool, error) {
	state, err := s.authorized(ctx, p, id, "view")
	if err != nil {
		return api.NetworkRADIUSPool{}, err
	}
	return s.describe(ctx, state)
}
func (s *Service) Create(ctx context.Context, p identity.Principal, input api.NetworkRADIUSPoolInput) (api.NetworkRADIUSPool, error) {
	if err := s.authority.AuthorizeRADIUS(ctx, p, input.SiteID, input.NASBindingID, "create"); err != nil {
		return api.NetworkRADIUSPool{}, err
	}
	slot, ok := s.slots[input.SlotID]
	if !ok || slot.SiteID != input.SiteID || slot.NASBindingID != input.NASBindingID {
		return api.NetworkRADIUSPool{}, apperrors.ErrAccessDenied
	}
	configuration, ok := slot.Configuration(input.ConfigurationRef)
	if !ok {
		return api.NetworkRADIUSPool{}, apperrors.ErrInvalidArgument
	}
	if input.Name == "" || len(input.Name) > 128 || input.RequestID == "" || (!input.CreateApprovedBinding && (input.ExpectedBindingUpdatedAt == nil || input.ExpectedBindingUpdatedAt.IsZero())) || (input.CreateApprovedBinding && input.ExpectedBindingUpdatedAt != nil) {
		return api.NetworkRADIUSPool{}, apperrors.ErrInvalidArgument
	}
	id := uuid.NewString()
	state := domain.State{Pool: api.NetworkRADIUSPool{PoolID: id, Name: input.Name, SiteID: input.SiteID, NASBindingID: input.NASBindingID, LogicalRuntimeID: domain.PoolPrefix + id, SlotID: slot.ID, ObjectUID: slot.UID, TemplateHash: slot.TemplateDigest, Revision: 1, OwnerEpoch: 1, ConfigurationRevision: 1, ConfigurationRef: input.ConfigurationRef, State: "stopped", Members: []api.NetworkRADIUSMember{}}, Slot: slot, SlotHash: slot.Hash(), Configuration: configuration, ControlRevision: 1}
	state.Pool.ConfigurationHash = state.ConfigurationHash()
	return s.commitFor(ctx, p, Mutation{State: state, RequestID: input.RequestID, Create: true, CreateApprovedBinding: input.CreateApprovedBinding, ExpectedBindingUpdatedAt: input.ExpectedBindingUpdatedAt}, input)
}
func (s *Service) Configure(ctx context.Context, p identity.Principal, id string, input api.NetworkRADIUSConfigurationInput) (api.NetworkRADIUSPool, error) {
	state, err := s.authorized(ctx, p, id, "update")
	if err != nil {
		return api.NetworkRADIUSPool{}, err
	}
	if err := s.pinned(state); err != nil {
		return api.NetworkRADIUSPool{}, err
	}
	if state.Pool.Enabled || state.Pool.ObservedReplicas == nil || *state.Pool.ObservedReplicas != 0 || state.Pool.State == "unknown" {
		return api.NetworkRADIUSPool{}, fmt.Errorf("%w: stop_required", apperrors.ErrConflict)
	}
	configuration, ok := state.Slot.Configuration(input.ConfigurationRef)
	if !ok {
		return api.NetworkRADIUSPool{}, apperrors.ErrAccessDenied
	}
	state.Configuration = configuration
	state.Pool.ConfigurationRef = input.ConfigurationRef
	state.Pool.ConfigurationRevision++
	state.ControlRevision++
	state.Pool.Revision = input.ExpectedRevision + 1
	state.Pool.ConfigurationHash = state.ConfigurationHash()
	return s.commitFor(ctx, p, Mutation{State: state, ExpectedRevision: int64(input.ExpectedRevision), RequestID: input.RequestID}, input)
}
func (s *Service) Control(ctx context.Context, p identity.Principal, id string, input api.NetworkRADIUSControlInput) (api.NetworkRADIUSPool, error) {
	state, err := s.authorized(ctx, p, id, "update")
	if err != nil {
		return api.NetworkRADIUSPool{}, err
	}
	if err := s.pinned(state); err != nil {
		return api.NetworkRADIUSPool{}, err
	}
	if !s.executors[state.Slot.ExecutorID] {
		return api.NetworkRADIUSPool{}, apperrors.ErrUnsupportedOperation
	}
	if state.Pool.State == "unknown" && input.Action != "reconcile" {
		return api.NetworkRADIUSPool{}, fmt.Errorf("%w: readback_required", apperrors.ErrConflict)
	}
	target, err := controlTarget(state, input)
	if err != nil {
		return api.NetworkRADIUSPool{}, err
	}
	now := time.Now().UTC()
	state.Pool.Revision = input.ExpectedRevision + 1
	state.ControlRevision++
	state.Pool.DesiredReplicas = int(target)
	if input.Action != "reconcile" {
		state.Pool.Enabled = target > 0
		state.RetiringFrom = target
		observed, err := maxObserved(state)
		if err != nil {
			return api.NetworkRADIUSPool{}, err
		}
		state.RetiringEnd = max(state.RetiringEnd, observed)
		state.DrainUntil = nil
		state.StopConfirmUntil = nil
		if target < state.RetiringEnd || target == 0 {
			drain := input.DrainSeconds
			if drain == 0 {
				drain = 60
			}
			if drain < 1 || drain > 120 {
				return api.NetworkRADIUSPool{}, apperrors.ErrInvalidArgument
			}
			until := now.Add(time.Duration(drain) * time.Second)
			confirm := until.Add(30 * time.Second)
			state.DrainUntil = &until
			state.StopConfirmUntil = &confirm
		}
	}
	// A new explicit administrative reconcile may grant a new bounded stop-only
	// receipt window. It never changes the original State drain/serve deadline.
	if input.Action == "reconcile" && state.RetiringEnd > state.RetiringFrom && (state.StopConfirmUntil == nil || !state.StopConfirmUntil.After(now)) {
		until := now.Add(30 * time.Second)
		state.StopConfirmUntil = &until
	}
	state.DrainProven = false
	state.Pool.State = "pending"
	state.Pool.ReasonCode = ""
	command := domain.Command{PoolID: id, Slot: state.Slot, SlotHash: state.SlotHash, Revision: int64(state.Pool.Revision), OwnerEpoch: int64(state.Pool.OwnerEpoch), Action: string(input.Action), Replicas: target, Deadline: now.Add(240 * time.Second)}
	task := delivery.ExecutionTask{ID: uuid.NewString(), TaskKind: domain.TaskKind, ProviderKind: state.Slot.ProviderKind(), TargetKind: "radius_slot", QueueKey: state.Slot.ID, LockKey: "radius-slot/" + state.Slot.ID, Status: "queued", TimeoutSeconds: 240, CallbackToken: uuid.NewString(), Payload: map[string]any{"radiusDeployment": command}, CreatedAt: now, UpdatedAt: now}
	state.Pool.OperationID = task.ID
	return s.commitFor(ctx, p, Mutation{State: state, ExpectedRevision: int64(input.ExpectedRevision), RequestID: input.RequestID, Task: &task}, input)
}
func controlTarget(state domain.State, input api.NetworkRADIUSControlInput) (int32, error) {
	target := 0
	switch input.Action {
	case "start":
		target = 1
		if input.Replicas != nil {
			target = *input.Replicas
		}
	case "scale":
		if input.Replicas == nil {
			return 0, apperrors.ErrInvalidArgument
		}
		target = *input.Replicas
	case "stop":
		if input.Replicas != nil && *input.Replicas != 0 {
			return 0, apperrors.ErrInvalidArgument
		}
	case "reconcile":
		if input.Replicas != nil || input.DrainSeconds != 0 {
			return 0, apperrors.ErrInvalidArgument
		}
		target = state.Pool.DesiredReplicas
	default:
		return 0, apperrors.ErrInvalidArgument
	}
	if input.Replicas != nil && (*input.Replicas < 0 || *input.Replicas > int(state.Slot.MaxReplicas)) || target < 0 || target > math.MaxInt32 || target > int(state.Slot.MaxReplicas) || input.Action == "start" && target == 0 {
		return 0, apperrors.ErrInvalidArgument
	}
	return int32(target), nil
}
func maxObserved(state domain.State) (int32, error) {
	value := int32(0)
	if state.Pool.ObservedReplicas != nil {
		observed := *state.Pool.ObservedReplicas
		if observed < 0 || observed > math.MaxInt32 {
			return 0, apperrors.ErrConflict
		}
		value = int32(observed)
	}
	if state.ObservedDesiredReplicas != nil {
		value = max(value, *state.ObservedDesiredReplicas)
	}
	return value, nil
}
func (s *Service) commitFor(ctx context.Context, p identity.Principal, m Mutation, i any) (api.NetworkRADIUSPool, error) {
	v, e := s.commit(ctx, m, i)
	if e == nil {
		if r, ok := s.authority.(interface {
			RecordRADIUS(context.Context, identity.Principal, string, string, int64)
		}); ok {
			action := "configure"
			if m.Create {
				action = "create"
			} else if m.Task != nil {
				action = "control"
			}
			r.RecordRADIUS(ctx, p, v.PoolID, action, int64(v.Revision))
		}
	}
	return v, e
}
func (s *Service) commit(ctx context.Context, mutation Mutation, input any) (api.NetworkRADIUSPool, error) {
	if mutation.RequestID == "" || len(mutation.RequestID) > 128 || mutation.ExpectedRevision < 0 {
		return api.NetworkRADIUSPool{}, apperrors.ErrInvalidArgument
	}
	encoded, _ := json.Marshal(input)
	mutation.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(encoded))
	state, err := s.repo.Commit(ctx, mutation)
	if err != nil {
		return api.NetworkRADIUSPool{}, err
	}
	return s.describe(ctx, state)
}

func (s *Service) describe(ctx context.Context, state domain.State) (api.NetworkRADIUSPool, error) {
	view := state.Pool
	view.Members = []api.NetworkRADIUSMember{}
	if err := s.pinned(state); err != nil {
		view.State = "conflict"
		view.ReasonCode = "approved_slot_changed"
		return view, nil
	}
	members, err := s.repo.Members(ctx, view.PoolID)
	if err != nil {
		return view, err
	}
	now := time.Now().UTC()
	ready := 0
	readyKnown := view.ObservedReplicas != nil
	for _, record := range members {
		m := api.NetworkRADIUSMember{MemberID: record.Member.MemberID, RuntimeID: record.Member.RuntimeID, Ordinal: int(record.Member.Ordinal), Endpoint: record.Member.Endpoint, ReasonCode: "NAS_endpoint_configuration_required"}
		if record.Control != nil {
			m.CredentialID = record.Control.CredentialID
			m.ProcessEpoch = record.Control.ProcessEpoch
			until := record.Control.ControlLeaseUntil
			m.LeaseUntil = &until
		}
		if record.Fresh(state, now) {
			a := record.ACK
			m.ConfigurationRevision = a.ConfigurationRevision
			m.ControlRevision = a.ControlRevision
			m.RuntimeVersion = a.RuntimeVersion
			m.LastObservedAt = record.ReceivedAt
			m.StateCountKnown = a.StateCountKnown
			m.ActiveStates = a.ActiveStates
			m.ProcessStopped = a.ProcessStopped
			m.AcceptingNewAuthentications = a.AcceptingNewAuthentications
			m.Ready = record.Ready(state, now)
			m.TrafficReady = m.Ready && record.NASUsedAt != nil
			m.ReasonCode = ""
			if !m.TrafficReady {
				m.ReasonCode = "NAS_endpoint_configuration_required"
			}
		}
		if m.Ordinal < view.DesiredReplicas && !record.Fresh(state, now) {
			readyKnown = false
		}
		if m.Ready && m.Ordinal < view.DesiredReplicas {
			ready++
		}
		view.Members = append(view.Members, m)
	}
	if readyKnown {
		view.ReadyReplicas = &ready
	} else {
		view.ReadyReplicas = nil
	}
	return view, nil
}
func (s *Service) RuntimeControl(ctx context.Context, id networkidentity.Identity, member, epoch string) (api.NetworkRADIUSRuntimeControl, error) {
	state, err := s.stateForMember(ctx, id.ID)
	if err != nil {
		return api.NetworkRADIUSRuntimeControl{}, err
	}
	if err := s.pinned(state); err != nil {
		return api.NetworkRADIUSRuntimeControl{}, err
	}
	return s.repo.GrantControl(ctx, id, member, epoch, state.SlotHash)
}
func (s *Service) RuntimeACK(ctx context.Context, id networkidentity.Identity, ack api.NetworkRADIUSRuntimeACK) error {
	state, err := s.repo.Get(ctx, ack.PoolID)
	if err != nil {
		return err
	}
	if err := s.pinned(state); err != nil {
		return err
	}
	return s.repo.RecordACK(ctx, id, ack, state.SlotHash)
}
func (s *Service) stateForMember(ctx context.Context, runtime string) (domain.State, error) {
	states, err := s.repo.List(ctx)
	if err != nil {
		return domain.State{}, err
	}
	for _, state := range states {
		if slices.ContainsFunc(state.Slot.Members, func(m domain.Member) bool { return m.RuntimeID == runtime }) {
			return state, nil
		}
	}
	return domain.State{}, apperrors.ErrUnauthorized
}
func DecodeCommand(task delivery.ExecutionTask) (domain.Command, error) {
	var command domain.Command
	encoded, err := json.Marshal(task.Payload["radiusDeployment"])
	if err == nil {
		err = json.Unmarshal(encoded, &command)
	}
	if err != nil || task.TaskKind != domain.TaskKind || command.Revision < 1 || command.SlotHash != command.Slot.Hash() || task.ProviderKind != command.Slot.ProviderKind() {
		return domain.Command{}, apperrors.ErrAccessDenied
	}
	return command, nil
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
	if value, ok := task.Result["radiusDeployment"]; ok {
		encoded, _ := json.Marshal(value)
		if err := json.Unmarshal(encoded, &receipt); err != nil {
			return err
		}
	}
	return s.repo.RecordResult(ctx, task, command, receipt)
}
