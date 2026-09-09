package networkaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type Store interface {
	ListDevices(context.Context, domainnetworkaccess.DeviceFilter) ([]domainnetworkaccess.Device, error)
	GetDevice(context.Context, string) (domainnetworkaccess.Device, error)
	RegisterDevice(context.Context, domainnetworkaccess.Device) (domainnetworkaccess.Device, error)
	UpdateDevice(context.Context, string, domainnetworkaccess.DeviceInput, time.Time) (domainnetworkaccess.Device, error)
	ListSites(context.Context, domainnetworkaccess.SiteFilter) ([]domainnetworkaccess.Site, error)
	GetSite(context.Context, string) (domainnetworkaccess.Site, error)
	CreateSite(context.Context, domainnetworkaccess.Site) (domainnetworkaccess.Site, error)
	UpdateSite(context.Context, string, domainnetworkaccess.SiteInput, time.Time) (domainnetworkaccess.Site, error)
	DeleteSite(context.Context, string) error
	ListSpaces(context.Context, domainnetworkaccess.SpaceFilter) ([]domainnetworkaccess.Space, error)
	GetSpace(context.Context, string) (domainnetworkaccess.Space, error)
	CreateSpace(context.Context, domainnetworkaccess.Space) (domainnetworkaccess.Space, error)
	UpdateSpace(context.Context, string, domainnetworkaccess.SpaceInput, time.Time) (domainnetworkaccess.Space, error)
	DeleteSpace(context.Context, string) error
	ListResources(context.Context, domainnetworkaccess.ResourceFilter) ([]domainnetworkaccess.Resource, error)
	GetResource(context.Context, string) (domainnetworkaccess.Resource, error)
	CreateResource(context.Context, domainnetworkaccess.Resource) (domainnetworkaccess.Resource, error)
	UpdateResource(context.Context, string, domainnetworkaccess.ResourceInput, time.Time) (domainnetworkaccess.Resource, error)
	DeleteResource(context.Context, string) error
	ListGateways(context.Context, domainnetworkaccess.GatewayFilter) ([]domainnetworkaccess.Gateway, error)
	GetGateway(context.Context, string) (domainnetworkaccess.Gateway, error)
	CreateGateway(context.Context, domainnetworkaccess.Gateway) (domainnetworkaccess.Gateway, error)
	UpdateGateway(context.Context, string, domainnetworkaccess.GatewayInput, time.Time) (domainnetworkaccess.Gateway, error)
	ListMihomoProfiles(context.Context, domainnetworkaccess.MihomoProfileFilter) ([]domainnetworkaccess.MihomoProfile, error)
	GetMihomoProfile(context.Context, string) (domainnetworkaccess.MihomoProfile, error)
	CreateMihomoProfile(context.Context, domainnetworkaccess.MihomoProfile) (domainnetworkaccess.MihomoProfile, error)
	UpdateMihomoProfile(context.Context, string, domainnetworkaccess.MihomoProfile, time.Time) (domainnetworkaccess.MihomoProfile, error)
	DeleteMihomoProfile(context.Context, string) error
	ListNASBindings(context.Context, domainnetworkaccess.NASBindingFilter) ([]domainnetworkaccess.NASBinding, error)
	GetNASBinding(context.Context, string) (domainnetworkaccess.NASBinding, error)
	CreateNASBinding(context.Context, domainnetworkaccess.NASBinding) (domainnetworkaccess.NASBinding, error)
	UpdateNASBinding(context.Context, string, domainnetworkaccess.NASBindingInput, time.Time) (domainnetworkaccess.NASBinding, error)
	DeleteNASBinding(context.Context, string) error
	ListSiteProfileBindings(context.Context, domainnetworkaccess.SiteProfileBindingFilter) ([]domainnetworkaccess.SiteProfileBinding, error)
	GetSiteProfileBinding(context.Context, string) (domainnetworkaccess.SiteProfileBinding, error)
	CreateSiteProfileBinding(context.Context, domainnetworkaccess.SiteProfileBinding) (domainnetworkaccess.SiteProfileBinding, error)
	UpdateSiteProfileBinding(context.Context, string, domainnetworkaccess.SiteProfileBindingInput, time.Time) (domainnetworkaccess.SiteProfileBinding, error)
	DeleteSiteProfileBinding(context.Context, string) error
	ListSessions(context.Context, domainnetworkaccess.SessionFilter) ([]domainnetworkaccess.Session, error)
	GetSession(context.Context, string) (domainnetworkaccess.Session, error)
	FindActiveNASBinding(context.Context, string, string) (domainnetworkaccess.NASBinding, error)
	FindSiteProfileBinding(context.Context, string, string) (domainnetworkaccess.SiteProfileBinding, error)
	CreateSessionCommand(context.Context, domainnetworkaccess.SessionCommand) (domainnetworkaccess.SessionCommand, error)
	GetSubject(context.Context, string) (domainnetworkaccess.Subject, error)
	ListPolicies(context.Context, domainnetworkaccess.PolicyFilter) ([]domainnetworkaccess.Policy, error)
	GetPolicy(context.Context, string) (domainnetworkaccess.Policy, error)
	CreatePolicy(context.Context, domainnetworkaccess.Policy) (domainnetworkaccess.Policy, error)
	UpdatePolicy(context.Context, string, domainnetworkaccess.PolicyInput, time.Time) (domainnetworkaccess.Policy, error)
	DeletePolicy(context.Context, string) error
	ListPoliciesForSnapshot(context.Context) ([]domainnetworkaccess.Policy, error)
	ListProtectedResourceIDs(context.Context) ([]string, error)
	GetPolicySnapshot(context.Context) (domainnetworkaccess.PolicySnapshot, error)
	PublishPolicySnapshot(context.Context, domainnetworkaccess.PolicySnapshot) (domainnetworkaccess.PolicySnapshot, error)
	ListConflictRanges(context.Context) ([]domainnetworkaccess.ConflictRange, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	store       Store
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
	keys        keyring.Ring
}

func New(store Store, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder, keys keyring.Ring) (*Service, error) {
	for name, dependency := range map[string]any{
		"store": store, "permissions": permissions, "audit": audit, "operations": operations,
	} {
		if isNilDependency(dependency) {
			return nil, fmt.Errorf("network access service: %s dependency is required", name)
		}
	}
	if keys.Active().ID() == "" {
		return nil, fmt.Errorf("network access service: credential encryption key is required")
	}
	return &Service{store: store, permissions: permissions, audit: audit, operations: operations, keys: keys}, nil
}

func (s *Service) ListDevices(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.DeviceFilter) ([]domainnetworkaccess.Device, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessEndpointDevicesView); err != nil {
		return nil, err
	}
	filter.Search, filter.OwnerUserID, filter.SiteID, filter.Status = strings.TrimSpace(filter.Search), strings.TrimSpace(filter.OwnerUserID), strings.TrimSpace(filter.SiteID), strings.TrimSpace(filter.Status)
	if err := validateDeviceFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListDevices(ctx, filter)
}

