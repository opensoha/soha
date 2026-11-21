package apphosting

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *MemoryRepository) ListVisiblePortalApps(_ context.Context, principal domainidentity.Principal) ([]domainapphosting.App, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	settings := r.settings.WithDefaults()
	items := make([]domainapphosting.App, 0, len(r.appsByID))
	for _, app := range r.appsByID {
		if domainapphosting.VisibleInPortal(app, principal, settings) {
			items = append(items, cloneApp(app))
		}
	}
	return items, nil
}

func (r *MemoryRepository) LoadAppEnrichments(_ context.Context, appIDs []string) (map[string]appapphosting.AppEnrichment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make(map[string]appapphosting.AppEnrichment, len(appIDs))
	for _, id := range appIDs {
		app, ok := r.appsByID[id]
		if !ok {
			continue
		}
		var e appapphosting.AppEnrichment
		if app.ActiveRevisionID != "" {
			if idx := findRevisionIndex(r.revisions[id], app.ActiveRevisionID); idx >= 0 {
				e.ActiveRevisionNumber = r.revisions[id][idx].RevisionNumber
				e.TotalBytes = r.revisions[id][idx].TotalBytes
			}
		}
		e.PendingApproval = r.findPendingApprovalForAppLocked(id)
		out[id] = e
	}
	return out, nil
}

func (r *MemoryRepository) UpdateAppVisibility(_ context.Context, input appapphosting.UpdateVisibilityTxInput) (appapphosting.UpdateVisibilityTxOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	settings := cloneSettings(r.settings.WithDefaults())
	app, ok := r.appsByID[strings.TrimSpace(input.AppID)]
	if !ok {
		return appapphosting.UpdateVisibilityTxOutput{}, apperrors.ErrNotFound
	}
	if !app.IsOwner(input.ActorUserID) {
		return appapphosting.UpdateVisibilityTxOutput{}, apperrors.ErrAccessDenied
	}
	nowUTC := input.Now.UTC()
	if input.RequestedScope.Mode == sohaapi.HostedAppVisibilityModeOrganization {
		if settings.EffectiveOrganizationPolicy() == sohaapi.AppHostingOrganizationVisibilityPolicyDisabled {
			return appapphosting.UpdateVisibilityTxOutput{}, apperrors.NewBusiness(apperrors.ErrAccessDenied, "organization_visibility_disabled", "organization-wide visibility is disabled by platform policy", "平台管理员当前已禁用全公司公开可见范围")
		}
		if settings.EffectiveOrganizationPolicy() == sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval &&
			app.Visibility.Mode != sohaapi.HostedAppVisibilityModeOrganization {
			pending := r.upsertPendingApprovalLocked(app, settings, input, nowUTC)
			return appapphosting.UpdateVisibilityTxOutput{
				App:               cloneApp(app),
				Settings:          settings,
				PendingApproval:   &pending,
				RequestedApproval: true,
			}, nil
		}
	}

	app.Visibility = domainapphosting.CloneVisibilityScope(input.RequestedScope)
	app.RouteGeneration++
	app.UpdatedAt = nowUTC
	r.appsByID[app.ID] = cloneApp(app)
	r.invalidatePendingApprovalForAppLocked(app.ID, "visibility_changed", nowUTC)
	return appapphosting.UpdateVisibilityTxOutput{
		App:               cloneApp(app),
		Settings:          settings,
		RequestedApproval: false,
	}, nil
}

