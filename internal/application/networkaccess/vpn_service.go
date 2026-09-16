package networkaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	access "github.com/opensoha/soha/internal/domain/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type VPNProfileStore interface {
	ListVPNProfiles(context.Context, domain.VPNDocumentFilter) ([]domain.VPNProfile, error)
	GetVPNProfile(context.Context, string) (domain.VPNProfile, error)
	SaveVPNProfile(context.Context, string, int, domain.VPNProfileConfig, string, time.Time) (domain.VPNProfile, error)
	PublishVPNProfile(context.Context, string, int, int, string, time.Time) (domain.VPNProfile, error)
	DeleteVPNProfile(context.Context, string, int, time.Time) error
	VPNProfileRevisions(context.Context, string) ([]domain.VPNRevision[domain.VPNProfileConfig], error)
}

type VPNSelectionPolicyStore interface {
	ListVPNSelectionPolicies(context.Context, domain.VPNDocumentFilter) ([]domain.VPNSelectionPolicy, error)
	GetVPNSelectionPolicy(context.Context, string) (domain.VPNSelectionPolicy, error)
	SaveVPNSelectionPolicy(context.Context, string, int, domain.VPNSelectionPolicyConfig, string, time.Time) (domain.VPNSelectionPolicy, error)
	PublishVPNSelectionPolicy(context.Context, string, int, int, string, time.Time) (domain.VPNSelectionPolicy, error)
	DeleteVPNSelectionPolicy(context.Context, string, int, time.Time) error
	VPNSelectionPolicyRevisions(context.Context, string) ([]domain.VPNRevision[domain.VPNSelectionPolicyConfig], error)
}

type VPNConnectionStore interface {
	VPNEndpointCredential(context.Context, string, time.Time) (runtime.Credential, error)
	VPNGatewayCandidates(context.Context, domain.VPNProfileConfig, time.Time) ([]runtime.VPNGatewayCandidate, error)
	CreateVPNIntent(context.Context, domain.VPNIntent) error
}

type VPNScopeAuthorizer interface {
	Authorize(context.Context, access.Request) (access.Decision, error)
}

type VPNService struct {
	metrics     VPNMetricsReader
	base        *Service
	profiles    VPNProfileStore
	policies    VPNSelectionPolicyStore
	connections VPNConnectionStore
	scope       VPNScopeAuthorizer
	stepUp      StepUpVerifier
	now         func() time.Time
}

func NewVPNService(base *Service, profiles VPNProfileStore, policies VPNSelectionPolicyStore, connections VPNConnectionStore, scope VPNScopeAuthorizer, stepUp StepUpVerifier) (*VPNService, error) {
	for name, dependency := range map[string]any{"base": base, "profiles": profiles, "policies": policies, "connections": connections, "scope": scope, "stepUp": stepUp} {
		if isNilDependency(dependency) {
			return nil, fmt.Errorf("VPN service: %s dependency is required", name)
		}
	}
	return &VPNService{base: base, profiles: profiles, policies: policies, connections: connections, scope: scope, stepUp: stepUp, now: time.Now}, nil
}

func (s *VPNService) authorize(ctx context.Context, principal identity.Principal, key, kind, id, siteID, spaceID string) error {
	if principal.UserID == "" {
		return apperrors.ErrUnauthorized
	}
	decision, err := s.scope.Authorize(ctx, access.Request{
		Principal: principal, PermissionKey: key,
		Subject:  access.SubjectAttributes{UserID: principal.UserID, Roles: principal.Roles, Teams: principal.Teams, Projects: principal.Projects, Tags: principal.Tags},
		Resource: access.ResourceAttributes{Group: "network_access", Kind: kind, Name: id, Labels: map[string]string{"network.siteId": siteID, "network.spaceId": spaceID}},
		Context:  access.ContextAttributes{Source: "api", OccurredAt: s.now().UTC()},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "vpn_scope_denied", "The VPN resource is outside your permitted scope.", "该 VPN 资源不在你的授权范围内。")
	}
	return nil
}

