package proxydeployment

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/idempotency"
)

// SlotPolicyHash covers every installed runtime/security coordinate. Adoption
// evidence is checked at initial takeover; it is not a running policy pin.
func SlotPolicyHash(slot domain.Slot) (string, error) {
	if slot.WorkloadKind == "" {
		slot.WorkloadKind = "Deployment"
	}
	slot.AllowAdoption = false
	slot.PreviousOwnerStopped = false
	_, hash, err := idempotency.Derive("proxy-deployment-slot-policy", "", "", slot)
	if err != nil {
		return "", err
	}
	return "sha256:" + hash, nil
}
func SameSlotPolicy(a, b domain.Slot) bool {
	left, err := SlotPolicyHash(a)
	if err != nil {
		return false
	}
	right, err := SlotPolicyHash(b)
	return err == nil && left == right
}
func RebindRequired() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "rebind_required", "The installed proxy policy changed or its accepted pin is missing. Stop under the accepted policy and explicitly bind the installer-approved policy before starting.", "安装策略已变更或缺少已接受的绑定。请先按原策略确认停止，再显式绑定安装者批准的策略后启动。")
}
func hasAcceptedBinding(state domain.State) bool {
	return state.Slot != nil || state.SlotID != "" || state.UID != "" || state.Owner == "soha-managed" || state.Generation != 0
}
func AcceptedPolicyHash(state domain.State) (string, error) {
	if state.Slot == nil || state.Slot.ID != state.SlotID || state.Slot.ProxyID != state.ProxyID || state.Slot.UID != state.UID {
		return "", RebindRequired()
	}
	if err := domain.ValidatePolicy(*state.Slot); err != nil {
		return "", RebindRequired()
	}
	return SlotPolicyHash(*state.Slot)
}
func RequireAcceptedPolicyHash(state domain.State, expected string) error {
	accepted, err := AcceptedPolicyHash(state)
	if err != nil {
		return err
	}
	if expected == "" || accepted != expected {
		return RebindRequired()
	}
	return nil
}
func (s *Service) installedPin(state domain.State) (domain.Slot, string, error) {
	installed, ok := s.slots[state.SlotID]
	if !ok {
		return domain.Slot{}, "", RebindRequired()
	}
	expected, err := SlotPolicyHash(installed)
	if err != nil {
		return domain.Slot{}, "", err
	}
	if err = RequireAcceptedPolicyHash(state, expected); err != nil {
		return domain.Slot{}, "", err
	}
	return installed, expected, nil
}

// StoppedForRebind is necessary but not sufficient: Commit also locks and checks
// the matching successful stop task before accepting a changed pin.
func StoppedForRebind(state domain.State) bool {
	return state.Owner == "soha-managed" && state.Phase == "converged" && state.Generation > 0 && state.ObservedGeneration == state.Generation && state.DesiredReplicas == 0 && state.ActualReplicas != nil && *state.ActualReplicas == 0 && state.ObservedDesiredReplicas != nil && *state.ObservedDesiredReplicas == 0 && state.ObservedAt != nil && !state.ObservedAt.IsZero() && state.LatestTaskID != ""
}
func ValidateBindingPin(current domain.State, next domain.Slot) error {
	if !hasAcceptedBinding(current) {
		return nil
	}
	if _, err := AcceptedPolicyHash(current); err != nil {
		return err
	}
	if current.SlotID != next.ID || current.UID != next.UID {
		return RebindRequired()
	}
	if !SameSlotPolicy(*current.Slot, next) && !StoppedForRebind(current) {
		return RebindRequired()
	}
	return nil
}
func (s *Service) runtimePin(ctx context.Context, runtimeID, instance string) (string, error) {
	state, err := s.repo.GetForMember(ctx, runtimeID, instance)
	if err != nil {
		return "", err
	}
	_, hash, err := s.installedPin(state)
	if err != nil {
		return "", err
	}
	if !PolicyContainsMember(*state.Slot, runtimeID, instance) {
		return "", apperrors.ErrAccessDenied
	}
	return hash, nil
}

func PolicyContainsMember(slot domain.Slot, runtimeID, instanceID string) bool {
	for _, m := range slot.Members {
		if m.RuntimeID == runtimeID && m.InstanceID == instanceID {
			return true
		}
	}
	return false
}