func (s *Service) GetDevice(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.Device, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessEndpointDevicesView); err != nil {
		return domainnetworkaccess.Device{}, err
	}
	return s.store.GetDevice(ctx, strings.TrimSpace(id))
}

func (s *Service) RegisterDevice(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.DeviceRegistrationInput) (domainnetworkaccess.Device, error) {
	ownerUserID := strings.TrimSpace(principal.UserID)
	if ownerUserID == "" {
		return domainnetworkaccess.Device{}, fmt.Errorf("%w: authenticated user id is required", apperrors.ErrUnauthorized)
	}
	id = strings.TrimSpace(id)
	input = normalizeDeviceRegistrationInput(input)
	if input.DeviceType == "" {
		input.DeviceType = domainnetworkaccess.DeviceTypeUnknown
	}
	if err := validateDeviceRegistration(id, input); err != nil {
		return domainnetworkaccess.Device{}, err
	}
	now := time.Now().UTC()
	item, err := s.store.RegisterDevice(ctx, domainnetworkaccess.Device{
		ID: id, OwnerUserID: ownerUserID, Name: input.Name, Hostname: input.Hostname, Platform: input.Platform,
		DeviceType: input.DeviceType, OwnershipType: domainnetworkaccess.DeviceOwnershipUnassigned, ReportedFacts: input.ReportedFacts,
		Status: domainnetworkaccess.DeviceStatusPending, PostureStatus: domainnetworkaccess.PostureUnknown,
		PostureVersion: 1, CredentialGeneration: 1, LastSeenAt: &now, CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.endpoint_devices.register", "EndpointDevice", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) UpdateDevice(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.DeviceInput) (domainnetworkaccess.Device, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessEndpointDevicesUpdate); err != nil {
		return domainnetworkaccess.Device{}, err
	}
	input.Name, input.SiteID, input.Status, input.PostureStatus, input.DeviceType, input.OwnershipType = strings.TrimSpace(input.Name), strings.TrimSpace(input.SiteID), strings.TrimSpace(input.Status), strings.TrimSpace(input.PostureStatus), strings.TrimSpace(input.DeviceType), strings.TrimSpace(input.OwnershipType)
	if err := validateDeviceInput(input); err != nil {
		return domainnetworkaccess.Device{}, err
	}
	item, err := s.store.UpdateDevice(ctx, strings.TrimSpace(id), input, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.endpoint_devices.update", "EndpointDevice", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) ListSites(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.SiteFilter) ([]domainnetworkaccess.Site, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return nil, err
	}
	filter.Search, filter.Status = strings.TrimSpace(filter.Search), strings.TrimSpace(filter.Status)
	if err := validateSiteFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListSites(ctx, filter)
}

func (s *Service) GetSite(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.Site, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return domainnetworkaccess.Site{}, err
	}
	return s.store.GetSite(ctx, strings.TrimSpace(id))
}

func (s *Service) CreateSite(ctx context.Context, principal domainidentity.Principal, input domainnetworkaccess.SiteInput) (domainnetworkaccess.Site, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesCreate); err != nil {
		return domainnetworkaccess.Site{}, err
	}
	input = normalizeSiteInput(input)
	if err := validateSiteInput(input); err != nil {
		return domainnetworkaccess.Site{}, err
	}
	now := time.Now().UTC()
	item := domainnetworkaccess.Site{ID: uuid.NewString(), Name: input.Name, Description: input.Description, Location: input.Location, Status: input.Status, CreatedAt: now, UpdatedAt: now}
	item, err := s.store.CreateSite(ctx, item)
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.sites.create", "NetworkSite", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) UpdateSite(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.SiteInput) (domainnetworkaccess.Site, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return domainnetworkaccess.Site{}, err
	}
	input = normalizeSiteInput(input)
	if err := validateSiteInput(input); err != nil {
		return domainnetworkaccess.Site{}, err
	}
	item, err := s.store.UpdateSite(ctx, strings.TrimSpace(id), input, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.sites.update", "NetworkSite", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) DeleteSite(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesDelete); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if err := s.store.DeleteSite(ctx, id); err != nil {
		return err
	}
	s.recordMutation(ctx, principal, "network_access.sites.delete", "NetworkSite", id, id)
	return nil
}

func (s *Service) ListSpaces(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.SpaceFilter) ([]domainnetworkaccess.Space, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSpacesView); err != nil {
		return nil, err
	}
	filter.Search, filter.SiteID, filter.Status = strings.TrimSpace(filter.Search), strings.TrimSpace(filter.SiteID), strings.TrimSpace(filter.Status)
	if err := validateSpaceFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListSpaces(ctx, filter)
}

func (s *Service) GetSpace(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.Space, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSpacesView); err != nil {
		return domainnetworkaccess.Space{}, err
	}
	return s.store.GetSpace(ctx, strings.TrimSpace(id))
}

