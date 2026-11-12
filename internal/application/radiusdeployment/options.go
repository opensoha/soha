package radiusdeployment

import (
	"context"
	"errors"
	"sort"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) Options(ctx context.Context, p identity.Principal) (api.NetworkRADIUSOptions, error) {
	result := api.NetworkRADIUSOptions{Slots: []api.NetworkRADIUSSlotOption{}}
	if err := s.authority.AuthorizeRADIUS(ctx, p, "", "", "view"); err != nil {
		return result, err
	}
	keys := make([]string, 0, len(s.slots))
	for key := range s.slots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		slot := s.slots[key]
		err := s.authority.AuthorizeRADIUS(ctx, p, slot.SiteID, slot.NASBindingID, "view")
		if errors.Is(err, apperrors.ErrAccessDenied) {
			continue
		}
		if err != nil {
			return result, err
		}
		option, err := s.slotOption(ctx, p, slot)
		if err != nil {
			return result, err
		}
		result.Slots = append(result.Slots, option)
	}
	return result, nil
}
func (s *Service) slotOption(ctx context.Context, p identity.Principal, slot domain.Slot) (api.NetworkRADIUSSlotOption, error) {
	option := api.NetworkRADIUSSlotOption{SlotID: slot.ID, SiteID: slot.SiteID, NASBindingID: slot.NASBindingID, MaxReplicas: int(slot.MaxReplicas), ConfigurationRefs: []string{}, Available: true, ReasonCode: ""}
	for _, configuration := range slot.Configurations {
		option.ConfigurationRefs = append(option.ConfigurationRefs, configuration.Material.Reference)
	}
	active, err := s.repo.SiteActive(ctx, slot.SiteID)
	if err != nil {
		return option, err
	}
	if !active {
		option.Available = false
		option.ReasonCode = "site_inactive_or_missing"
	}
	binding, err := s.repo.Binding(ctx, slot.NASBindingID)
	if errors.Is(err, apperrors.ErrNotFound) {
		option.CreateApprovedBindingRequired = true
	} else if err != nil {
		return option, err
	} else {
		option.BindingExists = true
		activity, activityErr := s.repo.BindingActivity(ctx, binding)
		if activityErr != nil {
			return option, activityErr
		}
		if binding.Status == "active" || activity {
			option.Available = false
			option.ReasonCode = "binding_active_or_served"
		}
		if binding.SiteID == slot.SiteID {
			at := binding.UpdatedAt
			option.BindingUpdatedAt = &at
		} else {
			option.Available = false
			option.ReasonCode = "binding_scope_conflict"
		}
	}
	kind, owner, err := s.repo.Reservation(ctx, slot.UID)
	if err != nil {
		return option, err
	}
	if kind != "" {
		option.Available = false
		option.ReasonCode = "slot_reserved_long_term"
		if kind == "radius" {
			state, err := s.repo.Get(ctx, owner)
			if err == nil && s.authority.AuthorizeRADIUS(ctx, p, state.Pool.SiteID, state.Pool.NASBindingID, "view") == nil {
				option.ReservedPoolID = owner
			}
		}
	}
	return option, nil
}
