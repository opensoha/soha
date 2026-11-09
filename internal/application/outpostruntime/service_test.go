package outpostruntime

import (
	"context"
	"errors"
	"github.com/opensoha/soha/internal/application/access"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type memoryRepository struct {
	state    domain.State
	mutation Mutation
	task     delivery.ExecutionTask
	commits  int
}

func (r *memoryRepository) Get(context.Context, string) (domain.State, error) { return r.state, nil }
func (r *memoryRepository) Commit(_ context.Context, m Mutation) (domain.State, error) {
	r.mutation = m
	r.commits++
	r.state = m.State
	return m.State, nil
}
func (*memoryRepository) RecordResult(context.Context, delivery.ExecutionTask, domain.Command) error {
	return nil
}
func (r *memoryRepository) TaskByToken(context.Context, string) (delivery.ExecutionTask, error) {
	return r.task, nil
}
func (*memoryRepository) RecordInstance(context.Context, domain.InstanceObservation) error {
	return nil
}
func (*memoryRepository) DrainStatus(context.Context, string, int64, time.Time) (domain.DrainStatus, error) {
	return domain.DrainStatus{}, nil
}
func (r *memoryRepository) GrantControl(_ context.Context, _, _, _ string, credential domain.InstanceCredential) (domain.InstanceControl, error) {
	control := domain.ControlForState(r.state, time.Now().UTC())
	if credential.ExpiresAt.Before(control.ExpiresAt) {
		control.ExpiresAt = credential.ExpiresAt
	}
	return control, nil
}

func testSlot() domain.Slot {
	return domain.Slot{ID: "slot", OutpostID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "sha256:template", MaxReplicas: 3, AllowAdoption: true, PreviousOwnerStopped: true}
}
func testPrincipal() identity.Principal {
	return identity.Principal{UserID: "operator", Roles: []string{"outpost-operator"}, PermissionKeys: []string{"identity.outposts.manage", "identity.outposts.view"}}
}

type rolePermissions struct{}

func (rolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"outpost-operator": {"identity.outposts.manage", "identity.outposts.view"}}, nil
}
func testPermissions() *access.PermissionResolver {
	return access.NewPermissionResolver(rolePermissions{})
}

func TestSlotAdoptionAndPermissions(t *testing.T) {
	for _, test := range []struct {
		name      string
		slot      domain.Slot
		input     domain.BindingInput
		principal identity.Principal
		denied    bool
	}{
		{"external stays external", testSlot(), domain.BindingInput{SlotID: "slot", RequestID: "bind"}, testPrincipal(), false},
		{"explicit approved adoption", testSlot(), domain.BindingInput{SlotID: "slot", RequestID: "adopt", ExplicitAdoption: true}, testPrincipal(), false},
		{"missing stopped proof", func() domain.Slot { s := testSlot(); s.PreviousOwnerStopped = false; return s }(), domain.BindingInput{SlotID: "slot", RequestID: "adopt", ExplicitAdoption: true}, testPrincipal(), true},
		{"no permission", testSlot(), domain.BindingInput{SlotID: "slot", RequestID: "adopt", ExplicitAdoption: true}, identity.Principal{}, true},
		{"unapproved slot", testSlot(), domain.BindingInput{SlotID: "arbitrary", RequestID: "bind"}, testPrincipal(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &memoryRepository{state: domain.State{OutpostID: "edge", Owner: "external"}}
			s, err := New(repo, testPermissions(), []domain.Slot{test.slot}, nil)
			if err != nil {
				t.Fatal(err)
			}
			state, err := s.Bind(context.Background(), test.principal, "edge", test.input)
			if test.denied {
				if err == nil || repo.commits != 0 {
					t.Fatalf("unauthorized mutation: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (state.Owner == "soha") != test.input.ExplicitAdoption {
				t.Fatalf("owner=%s", state.Owner)
			}
		})
	}
}

func TestControlTargetsAndTaskAreFrozen(t *testing.T) {
	for _, test := range []struct {
		name  string
		input domain.ControlInput
		valid bool
	}{
		{"start", domain.ControlInput{Action: "start", RequestID: "start", ExpectedGeneration: 1}, true},
		{"scale above limit", domain.ControlInput{Action: "scale", Replicas: ptr(int32(4)), RequestID: "scale", ExpectedGeneration: 1}, false},
		{"negative scale", domain.ControlInput{Action: "scale", Replicas: ptr(int32(-1)), RequestID: "scale", ExpectedGeneration: 1}, false},
		{"stop bounded", domain.ControlInput{Action: "stop", RequestID: "stop", ExpectedGeneration: 1, DrainTimeoutSeconds: 30}, true},
		{"unbounded drain", domain.ControlInput{Action: "stop", RequestID: "stop", ExpectedGeneration: 1, DrainTimeoutSeconds: 31}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &memoryRepository{state: domain.State{OutpostID: "edge", Owner: "soha", OwnerEpoch: 1, Generation: 1, SlotID: "slot", UID: "uid"}}
			s, _ := New(repo, testPermissions(), []domain.Slot{testSlot()}, nil)
			s.executors["executor"] = true
			_, err := s.Control(context.Background(), testPrincipal(), "edge", test.input)
			if (err == nil) != test.valid {
				t.Fatalf("err=%v", err)
			}
			if !test.valid {
				return
			}
			if repo.mutation.Task == nil || repo.mutation.State.Generation != 2 {
				t.Fatal("missing atomic task/target")
			}
			command, err := DecodeCommand(*repo.mutation.Task)
			if err != nil || command.Slot != testSlot() || command.OwnerEpoch != 1 {
				t.Fatalf("command=%+v err=%v", command, err)
			}
		})
	}
	repo := &memoryRepository{state: domain.State{OutpostID: "edge", Owner: "external", SlotID: "slot", UID: "uid"}}
	s, _ := New(repo, testPermissions(), []domain.Slot{testSlot()}, nil)
	if _, err := s.Control(context.Background(), testPrincipal(), "edge", domain.ControlInput{Action: "start", RequestID: "x"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal(err)
	}
}

func TestInstanceObservationRejectsFutureTimestamp(t *testing.T) {
	s, _ := New(&memoryRepository{}, nil, nil, nil)
	err := s.RecordInstanceObservation(context.Background(), domain.InstanceObservation{InstanceID: "pod", ProcessEpoch: "epoch", ObservedAt: time.Now().Add(time.Hour), LeaseExpiresAt: time.Now().Add(2 * time.Hour)})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal(err)
	}
}

func ptr[T any](value T) *T { return &value }