func (s *Service) CreateSpace(ctx context.Context, principal domainidentity.Principal, input domainnetworkaccess.SpaceInput) (domainnetworkaccess.Space, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSpacesCreate); err != nil {
		return domainnetworkaccess.Space{}, err
	}
	input = normalizeSpaceInput(input)
	if err := validateSpaceInput(input); err != nil {
		return domainnetworkaccess.Space{}, err
	}
	now := time.Now().UTC()
	item, err := s.store.CreateSpace(ctx, domainnetworkaccess.Space{ID: uuid.NewString(), SiteID: input.SiteID, Name: input.Name, CIDRs: input.CIDRs, Status: input.Status, CreatedAt: now, UpdatedAt: now})
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.spaces.create", "NetworkSpace", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) UpdateSpace(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.SpaceInput) (domainnetworkaccess.Space, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSpacesUpdate); err != nil {
		return domainnetworkaccess.Space{}, err
	}
	input = normalizeSpaceInput(input)
	if err := validateSpaceInput(input); err != nil {
		return domainnetworkaccess.Space{}, err
	}
	item, err := s.store.UpdateSpace(ctx, strings.TrimSpace(id), input, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.spaces.update", "NetworkSpace", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) DeleteSpace(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSpacesDelete); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if err := s.store.DeleteSpace(ctx, id); err != nil {
		return err
	}
	s.recordMutation(ctx, principal, "network_access.spaces.delete", "NetworkSpace", id, id)
	return nil
}

func (s *Service) ListResources(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.ResourceFilter) ([]domainnetworkaccess.Resource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessResourcesView); err != nil {
		return nil, err
	}
	filter.Search, filter.SpaceID, filter.Kind = strings.TrimSpace(filter.Search), strings.TrimSpace(filter.SpaceID), strings.TrimSpace(filter.Kind)
	if err := validateResourceFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListResources(ctx, filter)
}

func (s *Service) GetResource(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.Resource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessResourcesView); err != nil {
		return domainnetworkaccess.Resource{}, err
	}
	return s.store.GetResource(ctx, strings.TrimSpace(id))
}

func (s *Service) CreateResource(ctx context.Context, principal domainidentity.Principal, input domainnetworkaccess.ResourceInput) (domainnetworkaccess.Resource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessResourcesCreate); err != nil {
		return domainnetworkaccess.Resource{}, err
	}
	input = normalizeResourceInput(input)
	if err := validateResourceInput(input); err != nil {
		return domainnetworkaccess.Resource{}, err
	}
	now := time.Now().UTC()
	item, err := s.store.CreateResource(ctx, domainnetworkaccess.Resource{ID: uuid.NewString(), SpaceID: input.SpaceID, Name: input.Name, Kind: input.Kind, Target: input.Target, Protocol: input.Protocol, Ports: input.Ports, Protected: input.Protected, PathMode: input.PathMode, CreatedAt: now, UpdatedAt: now})
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.resources.create", "NetworkResource", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) UpdateResource(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.ResourceInput) (domainnetworkaccess.Resource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessResourcesUpdate); err != nil {
		return domainnetworkaccess.Resource{}, err
	}
	input = normalizeResourceInput(input)
	if err := validateResourceInput(input); err != nil {
		return domainnetworkaccess.Resource{}, err
	}
	item, err := s.store.UpdateResource(ctx, strings.TrimSpace(id), input, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.resources.update", "NetworkResource", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) DeleteResource(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessResourcesDelete); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if err := s.store.DeleteResource(ctx, id); err != nil {
		return err
	}
	s.recordMutation(ctx, principal, "network_access.resources.delete", "NetworkResource", id, id)
	return nil
}

func (s *Service) ListGateways(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.GatewayFilter) ([]domainnetworkaccess.Gateway, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessGatewaysView); err != nil {
		return nil, err
	}
	filter.Search, filter.SiteID, filter.Status = strings.TrimSpace(filter.Search), strings.TrimSpace(filter.SiteID), strings.TrimSpace(filter.Status)
	if err := validateGatewayFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListGateways(ctx, filter)
}

func (s *Service) GetGateway(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.Gateway, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessGatewaysView); err != nil {
		return domainnetworkaccess.Gateway{}, err
	}
	return s.store.GetGateway(ctx, strings.TrimSpace(id))
}

func (s *Service) CreateGateway(ctx context.Context, principal domainidentity.Principal, input domainnetworkaccess.GatewayInput) (domainnetworkaccess.Gateway, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessGatewaysCreate); err != nil {
		return domainnetworkaccess.Gateway{}, err
	}
	input = normalizeGatewayInput(input)
	if err := validateGatewayInput(input); err != nil {
		return domainnetworkaccess.Gateway{}, err
	}
	now := time.Now().UTC()
	item, err := s.store.CreateGateway(ctx, domainnetworkaccess.Gateway{
		ID: uuid.NewString(), RuntimeID: input.RuntimeID, SiteID: input.SiteID, Name: input.Name,
		AdministrativeStatus: input.AdministrativeStatus, Status: domainnetworkaccess.GatewayOffline,
		PublicEndpointHost: input.PublicEndpointHost, PublicEndpointPort: input.PublicEndpointPort,
		OverlayCIDR: input.OverlayCIDR, RoutingMode: input.RoutingMode, MTU: input.MTU,
		HubGatewayID: input.HubGatewayID, AdvertisedCIDRs: input.AdvertisedCIDRs,
		PersistentKeepaliveSeconds: input.PersistentKeepaliveSeconds, DNSServers: input.DNSServers,
		Capabilities: []string{}, CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.gateways.create", "NetworkGateway", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) UpdateGateway(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.GatewayInput) (domainnetworkaccess.Gateway, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessGatewaysUpdate); err != nil {
		return domainnetworkaccess.Gateway{}, err
	}
	input = normalizeGatewayInput(input)
	if err := validateGatewayInput(input); err != nil {
		return domainnetworkaccess.Gateway{}, err
	}
	item, err := s.store.UpdateGateway(ctx, strings.TrimSpace(id), input, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.gateways.update", "NetworkGateway", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) ListMihomoProfiles(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.MihomoProfileFilter) ([]domainnetworkaccess.MihomoProfile, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessMihomoProfilesView); err != nil {
		return nil, err
	}
	filter.Search = strings.TrimSpace(filter.Search)
	filter.DeviceID = strings.TrimSpace(filter.DeviceID)
	filter.Mode = strings.TrimSpace(filter.Mode)
	filter.Status = strings.TrimSpace(filter.Status)
	if err := validateMihomoProfileFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListMihomoProfiles(ctx, filter)
}

