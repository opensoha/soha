package networkaccess

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	access "github.com/opensoha/soha/internal/application/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type WorkloadBindingStore interface {
	GetWorkloadBinding(context.Context, string) (domain.WorkloadBinding, error)
	ListWorkloadBindings(context.Context, int, int) ([]domain.WorkloadBinding, error)
	PutWorkloadBinding(context.Context, domain.WorkloadBinding, time.Time) (domain.WorkloadBinding, error)
}

func (s *Service) ListWorkloadBindings(ctx context.Context, p identity.Principal) ([]domain.WorkloadBinding, error) {
	if err := s.authorize(ctx, p, access.PermNetworkAccessPolicyView); err != nil {
		return nil, err
	}
	store, ok := s.store.(WorkloadBindingStore)
	if !ok {
		return nil, apperrors.ErrServiceUnavailable
	}
	return collectVisible(ctx, 200, func(limit, offset int) ([]domain.WorkloadBinding, error) {
		return store.ListWorkloadBindings(ctx, limit, offset)
	}, func(b domain.WorkloadBinding) error {
		return s.authorizeWorkloadScope(ctx, p, access.PermNetworkAccessPolicyView, b)
	})
}
func (s *Service) authorizeWorkloadScope(ctx context.Context, p identity.Principal, key string, b domain.WorkloadBinding) error {
	if err := s.authorizeScope(ctx, p, key, resourceScope{kind: "NetworkWorkloadBinding", id: b.ID, siteID: b.SiteID, spaceID: b.NetworkSpaceID, deviceID: b.DeviceID}); err != nil {
		return err
	}
	device, err := s.store.GetDevice(ctx, b.DeviceID)
	if err != nil {
		return err
	}
	if err := s.authorizeScope(ctx, p, access.PermNetworkAccessEndpointDevicesView, deviceScope(device)); err != nil {
		return err
	}
	for _, id := range b.ResourceIDs {
		if err := s.authorizeScope(ctx, p, access.PermNetworkAccessResourcesView, resourceScope{kind: "NetworkResource", id: id, siteID: b.SiteID, spaceID: b.NetworkSpaceID}); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) PutWorkloadBinding(ctx context.Context, p identity.Principal, b domain.WorkloadBinding) (domain.WorkloadBinding, error) {
	if err := networkUserSubject(p.UserID); err != nil {
		return domain.WorkloadBinding{}, err
	}
	if err := s.authorize(ctx, p, access.PermNetworkAccessPolicyUpdate); err != nil {
		return domain.WorkloadBinding{}, err
	}
	if !runtimeIdentifierPattern.MatchString(b.ID) || !runtimeIdentifierPattern.MatchString(b.RuntimeID) || !runtimeIdentifierPattern.MatchString(b.ServiceAccountID) || len(b.ResourceIDs) == 0 || len(b.ResourceIDs) > 256 || b.Revision < 0 || (b.Enabled && (!b.ExpiresAt.After(time.Now()) || b.ExpiresAt.After(time.Now().Add(90*24*time.Hour)))) {
		return domain.WorkloadBinding{}, invalid("invalid workload binding")
	}
	slices.Sort(b.ResourceIDs)
	if len(slices.Compact(slices.Clone(b.ResourceIDs))) != len(b.ResourceIDs) {
		return domain.WorkloadBinding{}, invalid("duplicate workload resources")
	}
	store, ok := s.store.(WorkloadBindingStore)
	if !ok {
		return domain.WorkloadBinding{}, apperrors.ErrServiceUnavailable
	}
	old, err := store.GetWorkloadBinding(ctx, b.ID)
	if err == nil {
		if err := s.authorizeWorkloadScope(ctx, p, access.PermNetworkAccessPolicyUpdate, old); err != nil {
			return domain.WorkloadBinding{}, err
		}
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return domain.WorkloadBinding{}, err
	}
	if err := s.authorizeWorkloadScope(ctx, p, access.PermNetworkAccessPolicyUpdate, b); err != nil {
		return domain.WorkloadBinding{}, err
	}
	item, err := store.PutWorkloadBinding(ctx, b, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, p, "network_access.workload_bindings.put", "NetworkWorkloadBinding", item.ID, item.ID)
	}
	return item, err
}

func EvaluateWorkloadPolicy(input PreviewInput, subject domain.Subject, device domain.Device, site domain.Site, space domain.Space, resource domain.Resource, snapshot domain.PolicySnapshot) string {
	if !workloadResourceEligible(input, subject, device, site, space, resource) {
		return ""
	}

	var selected *domain.Policy
	for i := range snapshot.Policies {
		policy := &snapshot.Policies[i]
		if !policy.Enabled || !matchesNetworkPolicy(*policy, input, subject, device, site, resource) {
			continue
		}
		if policy.Effect == domain.PolicyEffectDeny {
			return ""
		}
		if explicitWorkloadAllow(*policy, subject, device, site, resource) && (selected == nil || policy.Priority < selected.Priority) {
			selected = policy
		}
	}
	if selected == nil || selected.AccessProfile == domain.ProfileDeny {
		return ""
	}
	return selected.AccessProfile
}

func workloadResourceEligible(input PreviewInput, subject domain.Subject, device domain.Device, site domain.Site, space domain.Space, resource domain.Resource) bool {
	if !strings.HasPrefix(subject.UserID, "service_account:") || subject.Status != domain.StatusActive || device.Status != domain.DeviceStatusActive || site.Status != domain.StatusActive || space.Status != domain.StatusActive || space.SiteID != site.ID || resource.SpaceID != space.ID || input.Mode != domain.ModeExternalDirectZTNA {
		return false
	}
	if reason := resourceGovernanceReason(site, device, resource, false, true); reason != "" {
		return false
	}
	if resource.PathMode != "" && resource.PathMode != domain.PathAutomatic && resource.PathMode != domain.PathWireGuardZTNA {
		return false
	}
	return true
}

func explicitWorkloadAllow(policy domain.Policy, subject domain.Subject, device domain.Device, site domain.Site, resource domain.Resource) bool {
	return policy.Effect == domain.PolicyEffectAllow && slices.Contains(policy.Subjects.Users, subject.UserID) && slices.Contains(policy.ResourceIDs, resource.ID) && governancePolicyAllows(site, device, policy, resource.ID)
}
