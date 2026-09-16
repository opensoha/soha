package networkaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const defaultAccessGrantTTL = 5 * time.Minute

type AccessGrantStore interface {
	CreateAccessGrant(context.Context, domainnetworkruntime.AccessGrant) error
	GetAccessGrant(context.Context, string, time.Time) (domainnetworkruntime.AccessGrant, error)
	ListAccessGrants(context.Context, domainnetworkruntime.AccessGrantFilter, time.Time) ([]domainnetworkruntime.AccessGrant, error)
	RevokeAccessGrant(context.Context, string, time.Time) error
}

type AccessGrantPolicyStore interface {
	GetSubject(context.Context, string) (domainnetworkaccess.Subject, error)
	GetDevice(context.Context, string) (domainnetworkaccess.Device, error)
	GetSite(context.Context, string) (domainnetworkaccess.Site, error)
	GetSpace(context.Context, string) (domainnetworkaccess.Space, error)
	GetResource(context.Context, string) (domainnetworkaccess.Resource, error)
	GetPolicySnapshot(context.Context) (domainnetworkaccess.PolicySnapshot, error)
}

type StepUpVerifier interface {
	RequireRecentStepUp(context.Context, string, string) error
}

type AccessGrantInput struct {
	DeviceID       string
	SiteID         string
	NetworkSpaceID string
	Mode           string
	ResourceIDs    []string
	TTL            time.Duration
}

type AccessGrantService struct {
	store       AccessGrantStore
	policy      AccessGrantPolicyStore
	stepUp      StepUpVerifier
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
	now         func() time.Time
}

func NewAccessGrantService(store AccessGrantStore, policy AccessGrantPolicyStore, stepUp StepUpVerifier, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder) (*AccessGrantService, error) {
	for name, dependency := range map[string]any{
		"store": store, "policy": policy, "stepUp": stepUp, "permissions": permissions, "audit": audit, "operations": operations,
	} {
		if isNilDependency(dependency) {
			return nil, fmt.Errorf("network access grant service: %s dependency is required", name)
		}
	}
	return &AccessGrantService{store: store, policy: policy, stepUp: stepUp, permissions: permissions, audit: audit, operations: operations, now: time.Now}, nil
}

func (s *AccessGrantService) Create(ctx context.Context, principal domainidentity.Principal, authSessionID string, input AccessGrantInput) (domainnetworkruntime.AccessGrantSecret, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessAccessGrantsCreate); err != nil {
		return domainnetworkruntime.AccessGrantSecret{}, err
	}
	input = normalizeAccessGrantInput(input)
	if input.TTL == 0 {
		input.TTL = defaultAccessGrantTTL
	}
	authSessionID = strings.TrimSpace(authSessionID)
	if err := validateAccessGrantInput(authSessionID, input); err != nil {
		return domainnetworkruntime.AccessGrantSecret{}, err
	}
	if err := s.stepUp.RequireRecentStepUp(ctx, principal.UserID, authSessionID); err != nil {
		return domainnetworkruntime.AccessGrantSecret{}, err
	}

	snapshot, err := s.authorizeResources(ctx, principal.UserID, input)
	if err != nil {
		return domainnetworkruntime.AccessGrantSecret{}, err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return domainnetworkruntime.AccessGrantSecret{}, fmt.Errorf("generate network access grant token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	digest := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	grant := domainnetworkruntime.AccessGrant{
		ID: uuid.NewString(), SubjectID: principal.UserID, AuthSessionID: authSessionID, DeviceID: input.DeviceID,
		SiteID: input.SiteID, NetworkSpaceID: input.NetworkSpaceID, Mode: input.Mode, ResourceIDs: slices.Clone(input.ResourceIDs),
		PolicyVersion: snapshot.PolicyVersion, Status: domainnetworkruntime.AccessGrantIssued,
		TokenHash: fmt.Sprintf("sha256:%x", digest), ReasonCode: "policy_allowed", ExpiresAt: now.Add(input.TTL),
		CreatedBy: principal.UserID, CreatedAt: now, UpdatedAt: now, ResourceLeaseIDs: []string{},
	}
	if err := s.store.CreateAccessGrant(ctx, grant); err != nil {
		return domainnetworkruntime.AccessGrantSecret{}, err
	}
	s.recordMutation(ctx, principal, "network_access.access_grants.create", grant)
	return domainnetworkruntime.AccessGrantSecret{Grant: publicAccessGrant(grant), Token: token}, nil
}

