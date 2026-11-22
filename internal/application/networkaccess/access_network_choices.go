package networkaccess

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type BootstrapNetworkChoice struct {
	AccessNetworkID string `json:"accessNetworkId"`
	SiteID          string `json:"siteId"`
	Name            string `json:"name"`
	SSID            string `json:"ssid"`
	Priority        int    `json:"priority"`
	AutoConnect     bool   `json:"autoConnect"`
}

type AccessNetworkChoiceOptions struct {
	IncludeBootstrap  bool
	IncludeNASSession bool
}

func (s *Service) ListConnectionOptions(ctx context.Context, principal identity.Principal, deviceID string) ([]domain.ConnectionOption, error) {
	items, _, err := s.ListAccessNetworkChoices(ctx, principal, deviceID, AccessNetworkChoiceOptions{})
	return items, err
}

func (s *Service) ListAccessNetworkChoices(ctx context.Context, principal identity.Principal, deviceID string, options AccessNetworkChoiceOptions) ([]domain.ConnectionOption, []BootstrapNetworkChoice, error) {
	if s.accessNetworks == nil {
		items, err := s.legacyConnectionOptions(ctx, principal, deviceID)
		return items, nil, err
	}
	device, err := s.connectionOptionDevice(ctx, principal, deviceID)
	if err != nil {
		return nil, nil, err
	}
	if device.SiteID == "" || device.Status != domain.DeviceStatusActive {
		return []domain.ConnectionOption{}, nil, nil
	}
	networks, err := s.accessNetworks.ListAccessNetworks(ctx, device.SiteID)
	if err != nil {
		return nil, nil, err
	}
	if len(networks) == 0 {
		items, err := s.legacyConnectionOptions(ctx, principal, deviceID)
		return items, nil, err
	}
	subject, err := s.store.GetSubject(ctx, device.OwnerUserID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return []domain.ConnectionOption{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if subject.Status != domain.StatusActive || subject.UserID != device.OwnerUserID {
		return []domain.ConnectionOption{}, nil, nil
	}
	site, err := s.store.GetSite(ctx, device.SiteID)
	if err != nil {
		return nil, nil, err
	}
	if site.Status != domain.StatusActive {
		return []domain.ConnectionOption{}, nil, nil
	}
	items, bootstrap, err := s.accessNetworkChoices(ctx, principal, site, subject, device, networks, options.IncludeBootstrap)
	if err != nil {
		return items, bootstrap, err
	}
	if options.IncludeBootstrap && slices.ContainsFunc(items, func(item domain.ConnectionOption) bool { return item.StrictOnline != nil }) {
		presence, err := s.accessNetworks.EndpointPresence(ctx, device.ID)
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return nil, nil, err
		}
		if presence.StrictUI.CurrentFor(device.OwnerUserID, time.Now()) {
			for _, item := range items {
				if item.StrictOnline != nil {
					item.StrictOnline.LeaseUntil = presence.StrictUI.Until
				}
			}
		}
	}
	if options.IncludeNASSession {
		err = s.readNASSessionChoices(ctx, device, items)
	}
	return items, bootstrap, err
}

func (s *Service) readNASSessionChoices(ctx context.Context, device domain.Device, items []domain.ConnectionOption) error {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.AccessNetworkID != "" {
			ids = append(ids, item.AccessNetworkID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sessions, err := s.accessNetworks.LatestAccessNetworkNASSessions(ctx, device, ids)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].AccessNetworkID == "" {
			continue
		}
		session := sessions[items[i].AccessNetworkID]
		if session.State == "" {
			session.State = "pending"
		}
		items[i].NASSession = &session
	}
	return nil
}

func (s *Service) connectionOptionDevice(ctx context.Context, principal identity.Principal, deviceID string) (domain.Device, error) {
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return domain.Device{}, fmt.Errorf("%w: authenticated user id is required", apperrors.ErrUnauthorized)
	}
	deviceID = strings.TrimSpace(deviceID)
	if !runtimeIdentifierPattern.MatchString(deviceID) {
		return domain.Device{}, invalid("deviceId is invalid")
	}
	device, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return domain.Device{}, err
	}
	if device.OwnerUserID != userID {
		return domain.Device{}, apperrors.NewBusiness(apperrors.ErrAccessDenied, "network_device_access_denied", "The endpoint does not belong to the current user.", "该终端不属于当前用户。")
	}
	return device, nil
}