func (s *VPNService) authorizeProfile(ctx context.Context, p identity.Principal, key string, item domain.VPNProfile) error {
	if err := s.authorize(ctx, p, key, "NetworkVPNProfile", item.ID, item.Configuration.SiteID, item.Configuration.NetworkSpaceID); err != nil {
		return err
	}
	if old := item.PublishedConfiguration; old != nil && (old.SiteID != item.Configuration.SiteID || old.NetworkSpaceID != item.Configuration.NetworkSpaceID) {
		return s.authorize(ctx, p, key, "NetworkVPNProfile", item.ID, old.SiteID, old.NetworkSpaceID)
	}
	return nil
}

func (s *VPNService) ListProfiles(ctx context.Context, p identity.Principal, filter domain.VPNDocumentFilter) ([]domain.VPNProfile, error) {
	const key = "network_access.vpn_profiles.view"
	if err := s.base.authorize(ctx, p, key); err != nil {
		return nil, err
	}
	limit := boundedLimit(filter.Limit)
	filter.Limit = 200
	items := make([]domain.VPNProfile, 0, limit)
	for {
		page, err := s.profiles.ListVPNProfiles(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, item := range page {
			if err := s.authorizeProfile(ctx, p, key, item); errors.Is(err, apperrors.ErrAccessDenied) {
				continue
			} else if err != nil {
				return nil, err
			}
			items = append(items, item)
			if len(items) >= limit {
				return items, nil
			}
		}
		if len(page) < 200 {
			return items, nil
		}
		filter.AfterID = page[len(page)-1].ID
	}
}

func (s *VPNService) GetProfile(ctx context.Context, p identity.Principal, id string) (domain.VPNProfile, error) {
	item, err := s.profiles.GetVPNProfile(ctx, id)
	if err != nil {
		return item, err
	}
	return item, s.authorizeProfile(ctx, p, "network_access.vpn_profiles.view", item)
}

func (s *VPNService) SaveProfile(ctx context.Context, p identity.Principal, id string, expected int, config domain.VPNProfileConfig) (domain.VPNProfile, error) {
	config = normalizeVPNProfile(config)
	key := "network_access.vpn_profiles.update"
	if id == "" {
		if expected != 0 {
			return domain.VPNProfile{}, invalid("new profile expectedRevision must be zero")
		}
		id = uuid.NewString()
		key = "network_access.vpn_profiles.create"
	} else {
		previous, err := s.profiles.GetVPNProfile(ctx, id)
		if err != nil {
			return domain.VPNProfile{}, err
		}
		if err := s.authorizeProfile(ctx, p, key, previous); err != nil {
			return domain.VPNProfile{}, err
		}
		if expected < 1 || previous.Revision != expected {
			return domain.VPNProfile{}, apperrors.ErrConflict
		}
	}
	if err := s.authorize(ctx, p, key, "NetworkVPNProfile", id, config.SiteID, config.NetworkSpaceID); err != nil {
		return domain.VPNProfile{}, err
	}
	if err := s.validateProfile(ctx, config, false); err != nil {
		return domain.VPNProfile{}, err
	}
	item, err := s.profiles.SaveVPNProfile(ctx, id, expected, config, p.UserID, s.now().UTC())
	if err == nil {
		s.base.recordMutation(ctx, p, key, "NetworkVPNProfile", item.ID, item.Configuration.Name)
	}
	return item, err
}

func (s *VPNService) PublishProfile(ctx context.Context, p identity.Principal, id string, expected, target int) (domain.VPNProfile, error) {
	item, err := s.profiles.GetVPNProfile(ctx, id)
	if err != nil {
		return item, err
	}
	const key = "network_access.vpn_profiles.publish"
	if err := s.authorizeProfile(ctx, p, key, item); err != nil {
		return domain.VPNProfile{}, err
	}
	if expected != item.Revision || target < 0 {
		return domain.VPNProfile{}, apperrors.ErrConflict
	}
	config := item.Configuration
	if target > 0 {
		revisions, err := s.profiles.VPNProfileRevisions(ctx, id)
		if err != nil {
			return domain.VPNProfile{}, err
		}
		var found bool
		for _, revision := range revisions {
			if revision.Revision == target {
				config = revision.Configuration
				found = true
				break
			}
		}
		if !found {
			return domain.VPNProfile{}, apperrors.ErrNotFound
		}
	}
	if err := s.authorize(ctx, p, key, "NetworkVPNProfile", id, config.SiteID, config.NetworkSpaceID); err != nil {
		return domain.VPNProfile{}, err
	}
	if err := s.validateProfile(ctx, config, true); err != nil {
		return domain.VPNProfile{}, err
	}
	item, err = s.profiles.PublishVPNProfile(ctx, id, expected, target, p.UserID, s.now().UTC())
	if err == nil {
		s.base.recordMutation(ctx, p, key, "NetworkVPNProfile", item.ID, item.Configuration.Name)
	}
	return item, err
}

func (s *VPNService) DeleteProfile(ctx context.Context, p identity.Principal, id string, expected int) error {
	item, err := s.profiles.GetVPNProfile(ctx, id)
	if err != nil {
		return err
	}
	const key = "network_access.vpn_profiles.delete"
	if err := s.authorizeProfile(ctx, p, key, item); err != nil {
		return err
	}
	if err := s.profiles.DeleteVPNProfile(ctx, id, expected, s.now().UTC()); err != nil {
		return err
	}
	s.base.recordMutation(ctx, p, key, "NetworkVPNProfile", id, item.Configuration.Name)
	return nil
}

func (s *VPNService) ProfileRevisions(ctx context.Context, p identity.Principal, id string) ([]domain.VPNRevision[domain.VPNProfileConfig], error) {
	if _, err := s.GetProfile(ctx, p, id); err != nil {
		return nil, err
	}
	items, err := s.profiles.VPNProfileRevisions(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]domain.VPNRevision[domain.VPNProfileConfig], 0, len(items))
	for _, item := range items {
		if err := s.authorize(ctx, p, "network_access.vpn_profiles.view", "NetworkVPNProfile", id, item.Configuration.SiteID, item.Configuration.NetworkSpaceID); errors.Is(err, apperrors.ErrAccessDenied) {
			continue
		} else if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *VPNService) ListSelectionPolicies(ctx context.Context, p identity.Principal, filter domain.VPNDocumentFilter) ([]domain.VPNSelectionPolicy, error) {
	const key = "network_access.vpn_selection_policies.view"
	if err := s.base.authorize(ctx, p, key); err != nil {
		return nil, err
	}
	limit := boundedLimit(filter.Limit)
	filter.Limit = 200
	items := make([]domain.VPNSelectionPolicy, 0, limit)
	for {
		page, err := s.policies.ListVPNSelectionPolicies(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, item := range page {
			if err := s.authorize(ctx, p, key, "NetworkVPNSelectionPolicy", item.ID, "", ""); errors.Is(err, apperrors.ErrAccessDenied) {
				continue
			} else if err != nil {
				return nil, err
			}
			items = append(items, item)
			if len(items) >= limit {
				return items, nil
			}
		}
		if len(page) < 200 {
			return items, nil
		}
		filter.AfterID = page[len(page)-1].ID
	}
}

func (s *VPNService) GetSelectionPolicy(ctx context.Context, p identity.Principal, id string) (domain.VPNSelectionPolicy, error) {
	if err := s.authorize(ctx, p, "network_access.vpn_selection_policies.view", "NetworkVPNSelectionPolicy", id, "", ""); err != nil {
		return domain.VPNSelectionPolicy{}, err
	}
	return s.policies.GetVPNSelectionPolicy(ctx, id)
}

func (s *VPNService) SaveSelectionPolicy(ctx context.Context, p identity.Principal, id string, expected int, config domain.VPNSelectionPolicyConfig) (domain.VPNSelectionPolicy, error) {
	key := "network_access.vpn_selection_policies.update"
	if id == "" {
		if expected != 0 {
			return domain.VPNSelectionPolicy{}, invalid("new policy expectedRevision must be zero")
		}
		id = uuid.NewString()
		key = "network_access.vpn_selection_policies.create"
	} else if expected < 1 {
		return domain.VPNSelectionPolicy{}, apperrors.ErrConflict
	}
	if err := s.authorize(ctx, p, key, "NetworkVPNSelectionPolicy", id, "", ""); err != nil {
		return domain.VPNSelectionPolicy{}, err
	}
	config.Name = strings.TrimSpace(config.Name)
	if err := validateVPNSelectionPolicy(config); err != nil {
		return domain.VPNSelectionPolicy{}, err
	}
	item, err := s.policies.SaveVPNSelectionPolicy(ctx, id, expected, config, p.UserID, s.now().UTC())
	if err == nil {
		s.base.recordMutation(ctx, p, key, "NetworkVPNSelectionPolicy", id, config.Name)
	}
	return item, err
}

func (s *VPNService) PublishSelectionPolicy(ctx context.Context, p identity.Principal, id string, expected, target int) (domain.VPNSelectionPolicy, error) {
	const key = "network_access.vpn_selection_policies.publish"
	if err := s.authorize(ctx, p, key, "NetworkVPNSelectionPolicy", id, "", ""); err != nil {
		return domain.VPNSelectionPolicy{}, err
	}
	if target < 0 || expected < 1 {
		return domain.VPNSelectionPolicy{}, invalid("revision is invalid")
	}
	if err := s.authorizePolicyReferences(ctx, p, id, key); err != nil {
		return domain.VPNSelectionPolicy{}, err
	}
	item, err := s.policies.PublishVPNSelectionPolicy(ctx, id, expected, target, p.UserID, s.now().UTC())
	if err == nil {
		s.base.recordMutation(ctx, p, key, "NetworkVPNSelectionPolicy", id, item.Configuration.Name)
	}
	return item, err
}

func (s *VPNService) DeleteSelectionPolicy(ctx context.Context, p identity.Principal, id string, expected int) error {
	const key = "network_access.vpn_selection_policies.delete"
	if err := s.authorize(ctx, p, key, "NetworkVPNSelectionPolicy", id, "", ""); err != nil {
		return err
	}
	if err := s.policies.DeleteVPNSelectionPolicy(ctx, id, expected, s.now().UTC()); err != nil {
		return err
	}
	s.base.recordMutation(ctx, p, key, "NetworkVPNSelectionPolicy", id, id)
	return nil
}

func (s *VPNService) SelectionPolicyRevisions(ctx context.Context, p identity.Principal, id string) ([]domain.VPNRevision[domain.VPNSelectionPolicyConfig], error) {
	if _, err := s.GetSelectionPolicy(ctx, p, id); err != nil {
		return nil, err
	}
	return s.policies.VPNSelectionPolicyRevisions(ctx, id)
}

func (s *VPNService) authorizePolicyReferences(ctx context.Context, p identity.Principal, id, key string) error {
	filter := domain.VPNDocumentFilter{Limit: 200}
	for {
		items, err := s.profiles.ListVPNProfiles(ctx, filter)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.Configuration.SelectionPolicyID == id || (item.PublishedConfiguration != nil && item.PublishedConfiguration.SelectionPolicyID == id) {
				if err := s.authorizeProfile(ctx, p, key, item); err != nil {
					return err
				}
			}
		}
		if len(items) < 200 {
			return nil
		}
		filter.AfterID = items[len(items)-1].ID
	}
}
