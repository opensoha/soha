package networkaccess

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type AccessNetworkCatalog interface {
	ListAccessNetworks(context.Context, string) ([]domain.AccessNetwork, error)
	GetAccessNetwork(context.Context, string) (domain.AccessNetwork, error)
	SaveAccessNetwork(context.Context, domain.AccessNetwork, bool) (domain.AccessNetwork, error)
	DeleteAccessNetwork(context.Context, string, time.Time) error
	LatestAccessNetworkNASSessions(ctx context.Context, device domain.Device, networkIDs []string) (map[string]domain.ConnectionNASSession, error)
	EndpointPresence(context.Context, string) (domain.PresenceRecord, error)
}

func (s *Service) SetAccessNetworkCatalog(catalog AccessNetworkCatalog) {
	s.accessNetworks = catalog
}

func (s *Service) authorizeAccessNetworkSite(ctx context.Context, principal identity.Principal, permission, siteID string) error {
	if err := s.authorize(ctx, principal, permission); err != nil {
		return err
	}
	if !runtimeIdentifierPattern.MatchString(siteID) {
		return invalid("siteId is invalid")
	}
	if s.accessNetworks == nil {
		return apperrors.ErrServiceUnavailable
	}
	return s.authorizeScope(ctx, principal, permission, resourceScope{kind: "NetworkSite", id: siteID, siteID: siteID})
}

func (s *Service) ListAccessNetworks(ctx context.Context, principal identity.Principal, siteID string) ([]domain.AccessNetwork, error) {
	if err := s.authorizeAccessNetworkSite(ctx, principal, appaccess.PermNetworkAccessSitesView, siteID); err != nil {
		return nil, err
	}
	items, err := s.accessNetworks.ListAccessNetworks(ctx, siteID)
	return slices.DeleteFunc(items, func(item domain.AccessNetwork) bool { return item.Deleted }), err
}

func (s *Service) SaveAccessNetwork(ctx context.Context, principal identity.Principal, id string, input domain.AccessNetworkInput) (domain.AccessNetwork, error) {
	if err := s.authorizeAccessNetworkSite(ctx, principal, appaccess.PermNetworkAccessSitesUpdate, input.SiteID); err != nil {
		return domain.AccessNetwork{}, err
	}
	if err := validateAccessNetwork(&input); err != nil {
		return domain.AccessNetwork{}, err
	}
	now := time.Now().UTC()
	item := domain.AccessNetwork{ID: id, Configuration: input, CreatedAt: now, UpdatedAt: now}
	create := id == ""
	if create {
		item.ID = uuid.NewString()
	} else {
		existing, err := s.accessNetworks.GetAccessNetwork(ctx, id)
		if err != nil {
			return domain.AccessNetwork{}, err
		}
		if existing.Configuration.SiteID != input.SiteID {
			return domain.AccessNetwork{}, apperrors.ErrNotFound
		}
		item.CreatedAt = existing.CreatedAt
	}
	result, err := s.accessNetworks.SaveAccessNetwork(ctx, item, create)
	if err == nil {
		action := "update"
		if create {
			action = "create"
		}
		s.recordMutation(ctx, principal, "network_access.access_networks."+action, "NetworkAccessNetwork", result.ID, input.Name)
	}
	return result, err
}

func (s *Service) DeleteAccessNetwork(ctx context.Context, principal identity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return err
	}
	if s.accessNetworks == nil {
		return apperrors.ErrServiceUnavailable
	}
	item, err := s.accessNetworks.GetAccessNetwork(ctx, id)
	if err != nil {
		return err
	}
	if err := s.authorizeAccessNetworkSite(ctx, principal, appaccess.PermNetworkAccessSitesUpdate, item.Configuration.SiteID); err != nil {
		return err
	}
	if err := s.accessNetworks.DeleteAccessNetwork(ctx, id, time.Now().UTC()); err != nil {
		return err
	}
	s.recordMutation(ctx, principal, "network_access.access_networks.delete", "NetworkAccessNetwork", item.ID, item.Configuration.Name)
	return nil
}

