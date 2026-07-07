package providerportal

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

var slugCleaner = regexp.MustCompile(`[^a-z0-9-]+`)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OIDCLaunchResolver interface {
	OIDCLaunchURL(context.Context, domainportal.Application) (string, error)
}

type ProfileReader interface {
	CurrentProfile(context.Context, domainidentity.Principal) (domainidentity.UserProfile, error)
}

type Service struct {
	repo               domainportal.Repository
	permissions        *appaccess.PermissionResolver
	audit              AuditRecorder
	oidcLaunchResolver OIDCLaunchResolver
	profileReader      ProfileReader
}

func New(repo domainportal.Repository, permissions *appaccess.PermissionResolver, audit AuditRecorder) *Service {
	return &Service{repo: repo, permissions: permissions, audit: audit}
}

func (s *Service) SetOIDCLaunchResolver(resolver OIDCLaunchResolver) {
	s.oidcLaunchResolver = resolver
}

func (s *Service) SetProfileReader(reader ProfileReader) {
	s.profileReader = reader
}

func (s *Service) PortalBootstrap(ctx context.Context, principal domainidentity.Principal) (domainportal.PortalBootstrap, error) {
	applications, err := s.ListPortalApplications(ctx, principal)
	if err != nil {
		return domainportal.PortalBootstrap{}, err
	}
	recent, err := s.ListRecent(ctx, principal, 8)
	if err != nil {
		return domainportal.PortalBootstrap{}, err
	}
	favorites := make([]domainportal.Application, 0)
	for _, item := range applications {
		if item.Favorite {
			favorites = append(favorites, item)
		}
	}
	return domainportal.PortalBootstrap{
		Principal:    principal,
		Applications: applications,
		Favorites:    favorites,
		Recent:       recent,
		Categories:   categoriesForApplications(applications),
		Security:     s.SecuritySummary(ctx, principal),
	}, nil
}

func (s *Service) ListPortalApplications(ctx context.Context, principal domainidentity.Principal) ([]domainportal.Application, error) {
	if err := requireAuthenticated(principal); err != nil {
		return nil, err
	}
	items, err := s.repo.ListPortalApplications(ctx)
	if err != nil {
		return nil, err
	}
	items = filterApplicationsByAccess(principal, items)
	return s.enrichPortalApplications(ctx, principal.UserID, items)
}

func (s *Service) GetPortalApplication(ctx context.Context, principal domainidentity.Principal, applicationID string) (domainportal.Application, error) {
	if err := requireAuthenticated(principal); err != nil {
		return domainportal.Application{}, err
	}
	item, err := s.repo.GetApplication(ctx, applicationID)
	if err != nil {
		return domainportal.Application{}, err
	}
	if item.Status != domainportal.ApplicationStatusEnabled || !item.PortalVisible || !domainportal.CanAccessApplication(principal, item) {
		return domainportal.Application{}, fmt.Errorf("%w: application is not available", apperrors.ErrAccessDenied)
	}
	items, err := s.enrichPortalApplications(ctx, principal.UserID, []domainportal.Application{item})
	if err != nil {
		return domainportal.Application{}, err
	}
	return items[0], nil
}

func (s *Service) Launch(ctx context.Context, principal domainidentity.Principal, applicationID string) (domainportal.LaunchDecision, error) {
	if err := requireAuthenticated(principal); err != nil {
		return domainportal.LaunchDecision{}, err
	}
	item, err := s.GetPortalApplication(ctx, principal, applicationID)
	if err != nil {
		s.recordLaunch(ctx, principal, domainportal.Application{ID: applicationID}, "denied", err.Error(), "")
		return domainportal.LaunchDecision{}, err
	}
	launchURL, err := s.launchURL(ctx, item)
	if err != nil {
		s.recordLaunch(ctx, principal, item, "denied", err.Error(), "")
		return domainportal.LaunchDecision{}, err
	}
	if strings.TrimSpace(launchURL) == "" {
		reason := "launch URL is not configured"
		s.recordLaunch(ctx, principal, item, "denied", reason, "")
		return domainportal.LaunchDecision{}, fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, reason)
	}
	s.recordLaunch(ctx, principal, item, "allow", "portal launch allowed", launchURL)
	s.recordAudit(ctx, principal, "portal.launch", "success", item, "launched identity application", map[string]any{
		"providerType": item.ProviderType,
		"launchUrl":    launchURL,
	})
	return domainportal.LaunchDecision{
		Application:  item,
		LaunchURL:    launchURL,
		ProviderType: item.ProviderType,
		Decision:     "allow",
	}, nil
}