func (s *Service) accessNetworkChoices(ctx context.Context, principal identity.Principal, site domain.Site, subject domain.Subject, device domain.Device, networks []domain.AccessNetwork, includeBootstrap bool) ([]domain.ConnectionOption, []BootstrapNetworkChoice, error) {
	bootstrap := make([]BootstrapNetworkChoice, 0)
	slices.SortFunc(networks, func(left, right domain.AccessNetwork) int {
		if delta := left.Configuration.Priority - right.Configuration.Priority; delta != 0 {
			return delta
		}
		return strings.Compare(left.ID, right.ID)
	})
	for _, item := range networks {
		cfg := item.Configuration
		if includeBootstrap && !item.Deleted && cfg.Kind == domain.AccessNetworkBootstrap && cfg.Enabled {
			bootstrap = append(bootstrap, BootstrapNetworkChoice{AccessNetworkID: item.ID, SiteID: site.ID, Name: cfg.Name, SSID: cfg.SSID, Priority: cfg.Priority, AutoConnect: cfg.AutoConnect})
		}
	}
	_, managed := MatchAccessNetwork(networks, domain.NASBinding{SiteID: site.ID}, AccessAttachment{})
	if !managed {
		items, err := s.legacyConnectionOptions(ctx, principal, device.ID)
		return items, bootstrap, err
	}
	snapshot, err := s.store.GetPolicySnapshot(ctx)
	if errors.Is(err, apperrors.ErrNotFound) {
		return []domain.ConnectionOption{}, bootstrap, nil
	}
	if err != nil {
		return nil, nil, err
	}
	bindings, err := s.activeAccessNetworkBindings(ctx, site.ID)
	if err != nil {
		return nil, nil, err
	}
	items := make([]domain.ConnectionOption, 0)
	now := time.Now()
	for _, item := range networks {
		cfg := item.Configuration
		decision, err := s.evaluateAvailableAccessNetwork(ctx, now, item, subject, device, site, snapshot, bindings)
		if err != nil {
			return nil, nil, err
		}
		if decision.Allowed {
			option := domain.ConnectionOption{
				SiteID: site.ID, SiteName: site.Name, AccessMedium: cfg.AccessMedium, SSID: cfg.SSID,
				Authentication: domain.ConnectionAuthenticationRadius8021X, AccessProfile: decision.AccessProfile, PolicyVersion: snapshot.PolicyVersion,
				AccessNetworkID: item.ID, NetworkName: cfg.Name, RecommendationPriority: cfg.Priority, AutoConnect: cfg.AutoConnect,
			}
			if includeBootstrap && decision.StrictOnline {
				option.StrictOnline = &domain.ConnectionStrictOnline{}
			}
			items = append(items, option)
		}
	}
	return items, bootstrap, nil
}

func (s *Service) evaluateAvailableAccessNetwork(ctx context.Context, now time.Time, item domain.AccessNetwork, subject domain.Subject, device domain.Device, site domain.Site, snapshot domain.PolicySnapshot, bindings map[string]domain.NASBinding) (AccessNetworkDecision, error) {
	for _, method := range []string{"eap_tls", "eap_ttls_pap"} {
		if hasActiveAccessNetworkBinding(item, bindings, method) {
			decision, err := CheckAccessNetworkProfile(ctx, evaluateAccessNetwork(now, item, method, subject, device, site, snapshot, item.Configuration.Kind == domain.AccessNetworkStrict), method, site.ID, s.store.FindSiteProfileBinding)
			if err != nil || decision.Allowed {
				return decision, err
			}
		}
	}
	return AccessNetworkDecision{}, nil
}