func (s *AccessGrantService) List(ctx context.Context, principal domainidentity.Principal, filter domainnetworkruntime.AccessGrantFilter) ([]domainnetworkruntime.AccessGrant, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessAccessGrantsView); err != nil {
		return nil, err
	}
	filter.SubjectID, filter.DeviceID, filter.Status = strings.TrimSpace(filter.SubjectID), strings.TrimSpace(filter.DeviceID), strings.TrimSpace(filter.Status)
	if err := validateAccessGrantFilter(filter); err != nil {
		return nil, err
	}
	filter.Limit = boundedLimit(filter.Limit)
	items, err := s.store.ListAccessGrants(ctx, filter, s.now().UTC())
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index] = publicAccessGrant(items[index])
	}
	return items, nil
}

func (s *AccessGrantService) Get(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkruntime.AccessGrant, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessAccessGrantsView); err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	id = strings.TrimSpace(id)
	if err := requiredText("grantId", id, 128); err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	grant, err := s.store.GetAccessGrant(ctx, id, s.now().UTC())
	return publicAccessGrant(grant), err
}

func (s *AccessGrantService) Revoke(ctx context.Context, principal domainidentity.Principal, id string) (domainnetworkruntime.AccessGrant, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermNetworkAccessAccessGrantsRevoke); err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	id = strings.TrimSpace(id)
	if err := requiredText("grantId", id, 128); err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	grant, err := s.store.GetAccessGrant(ctx, id, s.now().UTC())
	if err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	now := s.now().UTC()
	if err := s.store.RevokeAccessGrant(ctx, grant.ID, now); err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	grant, err = s.store.GetAccessGrant(ctx, grant.ID, now)
	if err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	s.recordMutation(ctx, principal, "network_access.access_grants.revoke", grant)
	return publicAccessGrant(grant), nil
}

func (s *AccessGrantService) authorizeResources(ctx context.Context, subjectID string, input AccessGrantInput) (domainnetworkaccess.PolicySnapshot, error) {
	return authorizeNetworkResources(ctx, s.policy, subjectID, input)
}

func authorizeNetworkResources(ctx context.Context, policy AccessGrantPolicyStore, subjectID string, input AccessGrantInput) (domainnetworkaccess.PolicySnapshot, error) {
	subject, err := policy.GetSubject(ctx, subjectID)
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	device, err := policy.GetDevice(ctx, input.DeviceID)
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	site, err := policy.GetSite(ctx, input.SiteID)
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	space, err := policy.GetSpace(ctx, input.NetworkSpaceID)
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	snapshot, err := policy.GetPolicySnapshot(ctx)
	if err != nil {
		return domainnetworkaccess.PolicySnapshot{}, err
	}
	wantNetworkLease := input.Mode == domainnetworkaccess.ModeExternalVPNZTNA
	accessProfile := ""
	for _, resourceID := range input.ResourceIDs {
		resource, err := policy.GetResource(ctx, resourceID)
		if err != nil {
			return domainnetworkaccess.PolicySnapshot{}, err
		}
		if _, supported := domainnetworkaccess.WireGuardResourceTarget(resource, space); !supported {
			return domainnetworkaccess.PolicySnapshot{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "resource_transport_not_supported", "The resource cannot be enforced by the WireGuard ZTNA path.", "该资源无法通过 WireGuard ZTNA 路径执行。")
		}
		preview := EvaluatePolicy(PreviewInput{SubjectUserID: subjectID, DeviceID: input.DeviceID, ResourceID: resourceID, SiteID: input.SiteID, Mode: input.Mode}, subject, device, site, space, resource, snapshot)
		if preview.Decision != domainnetworkaccess.DecisionAllow || !preview.ResourceLeaseRequired || preview.NetworkLeaseRequired != wantNetworkLease {
			return domainnetworkaccess.PolicySnapshot{}, apperrors.NewBusiness(apperrors.ErrAccessDenied, "network_access_denied", "The requested resource access is not allowed.", "请求的资源访问未获授权。")
		}
		if accessProfile != "" && accessProfile != preview.NetworkProfile {
			return domainnetworkaccess.PolicySnapshot{}, apperrors.NewBusiness(apperrors.ErrConflict, "resource_access_profile_conflict", "The requested resources require different access profiles.", "请求的资源需要不同的访问等级。")
		}
		accessProfile = preview.NetworkProfile
	}
	return snapshot, nil
}

