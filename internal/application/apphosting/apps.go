package apphosting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListAudienceCandidates(ctx context.Context, principal domainidentity.Principal, query string, limit int) (sohaapi.AppHostingAudienceCandidates, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.AppHostingAudienceCandidates{}, err
	}
	trimmedQuery := strings.TrimSpace(query)
	if utf8.RuneCountInString(trimmedQuery) > 200 {
		return sohaapi.AppHostingAudienceCandidates{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_query", "query must not exceed 200 characters", "搜索关键词长度不能超过 200 个字符")
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return sohaapi.AppHostingAudienceCandidates{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_limit", "limit must be between 1 and 100", "分页条数必须在 1 到 100 之间")
	}
	if s.audience == nil {
		return sohaapi.AppHostingAudienceCandidates{
			Users: []sohaapi.AppHostingAudienceUser{},
			Teams: []sohaapi.AppHostingAudienceTeam{},
		}, nil
	}
	out, err := s.audience.SearchCandidates(ctx, trimmedQuery, limit)
	if err != nil {
		return sohaapi.AppHostingAudienceCandidates{}, err
	}
	if out.Users == nil {
		out.Users = []sohaapi.AppHostingAudienceUser{}
	}
	if out.Teams == nil {
		out.Teams = []sohaapi.AppHostingAudienceTeam{}
	}
	return out, nil
}

func (s *Service) ListApps(ctx context.Context, principal domainidentity.Principal, filter domainapphosting.AppListFilter) (sohaapi.HostedAppList, error) {
	normalized, err := s.authorizeAndNormalizeListFilter(ctx, principal, filter)
	if err != nil {
		return sohaapi.HostedAppList{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedAppList{}, err
	}
	apps, total, err := s.repo.ListApps(ctx, normalized)
	if err != nil {
		return sohaapi.HostedAppList{}, err
	}
	items := make([]sohaapi.HostedApp, 0, len(apps))
	for _, item := range apps {
		items = append(items, item.ToAPI(settings))
	}
	return sohaapi.HostedAppList{Items: items, Total: total}, nil
}

func (s *Service) authorizeAndNormalizeListFilter(ctx context.Context, principal domainidentity.Principal, filter domainapphosting.AppListFilter) (domainapphosting.AppListFilter, error) {
	out := filter
	if out.Scope == "" {
		out.Scope = sohaapi.HostedAppListScopeMine
	}
	if !out.Scope.Valid() {
		return domainapphosting.AppListFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_scope", "scope must be mine or all", "查询范围必须为 mine 或 all")
	}
	if out.Scope == sohaapi.HostedAppListScopeAll {
		if err := s.authorize(ctx, principal, appaccess.PermAppHostingAdminManage); err != nil {
			return domainapphosting.AppListFilter{}, err
		}
		out.MemberUserID = ""
	} else {
		if err := s.authorizeAny(ctx, principal,
			appaccess.PermAppHostingAppsView,
			appaccess.PermAppHostingAppsPublish,
			appaccess.PermAppHostingAdminView,
			appaccess.PermAppHostingAdminManage,
		); err != nil {
			return domainapphosting.AppListFilter{}, err
		}
		out.MemberUserID = strings.TrimSpace(principal.UserID)
	}
	if out.Status != "" && !out.Status.Valid() {
		return domainapphosting.AppListFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_status", "status filter is invalid", "应用状态过滤参数无效")
	}
	out.Query = strings.TrimSpace(out.Query)
	if utf8.RuneCountInString(out.Query) > 200 {
		return domainapphosting.AppListFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_query", "query must not exceed 200 characters", "搜索关键词长度不能超过 200 个字符")
	}
	if out.Limit == 0 {
		out.Limit = 50
	}
	if out.Limit < 1 || out.Limit > 200 || out.Offset < 0 {
		return domainapphosting.AppListFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_pagination", "limit must be 1..200 and offset >= 0", "分页参数无效")
	}
	return out, nil
}

func (s *Service) CreateApp(ctx context.Context, principal domainidentity.Principal, req sohaapi.CreateHostedAppRequest) (sohaapi.HostedApp, bool, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedApp{}, false, err
	}
	ownerID := strings.TrimSpace(principal.UserID)
	if ownerID == "" {
		return sohaapi.HostedApp{}, false, apperrors.ErrUnauthorized
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, false, err
	}
	settings = settings.WithDefaults()

	candidate, err := s.buildAppCandidate(ownerID, req, settings)
	if err != nil {
		return sohaapi.HostedApp{}, false, err
	}

	for attempt := 0; attempt < 5; attempt++ {
		dnsLabel, err := domainapphosting.AllocateDNSLabel(req.Slug, candidate.Name)
		if err != nil {
			return sohaapi.HostedApp{}, false, err
		}
		candidate.DNSLabel = dnsLabel
		created, reused, err := s.repo.CreateApp(ctx, candidate, settings.MaxAppsPerOwner)
		if errors.Is(err, ErrDNSLabelConflict) {
			continue
		}
		if err != nil {
			return sohaapi.HostedApp{}, false, wrapAppQuotaError(err, settings.MaxAppsPerOwner)
		}
		if !reused {
			s.recordAudit(ctx, principal, "app.create", "success", created.ID, "Created hosted application", map[string]any{
				"appId":          created.ID,
				"dnsLabel":       created.DNSLabel,
				"kind":           created.Kind,
				"visibilityMode": created.Visibility.Mode,
			})
		}
		return created.ToAPI(settings), reused, nil
	}
	return sohaapi.HostedApp{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "dns_label_exhausted", "unable to allocate a unique subdomain label", "无法分配唯一子域标识，请稍后重试")
}

func (s *Service) buildAppCandidate(ownerID string, req sohaapi.CreateHostedAppRequest, settings domainapphosting.Settings) (domainapphosting.App, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_app_name", "name must be 1..128 characters", "应用名称长度必须在 1 到 128 个字符之间")
	}
	desc := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(desc) > 1024 {
		return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_description", "description must not exceed 1024 characters", "应用简介不能超过 1024 个字符")
	}
	kind := req.Kind
	if kind == "" {
		kind = sohaapi.HostedAppKindStatic
	}
	if !kind.Valid() {
		return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_app_kind", "kind must be static or dynamic", "应用类型必须为 static 或 dynamic")
	}
	idemKey := strings.TrimSpace(req.IdempotencyKey)
	if len(idemKey) > 128 {
		return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_idempotency_key", "idempotencyKey must not exceed 128 characters", "幂等键长度不能超过 128 个字符")
	}
	visibility, err := domainapphosting.NormalizeVisibilityInput(req.Visibility, settings)
	if err != nil {
		return domainapphosting.App{}, err
	}
	explicitSlug, err := domainapphosting.ValidateExplicitSlug(req.Slug)
	if err != nil {
		return domainapphosting.App{}, err
	}
	spaFallback := req.SpaFallback != nil && *req.SpaFallback
	fingerprint := computeCreateFingerprint(name, explicitSlug, desc, kind, spaFallback, visibility)
	now := s.now()
	return domainapphosting.App{
		ID:                 "app_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		Name:               name,
		Description:        desc,
		Kind:               kind,
		SpaFallback:        spaFallback,
		OwnerUserID:        ownerID,
		MaintainerUserIDs:  []string{},
		Visibility:         visibility,
		Status:             sohaapi.HostedAppStatusDraft,
		RouteGeneration:    1,
		IdempotencyKey:     idemKey,
		RequestFingerprint: fingerprint,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func computeCreateFingerprint(name, slug, desc string, kind sohaapi.HostedAppKind, spaFallback bool, visibility sohaapi.HostedAppVisibilityScope) string {
	raw, _ := json.Marshal([]any{name, slug, desc, kind, spaFallback, visibility})
	return sha256HexString(string(raw))
}

func wrapAppQuotaError(err error, limit int32) error {
	if errors.Is(err, domainapphosting.ErrQuotaExceeded) {
		return apperrors.NewBusiness(
			domainapphosting.ErrQuotaExceeded,
			"quota_exceeded",
			fmt.Sprintf("maximum hosted applications per owner (%d) reached", limit),
			fmt.Sprintf("已达到每位作者可创建的托管应用上限（%d 个）", limit),
		)
	}
	return err
}

func (s *Service) GetApp(ctx context.Context, principal domainidentity.Principal, appID string) (sohaapi.HostedApp, error) {
	app, settings, err := s.loadAuthorizedAppRead(ctx, principal, appID)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	return app.ToAPI(settings), nil
}

func (s *Service) UpdateApp(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.UpdateHostedAppRequest) (sohaapi.HostedApp, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedApp{}, err
	}
	if req.Name == nil && req.Description == nil && req.SpaFallback == nil {
		return sohaapi.HostedApp{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "empty_update", "at least one field must be provided", "至少需要提供一个待更新字段")
	}
	trimmedID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	now := s.now()
	updated, err := s.repo.MutateApp(ctx, trimmedID, func(current domainapphosting.App) (domainapphosting.App, error) {
		if !current.IsOwner(principal.UserID) {
			return domainapphosting.App{}, apperrors.ErrAccessDenied
		}
		return applyMetadataPatch(current, req, now)
	})
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	s.recordAudit(ctx, principal, "app.update", "success", updated.ID, "Updated hosted application metadata", map[string]any{
		"appId":           updated.ID,
		"spaFallback":     updated.SpaFallback,
		"routeGeneration": updated.RouteGeneration,
	})
	return updated.ToAPI(settings), nil
}

func applyMetadataPatch(current domainapphosting.App, req sohaapi.UpdateHostedAppRequest, now time.Time) (domainapphosting.App, error) {
	next := current
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" || utf8.RuneCountInString(trimmed) > 128 {
			return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_app_name", "name must be 1..128 characters", "应用名称长度必须在 1 到 128 个字符之间")
		}
		next.Name = trimmed
	}
	if req.Description != nil {
		trimmed := strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(trimmed) > 1024 {
			return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_description", "description must not exceed 1024 characters", "应用简介不能超过 1024 个字符")
		}
		next.Description = trimmed
	}
	if req.SpaFallback != nil && *req.SpaFallback != next.SpaFallback {
		next.SpaFallback = *req.SpaFallback
		next.RouteGeneration++
	}
	next.UpdatedAt = now
	return next, nil
}

