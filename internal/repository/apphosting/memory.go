package apphosting

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var (
	_ appapphosting.Repository        = (*MemoryRepository)(nil)
	_ appapphosting.AudienceDirectory = (*MemoryAudienceDirectory)(nil)
)

type MemoryRepository struct {
	mu                  sync.Mutex
	settings            domainapphosting.Settings
	appsByID            map[string]domainapphosting.App
	appIDByDNS          map[string]string
	revisions           map[string][]domainapphosting.Revision
	deletedRuntimeWork  map[string]domainapphosting.Revision
	authTickets         map[string]domainapphosting.AuthTicket
	rollbackIdempotency map[string]domainapphosting.RollbackIdempotencyRecord
	approvals           map[string]domainapphosting.VisibilityApproval
	runtimeConfigs      map[string]domainapphosting.RuntimeConfig
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		settings:            domainapphosting.DefaultSettings(),
		appsByID:            make(map[string]domainapphosting.App),
		appIDByDNS:          make(map[string]string),
		revisions:           make(map[string][]domainapphosting.Revision),
		deletedRuntimeWork:  make(map[string]domainapphosting.Revision),
		authTickets:         make(map[string]domainapphosting.AuthTicket),
		rollbackIdempotency: make(map[string]domainapphosting.RollbackIdempotencyRecord),
		approvals:           make(map[string]domainapphosting.VisibilityApproval),
		runtimeConfigs:      make(map[string]domainapphosting.RuntimeConfig),
	}
}

func (r *MemoryRepository) GetSettings(_ context.Context) (domainapphosting.Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneSettings(r.settings.WithDefaults()), nil
}

func (r *MemoryRepository) SaveSettings(_ context.Context, mutate func(current domainapphosting.Settings) (domainapphosting.Settings, error)) (domainapphosting.Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := cloneSettings(r.settings.WithDefaults())
	next, err := mutate(prev)
	if err != nil {
		return domainapphosting.Settings{}, err
	}
	normalized := cloneSettings(next.WithDefaults())
	if normalized.OrganizationVisibilityPolicy != prev.OrganizationVisibilityPolicy || normalized.PolicyVersion != prev.PolicyVersion {
		decidedAt := time.Now().UTC()
		if normalized.UpdatedAt != nil {
			decidedAt = normalized.UpdatedAt.UTC()
		}
		r.invalidateAllPendingApprovalsLocked("policy_changed", decidedAt)
	}
	r.settings = normalized
	return cloneSettings(r.settings), nil
}

func (r *MemoryRepository) CreateApp(ctx context.Context, candidate domainapphosting.App, maxAppsPerOwner int32) (domainapphosting.App, bool, error) {
	created, _, reused, err := r.CreateAppWithApproval(ctx, candidate, maxAppsPerOwner, nil)
	return created, reused, err
}

func (r *MemoryRepository) CreateAppWithApproval(
	_ context.Context,
	candidate domainapphosting.App,
	maxAppsPerOwner int32,
	initialApproval *domainapphosting.VisibilityApproval,
) (domainapphosting.App, *domainapphosting.VisibilityApproval, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if candidate.IdempotencyKey != "" {
		for _, existing := range r.appsByID {
			if existing.OwnerUserID == candidate.OwnerUserID && existing.IdempotencyKey == candidate.IdempotencyKey {
				if existing.RequestFingerprint != candidate.RequestFingerprint {
					return domainapphosting.App{}, nil, false, apperrors.NewBusiness(apperrors.ErrConflict, "idempotency_key_conflict", "idempotencyKey already used with different create parameters", "同一幂等键已被不同参数的应用创建请求占用")
				}
				return cloneApp(existing), r.findPendingApprovalForAppLocked(existing.ID), true, nil
			}
		}
	}

	var ownerCount int32
	for _, existing := range r.appsByID {
		if existing.OwnerUserID == candidate.OwnerUserID && existing.Kind == candidate.Kind {
			ownerCount++
		}
	}
	if maxAppsPerOwner > 0 && ownerCount >= maxAppsPerOwner {
		return domainapphosting.App{}, nil, false, domainapphosting.ErrQuotaExceeded
	}
	lowerDNS := strings.ToLower(candidate.DNSLabel)
	if _, exists := r.appIDByDNS[lowerDNS]; exists {
		return domainapphosting.App{}, nil, false, appapphosting.ErrDNSLabelConflict
	}

	stored := cloneApp(candidate)
	stored.DNSLabel = lowerDNS
	r.appsByID[stored.ID] = stored
	r.appIDByDNS[lowerDNS] = stored.ID

	var savedApproval *domainapphosting.VisibilityApproval
	if initialApproval != nil {
		apv := cloneApproval(*initialApproval)
		apv.AppID = stored.ID
		apv.AppName = stored.Name
		apv.AppDNSLabel = stored.DNSLabel
		r.approvals[apv.ID] = apv
		copied := cloneApproval(apv)
		savedApproval = &copied
	}
	return cloneApp(stored), savedApproval, false, nil
}