func (s *Service) launchURL(ctx context.Context, item domainportal.Application) (string, error) {
	if item.ProviderType == domainportal.ProviderTypeOIDC && s.oidcLaunchResolver != nil {
		return s.oidcLaunchResolver.OIDCLaunchURL(ctx, item)
	}
	return strings.TrimSpace(item.LaunchURL), nil
}

func (s *Service) SetFavorite(ctx context.Context, principal domainidentity.Principal, applicationID string) (domainportal.Application, error) {
	item, err := s.GetPortalApplication(ctx, principal, applicationID)
	if err != nil {
		return domainportal.Application{}, err
	}
	if err := s.repo.SetFavorite(ctx, principal.UserID, item.ID); err != nil {
		return domainportal.Application{}, err
	}
	item.Favorite = true
	return item, nil
}

func (s *Service) DeleteFavorite(ctx context.Context, principal domainidentity.Principal, applicationID string) error {
	if err := requireAuthenticated(principal); err != nil {
		return err
	}
	item, err := s.repo.GetApplication(ctx, applicationID)
	if err != nil {
		return err
	}
	return s.repo.DeleteFavorite(ctx, principal.UserID, item.ID)
}

func (s *Service) ListRecent(ctx context.Context, principal domainidentity.Principal, limit int) ([]domainportal.ApplicationLaunch, error) {
	if err := requireAuthenticated(principal); err != nil {
		return nil, err
	}
	return s.repo.ListRecentLaunches(ctx, principal.UserID, limit)
}

func (s *Service) SecuritySummary(ctx context.Context, principal domainidentity.Principal) domainportal.PortalSecuritySummary {
	activeSession := 0
	if principal.UserID != "" {
		activeSession = 1
	}
	if s.profileReader != nil && principal.UserID != "" {
		if profile, err := s.profileReader.CurrentProfile(ctx, principal); err == nil {
			return domainportal.PortalSecuritySummary{
				Principal:     principalForProfile(principal, profile),
				MFAEnabled:    false,
				LinkedSources: linkedSourcesForProfile(profile),
				ActiveSession: len(profile.Sessions),
				RecentLoginAt: profile.LastLoginAt,
			}
		}
	}
	return domainportal.PortalSecuritySummary{
		Principal:     principal,
		MFAEnabled:    false,
		LinkedSources: []string{},
		ActiveSession: activeSession,
	}
}

func (s *Service) ListApplications(ctx context.Context, principal domainidentity.Principal, filter domainportal.ApplicationFilter) ([]domainportal.Application, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityApplicationsView); err != nil {
		return nil, err
	}
	return s.repo.ListApplications(ctx, filter)
}

func (s *Service) GetApplication(ctx context.Context, principal domainidentity.Principal, applicationID string) (domainportal.Application, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityApplicationsView); err != nil {
		return domainportal.Application{}, err
	}
	return s.repo.GetApplication(ctx, applicationID)
}

