package proxydeployment

import (
	"context"
	"errors"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"slices"
	"testing"
	"time"
)

func copyPolicy(slot domain.Slot) domain.Slot { slot.Members = slices.Clone(slot.Members); return slot }
func stoppedPinState(slot domain.Slot) domain.State {
	zero := int32(0)
	now := time.Now().UTC()
	return domain.State{ProxyID: slot.ProxyID, SlotID: slot.ID, UID: slot.UID, Slot: &slot, Members: slices.Clone(slot.Members), MemberSetHash: slot.MemberSetHash(), Owner: "soha-managed", OwnerEpoch: 1, Generation: 4, ObservedGeneration: 4, ControlRevision: 7, Phase: "converged", DesiredReplicas: 0, ActualReplicas: &zero, ObservedDesiredReplicas: &zero, ObservedAt: &now, LatestTaskID: "stop-task"}
}
func isRebind(err error) bool {
	var business *apperrors.BusinessError
	return errors.Is(err, apperrors.ErrConflict) && errors.As(err, &business) && business.Code() == "rebind_required"
}
func newPinService(t *testing.T, old, installed domain.Slot) (*Service, *memoryRepository) {
	t.Helper()
	repo := &memoryRepository{state: stoppedPinState(old)}
	service, err := New(repo, nil, []domain.Slot{installed}, nil, scope{})
	if err != nil {
		t.Fatal(err)
	}
	service.executors[installed.ExecutorID] = true
	return service, repo
}
func TestSlotPolicyCanonicalHashPinsRuntimeCoordinatesOnly(t *testing.T) {
	old := testSlot()
	oldHash, _ := SlotPolicyHash(old)
	t.Logf("accepted policy hash=%s", oldHash)
	evidence := copyPolicy(old)
	evidence.AllowAdoption = !old.AllowAdoption
	evidence.PreviousOwnerStopped = !old.PreviousOwnerStopped
	if !SameSlotPolicy(old, evidence) {
		t.Fatal("takeover evidence treated as runtime pin")
	}
	for _, change := range []func(*domain.Slot){func(s *domain.Slot) { s.TemplateDigest = "new-template" }, func(s *domain.Slot) { s.ExecutorID = "other-exec" }, func(s *domain.Slot) { s.Members[1].RuntimeID = "other-runtime" }, func(s *domain.Slot) { s.Namespace = "other-namespace" }, func(s *domain.Slot) { s.Name = "other-name" }, func(s *domain.Slot) { s.UID = "replacement" }, func(s *domain.Slot) { s.ProxyID = "other-owner" }, func(s *domain.Slot) { s.MaxReplicas++ }, func(s *domain.Slot) { s.WorkloadKind = "Deployment" }} {
		next := copyPolicy(old)
		change(&next)
		if SameSlotPolicy(old, next) {
			t.Fatalf("changed runtime policy preserved hash: %+v", next)
		}
	}
	a, b := old, old
	a.WorkloadKind = ""
	b.WorkloadKind = "Deployment"
	if !SameSlotPolicy(a, b) {
		t.Fatal("default Deployment kind was not canonical")
	}
	next := copyPolicy(old)
	next.TemplateDigest = "new-template"
	nextHash, _ := SlotPolicyHash(next)
	t.Logf("installed policy hash=%s", nextHash)
}
func TestInstalledPolicyChangeBlocksEveryControlAndDirectConsumer(t *testing.T) {
	old := testSlot()
	for _, kind := range []string{"template", "executor", "member map"} {
		t.Run(kind, func(t *testing.T) {
			installed := copyPolicy(old)
			switch kind {
			case "template":
				installed.TemplateDigest = "new-template"
			case "executor":
				installed.ExecutorID = "new-exec"
			case "member map":
				installed.Members[1].RuntimeID = "new-runtime"
			}
			service, repo := newPinService(t, old, installed)
			p := identity.Principal{UserID: "op"}
			for _, action := range []string{"start", "scale", "stop", "reconcile"} {
				target := int32(1)
				_, err := service.Control(context.Background(), p, "master", domain.ControlInput{Action: action, Replicas: &target, RequestID: "request", ExpectedGeneration: 4})
				if !isRebind(err) || repo.commits != 0 {
					t.Fatalf("%s crossed installed pin: %v", action, err)
				}
			}
			view, err := service.View(context.Background(), p, "master")
			if err != nil || view.State != "conflict" || view.ReasonCode != "rebind_required" || view.TemplateHash != old.TemplateDigest || repo.state.Slot.TemplateDigest != old.TemplateDigest {
				t.Fatalf("view repinned or hid conflict: %+v %v", view, err)
			}
			if _, err = service.ApprovedPool(context.Background(), "master"); !isRebind(err) {
				t.Fatal("publisher retained mismatched installed policy")
			}
			if _, err = service.RuntimeControl(context.Background(), networkidentity.Identity{Scope: networkidentity.ScopeNetworkControl, Kind: "proxy", ID: "master"}, "pool-0", "boot"); !isRebind(err) || repo.grants != 0 {
				t.Fatal("runtime renewed mismatched accepted pin")
			}
		})
	}
}
func TestChangedBindingRequiresCurrentTrustworthyStoppedSnapshot(t *testing.T) {
	old := testSlot()
	installed := copyPolicy(old)
	installed.TemplateDigest = "new-template"
	for _, name := range []string{"stopped", "running", "unknown", "pending", "stale generation", "unknown replicas", "missing pin"} {
		t.Run(name, func(t *testing.T) {
			service, repo := newPinService(t, old, installed)
			switch name {
			case "running":
				n := int32(2)
				repo.state.ActualReplicas = &n
				repo.state.DesiredReplicas = 2
			case "unknown":
				repo.state.Phase = "unknown"
			case "pending":
				repo.state.Phase = "queued"
			case "stale generation":
				repo.state.ObservedGeneration--
			case "unknown replicas":
				repo.state.ObservedDesiredReplicas = nil
			case "missing pin":
				repo.state.Slot = nil
			}
			result, err := service.BindRequest(context.Background(), identity.Principal{UserID: "op"}, "master", domain.BindingRequest{SlotID: installed.ID, ObjectUID: installed.UID, TemplateHash: installed.TemplateDigest, Owner: "soha-managed", OwnerEpoch: 1, ExpectedRevision: 4, RequestID: "bind-new", Adopt: true})
			if name == "stopped" {
				if err != nil || result.TemplateHash != installed.TemplateDigest || repo.commits != 1 {
					t.Fatalf("approved stopped rebind failed: %+v %v", result, err)
				}
			} else if !isRebind(err) || repo.commits != 0 {
				t.Fatalf("%s rebind was not stopped: %v", name, err)
			}
		})
	}
}
func TestRollbackRequiresSameStopAndExplicitApprovedRebind(t *testing.T) {
	old := testSlot()
	current := copyPolicy(old)
	current.TemplateDigest = "upgraded-template"
	service, repo := newPinService(t, current, old)
	repo.state.Phase = "unknown"
	request := domain.BindingRequest{SlotID: old.ID, ObjectUID: old.UID, TemplateHash: old.TemplateDigest, Owner: "soha-managed", OwnerEpoch: 1, ExpectedRevision: 4, RequestID: "rollback", Adopt: true}
	if _, err := service.BindRequest(context.Background(), identity.Principal{UserID: "op"}, "master", request); !isRebind(err) {
		t.Fatal("rollback bypassed stopped proof")
	}
	repo.state = stoppedPinState(current)
	if _, err := service.BindRequest(context.Background(), identity.Principal{UserID: "op"}, "master", request); err != nil || repo.state.Slot.TemplateDigest != old.TemplateDigest {
		t.Fatal("approved stopped rollback rejected")
	}
}
func TestUnchangedPinAndTakeoverEvidencePreserveControlsAndBinding(t *testing.T) {
	old := testSlot()
	installed := copyPolicy(old)
	installed.AllowAdoption = false
	installed.PreviousOwnerStopped = false
	service, repo := newPinService(t, old, installed)
	n := int32(2)
	repo.state.DesiredReplicas = 2
	repo.state.ActualReplicas = &n
	repo.state.ObservedDesiredReplicas = &n
	if _, err := service.Control(context.Background(), identity.Principal{UserID: "op"}, "master", domain.ControlInput{Action: "start", ExpectedGeneration: 4, RequestID: "same-runtime"}); err != nil {
		t.Fatal(err)
	}
	repo.state = stoppedPinState(old)
	repo.state.DesiredReplicas = 2
	repo.state.ActualReplicas = &n
	if _, err := service.BindRequest(context.Background(), identity.Principal{UserID: "op"}, "master", domain.BindingRequest{SlotID: old.ID, ObjectUID: old.UID, TemplateHash: old.TemplateDigest, Owner: "soha-managed", OwnerEpoch: 1, ExpectedRevision: 4, RequestID: "same-binding"}); err != nil {
		t.Fatal(err)
	}
}
func TestMissingAcceptedPinCannotCreateTasksOrRenewRuntime(t *testing.T) {
	slot := testSlot()
	service, repo := newPinService(t, slot, slot)
	repo.state.Slot = nil
	if _, err := service.Control(context.Background(), identity.Principal{UserID: "op"}, "master", domain.ControlInput{Action: "start", ExpectedGeneration: 4, RequestID: "missing"}); !isRebind(err) || repo.commits != 0 {
		t.Fatal("missing pin created a task")
	}
	if _, err := service.ApprovedPool(context.Background(), "master"); !isRebind(err) {
		t.Fatal("missing pin fell back to unbound pool")
	}
	if _, err := service.RuntimeControl(context.Background(), networkidentity.Identity{Scope: networkidentity.ScopeNetworkControl, Kind: "proxy", ID: "master"}, "pool-0", "boot"); !isRebind(err) || repo.grants != 0 {
		t.Fatal("missing pin renewed runtime")
	}
}

func TestRemovedMemberCannotRenewFromOldAssociationAndCurrentPin(t *testing.T) {
	old := testSlot()
	current := copyPolicy(old)
	current.Members[1].RuntimeID = "new-runtime"
	service, repo := newPinService(t, current, current)
	identity := networkidentity.Identity{Scope: networkidentity.ScopeNetworkControl, Kind: "proxy", ID: old.Members[1].RuntimeID}
	if _, err := service.RuntimeControl(context.Background(), identity, old.Members[1].InstanceID, "boot"); !errors.Is(err, apperrors.ErrAccessDenied) || repo.grants != 0 {
		t.Fatal("old association renewed under a current new pin")
	}
}
