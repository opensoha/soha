package networkaccess

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type EndpointResponseStore interface {
	EndpointResponsePolicy(context.Context, string) (domain.EndpointResponsePolicy, error)
	EndpointResponsePolicies(context.Context, domain.EndpointResponseFilter) ([]domain.EndpointResponsePolicy, error)
	SaveEndpointResponsePolicy(context.Context, string, int, domain.EndpointResponsePolicyConfig, string, time.Time) (domain.EndpointResponsePolicy, error)
	PublishEndpointResponsePolicy(context.Context, string, int, int, string, time.Time) (domain.EndpointResponsePolicy, error)
	DeleteEndpointResponsePolicy(context.Context, string, int, time.Time) error
	EndpointResponsePolicyRevision(context.Context, string, int) (domain.EndpointResponsePolicyRevision, error)
	EndpointResponsePolicyRevisions(context.Context, string) ([]domain.EndpointResponsePolicyRevision, error)
	EndpointPresence(context.Context, string) (domain.PresenceRecord, error)
	EndpointResponseEvent(context.Context, string) (domain.EndpointResponseRecord, error)
	EndpointResponseEvents(context.Context, domain.EndpointResponseFilter) ([]domain.EndpointResponseRecord, error)
	EndpointMaintenance(context.Context, domain.EndpointResponseFilter, time.Time) ([]domain.EndpointMaintenance, error)
	SaveEndpointMaintenance(context.Context, domain.EndpointMaintenance) error
	EndEndpointMaintenance(context.Context, string, time.Time) (domain.EndpointMaintenance, error)
	EndpointMaintenanceByID(context.Context, string) (domain.EndpointMaintenance, error)
	ClaimEndpointNotification(context.Context, time.Time) (domain.EndpointResponseRecord, error)
	CompleteEndpointNotification(context.Context, string, int, bool, time.Time) error
	EndpointResponseSnapshot(context.Context, string, string, time.Time) (domain.EndpointResponseSnapshot, error)
	EndpointResponseCandidates(context.Context, string, int) ([]domain.EndpointResponseCandidate, error)
	NASAdmissionCandidates(context.Context, string, int) ([]string, error)
	ReconcileNASAdmission(context.Context, string, time.Time, func(domain.NASAdmissionSnapshot) *domain.NASAdmissionMutation) error
	ReconcileEndpointResponse(context.Context, string, string, string, time.Time, func(domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error)) (domain.EndpointResponseRecord, error)
}

type EndpointResponseNotifications interface {
	ValidateEndpointNotification(context.Context, domainidentity.Principal, string) error
	DeliverEndpointNotification(context.Context, string, string, map[string]string) error
}

type EndpointResponseService struct {
	base          *Service
	store         EndpointResponseStore
	notifications EndpointResponseNotifications
	now           func() time.Time
}

func NewEndpointResponseService(base *Service, store EndpointResponseStore, notifications EndpointResponseNotifications) (*EndpointResponseService, error) {
	if base == nil || isNilDependency(store) || isNilDependency(notifications) {
		return nil, fmt.Errorf("endpoint response service: base, store and notifications are required")
	}
	return &EndpointResponseService{base: base, store: store, notifications: notifications, now: time.Now}, nil
}

func responsePolicyPermission(action string) string {
	return "network_access.endpoint_response_policies." + action
}
func responseEventPermission(action string) string {
	return "network_access.endpoint_response_events." + action
}