func (s *Service) CreateApplication(ctx context.Context, principal domainidentity.Principal, input domainportal.ApplicationInput) (domainportal.Application, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityApplicationsManage); err != nil {
		return domainportal.Application{}, err
	}
	now := time.Now().UTC()
	item, assignments, err := applicationFromInput("", input, principal, now)
	if err != nil {
		return domainportal.Application{}, err
	}
	item.CreatedAt = now
	item.UpdatedAt = now
	item.CreatedBy = actorID(principal)
	item.UpdatedBy = actorID(principal)
	if err := s.validateApplicationProviderBinding(ctx, item, false); err != nil {
		return domainportal.Application{}, err
	}
	item, err = s.repo.CreateApplicationWithAssignments(ctx, item, assignmentsForApplication(item.ID, assignments))
	if err != nil {
		return domainportal.Application{}, err
	}
	item, err = s.repo.GetApplication(ctx, item.ID)
	if err != nil {
		return domainportal.Application{}, err
	}
	s.recordAudit(ctx, principal, "identity.application.create", "success", item, "created identity application", map[string]any{"providerType": item.ProviderType})
	return item, nil
}

func (s *Service) UpdateApplication(ctx context.Context, principal domainidentity.Principal, applicationID string, input domainportal.ApplicationInput) (domainportal.Application, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityApplicationsManage); err != nil {
		return domainportal.Application{}, err
	}
	current, err := s.repo.GetApplication(ctx, applicationID)
	if err != nil {
		return domainportal.Application{}, err
	}
	now := time.Now().UTC()
	item, assignments, err := applicationFromInput(current.ID, input, principal, now)
	if err != nil {
		return domainportal.Application{}, err
	}
	item.CreatedAt = current.CreatedAt
	item.CreatedBy = current.CreatedBy
	item.UpdatedAt = now
	item.UpdatedBy = actorID(principal)
	if err := s.validateApplicationProviderBinding(ctx, item, true); err != nil {
		return domainportal.Application{}, err
	}
	item, err = s.repo.UpdateApplicationWithAssignments(ctx, item, assignmentsForApplication(item.ID, assignments))
	if err != nil {
		return domainportal.Application{}, err
	}
	item, err = s.repo.GetApplication(ctx, item.ID)
	if err != nil {
		return domainportal.Application{}, err
	}
	s.recordAudit(ctx, principal, "identity.application.update", "success", item, "updated identity application", map[string]any{"providerType": item.ProviderType})
	return item, nil
}

func (s *Service) DeleteApplication(ctx context.Context, principal domainidentity.Principal, applicationID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityApplicationsManage); err != nil {
		return err
	}
	item, err := s.repo.GetApplication(ctx, applicationID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteApplication(ctx, item.ID); err != nil {
		return err
	}
	s.recordAudit(ctx, principal, "identity.application.delete", "success", item, "deleted identity application", map[string]any{"providerType": item.ProviderType})
	return nil
}

func (s *Service) ListPolicies(ctx context.Context, principal domainidentity.Principal, filter domainportal.ApplicationFilter) ([]domainportal.ApplicationPolicy, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityPoliciesView); err != nil {
		return nil, err
	}
	applications, err := s.repo.ListApplications(ctx, filter)
	if err != nil {
		return nil, err
	}
	policies := make([]domainportal.ApplicationPolicy, 0, len(applications))
	for _, application := range applications {
		policies = append(policies, policyForApplication(application))
	}
	return policies, nil
}

func (s *Service) GetPolicy(ctx context.Context, principal domainidentity.Principal, applicationID string) (domainportal.ApplicationPolicy, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityPoliciesView); err != nil {
		return domainportal.ApplicationPolicy{}, err
	}
	application, err := s.repo.GetApplication(ctx, applicationID)
	if err != nil {
		return domainportal.ApplicationPolicy{}, err
	}
	return policyForApplication(application), nil
}

func (s *Service) UpdatePolicy(ctx context.Context, principal domainidentity.Principal, applicationID string, input domainportal.ApplicationPolicyInput) (domainportal.ApplicationPolicy, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityPoliciesManage); err != nil {
		return domainportal.ApplicationPolicy{}, err
	}
	application, err := s.repo.GetApplication(ctx, applicationID)
	if err != nil {
		return domainportal.ApplicationPolicy{}, err
	}
	assignments, err := normalizeAssignments(input.Assignments, principal, time.Now().UTC())
	if err != nil {
		return domainportal.ApplicationPolicy{}, err
	}
	if err := s.repo.ReplaceAssignments(ctx, application.ID, assignmentsForApplication(application.ID, assignments)); err != nil {
		return domainportal.ApplicationPolicy{}, err
	}
	application, err = s.repo.GetApplication(ctx, application.ID)
	if err != nil {
		return domainportal.ApplicationPolicy{}, err
	}
	s.recordAudit(ctx, principal, "identity.policy.update", "success", application, "updated identity application access policy", map[string]any{
		"assignmentCount": len(application.Assignments),
		"providerType":    application.ProviderType,
	})
	return policyForApplication(application), nil
}