func (r *MemoryRepository) upsertPendingApprovalLocked(
	app domainapphosting.App,
	settings domainapphosting.Settings,
	input appapphosting.UpdateVisibilityTxInput,
	nowUTC time.Time,
) domainapphosting.VisibilityApproval {
	if existing := r.findPendingApprovalForAppLocked(app.ID); existing != nil {
		if existing.RequesterUserID == input.ActorUserID &&
			existing.BaseRouteGeneration == app.RouteGeneration &&
			existing.PolicyVersion == settings.PolicyVersion {
			updated := *existing
			updated.AppName = app.Name
			updated.AppDNSLabel = app.DNSLabel
			if input.Reason != "" {
				updated.Reason = input.Reason
			}
			updated.UpdatedAt = nowUTC
			r.approvals[updated.ID] = cloneApproval(updated)
			return cloneApproval(updated)
		}
		r.invalidatePendingApprovalForAppLocked(app.ID, "visibility_changed", nowUTC)
	}

	created := domainapphosting.VisibilityApproval{
		ID:                  input.NewApprovalID,
		AppID:               app.ID,
		AppName:             app.Name,
		AppDNSLabel:         app.DNSLabel,
		RequesterUserID:     input.ActorUserID,
		CurrentVisibility:   domainapphosting.CloneVisibilityScope(app.Visibility),
		RequestedVisibility: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		BaseRouteGeneration: app.RouteGeneration,
		PolicyVersion:       settings.PolicyVersion,
		Status:              sohaapi.HostedAppVisibilityApprovalStatusPending,
		Reason:              input.Reason,
		CreatedAt:           nowUTC,
		UpdatedAt:           nowUTC,
	}
	r.approvals[created.ID] = cloneApproval(created)
	return cloneApproval(created)
}

func (r *MemoryRepository) WithdrawVisibilityApproval(_ context.Context, appID, ownerUserID string, now time.Time) (domainapphosting.App, domainapphosting.VisibilityApproval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.appsByID[strings.TrimSpace(appID)]
	if !ok {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, apperrors.ErrNotFound
	}
	if !app.IsOwner(ownerUserID) {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, apperrors.ErrAccessDenied
	}
	pending := r.findPendingApprovalForAppLocked(app.ID)
	if pending == nil {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, apperrors.NewBusiness(apperrors.ErrConflict, "approval_not_pending", "no pending organization visibility approval exists for this application", "当前应用没有待处理的公开审批申请")
	}
	nowUTC := now.UTC()
	pending.Status = sohaapi.HostedAppVisibilityApprovalStatusWithdrawn
	pending.UpdatedAt = nowUTC
	pending.DecidedAt = &nowUTC
	r.approvals[pending.ID] = cloneApproval(*pending)
	return cloneApp(app), cloneApproval(*pending), nil
}

func (r *MemoryRepository) ListVisibilityApprovals(_ context.Context, filter domainapphosting.VisibilityApprovalFilter) ([]domainapphosting.VisibilityApproval, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]domainapphosting.VisibilityApproval, 0, len(r.approvals))
	for _, apv := range r.approvals {
		if filter.Scope != sohaapi.HostedAppApprovalListScopeAll {
			isAppOwner := false
			if app, exists := r.appsByID[apv.AppID]; exists && app.IsOwner(filter.RequesterUserID) {
				isAppOwner = true
			}
			if apv.RequesterUserID != filter.RequesterUserID && !isAppOwner {
				continue
			}
		}
		if filter.Status != "" && apv.Status != filter.Status {
			continue
		}
		if filter.AppID != "" && apv.AppID != filter.AppID {
			continue
		}
		matched = append(matched, cloneApproval(apv))
	}
	slices.SortFunc(matched, func(a, b domainapphosting.VisibilityApproval) int {
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return b.CreatedAt.Compare(a.CreatedAt)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	total := len(matched)
	if filter.Offset >= total {
		return []domainapphosting.VisibilityApproval{}, total, nil
	}
	end := min(filter.Offset+filter.Limit, total)
	return matched[filter.Offset:end], total, nil
}

func (r *MemoryRepository) ApproveVisibilityApproval(
	ctx context.Context,
	approvalID, reviewerUserID, comment string,
	now time.Time,
	verifyRequester func(ctx context.Context, requesterUserID string) error,
) (domainapphosting.App, domainapphosting.VisibilityApproval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	apv, ok := r.approvals[strings.TrimSpace(approvalID)]
	if !ok {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, apperrors.ErrNotFound
	}
	if err := checkReviewerAndPendingStatus(apv, reviewerUserID); err != nil {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, err
	}
	app, ok := r.appsByID[apv.AppID]
	if !ok {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, apperrors.ErrNotFound
	}

	nowUTC := now.UTC()
	settings := r.settings.WithDefaults()
	if reason := detectApprovalInvalidationReason(settings, app, apv); reason != "" {
		apv.Status = sohaapi.HostedAppVisibilityApprovalStatusInvalidated
		apv.InvalidationReason = reason
		apv.UpdatedAt = nowUTC
		apv.DecidedAt = &nowUTC
		r.approvals[apv.ID] = cloneApproval(apv)
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, newApprovalInvalidatedError(reason)
	}
	if app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, apperrors.NewBusiness(apperrors.ErrConflict, "app_suspended_by_admin", "cannot approve visibility for an administratively suspended application", "该应用已被管理员强制封停，无法批准公开申请")
	}
	if verifyRequester != nil {
		if err := verifyRequester(ctx, apv.RequesterUserID); err != nil {
			return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, err
		}
	}

	app.Visibility = sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeOrganization}
	app.RouteGeneration++
	app.UpdatedAt = nowUTC
	r.appsByID[app.ID] = cloneApp(app)

	apv.Status = sohaapi.HostedAppVisibilityApprovalStatusApproved
	apv.ReviewerUserID = reviewerUserID
	apv.ReviewComment = comment
	apv.UpdatedAt = nowUTC
	apv.DecidedAt = &nowUTC
	r.approvals[apv.ID] = cloneApproval(apv)
	return cloneApp(app), cloneApproval(apv), nil
}