func (s *Service) GetMihomoProfile(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.MihomoProfile, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessMihomoProfilesView); err != nil {
		return domainnetworkaccess.MihomoProfile{}, err
	}
	return s.store.GetMihomoProfile(ctx, strings.TrimSpace(id))
}

func (s *Service) CreateMihomoProfile(ctx context.Context, principal domainidentity.Principal, input domainnetworkaccess.MihomoProfileInput) (domainnetworkaccess.MihomoProfile, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessMihomoProfilesCreate); err != nil {
		return domainnetworkaccess.MihomoProfile{}, err
	}
	input = normalizeMihomoProfileInput(input)
	if err := validateMihomoProfileInput(input); err != nil {
		return domainnetworkaccess.MihomoProfile{}, err
	}
	var subscriptionCiphertext, manualNodeCiphertext string
	var err error
	switch input.SourceType {
	case domainnetworkaccess.MihomoSourceManagedSubscription:
		if input.SubscriptionURL == nil {
			return domainnetworkaccess.MihomoProfile{}, invalid("managed_subscription requires subscriptionUrl when created")
		}
		subscriptionCiphertext, err = encryptMihomoSubscription(s.keys, input.SubscriptionURL)
	case domainnetworkaccess.MihomoSourceManualNode:
		if input.ManualNode == nil {
			return domainnetworkaccess.MihomoProfile{}, invalid("manual_node requires manualNode when created")
		}
		manualNodeCiphertext, err = encryptMihomoManualNode(s.keys, input.ManualNode)
	}
	if err != nil {
		return domainnetworkaccess.MihomoProfile{}, fmt.Errorf("encrypt mihomo source: %w", err)
	}
	now := time.Now().UTC()
	item, err := s.store.CreateMihomoProfile(ctx, mihomoProfileFromInput(uuid.NewString(), input, subscriptionCiphertext, manualNodeCiphertext, now))
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.mihomo_profiles.create", "NetworkMihomoProfile", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) UpdateMihomoProfile(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.MihomoProfileInput) (domainnetworkaccess.MihomoProfile, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessMihomoProfilesUpdate); err != nil {
		return domainnetworkaccess.MihomoProfile{}, err
	}
	id = strings.TrimSpace(id)
	input = normalizeMihomoProfileInput(input)
	if err := validateMihomoProfileInput(input); err != nil {
		return domainnetworkaccess.MihomoProfile{}, err
	}
	existing, err := s.store.GetMihomoProfile(ctx, id)
	if err != nil {
		return domainnetworkaccess.MihomoProfile{}, err
	}
	var subscriptionCiphertext, manualNodeCiphertext string
	switch input.SourceType {
	case domainnetworkaccess.MihomoSourceManagedSubscription:
		if input.SubscriptionURL == nil {
			if existing.SourceType != input.SourceType || existing.SubscriptionURLCiphertext == "" {
				return domainnetworkaccess.MihomoProfile{}, invalid("managed_subscription requires subscriptionUrl when no managed subscription exists")
			}
			subscriptionCiphertext = existing.SubscriptionURLCiphertext
		} else {
			subscriptionCiphertext, err = encryptMihomoSubscription(s.keys, input.SubscriptionURL)
		}
	case domainnetworkaccess.MihomoSourceManualNode:
		if input.ManualNode == nil {
			if existing.SourceType != input.SourceType || existing.ManualNodeCiphertext == "" {
				return domainnetworkaccess.MihomoProfile{}, invalid("manual_node requires manualNode when no manual node exists")
			}
			manualNodeCiphertext = existing.ManualNodeCiphertext
		} else {
			manualNodeCiphertext, err = encryptMihomoManualNode(s.keys, input.ManualNode)
		}
	}
	if err != nil {
		return domainnetworkaccess.MihomoProfile{}, fmt.Errorf("encrypt mihomo source: %w", err)
	}
	item := mihomoProfileFromInput(id, input, subscriptionCiphertext, manualNodeCiphertext, existing.CreatedAt)
	item, err = s.store.UpdateMihomoProfile(ctx, id, item, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.mihomo_profiles.update", "NetworkMihomoProfile", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) DeleteMihomoProfile(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessMihomoProfilesDelete); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if err := s.store.DeleteMihomoProfile(ctx, id); err != nil {
		return err
	}
	s.recordMutation(ctx, principal, "network_access.mihomo_profiles.delete", "NetworkMihomoProfile", id, id)
	return nil
}

