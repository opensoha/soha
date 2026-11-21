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
	if normalized.TeamID != "" && s.audience != nil {
		members, teamErr := s.audience.ListTeamMemberIDs(ctx, normalized.TeamID)
		if teamErr != nil {
			return sohaapi.HostedAppList{}, teamErr
		}
		normalized.TeamMemberUserIDs = members
		normalized.TeamFilterActive = true
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.HostedAppList{}, err
	}
	apps, total, err := s.repo.ListApps(ctx, normalized)
	if err != nil {
		return sohaapi.HostedAppList{}, err
	}
	items, err := s.enrichHostedApps(ctx, settings, apps)
	if err != nil {
		return sohaapi.HostedAppList{}, err
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
		if err := s.authorizeAny(ctx, principal, appaccess.PermAppHostingAdminView, appaccess.PermAppHostingAdminManage); err != nil {
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
	return normalizeListFilterFields(out)
}

func normalizeListFilterFields(out domainapphosting.AppListFilter) (domainapphosting.AppListFilter, error) {
	if out.Status != "" && !out.Status.Valid() {
		return domainapphosting.AppListFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_status", "status filter is invalid", "应用状态过滤参数无效")
	}
	out.OwnerUserID = strings.TrimSpace(out.OwnerUserID)
	out.TeamID = strings.TrimSpace(out.TeamID)
	if len(out.OwnerUserID) > 128 || len(out.TeamID) > 128 {
		return domainapphosting.AppListFilter{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_filter", "ownerUserId and teamId must not exceed 128 characters", "作者与部门筛选标识过长")
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
	return s.createAppWithReason(ctx, principal, req, "")
}

func (s *Service) createAppWithReason(ctx context.Context, principal domainidentity.Principal, req sohaapi.CreateHostedAppRequest, visibilityReason string) (sohaapi.HostedApp, bool, error) {
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

	candidate, initialApproval, err := s.buildAppCandidateWithApproval(ownerID, req, visibilityReason, settings)
	if err != nil {
		return sohaapi.HostedApp{}, false, err
	}

	for attempt := 0; attempt < 5; attempt++ {
		dnsLabel, err := domainapphosting.AllocateDNSLabel(req.Slug, candidate.Name)
		if err != nil {
			return sohaapi.HostedApp{}, false, err
		}
		candidate.DNSLabel = dnsLabel
		if initialApproval != nil {
			initialApproval.AppDNSLabel = dnsLabel
		}
		created, savedApproval, reused, err := s.repo.CreateAppWithApproval(ctx, candidate, settings.MaxAppsPerOwner, initialApproval)
		if errors.Is(err, ErrDNSLabelConflict) {
			continue
		}
		if err != nil {
			return sohaapi.HostedApp{}, false, wrapAppQuotaError(err, settings.MaxAppsPerOwner)
		}
		if !reused {
			s.recordCreateAppAudits(ctx, principal, created, savedApproval)
		}
		dto, err := s.enrichHostedApp(ctx, settings, created)
		if err != nil {
			return sohaapi.HostedApp{}, false, err
		}
		return dto, reused, nil
	}
	return sohaapi.HostedApp{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "dns_label_exhausted", "unable to allocate a unique subdomain label", "无法分配唯一子域标识，请稍后重试")
}

func (s *Service) recordCreateAppAudits(ctx context.Context, principal domainidentity.Principal, created domainapphosting.App, pendingApproval *domainapphosting.VisibilityApproval) {
	s.recordAudit(ctx, principal, "app.create", "success", created.ID, "Created hosted application", map[string]any{
		"appId":          created.ID,
		"dnsLabel":       created.DNSLabel,
		"kind":           created.Kind,
		"visibilityMode": created.Visibility.Mode,
	})
	if pendingApproval != nil {
		s.recordAudit(ctx, principal, "app.visibility.request_approval", "success", created.ID, "Requested organization-wide visibility approval on application creation", map[string]any{
			"appId":               created.ID,
			"approvalId":          pendingApproval.ID,
			"baseRouteGeneration": pendingApproval.BaseRouteGeneration,
			"policyVersion":       pendingApproval.PolicyVersion,
		})
	}
}

func (s *Service) buildAppCandidateWithApproval(
	ownerID string,
	req sohaapi.CreateHostedAppRequest,
	visibilityReason string,
	settings domainapphosting.Settings,
) (domainapphosting.App, *domainapphosting.VisibilityApproval, error) {
	name, desc, kind, idemKey, err := validateCreateAppFields(req)
	if err != nil {
		return domainapphosting.App{}, nil, err
	}
	requestedVis, err := domainapphosting.NormalizeVisibilityInput(req.Visibility, settings)
	if err != nil {
		return domainapphosting.App{}, nil, err
	}
	explicitSlug, err := domainapphosting.ValidateExplicitSlug(req.Slug)
	if err != nil {
		return domainapphosting.App{}, nil, err
	}
	spaFallback := req.SpaFallback != nil && *req.SpaFallback
	fingerprint := computeCreateFingerprint(name, explicitSlug, desc, kind, spaFallback, requestedVis)
	now := s.now()
	effectiveVis := requestedVis
	var approval *domainapphosting.VisibilityApproval
	if requestedVis.Mode == sohaapi.HostedAppVisibilityModeOrganization &&
		settings.EffectiveOrganizationPolicy() == sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval {
		effectiveVis = sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate}
		approval = &domainapphosting.VisibilityApproval{
			ID:                  "apv_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			AppName:             name,
			RequesterUserID:     ownerID,
			CurrentVisibility:   effectiveVis,
			RequestedVisibility: requestedVis,
			BaseRouteGeneration: 1,
			PolicyVersion:       settings.PolicyVersion,
			Status:              sohaapi.HostedAppVisibilityApprovalStatusPending,
			Reason:              strings.TrimSpace(visibilityReason),
			CreatedAt:           now,
			UpdatedAt:           now,
		}
	}
	app := domainapphosting.App{
		ID:                 "app_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		Name:               name,
		Description:        desc,
		Kind:               kind,
		SpaFallback:        spaFallback,
		OwnerUserID:        ownerID,
		MaintainerUserIDs:  []string{},
		Visibility:         effectiveVis,
		Status:             sohaapi.HostedAppStatusDraft,
		RouteGeneration:    1,
		IdempotencyKey:     idemKey,
		RequestFingerprint: fingerprint,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if approval != nil {
		approval.AppID = app.ID
	}
	return app, approval, nil
}

func validateCreateAppFields(req sohaapi.CreateHostedAppRequest) (string, string, sohaapi.HostedAppKind, string, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		return "", "", "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_app_name", "name must be 1..128 characters", "应用名称长度必须在 1 到 128 个字符之间")
	}
	desc := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(desc) > 1024 {
		return "", "", "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_description", "description must not exceed 1024 characters", "应用简介不能超过 1024 个字符")
	}
	kind := req.Kind
	if kind == "" {
		kind = sohaapi.HostedAppKindStatic
	}
	if !kind.Valid() {
		return "", "", "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_app_kind", "kind must be static or dynamic", "应用类型必须为 static 或 dynamic")
	}
	idemKey := strings.TrimSpace(req.IdempotencyKey)
	if len(idemKey) > 128 {
		return "", "", "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_idempotency_key", "idempotencyKey must not exceed 128 characters", "幂等键长度不能超过 128 个字符")
	}
	return name, desc, kind, idemKey, nil
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
	return s.enrichHostedApp(ctx, settings, app)
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
	return s.enrichHostedApp(ctx, settings, updated)
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
	trimmedReason := strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(trimmedReason) > 500 {
		return sohaapi.HostedApp{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_reason", "reason must not exceed 500 characters", "申请原因长度不能超过 500 个字符")
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
	out, err := s.repo.UpdateAppVisibility(ctx, UpdateVisibilityTxInput{
		AppID:          trimmedID,
		ActorUserID:    strings.TrimSpace(principal.UserID),
		RequestedScope: scope,
		Reason:         trimmedReason,
		NewApprovalID:  "apv_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		Now:            s.now(),
	})
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	if out.RequestedApproval && out.PendingApproval != nil {
		s.recordAudit(ctx, principal, "app.visibility.request_approval", "success", out.App.ID, "Submitted organization-wide visibility approval request", map[string]any{
			"appId":               out.App.ID,
			"approvalId":          out.PendingApproval.ID,
			"currentVisibility":   out.App.Visibility.Mode,
			"baseRouteGeneration": out.PendingApproval.BaseRouteGeneration,
			"policyVersion":       out.PendingApproval.PolicyVersion,
		})
	} else {
		s.recordAudit(ctx, principal, "app.visibility.update", "success", out.App.ID, "Updated hosted application visibility scope", map[string]any{
			"appId":           out.App.ID,
			"visibilityMode":  out.App.Visibility.Mode,
			"routeGeneration": out.App.RouteGeneration,
		})
	}
	return s.enrichHostedApp(ctx, out.Settings, out.App)
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
	return s.enrichHostedApp(ctx, settings, updated)
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
	return s.enrichHostedApp(ctx, settings, updated)
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
	return s.enrichHostedApp(ctx, settings, updated)
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
	return s.enrichHostedApp(ctx, settings, updated)
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