func (s *Service) UpdateVisibility(ctx context.Context, principal domainidentity.Principal, appID string, req sohaapi.UpdateHostedAppVisibilityRequest) (sohaapi.HostedApp, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedApp{}, err
	}
	trimmedID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	settings = settings.WithDefaults()
	scope, err := domainapphosting.NormalizeVisibilityInput(&req.Visibility, settings)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	now := s.now()
	updated, err := s.repo.MutateApp(ctx, trimmedID, func(current domainapphosting.App) (domainapphosting.App, error) {
		if !current.IsOwner(principal.UserID) {
			return domainapphosting.App{}, apperrors.ErrAccessDenied
		}
		next := current
		next.Visibility = scope
		next.RouteGeneration++
		next.UpdatedAt = now
		return next, nil
	})
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	s.recordAudit(ctx, principal, "app.visibility.update", "success", updated.ID, "Updated hosted application visibility scope", map[string]any{
		"appId":           updated.ID,
		"visibilityMode":  updated.Visibility.Mode,
		"routeGeneration": updated.RouteGeneration,
	})
	return updated.ToAPI(settings), nil
}

func (s *Service) TakeOffline(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedApp{}, err
	}
	trimmedID, trimmedReason, err := validateLifecycleArgs(appID, reason, false)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	now := s.now()
	updated, err := s.repo.MutateApp(ctx, trimmedID, func(current domainapphosting.App) (domainapphosting.App, error) {
		if !current.IsOwner(principal.UserID) {
			return domainapphosting.App{}, apperrors.ErrAccessDenied
		}
		if current.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
			return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrConflict, "app_suspended_by_admin", "application is suspended by administrator", "该应用已被管理员强制封停")
		}
		if current.Status == sohaapi.HostedAppStatusDraft {
			return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrConflict, "app_still_draft", "draft application cannot be taken offline", "草稿状态的应用尚未上线")
		}
		next := current
		next.Status = sohaapi.HostedAppStatusOffline
		next.RouteGeneration++
		next.UpdatedAt = now
		return next, nil
	})
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	s.recordAudit(ctx, principal, "app.offline", "success", updated.ID, "Took hosted application offline", map[string]any{
		"appId":           updated.ID,
		"reason":          trimmedReason,
		"routeGeneration": updated.RouteGeneration,
	})
	return updated.ToAPI(settings), nil
}