func mihomoProfileFromInput(id string, input domainnetworkaccess.MihomoProfileInput, subscriptionCiphertext, manualNodeCiphertext string, createdAt time.Time) domainnetworkaccess.MihomoProfile {
	return domainnetworkaccess.MihomoProfile{
		ID: id, DeviceID: input.DeviceID, Name: input.Name, Mode: input.Mode, SourceType: input.SourceType, Status: input.Status,
		SubscriptionConfigured: subscriptionCiphertext != "", SubscriptionURLCiphertext: subscriptionCiphertext,
		ManualNodeConfigured: manualNodeCiphertext != "", ManualNodeCiphertext: manualNodeCiphertext, Revision: 1,
		MixedPort: input.MixedPort, ControllerPort: input.ControllerPort, DNSMode: input.DNSMode,
		FakeIPRange: input.FakeIPRange, SelectorGroup: input.SelectorGroup, SelectedProxy: input.SelectedProxy,
		BypassCIDRs: input.BypassCIDRs, BypassHosts: input.BypassHosts, FailClosed: input.FailClosed,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
}

func encryptMihomoSubscription(keys keyring.Ring, value *string) (string, error) {
	if value == nil {
		return "", nil
	}
	return secretcrypto.EncryptStringWithKeyring(keys, *value)
}

func encryptMihomoManualNode(keys keyring.Ring, value *domainnetworkaccess.MihomoManualNode) (string, error) {
	if value == nil {
		return "", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return secretcrypto.EncryptStringWithKeyring(keys, string(raw))
}

func (s *Service) ListConnectionOptions(ctx context.Context, principal domainidentity.Principal, deviceID string) ([]domainnetworkaccess.ConnectionOption, error) {
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return nil, fmt.Errorf("%w: authenticated user id is required", apperrors.ErrUnauthorized)
	}
	deviceID = strings.TrimSpace(deviceID)
	if !runtimeIdentifierPattern.MatchString(deviceID) {
		return nil, invalid("deviceId is invalid")
	}
	device, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if device.OwnerUserID != userID {
		return nil, apperrors.NewBusiness(apperrors.ErrAccessDenied, "network_device_access_denied", "The endpoint does not belong to the current user.", "该终端不属于当前用户。")
	}
	subject, err := s.store.GetSubject(ctx, userID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return []domainnetworkaccess.ConnectionOption{}, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot, err := s.store.GetPolicySnapshot(ctx)
	if errors.Is(err, apperrors.ErrNotFound) {
		return []domainnetworkaccess.ConnectionOption{}, nil
	}
	if err != nil {
		return nil, err
	}
	bindings, err := s.store.ListNASBindings(ctx, domainnetworkaccess.NASBindingFilter{Status: domainnetworkaccess.StatusActive, Limit: 200})
	if err != nil {
		return nil, err
	}
	sites, err := s.store.ListSites(ctx, domainnetworkaccess.SiteFilter{Status: domainnetworkaccess.StatusActive, Limit: 200})
	if err != nil {
		return nil, err
	}
	siteByID := make(map[string]domainnetworkaccess.Site, len(sites))
	for _, site := range sites {
		siteByID[site.ID] = site
	}
	items := make([]domainnetworkaccess.ConnectionOption, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if !validConnectionBinding(binding) {
			continue
		}
		site, ok := siteByID[binding.SiteID]
		if !ok {
			continue
		}
		preview := EvaluateAdmission(AdmissionInput{SubjectUserID: userID, DeviceID: deviceID, SiteID: site.ID, Mode: domainnetworkaccess.ModeInternalDirect}, subject, device, site, snapshot)
		if preview.Decision != domainnetworkaccess.DecisionAllow {
			continue
		}
		key := binding.AccessMedium + "\x00" + site.ID + "\x00" + binding.SSID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, domainnetworkaccess.ConnectionOption{
			SiteID: site.ID, SiteName: site.Name, AccessMedium: binding.AccessMedium, SSID: binding.SSID,
			Authentication: domainnetworkaccess.ConnectionAuthenticationRadius8021X,
			AccessProfile:  preview.NetworkProfile, PolicyVersion: snapshot.PolicyVersion,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.AccessMedium != right.AccessMedium {
			return left.AccessMedium < right.AccessMedium
		}
		if left.SiteName != right.SiteName {
			return left.SiteName < right.SiteName
		}
		return left.SSID < right.SSID
	})
	return items, nil
}

func validConnectionBinding(binding domainnetworkaccess.NASBinding) bool {
	return binding.Status == domainnetworkaccess.StatusActive &&
		(binding.AccessMedium == domainnetworkaccess.AccessMediumWiFi || binding.AccessMedium == domainnetworkaccess.AccessMediumWired) &&
		(binding.AccessMedium != domainnetworkaccess.AccessMediumWiFi || binding.SSID != "")
}

func (s *Service) ListNASBindings(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.NASBindingFilter) ([]domainnetworkaccess.NASBinding, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return nil, err
	}
	filter.SiteID, filter.RuntimeID, filter.Status = strings.TrimSpace(filter.SiteID), strings.TrimSpace(filter.RuntimeID), strings.TrimSpace(filter.Status)
	if err := validateNASBindingFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListNASBindings(ctx, filter)
}

func (s *Service) GetNASBinding(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.NASBinding, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return domainnetworkaccess.NASBinding{}, err
	}
	return s.store.GetNASBinding(ctx, strings.TrimSpace(id))
}

func (s *Service) CreateNASBinding(ctx context.Context, principal domainidentity.Principal, input domainnetworkaccess.NASBindingInput) (domainnetworkaccess.NASBinding, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return domainnetworkaccess.NASBinding{}, err
	}
	input = normalizeNASBindingInput(input)
	if err := validateNASBindingInput(input); err != nil {
		return domainnetworkaccess.NASBinding{}, err
	}
	now := time.Now().UTC()
	item, err := s.store.CreateNASBinding(ctx, domainnetworkaccess.NASBinding{
		ID: uuid.NewString(), NASID: input.NASID, RuntimeID: input.RuntimeID, SiteID: input.SiteID, Name: input.Name,
		AccessMedium: input.AccessMedium, DeviceType: input.DeviceType, SSID: input.SSID, ManagementAddress: input.ManagementAddress,
		Status: input.Status, CoASupported: input.CoASupported, DisconnectSupported: input.DisconnectSupported, CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.nas_bindings.create", "NetworkNASBinding", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) UpdateNASBinding(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.NASBindingInput) (domainnetworkaccess.NASBinding, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return domainnetworkaccess.NASBinding{}, err
	}
	input = normalizeNASBindingInput(input)
	if err := validateNASBindingInput(input); err != nil {
		return domainnetworkaccess.NASBinding{}, err
	}
	item, err := s.store.UpdateNASBinding(ctx, strings.TrimSpace(id), input, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.nas_bindings.update", "NetworkNASBinding", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) DeleteNASBinding(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if err := s.store.DeleteNASBinding(ctx, id); err != nil {
		return err
	}
	s.recordMutation(ctx, principal, "network_access.nas_bindings.delete", "NetworkNASBinding", id, id)
	return nil
}

func (s *Service) ListSiteProfileBindings(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.SiteProfileBindingFilter) ([]domainnetworkaccess.SiteProfileBinding, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return nil, err
	}
	filter.SiteID, filter.AccessProfile = strings.TrimSpace(filter.SiteID), strings.TrimSpace(filter.AccessProfile)
	if err := validateSiteProfileBindingFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListSiteProfileBindings(ctx, filter)
}

func (s *Service) GetSiteProfileBinding(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.SiteProfileBinding, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesView); err != nil {
		return domainnetworkaccess.SiteProfileBinding{}, err
	}
	return s.store.GetSiteProfileBinding(ctx, strings.TrimSpace(id))
}