func (r *MemoryRepository) GetAppByID(_ context.Context, appID string) (domainapphosting.App, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	app, ok := r.appsByID[strings.TrimSpace(appID)]
	if !ok {
		return domainapphosting.App{}, apperrors.ErrNotFound
	}
	return cloneApp(app), nil
}

func (r *MemoryRepository) GetAppByDNSLabel(_ context.Context, dnsLabel string) (domainapphosting.App, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	appID, ok := r.appIDByDNS[strings.ToLower(strings.TrimSpace(dnsLabel))]
	if !ok {
		return domainapphosting.App{}, apperrors.ErrNotFound
	}
	app, ok := r.appsByID[appID]
	if !ok {
		return domainapphosting.App{}, apperrors.ErrNotFound
	}
	return cloneApp(app), nil
}

func (r *MemoryRepository) ListApps(_ context.Context, filter domainapphosting.AppListFilter) ([]domainapphosting.App, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]domainapphosting.App, 0, len(r.appsByID))
	needle := strings.ToLower(strings.TrimSpace(filter.Query))
	for _, item := range r.appsByID {
		if filter.Scope != sohaapi.HostedAppListScopeAll && !item.IsOwnerOrMaintainer(filter.MemberUserID) {
			continue
		}
		if filter.OwnerUserID != "" && item.OwnerUserID != filter.OwnerUserID {
			continue
		}
		if filter.TeamFilterActive && !slices.Contains(filter.TeamMemberUserIDs, item.OwnerUserID) {
			continue
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if needle != "" && !matchesAppQuery(item, needle) {
			continue
		}
		matched = append(matched, cloneApp(item))
	}
	slices.SortFunc(matched, func(a, b domainapphosting.App) int {
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return b.UpdatedAt.Compare(a.UpdatedAt)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	total := len(matched)
	if filter.Offset >= total {
		return []domainapphosting.App{}, total, nil
	}
	end := min(filter.Offset+filter.Limit, total)
	return matched[filter.Offset:end], total, nil
}

func matchesAppQuery(item domainapphosting.App, needle string) bool {
	return strings.Contains(strings.ToLower(item.Name), needle) ||
		strings.Contains(strings.ToLower(item.DNSLabel), needle) ||
		strings.Contains(strings.ToLower(item.Description), needle)
}

func (r *MemoryRepository) MutateApp(_ context.Context, appID string, mutate func(current domainapphosting.App) (domainapphosting.App, error)) (domainapphosting.App, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.appsByID[strings.TrimSpace(appID)]
	if !ok {
		return domainapphosting.App{}, apperrors.ErrNotFound
	}
	next, err := mutate(cloneApp(current))
	if err != nil {
		return domainapphosting.App{}, err
	}
	next.ID = current.ID
	next.DNSLabel = current.DNSLabel
	next.OwnerUserID = current.OwnerUserID
	if config, exists := r.runtimeConfigs[current.ID]; exists && !slices.Equal(current.MaintainerUserIDs, next.MaintainerUserIDs) {
		config.SensitivePublisherIDs = slices.DeleteFunc(config.SensitivePublisherIDs, func(id string) bool {
			return !slices.Contains(next.MaintainerUserIDs, id)
		})
		r.runtimeConfigs[current.ID] = config
	}
	decidedAt := next.UpdatedAt.UTC()
	if decidedAt.IsZero() {
		decidedAt = time.Now().UTC()
	}
	if next.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
		r.invalidatePendingApprovalForAppLocked(current.ID, "app_suspended", decidedAt)
	} else if next.RouteGeneration != current.RouteGeneration {
		r.invalidatePendingApprovalForAppLocked(current.ID, "route_generation_changed", decidedAt)
	} else if next.Name != current.Name {
		r.syncPendingApprovalAppNameLocked(current.ID, next.Name)
	}
	r.appsByID[current.ID] = cloneApp(next)
	return cloneApp(next), nil
}

func (r *MemoryRepository) FindRevisionByIdempotencyKey(_ context.Context, appID, idempotencyKey string) (domainapphosting.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rev := range r.revisions[appID] {
		if rev.IdempotencyKey != "" && rev.IdempotencyKey == idempotencyKey {
			return cloneRevision(rev), nil
		}
	}
	return domainapphosting.Revision{}, apperrors.ErrNotFound
}

func (r *MemoryRepository) CreateUploadingRevision(_ context.Context, appID string, draft domainapphosting.Revision) (domainapphosting.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.appsByID[appID]
	if !ok {
		return domainapphosting.Revision{}, apperrors.ErrNotFound
	}
	if app.Status == sohaapi.HostedAppStatusOffline || app.Status == sohaapi.HostedAppStatusSuspendedByAdmin {
		return domainapphosting.Revision{}, apperrors.NewBusiness(apperrors.ErrConflict, "app_not_deployable", "cannot upload revision while application is offline or administratively suspended", "应用处于下线或管理员封停状态时无法部署新版本")
	}
	list := r.revisions[appID]
	var maxNum int64
	for _, existing := range list {
		if draft.IdempotencyKey != "" && existing.IdempotencyKey == draft.IdempotencyKey {
			return domainapphosting.Revision{}, apperrors.ErrConflict
		}
		if existing.RevisionNumber > maxNum {
			maxNum = existing.RevisionNumber
		}
	}
	stored := cloneRevision(draft)
	stored.RuntimeEnvironment = nil
	stored.PublicationVisibilityRecorded = true
	stored.RevisionNumber = maxNum + 1
	stored.Status = sohaapi.HostedAppRevisionStatusUploading
	r.revisions[appID] = append(list, stored)
	return cloneRevision(stored), nil
}

func (r *MemoryRepository) FailRevision(_ context.Context, appID, revisionID, stage, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	list := r.revisions[appID]
	for i := range list {
		if list[i].ID == revisionID {
			if list[i].Kind == sohaapi.HostedAppKindDynamic && list[i].Status != sohaapi.HostedAppRevisionStatusUploading {
				return apperrors.ErrNotFound
			}
			list[i].Status = sohaapi.HostedAppRevisionStatusFailed
			list[i].FailureStage = stage
			list[i].FailureReason = truncateString(reason, 1024)
			return nil
		}
	}
	return apperrors.ErrNotFound
}

func (r *MemoryRepository) ActivateRevision(ctx context.Context, input appapphosting.ActivateRevisionInput) (appapphosting.ActivateRevisionOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.appsByID[input.AppID]
	if !ok {
		return appapphosting.ActivateRevisionOutput{}, apperrors.ErrNotFound
	}
	list := r.revisions[input.AppID]
	revIdx := findRevisionIndex(list, input.RevisionID)
	if revIdx < 0 {
		return appapphosting.ActivateRevisionOutput{}, apperrors.ErrNotFound
	}
	rev := &list[revIdx]
	if err := checkRevisionActivation(app, *rev, input); err != nil {
		return appapphosting.ActivateRevisionOutput{}, err
	}
	if rev.Kind == sohaapi.HostedAppKindDynamic && !r.settings.Enabled {
		return appapphosting.ActivateRevisionOutput{}, appapphosting.ErrDynamicRevisionFailed
	}
	failureReason, err := revisionActivationRejection(app, *rev, input, r.runtimeConfigs[app.ID])
	if err == nil && rev.Kind == sohaapi.HostedAppKindDynamic && hasNewerActivatedRevision(list, rev.RevisionNumber) {
		failureReason, err = revisionSuperseded()
	}
	if err != nil {
		rev.Status = sohaapi.HostedAppRevisionStatusFailed
		rev.FailureStage = "activate"
		rev.FailureReason = failureReason
		clearDynamicRevisionWork(rev)
		if dynamicCleanupStatus(*rev) {
			rev.NextCheckAt = input.Now.UTC()
		}
		return appapphosting.ActivateRevisionOutput{}, err
	}
	if input.BeforeActivate != nil {
		if err := input.BeforeActivate(ctx); err != nil {
			return appapphosting.ActivateRevisionOutput{}, err
		}
	}

	wasDraft := app.Status == sohaapi.HostedAppStatusDraft
	prevGen := app.RouteGeneration
	nowUTC := input.Now.UTC()
	rev.Status = sohaapi.HostedAppRevisionStatusReady
	rev.SpaFallback = input.SpaFallback
	rev.Files = slices.Clone(input.Files)
	rev.ActivatedAt = &nowUTC
	clearDynamicRevisionWork(rev)

	if wasDraft {
		app.Status = sohaapi.HostedAppStatusOnline
	}
	app.ActiveRevisionID = rev.ID
	app.SpaFallback = input.SpaFallback
	app.RouteGeneration++
	app.UpdatedAt = nowUTC
	r.appsByID[app.ID] = cloneApp(app)
	if wasDraft {
		r.advanceInitialDraftApprovalBaseGenLocked(app.ID, app.OwnerUserID, prevGen, app.RouteGeneration)
	} else {
		r.invalidatePendingApprovalForAppLocked(app.ID, "route_generation_changed", nowUTC)
	}

	var visibility *appapphosting.UpdateVisibilityTxOutput
	settings := r.settings.WithDefaults()
	if visibilityInput, ok := publicationVisibilityInput(app, prevGen, *rev, settings, nowUTC); ok {
		updated, err := r.applyVisibilityLocked(app, settings, visibilityInput)
		if err != nil {
			return appapphosting.ActivateRevisionOutput{}, err
		}
		app = updated.App
		visibility = &updated
	}

	evicted := evictExcessMemoryRevisions(list, app.ActiveRevisionID, input.RetainedRevisionsMax, nowUTC)
	return appapphosting.ActivateRevisionOutput{
		App:              cloneApp(app),
		Revision:         cloneRevision(*rev),
		EvictedRevisions: evicted,
		Visibility:       visibility,
	}, nil
}

func hasNewerActivatedRevision(revisions []domainapphosting.Revision, revisionNumber int64) bool {
	for _, rev := range revisions {
		if rev.RevisionNumber > revisionNumber && rev.ActivatedAt != nil {
			return true
		}
	}
	return false
}

func evictExcessMemoryRevisions(list []domainapphosting.Revision, activeID string, retainedMax int32, now time.Time) []domainapphosting.Revision {
	if retainedMax <= 0 {
		return nil
	}
	readyIndices := make([]int, 0, len(list))
	for i := range list {
		if list[i].Status == sohaapi.HostedAppRevisionStatusReady {
			readyIndices = append(readyIndices, i)
		}
	}
	slices.SortFunc(readyIndices, func(a, b int) int {
		return cmp.Compare(list[b].RevisionNumber, list[a].RevisionNumber)
	})
	var kept int32
	var evicted []domainapphosting.Revision
	for _, idx := range readyIndices {
		if list[idx].ID == activeID {
			kept++
			continue
		}
		if kept < retainedMax {
			kept++
			continue
		}
		list[idx].Status = sohaapi.HostedAppRevisionStatusEvicted
		if dynamicCleanupStatus(list[idx]) {
			list[idx].NextCheckAt = now
		}
		evicted = append(evicted, cloneRevision(list[idx]))
	}
	return evicted
}

func (r *MemoryRepository) GetRevision(_ context.Context, appID, revisionID string) (domainapphosting.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rev := range r.revisions[appID] {
		if rev.ID == revisionID {
			return cloneRevision(rev), nil
		}
	}
	return domainapphosting.Revision{}, apperrors.ErrNotFound
}

func (r *MemoryRepository) ListRevisions(_ context.Context, appID string, limit, offset int) ([]domainapphosting.Revision, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	list := slices.Clone(r.revisions[appID])
	slices.SortFunc(list, func(a, b domainapphosting.Revision) int {
		return cmp.Compare(b.RevisionNumber, a.RevisionNumber)
	})
	total := len(list)
	if offset >= total {
		return []domainapphosting.Revision{}, total, nil
	}
	end := min(offset+limit, total)
	items := make([]domainapphosting.Revision, 0, end-offset)
	for _, rev := range list[offset:end] {
		items = append(items, cloneRevision(rev))
	}
	return items, total, nil
}

func (r *MemoryRepository) RollbackRevision(ctx context.Context, appID, targetRevisionID, idempotencyKey string, now time.Time) (domainapphosting.App, domainapphosting.Revision, bool, error) {
	return r.rollbackRevision(ctx, appID, targetRevisionID, idempotencyKey, "", now)
}

func (r *MemoryRepository) RollbackRevisionAs(ctx context.Context, appID, targetRevisionID, idempotencyKey, actorUserID string, now time.Time) (domainapphosting.App, domainapphosting.Revision, bool, error) {
	if strings.TrimSpace(actorUserID) == "" {
		return domainapphosting.App{}, domainapphosting.Revision{}, false, apperrors.ErrAccessDenied
	}
	return r.rollbackRevision(ctx, appID, targetRevisionID, idempotencyKey, actorUserID, now)
}

func (r *MemoryRepository) rollbackRevision(_ context.Context, appID, targetRevisionID, idempotencyKey, actorUserID string, now time.Time) (domainapphosting.App, domainapphosting.Revision, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.appsByID[appID]
	if !ok {
		return domainapphosting.App{}, domainapphosting.Revision{}, false, apperrors.ErrNotFound
	}
	if (actorUserID != "" || app.Kind == sohaapi.HostedAppKindDynamic) && !app.IsOwnerOrMaintainer(actorUserID) {
		return domainapphosting.App{}, domainapphosting.Revision{}, false, apperrors.ErrAccessDenied
	}
	list := r.revisions[appID]
	trimmedKey := strings.TrimSpace(idempotencyKey)
	if trimmedKey != "" {
		if existing, found := r.rollbackIdempotency[appID+"\x00"+trimmedKey]; found {
			if existing.TargetRevisionID != targetRevisionID {
				return domainapphosting.App{}, domainapphosting.Revision{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "idempotency_key_conflict", "idempotencyKey was already used for a different targetRevisionId", "该幂等键已绑定到不同的目标回滚版本")
			}
			idx := findRevisionIndex(list, existing.TargetRevisionID)
			if idx < 0 {
				return domainapphosting.App{}, domainapphosting.Revision{}, false, apperrors.ErrNotFound
			}
			if err := checkRollbackRevisionKind(app, list[idx]); err != nil {
				return domainapphosting.App{}, domainapphosting.Revision{}, false, err
			}
			if err := checkRollbackRuntime(app, list[idx], actorUserID, r.runtimeConfigs[appID]); err != nil {
				return domainapphosting.App{}, domainapphosting.Revision{}, false, err
			}
			return cloneApp(app), cloneRevision(list[idx]), true, nil
		}
	}
	if app.Status == sohaapi.HostedAppStatusDraft {
		return domainapphosting.App{}, domainapphosting.Revision{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "app_still_draft", "draft application cannot roll back", "草稿状态应用无可回滚版本")
	}
	idx := findRevisionIndex(list, targetRevisionID)
	if idx < 0 {
		return domainapphosting.App{}, domainapphosting.Revision{}, false, apperrors.ErrNotFound
	}
	if err := checkRollbackCandidate(app, list[idx]); err != nil {
		return domainapphosting.App{}, domainapphosting.Revision{}, false, err
	}
	if err := checkRollbackRuntime(app, list[idx], actorUserID, r.runtimeConfigs[appID]); err != nil {
		return domainapphosting.App{}, domainapphosting.Revision{}, false, err
	}

	nowUTC := now.UTC()
	list[idx].ActivatedAt = &nowUTC
	app.ActiveRevisionID = list[idx].ID
	app.SpaFallback = list[idx].SpaFallback
	app.RouteGeneration++
	app.UpdatedAt = nowUTC
	r.appsByID[appID] = cloneApp(app)
	r.invalidatePendingApprovalForAppLocked(appID, "route_generation_changed", nowUTC)
	if trimmedKey != "" {
		r.rollbackIdempotency[appID+"\x00"+trimmedKey] = domainapphosting.RollbackIdempotencyRecord{
			AppID:            appID,
			IdempotencyKey:   trimmedKey,
			TargetRevisionID: list[idx].ID,
			RouteGeneration:  app.RouteGeneration,
			CreatedAt:        nowUTC,
		}
	}
	return cloneApp(app), cloneRevision(list[idx]), false, nil
}

func (r *MemoryRepository) CreateAuthTicket(_ context.Context, ticket domainapphosting.AuthTicket) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.authTickets[ticket.TicketSHA256] = cloneTicket(ticket)
	return nil
}