func (s *Service) ProviderCapabilities() []domainportal.ProviderCapability {
	return []domainportal.ProviderCapability{
		{
			Type:        domainportal.ProviderTypeOIDC,
			Status:      "enabled",
			Endpoints:   []string{"/.well-known/openid-configuration", "/oauth2/authorize", "/oauth2/token", "/oauth2/userinfo", "/oauth2/jwks", "/oauth2/introspect", "/oauth2/revoke"},
			Description: "OIDC provider baseline supports discovery, authorization code flow, token exchange, userinfo, JWKS, introspection, and revocation compatibility.",
		},
		{
			Type:        domainportal.ProviderTypeProxy,
			Status:      "enabled",
			Endpoints:   []string{"/api/v1/provider/proxy/auth", "/api/v1/provider/proxy/start", "/api/v1/provider/proxy/callback"},
			Description: "Proxy provider baseline supports embedded forward-auth checks with Soha identity headers.",
		},
	}
}

func (s *Service) validateApplicationProviderBinding(ctx context.Context, item domainportal.Application, existingApplication bool) error {
	providerID := strings.TrimSpace(item.ProviderID)
	if item.ProviderType == domainportal.ProviderTypeLink {
		if providerID != "" {
			return fmt.Errorf("%w: link applications cannot bind an identity provider", apperrors.ErrInvalidArgument)
		}
		return nil
	}
	if providerID == "" {
		return nil
	}
	if !existingApplication {
		return fmt.Errorf("%w: identity provider can only be attached after the application is created", apperrors.ErrInvalidArgument)
	}
	return s.repo.ValidateProviderBinding(ctx, providerID, item.ID, item.ProviderType)
}

func requireAuthenticated(principal domainidentity.Principal) error {
	if strings.TrimSpace(principal.UserID) == "" {
		return fmt.Errorf("%w: authentication required", apperrors.ErrUnauthorized)
	}
	return nil
}

func (s *Service) enrichPortalApplications(ctx context.Context, userID string, items []domainportal.Application) ([]domainportal.Application, error) {
	favorites, err := s.repo.ListFavoriteApplicationIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	lastLaunches, err := s.repo.GetLastLaunches(ctx, userID)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].Favorite = favorites[items[index].ID]
		if launchedAt, ok := lastLaunches[items[index].ID]; ok {
			value := launchedAt
			items[index].LastLaunchedAt = &value
		}
	}
	return items, nil
}