func (s *Service) CreateSiteProfileBinding(ctx context.Context, principal domainidentity.Principal, input domainnetworkaccess.SiteProfileBindingInput) (domainnetworkaccess.SiteProfileBinding, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return domainnetworkaccess.SiteProfileBinding{}, err
	}
	input = normalizeSiteProfileBindingInput(input)
	if err := validateSiteProfileBindingInput(input); err != nil {
		return domainnetworkaccess.SiteProfileBinding{}, err
	}
	now := time.Now().UTC()
	item, err := s.store.CreateSiteProfileBinding(ctx, domainnetworkaccess.SiteProfileBinding{ID: uuid.NewString(), SiteID: input.SiteID, AccessProfile: input.AccessProfile, VLANID: input.VLANID, FilterID: input.FilterID, SessionTimeoutSeconds: input.SessionTimeoutSeconds, CreatedAt: now, UpdatedAt: now})
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.site_profile_bindings.create", "NetworkSiteProfileBinding", item.ID, item.AccessProfile)
	}
	return item, err
}

func (s *Service) UpdateSiteProfileBinding(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.SiteProfileBindingInput) (domainnetworkaccess.SiteProfileBinding, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return domainnetworkaccess.SiteProfileBinding{}, err
	}
	input = normalizeSiteProfileBindingInput(input)
	if err := validateSiteProfileBindingInput(input); err != nil {
		return domainnetworkaccess.SiteProfileBinding{}, err
	}
	item, err := s.store.UpdateSiteProfileBinding(ctx, strings.TrimSpace(id), input, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.site_profile_bindings.update", "NetworkSiteProfileBinding", item.ID, item.AccessProfile)
	}
	return item, err
}

func (s *Service) DeleteSiteProfileBinding(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessSitesUpdate); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if err := s.store.DeleteSiteProfileBinding(ctx, id); err != nil {
		return err
	}
	s.recordMutation(ctx, principal, "network_access.site_profile_bindings.delete", "NetworkSiteProfileBinding", id, id)
	return nil
}

func (s *Service) ListPolicies(ctx context.Context, principal domainidentity.Principal, filter domainnetworkaccess.PolicyFilter) ([]domainnetworkaccess.Policy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyView); err != nil {
		return nil, err
	}
	filter.Search, filter.Effect = strings.TrimSpace(filter.Search), strings.TrimSpace(filter.Effect)
	if err := validatePolicyFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	return s.store.ListPolicies(ctx, filter)
}

func (s *Service) GetPolicy(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkaccess.Policy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyView); err != nil {
		return domainnetworkaccess.Policy{}, err
	}
	return s.store.GetPolicy(ctx, strings.TrimSpace(id))
}

func (s *Service) CreatePolicy(ctx context.Context, principal domainidentity.Principal, input domainnetworkaccess.PolicyInput) (domainnetworkaccess.Policy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyCreate); err != nil {
		return domainnetworkaccess.Policy{}, err
	}
	input = normalizePolicyInput(input)
	if err := validatePolicyInput(input); err != nil {
		return domainnetworkaccess.Policy{}, err
	}
	now := time.Now().UTC()
	item, err := s.store.CreatePolicy(ctx, domainnetworkaccess.Policy{ID: uuid.NewString(), Name: input.Name, Enabled: input.Enabled, Priority: input.Priority, Effect: input.Effect, Subjects: input.Subjects, SiteIDs: input.SiteIDs, ResourceIDs: input.ResourceIDs, Modes: input.Modes, DeviceStatuses: input.DeviceStatuses, PostureStatuses: input.PostureStatuses, AccessProfile: input.AccessProfile, Version: 1, CreatedAt: now, UpdatedAt: now})
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.policy.create", "NetworkAccessPolicy", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) UpdatePolicy(ctx context.Context, principal domainidentity.Principal, id string, input domainnetworkaccess.PolicyInput) (domainnetworkaccess.Policy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyUpdate); err != nil {
		return domainnetworkaccess.Policy{}, err
	}
	input = normalizePolicyInput(input)
	if err := validatePolicyInput(input); err != nil {
		return domainnetworkaccess.Policy{}, err
	}
	item, err := s.store.UpdatePolicy(ctx, strings.TrimSpace(id), input, time.Now().UTC())
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.policy.update", "NetworkAccessPolicy", item.ID, item.Name)
	}
	return item, err
}

func (s *Service) DeletePolicy(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyDelete); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if err := s.store.DeletePolicy(ctx, id); err != nil {
		return err
	}
	s.recordMutation(ctx, principal, "network_access.policy.delete", "NetworkAccessPolicy", id, id)
	return nil
}

func (s *Service) CompilePolicySnapshot(ctx context.Context, principal domainidentity.Principal) (domainnetworkaccess.PolicySnapshot, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyUpdate); err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	policies, err := s.store.ListPoliciesForSnapshot(ctx)
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	protectedResourceIDs, err := s.store.ListProtectedResourceIDs(ctx)
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	snapshot, err := newPolicySnapshot(policies, protectedResourceIDs, time.Now().UTC())
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	snapshot, err = s.store.PublishPolicySnapshot(ctx, snapshot)
	if err == nil {
		s.recordMutation(ctx, principal, "network_access.policy.compile", "NetworkPolicySnapshot", fmt.Sprintf("%d", snapshot.PolicyVersion), snapshot.ContentHash)
	}
	return snapshot, err
}

func (s *Service) GetPolicySnapshot(ctx context.Context, principal domainidentity.Principal) (domainnetworkaccess.PolicySnapshot, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyView); err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	return s.store.GetPolicySnapshot(ctx)
}