func (r *MemoryRepository) RejectVisibilityApproval(_ context.Context, approvalID, reviewerUserID, comment string, now time.Time) (domainapphosting.App, domainapphosting.VisibilityApproval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	apv, ok := r.approvals[strings.TrimSpace(approvalID)]
	if !ok {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, apperrors.ErrNotFound
	}
	if err := checkReviewerAndPendingStatus(apv, reviewerUserID); err != nil {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, err
	}
	app, ok := r.appsByID[apv.AppID]
	if !ok {
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, apperrors.ErrNotFound
	}
	nowUTC := now.UTC()
	settings := r.settings.WithDefaults()
	if reason := detectApprovalInvalidationReason(settings, app, apv); reason != "" {
		apv.Status = sohaapi.HostedAppVisibilityApprovalStatusInvalidated
		apv.InvalidationReason = reason
		apv.UpdatedAt = nowUTC
		apv.DecidedAt = &nowUTC
		r.approvals[apv.ID] = cloneApproval(apv)
		return domainapphosting.App{}, domainapphosting.VisibilityApproval{}, newApprovalInvalidatedError(reason)
	}
	apv.Status = sohaapi.HostedAppVisibilityApprovalStatusRejected
	apv.ReviewerUserID = reviewerUserID
	apv.ReviewComment = comment
	apv.UpdatedAt = nowUTC
	apv.DecidedAt = &nowUTC
	r.approvals[apv.ID] = cloneApproval(apv)
	return cloneApp(app), cloneApproval(apv), nil
}

func checkReviewerAndPendingStatus(apv domainapphosting.VisibilityApproval, reviewerUserID string) error {
	if apv.Status != sohaapi.HostedAppVisibilityApprovalStatusPending {
		return apperrors.NewBusiness(apperrors.ErrConflict, "approval_not_pending", "visibility approval request is no longer pending", "该公开审批申请已处理或已失效")
	}
	if apv.RequesterUserID == reviewerUserID {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "self_approval_forbidden", "requester cannot approve or reject their own visibility request", "申请人必须回避，禁止审批自己发起的全公司公开申请")
	}
	return nil
}

func detectApprovalInvalidationReason(settings domainapphosting.Settings, app domainapphosting.App, apv domainapphosting.VisibilityApproval) string {
	if settings.EffectiveOrganizationPolicy() == sohaapi.AppHostingOrganizationVisibilityPolicyDisabled || settings.PolicyVersion != apv.PolicyVersion {
		return "policy_changed"
	}
	if app.OwnerUserID != apv.RequesterUserID {
		return "ownership_transferred"
	}
	if app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
		return "app_suspended"
	}
	if app.RouteGeneration != apv.BaseRouteGeneration {
		return "route_generation_changed"
	}
	return ""
}