func (s *Service) BringOnline(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAppsPublish); err != nil {
		return sohaapi.HostedApp{}, err
	}
	trimmedID, trimmedReason, err := validateLifecycleArgs(appID, reason, false)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	now := s.now()
	updated, err := s.repo.MutateApp(ctx, trimmedID, func(current domainapphosting.App) (domainapphosting.App, error) {
		if !current.IsOwner(principal.UserID) {
			return domainapphosting.App{}, apperrors.ErrAccessDenied
		}
		if current.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
			return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrConflict, "app_suspended_by_admin", "suspended application cannot be brought online without administrator unsuspension", "被管理员封停的应用须在解除封停后才能重新上线")
		}
		if strings.TrimSpace(current.ActiveRevisionID) == "" {
			return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrConflict, "no_active_revision", "application has no ready revision to serve", "应用尚无可用于提供服务的可运行版本")
		}
		next := current
		next.Status = sohaapi.HostedAppStatusOnline
		next.RouteGeneration++
		next.UpdatedAt = now
		return next, nil
	})
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	s.recordAudit(ctx, principal, "app.online", "success", updated.ID, "Brought hosted application online", map[string]any{
		"appId":           updated.ID,
		"reason":          trimmedReason,
		"routeGeneration": updated.RouteGeneration,
	})
	return updated.ToAPI(settings), nil
}