func applicationFromInput(applicationID string, input domainportal.ApplicationInput, principal domainidentity.Principal, now time.Time) (domainportal.Application, []domainportal.ApplicationAssignment, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainportal.Application{}, nil, fmt.Errorf("%w: application name is required", apperrors.ErrInvalidArgument)
	}
	providerType := normalizeProviderType(input.ProviderType)
	if providerType == "" {
		return domainportal.Application{}, nil, fmt.Errorf("%w: provider type is required", apperrors.ErrInvalidArgument)
	}
	status := normalizeStatus(input.Status)
	if status == "" {
		return domainportal.Application{}, nil, fmt.Errorf("%w: invalid application status", apperrors.ErrInvalidArgument)
	}
	launchURL := strings.TrimSpace(input.LaunchURL)
	if providerType == domainportal.ProviderTypeLink && launchURL == "" {
		return domainportal.Application{}, nil, fmt.Errorf("%w: launch URL is required for link applications", apperrors.ErrInvalidArgument)
	}
	if launchURL != "" && !isSafeLaunchURL(launchURL) {
		return domainportal.Application{}, nil, fmt.Errorf("%w: launch URL must be http(s) or an absolute internal path", apperrors.ErrInvalidArgument)
	}
	if applicationID == "" {
		applicationID = uuid.NewString()
	}
	slug := normalizeSlug(input.Slug)
	if slug == "" {
		slug = normalizeSlug(name)
	}
	if slug == "" {
		slug = applicationID
	}
	sortOrder := input.SortOrder
	if sortOrder == 0 {
		sortOrder = 1000
	}
	item := domainportal.Application{
		ID:            applicationID,
		Slug:          slug,
		Name:          name,
		Description:   strings.TrimSpace(input.Description),
		IconURL:       strings.TrimSpace(input.IconURL),
		Category:      strings.TrimSpace(input.Category),
		Tags:          compactStrings(input.Tags),
		LaunchURL:     launchURL,
		ProviderID:    strings.TrimSpace(input.ProviderID),
		ProviderType:  providerType,
		PortalVisible: input.PortalVisible,
		Featured:      input.Featured,
		SortOrder:     sortOrder,
		Status:        status,
		Metadata:      input.Metadata,
		CreatedBy:     actorID(principal),
		UpdatedBy:     actorID(principal),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	assignments, err := normalizeAssignments(input.Assignments, principal, now)
	if err != nil {
		return domainportal.Application{}, nil, err
	}
	return item, assignments, nil
}

func normalizeProviderType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return domainportal.ProviderTypeLink
	case domainportal.ProviderTypeLink, domainportal.ProviderTypeOIDC, domainportal.ProviderTypeProxy:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return domainportal.ApplicationStatusDraft
	case domainportal.ApplicationStatusDraft, domainportal.ApplicationStatusEnabled, domainportal.ApplicationStatusDisabled, domainportal.ApplicationStatusMaintenance:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeAssignments(inputs []domainportal.ApplicationAssignmentInput, principal domainidentity.Principal, now time.Time) ([]domainportal.ApplicationAssignment, error) {
	out := make([]domainportal.ApplicationAssignment, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		subjectType := strings.ToLower(strings.TrimSpace(input.SubjectType))
		subjectID := strings.TrimSpace(input.SubjectID)
		if subjectType == "" && subjectID == "" {
			continue
		}
		if subjectID == "" {
			return nil, fmt.Errorf("%w: assignment subject id is required", apperrors.ErrInvalidArgument)
		}
		switch subjectType {
		case domainportal.AssignmentSubjectUser, domainportal.AssignmentSubjectRole, domainportal.AssignmentSubjectTeam, domainportal.AssignmentSubjectTag:
		default:
			return nil, fmt.Errorf("%w: unsupported assignment subject type", apperrors.ErrInvalidArgument)
		}
		effect := strings.ToLower(strings.TrimSpace(input.Effect))
		if effect == "" {
			effect = domainportal.AssignmentEffectAllow
		}
		if effect != domainportal.AssignmentEffectAllow {
			return nil, fmt.Errorf("%w: only allow assignments are supported in this phase", apperrors.ErrInvalidArgument)
		}
		key := subjectType + ":" + subjectID + ":" + effect
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, domainportal.ApplicationAssignment{
			ID:          uuid.NewString(),
			SubjectType: subjectType,
			SubjectID:   subjectID,
			Effect:      effect,
			CreatedBy:   actorID(principal),
			CreatedAt:   now,
		})
	}
	return out, nil
}

func assignmentsForApplication(applicationID string, assignments []domainportal.ApplicationAssignment) []domainportal.ApplicationAssignment {
	out := make([]domainportal.ApplicationAssignment, len(assignments))
	for index, item := range assignments {
		item.ApplicationID = applicationID
		out[index] = item
	}
	return out
}