func (s *Service) PreviewAccessNetworks(ctx context.Context, principal identity.Principal, siteID, userID, deviceID, method string, draft *domain.AccessNetworkInput) ([]AccessNetworkDecision, error) {
	items, err := s.ListAccessNetworks(ctx, principal, siteID)
	if err != nil {
		return nil, err
	}
	if draft != nil {
		item, err := accessNetworkPreviewDraft(siteID, draft)
		if err != nil {
			return nil, err
		}
		items = []domain.AccessNetwork{item}
	}
	if !runtimeIdentifierPattern.MatchString(userID) || !runtimeIdentifierPattern.MatchString(deviceID) {
		return nil, invalid("userId or deviceId is invalid")
	}
	if method == "" {
		method = "eap_tls"
	}
	if method != "eap_tls" && method != "eap_ttls_pap" {
		return nil, invalid("authenticationMethod is invalid")
	}
	subject, err := s.store.GetSubject(ctx, userID)
	if err != nil {
		return nil, err
	}
	device, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if device.SiteID != siteID || device.OwnerUserID != userID {
		return nil, fmt.Errorf("%w: device does not belong to the selected user and site", apperrors.ErrNotFound)
	}
	site, err := s.store.GetSite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.store.GetPolicySnapshot(ctx)
	if err != nil {
		return nil, err
	}
	bindings, err := s.activeAccessNetworkBindings(ctx, siteID)
	if err != nil {
		return nil, err
	}
	result := make([]AccessNetworkDecision, 0, len(items))
	now := time.Now()
	for _, item := range items {
		available := hasActiveAccessNetworkBinding(item, bindings, method)
		decision := evaluateAccessNetwork(now, item, method, subject, device, site, snapshot, available)
		if decision.Allowed && !available {
			decision.Allowed, decision.ReasonCode, decision.AccessProfile = false, "access_network_nas_unavailable", ""
		}
		decision, err = CheckAccessNetworkProfile(ctx, decision, method, siteID, s.store.FindSiteProfileBinding)
		if err != nil {
			return nil, err
		}
		result = append(result, decision)
	}
	return result, nil
}

func accessNetworkPreviewDraft(siteID string, draft *domain.AccessNetworkInput) (domain.AccessNetwork, error) {
	if draft.SiteID != siteID {
		return domain.AccessNetwork{}, invalid("draft site does not match preview site")
	}
	if err := validateAccessNetwork(draft); err != nil {
		return domain.AccessNetwork{}, err
	}
	if draft.Kind == domain.AccessNetworkStandard {
		draft.Enabled = true
	}
	return domain.AccessNetwork{ID: "draft", Configuration: *draft}, nil
}

func (s *Service) activeAccessNetworkBindings(ctx context.Context, siteID string) (map[string]domain.NASBinding, error) {
	active := map[string]domain.NASBinding{}
	const limit = 200
	for offset := 0; ; offset += limit {
		items, err := s.store.ListNASBindings(ctx, domain.NASBindingFilter{SiteID: siteID, Status: domain.StatusActive, Limit: limit, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.Status == domain.StatusActive && item.SiteID == siteID {
				active[item.ID] = item
			}
		}
		if len(items) < limit {
			return active, nil
		}
	}
}

func hasActiveAccessNetworkBinding(item domain.AccessNetwork, active map[string]domain.NASBinding, method string) bool {
	for _, id := range item.Configuration.NASBindingIDs {
		if binding, ok := active[id]; ok && item.ActiveNASBinding(binding) && bindingAdmissionMethod(binding) == method {
			return true
		}
	}
	return false
}

func bindingAdmissionMethod(binding domain.NASBinding) string {
	if binding.EAPServerTrust != nil && binding.EAPServerTrust.EapMethod == "ttls" {
		return "eap_ttls_pap"
	}
	return "eap_tls"
}