func (r *MemoryRepository) ConsumeAuthTicket(_ context.Context, ticketSHA256, host string, now time.Time) (domainapphosting.AuthTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ticket, ok := r.authTickets[ticketSHA256]
	if !ok || ticket.ConsumedAt != nil || !now.Before(ticket.ExpiresAt) || !strings.EqualFold(ticket.Host, host) {
		return domainapphosting.AuthTicket{}, apperrors.ErrAccessDenied
	}
	consumedAt := now.UTC()
	ticket.ConsumedAt = &consumedAt
	r.authTickets[ticketSHA256] = ticket
	return cloneTicket(ticket), nil
}

type MemoryUser struct {
	ID          string
	Username    string
	DisplayName string
	AvatarURL   string
	Email       string
	Department  string
	Status      string
	TeamIDs     []string
	Roles       []string
}

type MemoryAudienceDirectory struct {
	mu    sync.RWMutex
	users map[string]MemoryUser
	teams map[string]sohaapi.AppHostingAudienceTeam
}

func NewMemoryAudienceDirectory() *MemoryAudienceDirectory {
	return &MemoryAudienceDirectory{
		users: make(map[string]MemoryUser),
		teams: make(map[string]sohaapi.AppHostingAudienceTeam),
	}
}

func (d *MemoryAudienceDirectory) UpsertUser(user MemoryUser) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if strings.TrimSpace(user.Status) == "" {
		user.Status = "active"
	}
	user.TeamIDs = slices.Clone(user.TeamIDs)
	user.Roles = slices.Clone(user.Roles)
	d.users[user.ID] = user
}