func newApprovalInvalidatedError(reason string) error {
	return apperrors.NewBusiness(
		apperrors.ErrConflict,
		"approval_invalidated",
		"approval request was invalidated ("+reason+")",
		"该公开审批申请已自动失效（"+reason+"），请刷新查看最新状态",
	)
}

func (r *MemoryRepository) TransferAppOwnership(_ context.Context, appID, newOwnerUserID string, now time.Time) (domainapphosting.App, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.appsByID[strings.TrimSpace(appID)]
	if !ok {
		return domainapphosting.App{}, "", apperrors.ErrNotFound
	}
	if app.OwnerUserID == newOwnerUserID {
		return domainapphosting.App{}, "", apperrors.NewBusiness(apperrors.ErrConflict, "same_owner", "newOwnerUserId is already the application owner", "目标用户已是该应用的当前归属人")
	}
	previousOwner := app.OwnerUserID
	nowUTC := now.UTC()

	app.OwnerUserID = newOwnerUserID
	app.MaintainerUserIDs = slices.DeleteFunc(slices.Clone(app.MaintainerUserIDs), func(uid string) bool {
		return uid == newOwnerUserID
	})
	app.IdempotencyKey = ""
	app.RequestFingerprint = ""
	app.RouteGeneration++
	app.UpdatedAt = nowUTC
	r.appsByID[app.ID] = cloneApp(app)
	r.invalidatePendingApprovalForAppLocked(app.ID, "ownership_transferred", nowUTC)
	return cloneApp(app), previousOwner, nil
}

func (r *MemoryRepository) DeleteApp(_ context.Context, appID string, verify func(current domainapphosting.App) error) (domainapphosting.App, []domainapphosting.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	trimmedID := strings.TrimSpace(appID)
	app, ok := r.appsByID[trimmedID]
	if !ok {
		return domainapphosting.App{}, nil, apperrors.ErrNotFound
	}
	if verify != nil {
		if err := verify(cloneApp(app)); err != nil {
			return domainapphosting.App{}, nil, err
		}
	}

	revs := make([]domainapphosting.Revision, 0, len(r.revisions[trimmedID]))
	for _, rev := range r.revisions[trimmedID] {
		revs = append(revs, cloneRevision(rev))
	}
	delete(r.appsByID, trimmedID)
	delete(r.appIDByDNS, strings.ToLower(app.DNSLabel))
	delete(r.revisions, trimmedID)
	for hash, ticket := range r.authTickets {
		if ticket.AppID == trimmedID {
			delete(r.authTickets, hash)
		}
	}
	for key, rb := range r.rollbackIdempotency {
		if rb.AppID == trimmedID {
			delete(r.rollbackIdempotency, key)
		}
	}
	for id, apv := range r.approvals {
		if apv.AppID == trimmedID {
			delete(r.approvals, id)
		}
	}
	return cloneApp(app), revs, nil
}

func (r *MemoryRepository) findPendingApprovalForAppLocked(appID string) *domainapphosting.VisibilityApproval {
	for _, apv := range r.approvals {
		if apv.AppID == appID && apv.Status == sohaapi.HostedAppVisibilityApprovalStatusPending {
			copied := cloneApproval(apv)
			return &copied
		}
	}
	return nil
}

func (r *MemoryRepository) invalidatePendingApprovalForAppLocked(appID, reason string, decidedAt time.Time) {
	for id, apv := range r.approvals {
		if apv.AppID == appID && apv.Status == sohaapi.HostedAppVisibilityApprovalStatusPending {
			apv.Status = sohaapi.HostedAppVisibilityApprovalStatusInvalidated
			apv.InvalidationReason = reason
			apv.UpdatedAt = decidedAt
			t := decidedAt
			apv.DecidedAt = &t
			r.approvals[id] = apv
		}
	}
}

func (r *MemoryRepository) invalidateAllPendingApprovalsLocked(reason string, decidedAt time.Time) {
	for id, apv := range r.approvals {
		if apv.Status == sohaapi.HostedAppVisibilityApprovalStatusPending {
			apv.Status = sohaapi.HostedAppVisibilityApprovalStatusInvalidated
			apv.InvalidationReason = reason
			apv.UpdatedAt = decidedAt
			t := decidedAt
			apv.DecidedAt = &t
			r.approvals[id] = apv
		}
	}
}