func normalizeAccessGrantInput(input AccessGrantInput) AccessGrantInput {
	input.DeviceID, input.SiteID, input.NetworkSpaceID, input.Mode = strings.TrimSpace(input.DeviceID), strings.TrimSpace(input.SiteID), strings.TrimSpace(input.NetworkSpaceID), strings.TrimSpace(input.Mode)
	for index := range input.ResourceIDs {
		input.ResourceIDs[index] = strings.TrimSpace(input.ResourceIDs[index])
	}
	slices.Sort(input.ResourceIDs)
	return input
}

func validateAccessGrantInput(authSessionID string, input AccessGrantInput) error {
	for name, value := range map[string]string{"sessionId": authSessionID, "deviceId": input.DeviceID, "siteId": input.SiteID, "networkSpaceId": input.NetworkSpaceID} {
		if err := requiredText(name, value, 128); err != nil {
			return err
		}
	}
	if !oneOf(input.Mode, domainnetworkaccess.ModeInternalZTNA, domainnetworkaccess.ModeExternalVPNZTNA, domainnetworkaccess.ModeExternalDirectZTNA) {
		return invalid("mode is invalid")
	}
	if input.TTL < time.Minute || input.TTL > defaultAccessGrantTTL {
		return invalid("ttlSeconds must be between 60 and 300")
	}
	if len(input.ResourceIDs) == 0 {
		return invalid("resourceIds must contain between 1 and 256 entries")
	}
	return validateStringList("resourceIds", input.ResourceIDs, 256, nil)
}

func validateAccessGrantFilter(filter domainnetworkruntime.AccessGrantFilter) error {
	if err := validateListFilter("", filter.Limit); err != nil {
		return err
	}
	for name, value := range map[string]string{"subjectId": filter.SubjectID, "deviceId": filter.DeviceID} {
		if err := optionalText(name, value, 128); err != nil {
			return err
		}
	}
	if filter.Status != "" && !oneOf(filter.Status, domainnetworkruntime.AccessGrantIssued, domainnetworkruntime.AccessGrantConsumed, domainnetworkruntime.AccessGrantRevoked, domainnetworkruntime.AccessGrantExpired) {
		return invalid("status is invalid")
	}
	return nil
}

func publicAccessGrant(grant domainnetworkruntime.AccessGrant) domainnetworkruntime.AccessGrant {
	grant.AuthSessionID, grant.TokenHash = "", ""
	grant.ResourceIDs = slices.Clone(grant.ResourceIDs)
	grant.ResourceLeaseIDs = slices.Clone(grant.ResourceLeaseIDs)
	return grant
}

func (s *AccessGrantService) recordMutation(ctx context.Context, principal domainidentity.Principal, action string, grant domainnetworkruntime.AccessGrant) {
	meta := requestctx.FromContext(ctx)
	metadata := map[string]any{
		"grantId": grant.ID, "subjectId": grant.SubjectID, "deviceId": grant.DeviceID, "siteId": grant.SiteID,
		"networkSpaceId": grant.NetworkSpaceID, "mode": grant.Mode, "resourceIds": slices.Clone(grant.ResourceIDs),
		"policyVersion": grant.PolicyVersion, "status": grant.Status, "expiresAt": grant.ExpiresAt,
	}
	summary := action + " succeeded"
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "NetworkAccessGrant", ResourceName: grant.ID, Action: action, Result: "success", Summary: summary,
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: metadata,
	})
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, action, map[string]any{
		"module": "network_access", "resourceKind": "NetworkAccessGrant", "targetId": grant.ID,
		"targetLabel": grant.ID, "deviceId": grant.DeviceID, "mode": grant.Mode,
	}, "success", summary, nil))
}