func (s *EndpointResponseService) authorizeConfig(ctx context.Context, principal domainidentity.Principal, key, id string, config domain.EndpointResponsePolicyConfig) error {
	for _, siteID := range config.SiteIDs {
		if err := s.base.authorizeScope(ctx, principal, key, resourceScope{kind: "EndpointResponsePolicy", id: id, siteID: siteID}); err != nil {
			return err
		}
	}
	for _, deviceID := range config.DeviceIDs {
		device, err := s.base.store.GetDevice(ctx, deviceID)
		if err != nil {
			return err
		}
		if err := s.base.authorizeScope(ctx, principal, key, deviceScope(device)); err != nil {
			return err
		}
	}
	for _, bindingID := range config.NasBindingIDs {
		binding, err := s.base.store.GetNASBinding(ctx, bindingID)
		if err != nil {
			return err
		}
		if err := s.base.authorizeScope(ctx, principal, key, resourceScope{kind: "NASBinding", id: binding.ID, siteID: binding.SiteID}); err != nil {
			return err
		}
	}
	return nil
}

func (s *EndpointResponseService) authorizePolicy(ctx context.Context, principal domainidentity.Principal, key string, policy domain.EndpointResponsePolicy) error {
	if err := s.authorizeConfig(ctx, principal, key, policy.ID, policy.Configuration); err != nil {
		return err
	}
	if policy.PublishedConfiguration != nil {
		return s.authorizeConfig(ctx, principal, key, policy.ID, *policy.PublishedConfiguration)
	}
	return nil
}

func (s *EndpointResponseService) Policies(ctx context.Context, principal domainidentity.Principal, filter domain.EndpointResponseFilter) ([]domain.EndpointResponsePolicy, error) {
	key := responsePolicyPermission("view")
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return nil, err
	}
	return collectVisible(ctx, boundedLimit(filter.Limit), func(limit, offset int) ([]domain.EndpointResponsePolicy, error) {
		filter.Limit, filter.Offset = limit, offset
		return s.store.EndpointResponsePolicies(ctx, filter)
	}, func(item domain.EndpointResponsePolicy) error { return s.authorizePolicy(ctx, principal, key, item) })
}

func (s *EndpointResponseService) Policy(ctx context.Context, principal domainidentity.Principal, id string) (domain.EndpointResponsePolicy, error) {
	if err := s.base.authorize(ctx, principal, responsePolicyPermission("view")); err != nil {
		return domain.EndpointResponsePolicy{}, err
	}
	item, err := s.store.EndpointResponsePolicy(ctx, id)
	if err != nil {
		return item, err
	}
	return item, s.authorizePolicy(ctx, principal, responsePolicyPermission("view"), item)
}

func (s *EndpointResponseService) SavePolicy(ctx context.Context, principal domainidentity.Principal, id string, input domain.EndpointResponsePolicyInput) (domain.EndpointResponsePolicy, error) {
	action := "update"
	if id == "" {
		action = "create"
		id = uuid.NewString()
		if input.ExpectedRevision != 0 {
			return domain.EndpointResponsePolicy{}, invalidResponse("create_revision")
		}
	} else if input.ExpectedRevision < 1 {
		return domain.EndpointResponsePolicy{}, invalidResponse("update_revision")
	}
	key := responsePolicyPermission(action)
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return domain.EndpointResponsePolicy{}, err
	}
	if action == "update" {
		existing, err := s.store.EndpointResponsePolicy(ctx, id)
		if err != nil {
			return existing, err
		}
		if err := s.authorizePolicy(ctx, principal, key, existing); err != nil {
			return domain.EndpointResponsePolicy{}, err
		}
	}
	if err := s.validateConfig(ctx, principal, key, id, &input.Configuration); err != nil {
		return domain.EndpointResponsePolicy{}, err
	}
	item, err := s.store.SaveEndpointResponsePolicy(ctx, id, input.ExpectedRevision, input.Configuration, principal.UserID, s.now().UTC())
	if err == nil {
		s.base.recordMutation(ctx, principal, key, "EndpointResponsePolicy", id, item.Configuration.Name)
	}
	return item, err
}