func (r *MemoryRepository) syncPendingApprovalAppNameLocked(appID, newName string) {
	for id, apv := range r.approvals {
		if apv.AppID == appID && apv.Status == sohaapi.HostedAppVisibilityApprovalStatusPending {
			apv.AppName = newName
			r.approvals[id] = apv
		}
	}
}

func (r *MemoryRepository) advanceInitialDraftApprovalBaseGenLocked(appID, ownerUserID string, prevGen, newGen int64) {
	for id, apv := range r.approvals {
		if apv.AppID == appID &&
			apv.Status == sohaapi.HostedAppVisibilityApprovalStatusPending &&
			apv.RequesterUserID == ownerUserID &&
			apv.BaseRouteGeneration == prevGen {
			apv.BaseRouteGeneration = newGen
			r.approvals[id] = apv
		}
	}
}

func cloneApproval(a domainapphosting.VisibilityApproval) domainapphosting.VisibilityApproval {
	out := a
	out.CurrentVisibility = domainapphosting.CloneVisibilityScope(a.CurrentVisibility)
	out.RequestedVisibility = domainapphosting.CloneVisibilityScope(a.RequestedVisibility)
	if a.DecidedAt != nil {
		t := a.DecidedAt.UTC()
		out.DecidedAt = &t
	}
	return out
}

func (d *MemoryAudienceDirectory) ResolveUserSummaries(_ context.Context, userIDs []string) (map[string]sohaapi.HostedAppUserSummary, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	out := make(map[string]sohaapi.HostedAppUserSummary, len(userIDs))
	for _, rawID := range userIDs {
		uid := strings.TrimSpace(rawID)
		if uid == "" {
			continue
		}
		u, ok := d.users[uid]
		if !ok {
			continue
		}
		display := strings.TrimSpace(u.DisplayName)
		if display == "" {
			display = strings.TrimSpace(u.Username)
		}
		if display == "" {
			display = u.ID
		}
		teamNames := d.resolveMemoryUserTeamNamesLocked(u)
		out[u.ID] = sohaapi.HostedAppUserSummary{
			UserID:      u.ID,
			DisplayName: display,
			AvatarURL:   truncateString(strings.TrimSpace(u.AvatarURL), 2048),
			TeamNames:   teamNames,
		}
	}
	return out, nil
}

func (d *MemoryAudienceDirectory) resolveMemoryUserTeamNamesLocked(u MemoryUser) []string {
	names := make([]string, 0, len(u.TeamIDs))
	for _, tid := range u.TeamIDs {
		if t, exists := d.teams[tid]; exists && strings.TrimSpace(t.Name) != "" {
			if !slices.Contains(names, t.Name) {
				names = append(names, t.Name)
			}
		}
	}
	slices.Sort(names)
	if len(names) == 0 && strings.TrimSpace(u.Department) != "" {
		names = []string{strings.TrimSpace(u.Department)}
	}
	if len(names) > 50 {
		names = names[:50]
	}
	return names
}

func (d *MemoryAudienceDirectory) ResolveUserPrincipal(_ context.Context, userID string) (domainidentity.Principal, bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	u, ok := d.users[strings.TrimSpace(userID)]
	if !ok {
		return domainidentity.Principal{}, false, apperrors.ErrNotFound
	}
	return domainidentity.Principal{
		UserID:   u.ID,
		UserName: u.Username,
		Roles:    slices.Clone(u.Roles),
		Teams:    slices.Clone(u.TeamIDs),
	}, strings.EqualFold(u.Status, "active"), nil
}

func (d *MemoryAudienceDirectory) ListTeamMemberIDs(_ context.Context, teamID string) ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	trimmed := strings.TrimSpace(teamID)
	if trimmed == "" {
		return []string{}, nil
	}
	members := make([]string, 0)
	for _, u := range d.users {
		if slices.Contains(u.TeamIDs, trimmed) {
			members = append(members, u.ID)
		}
	}
	slices.Sort(members)
	return members, nil
}