func (s *Service) AnalyzeConflicts(ctx context.Context, principal domainidentity.Principal, runtimeRanges []domainnetworkaccess.ConflictRange) (domainnetworkaccess.ConflictAnalysis, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyView); err != nil {
		return domainnetworkaccess.ConflictAnalysis{}, err
	}
	for index := range runtimeRanges {
		runtimeRanges[index].SourceType = strings.TrimSpace(runtimeRanges[index].SourceType)
		runtimeRanges[index].SourceID = strings.TrimSpace(runtimeRanges[index].SourceID)
		runtimeRanges[index].Name = strings.TrimSpace(runtimeRanges[index].Name)
		runtimeRanges[index].CIDR = strings.TrimSpace(runtimeRanges[index].CIDR)
	}
	if err := validateConflictRanges(runtimeRanges); err != nil {
		return domainnetworkaccess.ConflictAnalysis{}, err
	}
	configured, err := s.store.ListConflictRanges(ctx)
	if err != nil {
		return domainnetworkaccess.ConflictAnalysis{}, err
	}
	ranges := append(configured, domainnetworkaccess.ConflictRange{SourceType: domainnetworkaccess.ConflictSourceReserved, Name: "RFC6598 shared address space", CIDR: "100.64.0.0/10"})
	ranges = append(ranges, runtimeRanges...)
	result, err := analyzeConflictRanges(ranges)
	if err == nil {
		s.recordAnalysis(ctx, principal, "network_access.conflicts.analyze", "NetworkConflictAnalysis", result.Valid, map[string]any{"rangesAnalyzed": result.RangesAnalyzed, "conflictCount": len(result.Conflicts)})
	}
	return result, err
}

func (s *Service) PreviewPolicy(ctx context.Context, principal domainidentity.Principal, input PreviewInput) (domainnetworkaccess.PolicyPreview, error) {
	if err := s.authorize(ctx, principal, appaccess.PermNetworkAccessPolicyView); err != nil {
		return domainnetworkaccess.PolicyPreview{}, err
	}
	input.SubjectUserID, input.DeviceID, input.ResourceID, input.SiteID, input.Mode = strings.TrimSpace(input.SubjectUserID), strings.TrimSpace(input.DeviceID), strings.TrimSpace(input.ResourceID), strings.TrimSpace(input.SiteID), strings.TrimSpace(input.Mode)
	if err := validatePreviewInput(input); err != nil {
		return domainnetworkaccess.PolicyPreview{}, err
	}
	subject, err := s.store.GetSubject(ctx, input.SubjectUserID)
	if errors.Is(err, apperrors.ErrNotFound) {
		subject = domainnetworkaccess.Subject{UserID: input.SubjectUserID}
	} else if err != nil {
		return domainnetworkaccess.PolicyPreview{}, err
	}
	device, err := s.store.GetDevice(ctx, input.DeviceID)
	if err != nil {
		return domainnetworkaccess.PolicyPreview{}, err
	}
	resource, err := s.store.GetResource(ctx, input.ResourceID)
	if err != nil {
		return domainnetworkaccess.PolicyPreview{}, err
	}
	space, err := s.store.GetSpace(ctx, resource.SpaceID)
	if err != nil {
		return domainnetworkaccess.PolicyPreview{}, err
	}
	site, err := s.store.GetSite(ctx, space.SiteID)
	if err != nil {
		return domainnetworkaccess.PolicyPreview{}, err
	}
	snapshot, err := s.store.GetPolicySnapshot(ctx)
	if errors.Is(err, apperrors.ErrNotFound) {
		snapshot = domainnetworkaccess.PolicySnapshot{}
	} else if err != nil {
		return domainnetworkaccess.PolicyPreview{}, err
	}
	result := EvaluatePolicy(input, subject, device, site, space, resource, snapshot)
	s.recordPreview(ctx, principal, input, result)
	return result, nil
}

func normalizeSiteInput(input domainnetworkaccess.SiteInput) domainnetworkaccess.SiteInput {
	input.Name, input.Description, input.Location, input.Status = strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), strings.TrimSpace(input.Location), strings.TrimSpace(input.Status)
	return input
}

func normalizeDeviceRegistrationInput(input domainnetworkaccess.DeviceRegistrationInput) domainnetworkaccess.DeviceRegistrationInput {
	input.Name, input.Hostname, input.Platform, input.DeviceType = strings.TrimSpace(input.Name), strings.TrimSpace(input.Hostname), strings.TrimSpace(input.Platform), strings.TrimSpace(input.DeviceType)
	if input.ReportedFacts == nil {
		return input
	}
	facts := input.ReportedFacts
	facts.OSName, facts.OSVersion, facts.OSBuild, facts.Architecture = strings.TrimSpace(facts.OSName), strings.TrimSpace(facts.OSVersion), strings.TrimSpace(facts.OSBuild), strings.TrimSpace(facts.Architecture)
	facts.Manufacturer, facts.Model, facts.SerialNumber, facts.AgentVersion = strings.TrimSpace(facts.Manufacturer), strings.TrimSpace(facts.Model), strings.TrimSpace(facts.SerialNumber), strings.TrimSpace(facts.AgentVersion)
	for index := range facts.NetworkInterfaces {
		item := &facts.NetworkInterfaces[index]
		item.Name, item.DisplayName, item.Kind, item.Status, item.MACAddress = strings.TrimSpace(item.Name), strings.TrimSpace(item.DisplayName), strings.TrimSpace(item.Kind), strings.TrimSpace(item.Status), strings.ToLower(strings.TrimSpace(item.MACAddress))
		for _, values := range [][]string{item.IPv4Addresses, item.IPv6Addresses, item.DNSServers} {
			for valueIndex := range values {
				values[valueIndex] = strings.TrimSpace(values[valueIndex])
			}
		}
	}
	return input
}

func normalizeSpaceInput(input domainnetworkaccess.SpaceInput) domainnetworkaccess.SpaceInput {
	input.SiteID, input.Name, input.Status = strings.TrimSpace(input.SiteID), strings.TrimSpace(input.Name), strings.TrimSpace(input.Status)
	for index := range input.CIDRs {
		input.CIDRs[index] = strings.TrimSpace(input.CIDRs[index])
	}
	return input
}

func normalizeResourceInput(input domainnetworkaccess.ResourceInput) domainnetworkaccess.ResourceInput {
	input.SpaceID, input.Name, input.Kind, input.Target, input.Protocol, input.PathMode = strings.TrimSpace(input.SpaceID), strings.TrimSpace(input.Name), strings.TrimSpace(input.Kind), strings.TrimSpace(input.Target), strings.TrimSpace(input.Protocol), strings.TrimSpace(input.PathMode)
	return input
}