func (s *EndpointResponseService) PublishPolicy(ctx context.Context, principal domainidentity.Principal, id string, expected, target int) (domain.EndpointResponsePolicy, error) {
	key := responsePolicyPermission("publish")
	if expected < 1 || target < 0 {
		return domain.EndpointResponsePolicy{}, invalidResponse("publish_revision")
	}
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return domain.EndpointResponsePolicy{}, err
	}
	item, err := s.store.EndpointResponsePolicy(ctx, id)
	if err != nil {
		return item, err
	}
	if err := s.authorizePolicy(ctx, principal, key, item); err != nil {
		return item, err
	}
	config := item.Configuration
	if target > 0 {
		revision, err := s.store.EndpointResponsePolicyRevision(ctx, id, target)
		if err != nil {
			return item, err
		}
		config = revision.Configuration
	}
	if err := s.validateConfig(ctx, principal, key, id, &config); err != nil {
		return item, err
	}
	item, err = s.store.PublishEndpointResponsePolicy(ctx, id, expected, target, principal.UserID, s.now().UTC())
	if err == nil {
		s.base.recordMutation(ctx, principal, key, "EndpointResponsePolicy", id, item.Configuration.Name)
	}
	return item, err
}

func (s *EndpointResponseService) DeletePolicy(ctx context.Context, principal domainidentity.Principal, id string, expected int) error {
	key := responsePolicyPermission("delete")
	if expected < 1 {
		return invalidResponse("delete_revision")
	}
	if err := s.base.authorize(ctx, principal, key); err != nil {
		return err
	}
	item, err := s.store.EndpointResponsePolicy(ctx, id)
	if err != nil {
		return err
	}
	if err := s.authorizePolicy(ctx, principal, key, item); err != nil {
		return err
	}
	if err := s.store.DeleteEndpointResponsePolicy(ctx, id, expected, s.now().UTC()); err != nil {
		return err
	}
	s.base.recordMutation(ctx, principal, key, "EndpointResponsePolicy", id, item.Configuration.Name)
	return nil
}

func (s *EndpointResponseService) Revisions(ctx context.Context, principal domainidentity.Principal, id string) ([]domain.EndpointResponsePolicyRevision, error) {
	if _, err := s.Policy(ctx, principal, id); err != nil {
		return nil, err
	}
	items, err := s.store.EndpointResponsePolicyRevisions(ctx, id)
	if err != nil {
		return nil, err
	}
	visible := make([]domain.EndpointResponsePolicyRevision, 0, len(items))
	for _, item := range items {
		if err := s.authorizeConfig(ctx, principal, responsePolicyPermission("view"), id, item.Configuration); errors.Is(err, apperrors.ErrAccessDenied) {
			continue
		} else if err != nil {
			return nil, err
		}
		visible = append(visible, item)
	}
	return visible, nil
}