func (d *MemoryAudienceDirectory) SetUserRoles(userID string, roles []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if u, ok := d.users[userID]; ok {
		u.Roles = slices.Clone(roles)
		d.users[userID] = u
	}
}

func (d *MemoryAudienceDirectory) SetUserStatus(userID, status string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if u, ok := d.users[userID]; ok {
		u.Status = status
		d.users[userID] = u
	}
}

func (d *MemoryAudienceDirectory) SetUserTeams(userID string, teamIDs []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if u, ok := d.users[userID]; ok {
		u.TeamIDs = slices.Clone(teamIDs)
		d.users[userID] = u
	}
}

func (d *MemoryAudienceDirectory) UpsertTeam(team sohaapi.AppHostingAudienceTeam) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.teams[team.ID] = team
}

func (d *MemoryAudienceDirectory) SearchCandidates(_ context.Context, query string, limit int) (sohaapi.AppHostingAudienceCandidates, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)
	needle := strings.ToLower(strings.TrimSpace(query))

	users := make([]sohaapi.AppHostingAudienceUser, 0)
	for _, u := range d.users {
		if !strings.EqualFold(u.Status, "active") {
			continue
		}
		display := u.DisplayName
		if strings.TrimSpace(display) == "" {
			display = u.Username
		}
		if needle != "" &&
			!strings.Contains(strings.ToLower(u.Username), needle) &&
			!strings.Contains(strings.ToLower(display), needle) &&
			!strings.Contains(strings.ToLower(u.Department), needle) {
			continue
		}
		users = append(users, sohaapi.AppHostingAudienceUser{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: display,
			Department:  u.Department,
		})
	}
	slices.SortFunc(users, func(a, b sohaapi.AppHostingAudienceUser) int {
		if c := cmp.Compare(a.Username, b.Username); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if len(users) > limit {
		users = users[:limit]
	}

	teams := make([]sohaapi.AppHostingAudienceTeam, 0)
	for _, t := range d.teams {
		if needle != "" &&
			!strings.Contains(strings.ToLower(t.Name), needle) &&
			!strings.Contains(strings.ToLower(t.Slug), needle) {
			continue
		}
		teams = append(teams, t)
	}
	slices.SortFunc(teams, func(a, b sohaapi.AppHostingAudienceTeam) int {
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if len(teams) > limit {
		teams = teams[:limit]
	}
	return sohaapi.AppHostingAudienceCandidates{Users: users, Teams: teams}, nil
}

func (d *MemoryAudienceDirectory) ResolveVisitor(_ context.Context, userID string) (domainapphosting.VisitorProfile, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	u, ok := d.users[strings.TrimSpace(userID)]
	if !ok {
		return domainapphosting.VisitorProfile{}, apperrors.ErrNotFound
	}
	if !strings.EqualFold(u.Status, "active") {
		return domainapphosting.VisitorProfile{}, domainapphosting.ErrVisitorDisabled
	}
	return domainapphosting.VisitorProfile{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Department:  u.Department,
		TeamIDs:     slices.Clone(u.TeamIDs),
	}, nil
}

func findRevisionIndex(list []domainapphosting.Revision, id string) int {
	for i := range list {
		if list[i].ID == id {
			return i
		}
	}
	return -1
}

func cloneSettings(s domainapphosting.Settings) domainapphosting.Settings {
	out := s
	if s.GatewayTokenRotatedAt != nil {
		t := s.GatewayTokenRotatedAt.UTC()
		out.GatewayTokenRotatedAt = &t
	}
	if s.UpdatedAt != nil {
		t := s.UpdatedAt.UTC()
		out.UpdatedAt = &t
	}
	return out
}

func cloneApp(a domainapphosting.App) domainapphosting.App {
	out := a
	out.MaintainerUserIDs = slices.Clone(a.MaintainerUserIDs)
	out.Visibility = domainapphosting.CloneVisibilityScope(a.Visibility)
	return out
}

func cloneRevision(r domainapphosting.Revision) domainapphosting.Revision {
	out := r
	out.Files = slices.Clone(r.Files)
	out.RuntimeEnvironment = maps.Clone(r.RuntimeEnvironment)
	if r.PublishVisibility != nil {
		visibility := *r.PublishVisibility
		visibility.Scope = domainapphosting.CloneVisibilityScope(visibility.Scope)
		out.PublishVisibility = &visibility
	}
	if r.ActivatedAt != nil {
		t := r.ActivatedAt.UTC()
		out.ActivatedAt = &t
	}
	return out
}

func cloneTicket(t domainapphosting.AuthTicket) domainapphosting.AuthTicket {
	out := t
	out.Teams = slices.Clone(t.Teams)
	if t.ConsumedAt != nil {
		c := t.ConsumedAt.UTC()
		out.ConsumedAt = &c
	}
	return out
}

func truncateString(value string, maxChars int) string {
	if len(value) <= maxChars {
		return value
	}
	return value[:maxChars]
}