func policyForApplication(item domainportal.Application) domainportal.ApplicationPolicy {
	assignments := append([]domainportal.ApplicationAssignment(nil), item.Assignments...)
	return domainportal.ApplicationPolicy{
		ApplicationID:   item.ID,
		ApplicationSlug: item.Slug,
		ApplicationName: item.Name,
		Category:        item.Category,
		ProviderID:      item.ProviderID,
		ProviderType:    item.ProviderType,
		PortalVisible:   item.PortalVisible,
		Status:          item.Status,
		Assignments:     assignments,
		UpdatedAt:       item.UpdatedAt,
	}
}

func filterApplicationsByAccess(principal domainidentity.Principal, items []domainportal.Application) []domainportal.Application {
	out := make([]domainportal.Application, 0, len(items))
	for _, item := range items {
		if domainportal.CanAccessApplication(principal, item) {
			out = append(out, item)
		}
	}
	return out
}

func categoriesForApplications(items []domainportal.Application) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, item := range items {
		category := strings.TrimSpace(item.Category)
		if category == "" || seen[category] {
			continue
		}
		seen[category] = true
		out = append(out, category)
	}
	sort.Strings(out)
	return out
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = slugCleaner.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return value
}

func isSafeLaunchURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.IsAbs() {
		return parsed.Scheme == "http" || parsed.Scheme == "https"
	}
	return strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")
}

func principalForProfile(principal domainidentity.Principal, profile domainidentity.UserProfile) domainidentity.Principal {
	out := principal
	if profile.UserID != "" {
		out.UserID = profile.UserID
	}
	if profile.DisplayName != "" {
		out.UserName = profile.DisplayName
	} else if profile.Username != "" {
		out.UserName = profile.Username
	}
	if profile.Email != "" {
		out.Email = profile.Email
	}
	if len(profile.Roles) > 0 {
		out.Roles = append([]string(nil), profile.Roles...)
	}
	if len(profile.Teams) > 0 {
		out.Teams = append([]string(nil), profile.Teams...)
	}
	if len(profile.Projects) > 0 {
		out.Projects = append([]string(nil), profile.Projects...)
	}
	if len(profile.Tags) > 0 {
		out.Tags = append([]string(nil), profile.Tags...)
	}
	return out
}

func linkedSourcesForProfile(profile domainidentity.UserProfile) []string {
	values := make([]string, 0, len(profile.Identities))
	for _, identity := range profile.Identities {
		source := firstNonEmpty(identity.ProviderID, identity.ProviderType)
		if source == "" {
			continue
		}
		if identity.ProviderType != "" && identity.ProviderID != "" && identity.ProviderID != identity.ProviderType {
			source = identity.ProviderType + ":" + identity.ProviderID
		}
		values = append(values, source)
	}
	return compactStrings(values)
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func actorID(principal domainidentity.Principal) string {
	return firstNonEmpty(principal.UserID, principal.UserName, "system")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Service) recordLaunch(ctx context.Context, principal domainidentity.Principal, item domainportal.Application, result, reason, launchURL string) {
	metadata := requestctx.FromContext(ctx)
	_ = s.repo.RecordLaunch(ctx, domainportal.ApplicationLaunch{
		ID:            uuid.NewString(),
		ApplicationID: item.ID,
		UserID:        principal.UserID,
		ProviderID:    item.ProviderID,
		ProviderType:  firstNonEmpty(item.ProviderType, domainportal.ProviderTypeLink),
		Result:        result,
		Reason:        reason,
		LaunchURL:     launchURL,
		SourceIP:      metadata.SourceIP,
		UserAgent:     metadata.UserAgent,
		CreatedAt:     time.Now().UTC(),
	})
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, action, result string, item domainportal.Application, summary string, metadata map[string]any) {
	if s.audit == nil {
		return
	}
	requestMetadata := requestctx.FromContext(ctx)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["applicationId"] = item.ID
	metadata["applicationName"] = item.Name
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       actorID(principal),
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		ResourceKind:  "identity_application",
		ResourceName:  item.ID,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   requestMetadata.Path,
		RequestMethod: requestMetadata.Method,
		RequestID:     requestMetadata.RequestID,
		SourceIP:      requestMetadata.SourceIP,
		Metadata:      metadata,
	})
}