func normalizeGatewayInput(input domainnetworkaccess.GatewayInput) domainnetworkaccess.GatewayInput {
	input.RuntimeID = strings.TrimSpace(input.RuntimeID)
	input.SiteID = strings.TrimSpace(input.SiteID)
	input.Name = strings.TrimSpace(input.Name)
	input.AdministrativeStatus = strings.TrimSpace(input.AdministrativeStatus)
	input.PublicEndpointHost = strings.ToLower(strings.TrimSpace(input.PublicEndpointHost))
	input.OverlayCIDR = strings.TrimSpace(input.OverlayCIDR)
	input.RoutingMode = strings.TrimSpace(input.RoutingMode)
	input.HubGatewayID = strings.TrimSpace(input.HubGatewayID)
	for index := range input.AdvertisedCIDRs {
		input.AdvertisedCIDRs[index] = strings.TrimSpace(input.AdvertisedCIDRs[index])
	}
	for index := range input.DNSServers {
		input.DNSServers[index] = strings.TrimSpace(input.DNSServers[index])
	}
	return input
}

func normalizeMihomoProfileInput(input domainnetworkaccess.MihomoProfileInput) domainnetworkaccess.MihomoProfileInput {
	input.DeviceID = strings.TrimSpace(input.DeviceID)
	input.Name = strings.TrimSpace(input.Name)
	input.Mode = strings.TrimSpace(input.Mode)
	input.SourceType = strings.TrimSpace(input.SourceType)
	if input.Mode == domainnetworkaccess.MihomoModeManagedFollow && input.SourceType == "" {
		input.SourceType = domainnetworkaccess.MihomoSourceManagedSubscription
	}
	input.Status = strings.TrimSpace(input.Status)
	if input.SubscriptionURL != nil {
		value := strings.TrimSpace(*input.SubscriptionURL)
		input.SubscriptionURL = &value
	}
	if input.ManualNode != nil {
		input.ManualNode.Protocol = strings.ToLower(strings.TrimSpace(input.ManualNode.Protocol))
		input.ManualNode.Server = strings.ToLower(strings.TrimSpace(input.ManualNode.Server))
	}
	input.DNSMode = strings.TrimSpace(input.DNSMode)
	input.FakeIPRange = strings.TrimSpace(input.FakeIPRange)
	input.SelectorGroup = strings.TrimSpace(input.SelectorGroup)
	input.SelectedProxy = strings.TrimSpace(input.SelectedProxy)
	for index := range input.BypassCIDRs {
		input.BypassCIDRs[index] = strings.TrimSpace(input.BypassCIDRs[index])
	}
	for index := range input.BypassHosts {
		input.BypassHosts[index] = strings.ToLower(strings.TrimSpace(input.BypassHosts[index]))
	}
	return input
}

func normalizePolicyInput(input domainnetworkaccess.PolicyInput) domainnetworkaccess.PolicyInput {
	input.Name, input.Effect, input.AccessProfile = strings.TrimSpace(input.Name), strings.TrimSpace(input.Effect), strings.TrimSpace(input.AccessProfile)
	for _, values := range [][]string{input.Subjects.Users, input.Subjects.Teams, input.Subjects.Tags, input.SiteIDs, input.ResourceIDs, input.Modes, input.DeviceStatuses, input.PostureStatuses} {
		for index := range values {
			values[index] = strings.TrimSpace(values[index])
		}
	}
	return input
}

func normalizeNASBindingInput(input domainnetworkaccess.NASBindingInput) domainnetworkaccess.NASBindingInput {
	input.NASID, input.RuntimeID, input.SiteID, input.Name = strings.TrimSpace(input.NASID), strings.TrimSpace(input.RuntimeID), strings.TrimSpace(input.SiteID), strings.TrimSpace(input.Name)
	input.AccessMedium, input.DeviceType, input.SSID, input.ManagementAddress, input.Status = strings.TrimSpace(input.AccessMedium), strings.TrimSpace(input.DeviceType), strings.TrimSpace(input.SSID), strings.TrimSpace(input.ManagementAddress), strings.TrimSpace(input.Status)
	return input
}

func normalizeSiteProfileBindingInput(input domainnetworkaccess.SiteProfileBindingInput) domainnetworkaccess.SiteProfileBindingInput {
	input.SiteID, input.AccessProfile, input.FilterID = strings.TrimSpace(input.SiteID), strings.TrimSpace(input.AccessProfile), strings.TrimSpace(input.FilterID)
	return input
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	return min(limit, 200)
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permission string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permission)
}

func (s *Service) recordMutation(ctx context.Context, principal domainidentity.Principal, action, resourceKind, id, name string) {
	meta := requestctx.FromContext(ctx)
	summary := action + " succeeded"
	_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams, ResourceKind: resourceKind, ResourceName: name, Action: action, Result: "success", Summary: summary, RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: map[string]any{"resourceId": id, "source": meta.Source}})
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, action, map[string]any{"module": "network_access", "resourceKind": resourceKind, "targetId": id, "targetLabel": name}, "success", summary, nil))
}

func (s *Service) recordPreview(ctx context.Context, principal domainidentity.Principal, input PreviewInput, result domainnetworkaccess.PolicyPreview) {
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams, ResourceKind: "NetworkPolicyPreview", ResourceName: input.ResourceID, Action: "network_access.policy.preview", Result: result.Decision, Summary: "network access policy preview " + result.Decision, RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: map[string]any{"subjectUserId": input.SubjectUserID, "deviceId": input.DeviceID, "resourceId": input.ResourceID, "siteId": input.SiteID, "mode": input.Mode, "path": result.Path, "networkProfile": result.NetworkProfile, "policyVersion": result.PolicyVersion, "protected": result.Protected, "networkLeaseRequired": result.NetworkLeaseRequired, "resourceLeaseRequired": result.ResourceLeaseRequired, "reasons": result.Reasons}})
}

func (s *Service) recordAnalysis(ctx context.Context, principal domainidentity.Principal, action, resourceKind string, valid bool, metadata map[string]any) {
	meta := requestctx.FromContext(ctx)
	result := "success"
	if !valid {
		result = "conflict"
	}
	_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams, ResourceKind: resourceKind, Action: action, Result: result, Summary: action + " completed", RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: metadata})
}

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