func (s *Service) SuspendApp(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAdminManage); err != nil {
		return sohaapi.HostedApp{}, err
	}
	trimmedID, trimmedReason, err := validateLifecycleArgs(appID, reason, true)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	now := s.now()
	updated, err := s.repo.MutateApp(ctx, trimmedID, func(current domainapphosting.App) (domainapphosting.App, error) {
		next := current
		next.Status = sohaapi.HostedAppStatusSuspendedByAdmin
		next.SuspendedReason = trimmedReason
		next.RouteGeneration++
		next.UpdatedAt = now
		return next, nil
	})
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	s.recordAudit(ctx, principal, "app.suspend", "success", updated.ID, "Administratively suspended hosted application", map[string]any{
		"appId":           updated.ID,
		"reason":          trimmedReason,
		"routeGeneration": updated.RouteGeneration,
	})
	return updated.ToAPI(settings), nil
}

func (s *Service) UnsuspendApp(ctx context.Context, principal domainidentity.Principal, appID, reason string) (sohaapi.HostedApp, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAdminManage); err != nil {
		return sohaapi.HostedApp{}, err
	}
	trimmedID, trimmedReason, err := validateLifecycleArgs(appID, reason, false)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	now := s.now()
	updated, err := s.repo.MutateApp(ctx, trimmedID, func(current domainapphosting.App) (domainapphosting.App, error) {
		if current.Status != sohaapi.HostedAppStatusSuspendedByAdmin {
			return domainapphosting.App{}, apperrors.NewBusiness(apperrors.ErrConflict, "app_not_suspended", "application is not administratively suspended", "该应用当前未处于管理员封停状态")
		}
		next := current
		if strings.TrimSpace(next.ActiveRevisionID) == "" {
			next.Status = sohaapi.HostedAppStatusDraft
		} else {
			next.Status = sohaapi.HostedAppStatusOffline
		}
		next.SuspendedReason = ""
		next.RouteGeneration++
		next.UpdatedAt = now
		return next, nil
	})
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	s.recordAudit(ctx, principal, "app.unsuspend", "success", updated.ID, "Lifted administrative suspension on hosted application", map[string]any{
		"appId":           updated.ID,
		"reason":          trimmedReason,
		"status":          updated.Status,
		"routeGeneration": updated.RouteGeneration,
	})
	return updated.ToAPI(settings), nil
}

func (s *Service) loadAuthorizedAppRead(ctx context.Context, principal domainidentity.Principal, appID string) (domainapphosting.App, domainapphosting.Settings, error) {
	adminAllowed := s.hasAdminRead(ctx, principal)
	if !adminAllowed {
		if err := s.authorizeAny(ctx, principal, appaccess.PermAppHostingAppsView, appaccess.PermAppHostingAppsPublish); err != nil {
			return domainapphosting.App{}, domainapphosting.Settings{}, err
		}
	}
	trimmedID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return domainapphosting.App{}, domainapphosting.Settings{}, err
	}
	app, err := s.repo.GetAppByID(ctx, trimmedID)
	if err != nil {
		return domainapphosting.App{}, domainapphosting.Settings{}, err
	}
	if !adminAllowed && !app.IsOwnerOrMaintainer(principal.UserID) {
		return domainapphosting.App{}, domainapphosting.Settings{}, apperrors.ErrAccessDenied
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return domainapphosting.App{}, domainapphosting.Settings{}, err
	}
	return app, settings, nil
}

func validateIdentifier(raw, field string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 128 {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_"+strings.ToLower(field), field+" must be 1..128 characters", field+" 参数无效")
	}
	return trimmed, nil
}

func validateLifecycleArgs(appID, reason string, requireReason bool) (string, string, error) {
	trimmedID, err := validateIdentifier(appID, "appId")
	if err != nil {
		return "", "", err
	}
	trimmedReason := strings.TrimSpace(reason)
	if requireReason && trimmedReason == "" {
		return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "missing_reason", "reason is required", "必须填写操作原因")
	}
	if utf8.RuneCountInString(trimmedReason) > 512 {
		return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_reason", "reason must not exceed 512 characters", "原因长度不能超过 512 个字符")
	}
	return trimmedID, trimmedReason, nil
}
