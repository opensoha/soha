package copilot

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type RuntimeProfileStore interface {
	PublishRuntimeProfile(context.Context, string, int64, domain.RuntimeProfile) (domain.RuntimeProfile, error)
	ListRuntimeProfileRevisions(context.Context, string) ([]domain.RuntimeProfile, error)
	ListCurrentRuntimeProfiles(context.Context, []string) ([]domain.RuntimeProfile, error)
	GetCurrentRuntimeProfile(context.Context, string, string) (domain.RuntimeProfile, error)
	RuntimeProfileRevisionAuthorized(context.Context, string, int64, string) (bool, error)
	SelectSessionRuntimeProfile(context.Context, string, string, string) (domain.Session, error)
}

func WithRuntimeProfiles(store RuntimeProfileStore) Option {
	return func(s *Service) { s.runtimeProfiles = store }
}

var (
	runtimeProfileIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	runtimeAssetVersionPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	errRuntimeProfileUnavailable = apperrors.NewBusiness(apperrors.ErrNotFound, "runtime_profile_unavailable", "The runtime profile is unavailable in this scope.", "运行配置在当前作用域不可用。")
)

func (s *Service) ListRuntimeProfileRevisions(ctx context.Context, principal identity.Principal, id string) ([]domain.RuntimeProfile, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermSettingsAIView); err != nil {
		return nil, err
	}
	if !runtimeProfileIDPattern.MatchString(id) {
		return nil, apperrors.ErrInvalidArgument
	}
	if s.runtimeProfiles == nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	return s.runtimeProfiles.ListRuntimeProfileRevisions(ctx, id)
}

func (s *Service) PublishRuntimeProfile(ctx context.Context, principal identity.Principal, expectedRevision int64, profile domain.RuntimeProfile) (domain.RuntimeProfile, error) {
	if principal.UserID == "" {
		return domain.RuntimeProfile{}, apperrors.ErrAccessDenied
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")); err != nil {
		return domain.RuntimeProfile{}, err
	}
	profile.Name = strings.TrimSpace(profile.Name)
	profile.ModelPreferences.PublicModel = strings.TrimSpace(profile.ModelPreferences.PublicModel)
	if err := validateRuntimeProfile(expectedRevision, profile); err != nil {
		return domain.RuntimeProfile{}, err
	}
	if s.runtimeProfiles == nil {
		return domain.RuntimeProfile{}, apperrors.ErrServiceUnavailable
	}
	if profile.Enabled {
		if err := s.validatePublishedRuntimeProfile(ctx, principal, profile); err != nil {
			return domain.RuntimeProfile{}, err
		}
	}
	item, err := s.runtimeProfiles.PublishRuntimeProfile(ctx, principal.UserID, expectedRevision, profile)
	if err == nil {
		s.auditRuntimeProfile(ctx, principal, "publish", item.ID, map[string]any{"revision": item.Revision, "scope": item.Scope, "enabled": item.Enabled})
	}
	return item, err
}