func (s *EndpointResponseService) validateConfig(ctx context.Context, principal domainidentity.Principal, key, id string, config *domain.EndpointResponsePolicyConfig) error {
	config.Name = strings.TrimSpace(config.Name)
	if err := validateResponseTiming(*config); err != nil {
		return err
	}
	if err := validateResponseSteps(*config); err != nil {
		return err
	}
	if err := s.authorizeConfig(ctx, principal, key, id, *config); err != nil {
		return err
	}
	for _, siteID := range config.SiteIDs {
		site, err := s.base.store.GetSite(ctx, siteID)
		if err != nil {
			return err
		}
		if site.Status != domain.StatusActive {
			return invalidResponse("site_inactive")
		}
	}
	for _, id := range config.NasBindingIDs {
		binding, err := s.base.store.GetNASBinding(ctx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(config.SiteIDs, binding.SiteID) {
			return invalidResponse("nas_site_mismatch")
		}
		if err := s.base.authorizeScope(ctx, principal, key, resourceScope{kind: "NASBinding", id: id, siteID: binding.SiteID}); err != nil {
			return err
		}
	}
	if config.NotificationPolicyID != "" {
		for _, siteID := range config.SiteIDs {
			if err := s.base.authorizeScope(ctx, principal, appaccess.PermObserveNotificationsView, resourceScope{group: "observe", kind: "NotificationPolicy", id: config.NotificationPolicyID, siteID: siteID}); err != nil {
				return err
			}
		}
		if err := s.notifications.ValidateEndpointNotification(ctx, principal, config.NotificationPolicyID); err != nil {
			return err
		}
	}
	if config.DeviceIDs == nil {
		config.DeviceIDs = []string{}
	}
	if config.NasBindingIDs == nil {
		config.NasBindingIDs = []string{}
	}
	return nil
}

func validateResponseTiming(config domain.EndpointResponsePolicyConfig) error {
	if config.Name == "" || len(config.Name) > 200 || !config.Signal.Valid() || len(config.SiteIDs) == 0 || len(config.SiteIDs) > 128 || len(config.DeviceIDs) > 512 || len(config.NasBindingIDs) > 128 || !config.FailureAction.Valid() {
		return invalidResponse("configuration")
	}
	if err := validateResponseSelectors(config); err != nil {
		return err
	}
	return validateResponseThresholds(config)
}

func validateResponseSelectors(config domain.EndpointResponsePolicyConfig) error {
	for _, ids := range [][]string{config.SiteIDs, config.DeviceIDs, config.NasBindingIDs, config.Subjects.Roles, config.Subjects.Users, config.Subjects.Teams, config.Subjects.Tags} {
		seen := map[string]bool{}
		for _, id := range ids {
			if !runtimeIdentifierPattern.MatchString(id) || seen[id] {
				return invalidResponse("selector")
			}
			seen[id] = true
		}
	}
	return nil
}

func validateResponseThresholds(config domain.EndpointResponsePolicyConfig) error {
	if config.HeartbeatIntervalSeconds < 30 || config.HeartbeatIntervalSeconds > 300 || config.MissedHeartbeats < 1 || config.MissedHeartbeats > 20 || config.GracePeriodSeconds < 0 || config.GracePeriodSeconds > 3600 || config.AccountingFreshnessSeconds < 30 || config.AccountingFreshnessSeconds > 3600 {
		return invalidResponse("detection_threshold")
	}
	if config.RestoreHeartbeats < 1 || config.RestoreHeartbeats > 20 || config.RestoreObservationSeconds < 30 || config.RestoreObservationSeconds > 3600 {
		return invalidResponse("recovery_threshold")
	}
	return nil
}

func validateResponseSteps(config domain.EndpointResponsePolicyConfig) error {
	if len(config.Steps) < 1 || len(config.Steps) > 4 {
		return invalidResponse("steps")
	}
	last := -1
	profile := ""
	disconnect := false
	for _, step := range config.Steps {
		if !step.Action.Valid() || step.AfterSeconds < 0 || step.AfterSeconds > 86400 || step.AfterSeconds <= last || disconnect {
			return invalidResponse("step_order")
		}
		last = step.AfterSeconds
		switch step.Action {
		case sohaapi.NetworkEndpointResponseActionNotify:
			if config.NotificationPolicyID == "" {
				return invalidResponse("notification_route_required")
			}
		case sohaapi.NetworkEndpointResponseActionRestrict:
			if !step.TargetAccessProfile.Valid() || domain.RestrictAccessProfile(string(step.TargetAccessProfile), profile) != string(step.TargetAccessProfile) {
				return invalidResponse("restriction_order")
			}
			profile = string(step.TargetAccessProfile)
		case sohaapi.NetworkEndpointResponseActionDisconnect:
			disconnect = true
		}
		if step.Action != sohaapi.NetworkEndpointResponseActionRestrict && step.TargetAccessProfile != "" {
			return invalidResponse("unexpected_profile")
		}
	}
	return nil
}

func invalidResponse(reason string) error {
	return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "endpoint_response_"+reason, "The endpoint response configuration is invalid.", "终端联动配置无效。")
}