func (s *Service) validatePublishedRuntimeProfile(ctx context.Context, principal identity.Principal, profile domain.RuntimeProfile) error {
	if err := s.validateRequiredProfileMCPs(ctx, profile); err != nil {
		return err
	}
	prefs := profile.ModelPreferences
	if prefs.PublicModel == "" && (prefs.ReasoningEffort == "" || prefs.ReasoningEffort == "auto") {
		return nil
	}
	settings, err := s.resolveAIWorkbenchSettings(ctx)
	if err != nil || !settings.Enabled || isNilDependency(s.runtime.ModelCatalog) {
		return apperrors.ErrServiceUnavailable
	}
	input := appaigateway.WorkbenchRelayRequest{PublicModel: prefs.PublicModel, Endpoint: settings.DefaultEndpoint, ReasoningEffort: prefs.ReasoningEffort}
	if input.PublicModel == "" {
		input.PublicModel, input.RouteID = settings.DefaultPublicModel, settings.DefaultRouteID
	}
	option, err := s.runtime.ModelCatalog.ResolveWorkbenchModelOption(ctx, principal, input)
	if errors.Is(err, apperrors.ErrNotFound) {
		return fmt.Errorf("%w: the profile model route is unavailable", apperrors.ErrInvalidArgument)
	}
	if err != nil {
		return err
	}
	if prefs.ReasoningEffort != "" && prefs.ReasoningEffort != "auto" && !slices.Contains(option.ReasoningEfforts, prefs.ReasoningEffort) {
		return fmt.Errorf("%w: reasoning effort is not supported by this model route", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) validateRequiredProfileMCPs(ctx context.Context, profile domain.RuntimeProfile) error {
	if !slices.ContainsFunc(profile.Assets, func(ref domain.RuntimeAssetRef) bool { return ref.Kind == "mcp" && ref.Required }) {
		return nil
	}
	_, descriptors, err := s.runtimeAssetCatalog(ctx)
	if err != nil {
		return err
	}
	personal := false
	for _, ref := range profile.Assets {
		if ref.Kind != "mcp" || !ref.Required {
			continue
		}
		asset, err := registeredRuntimeAsset(descriptors, domain.RuntimeAssetRef{Kind: ref.Kind, ID: ref.ID, Version: ref.Version})
		if err != nil {
			return err
		}
		if asset.Transport != "stdio" {
			continue
		}
		if profile.Scope != domain.RuntimeScopeHeya {
			return fmt.Errorf("%w: only Heya profiles may require stdio MCP connections", apperrors.ErrInvalidArgument)
		}
		if asset.ExecutionPosition != "personal-docker" {
			continue
		}
		if personal {
			return fmt.Errorf("%w: a profile may require only one personal Docker MCP", apperrors.ErrInvalidArgument)
		}
		personal = true
	}
	return nil
}

func validateRuntimeProfile(expectedRevision int64, profile domain.RuntimeProfile) error {
	if !runtimeProfileIDPattern.MatchString(profile.ID) || profile.Name == "" || utf8.RuneCountInString(profile.Name) > 100 || expectedRevision < 0 || expectedRevision == math.MaxInt64 {
		return fmt.Errorf("%w: invalid runtime profile identity or revision", apperrors.ErrInvalidArgument)
	}
	if profile.Scope != domain.RuntimeScopeChat && profile.Scope != domain.RuntimeScopeHeya {
		return fmt.Errorf("%w: invalid runtime profile scope", apperrors.ErrInvalidArgument)
	}
	if (profile.ID == "chat-default" || profile.ID == "heya-default") && profile.ID != profile.Scope+"-default" {
		return fmt.Errorf("%w: reserved runtime profile ID belongs to another scope", apperrors.ErrInvalidArgument)
	}
	if profile.MaxOutputTokens < 0 || profile.MaxOutputTokens > 4096 || utf8.RuneCountInString(profile.ModelPreferences.PublicModel) > 200 || !validWorkbenchReasoningEffort(profile.ModelPreferences.ReasoningEffort) {
		return fmt.Errorf("%w: invalid runtime profile model preferences", apperrors.ErrInvalidArgument)
	}
	return validateRuntimeAssets(profile.Assets)
}

func validateRuntimeAssets(assets []domain.RuntimeAssetRef) error {
	if assets == nil || len(assets) > 16 {
		return fmt.Errorf("%w: invalid runtime profile assets", apperrors.ErrInvalidArgument)
	}
	seen := make(map[[2]string]struct{}, len(assets))
	for _, asset := range assets {
		if (asset.Kind != "mcp" && asset.Kind != "skill" && asset.Kind != "hook") || !runtimeProfileIDPattern.MatchString(asset.ID) || !runtimeAssetVersionPattern.MatchString(asset.Version) {
			return fmt.Errorf("%w: invalid runtime profile asset reference", apperrors.ErrInvalidArgument)
		}
		key := [2]string{asset.Kind, asset.ID}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate runtime profile asset reference", apperrors.ErrInvalidArgument)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (s *Service) ListRuntimeProfiles(ctx context.Context, principal identity.Principal) ([]domain.RuntimeProfile, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return nil, err
	}
	if s.runtimeProfiles == nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	scopes := []string{domain.RuntimeScopeChat}
	err := s.requirePersonalAgent(ctx, principal)
	if err == nil {
		scopes = append(scopes, domain.RuntimeScopeHeya)
	} else if !errors.Is(err, apperrors.ErrAccessDenied) {
		return nil, err
	}
	return s.runtimeProfiles.ListCurrentRuntimeProfiles(ctx, scopes)
}

func (s *Service) SelectSessionRuntimeProfile(ctx context.Context, principal identity.Principal, sessionID, id string) (domain.Session, error) {
	if principal.UserID == "" {
		return domain.Session{}, apperrors.ErrAccessDenied
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domain.Session{}, err
	}
	if id != "" && !runtimeProfileIDPattern.MatchString(id) {
		return domain.Session{}, apperrors.ErrInvalidArgument
	}
	session, err := s.ownedSession(ctx, principal, sessionID)
	if err != nil {
		return domain.Session{}, err
	}
	metadata := parseSessionMetadata(session.Metadata)
	if metadata.ArchivedAt != "" || (metadata.Status != "" && metadata.Status != "active") {
		return domain.Session{}, fmt.Errorf("%w: runtime profiles require an active session", apperrors.ErrInvalidArgument)
	}
	if s.runtimeProfiles == nil {
		return domain.Session{}, apperrors.ErrServiceUnavailable
	}
	scope := runtimeProfileScope(session)
	if id != "" {
		if _, err := s.currentRuntimeProfile(ctx, id, scope); err != nil {
			return domain.Session{}, err
		}
	}
	updated, err := s.runtimeProfiles.SelectSessionRuntimeProfile(ctx, principal.UserID, session.ID, id)
	if err != nil {
		return domain.Session{}, err
	}
	s.auditRuntimeProfile(ctx, principal, "select", session.ID, map[string]any{"profileId": id, "scope": scope})
	return s.visibleSessionSummary(ctx, principal, updated), nil
}

func runtimeProfileScope(session domain.Session) string {
	if isPersonalSession(session) {
		return domain.RuntimeScopeHeya
	}
	return domain.RuntimeScopeChat
}

func (s *Service) resolveRuntimeProfile(ctx context.Context, principal identity.Principal, session domain.Session, metadata domain.SessionMetadata) (*domain.RuntimeProfile, error) {
	scope := runtimeProfileScope(session)
	if scope == domain.RuntimeScopeHeya {
		if err := s.requirePersonalAgent(ctx, principal); err != nil {
			return nil, err
		}
	}
	if metadata.RuntimeProfileID != "" {
		return s.currentRuntimeProfile(ctx, metadata.RuntimeProfileID, scope)
	}
	if s.runtimeProfiles == nil {
		return nil, nil
	}
	profile, err := s.currentRuntimeProfile(ctx, scope+"-default", scope)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, nil
	}
	return profile, err
}

func (s *Service) currentRuntimeProfile(ctx context.Context, id, scope string) (*domain.RuntimeProfile, error) {
	if s.runtimeProfiles == nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	profile, err := s.runtimeProfiles.GetCurrentRuntimeProfile(ctx, id, scope)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, errRuntimeProfileUnavailable
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *Service) authorizeRuntimeProfile(ctx context.Context, id string, frozenRevision int64, scope string) error {
	if id == "" && frozenRevision == 0 {
		return nil
	}
	if s.runtimeProfiles == nil {
		return apperrors.ErrServiceUnavailable
	}
	allowed, err := s.runtimeProfiles.RuntimeProfileRevisionAuthorized(ctx, id, frozenRevision, scope)
	if err != nil {
		return err
	}
	if !allowed {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "runtime_profile_revoked", "This runtime profile revision is no longer authorized.", "该运行配置版本已无权继续执行。")
	}
	return nil
}

func (s *Service) auditRuntimeProfile(ctx context.Context, principal identity.Principal, action, resource string, metadata map[string]any) {
	if s.audits == nil {
		return
	}
	request := requestctx.FromContext(ctx)
	_ = s.audits.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName,
		ResourceKind: "WorkbenchRuntimeProfile", ResourceName: resource, Action: "runtime_profile." + action, Result: "success",
		Summary: "Workbench runtime profile " + action, RequestID: request.RequestID, RequestPath: request.Path,
		RequestMethod: request.Method, SourceIP: request.SourceIP, Metadata: metadata})
}
