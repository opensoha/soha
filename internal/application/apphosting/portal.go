package apphosting

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) enrichHostedApp(ctx context.Context, settings domainapphosting.Settings, app domainapphosting.App) (sohaapi.HostedApp, error) {
	items, err := s.enrichHostedApps(ctx, settings, []domainapphosting.App{app})
	if err != nil {
		return sohaapi.HostedApp{}, err
	}
	return items[0], nil
}

func (s *Service) enrichHostedApps(ctx context.Context, settings domainapphosting.Settings, apps []domainapphosting.App) ([]sohaapi.HostedApp, error) {
	if len(apps) == 0 {
		return []sohaapi.HostedApp{}, nil
	}
	appIDs := make([]string, 0, len(apps))
	for _, app := range apps {
		appIDs = append(appIDs, app.ID)
	}
	enrichments, err := s.repo.LoadAppEnrichments(ctx, appIDs)
	if err != nil {
		return nil, err
	}
	userIDs := collectAppProfileUserIDs(apps, enrichments)
	summaries, err := s.resolveSummariesMap(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	out := make([]sohaapi.HostedApp, 0, len(apps))
	for _, app := range apps {
		out = append(out, buildEnrichedHostedAppDTO(app, settings, enrichments[app.ID], summaries))
	}
	return out, nil
}

func collectAppProfileUserIDs(apps []domainapphosting.App, enrichments map[string]AppEnrichment) []string {
	var ids []string
	add := func(id string) {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" && !slices.Contains(ids, trimmed) {
			ids = append(ids, trimmed)
		}
	}
	for _, app := range apps {
		add(app.OwnerUserID)
		for _, maintID := range app.MaintainerUserIDs {
			add(maintID)
		}
		if e, ok := enrichments[app.ID]; ok && e.PendingApproval != nil {
			add(e.PendingApproval.RequesterUserID)
		}
	}
	return ids
}

func buildEnrichedHostedAppDTO(
	app domainapphosting.App,
	settings domainapphosting.Settings,
	enrichment AppEnrichment,
	summaries map[string]sohaapi.HostedAppUserSummary,
) sohaapi.HostedApp {
	dto := app.ToAPI(settings)
	dto.ActiveRevisionNumber = enrichment.ActiveRevisionNumber
	dto.TotalBytes = enrichment.TotalBytes

	if ownerSummary, ok := summaries[dto.OwnerUserID]; ok {
		cloned := domainapphosting.CloneUserSummary(ownerSummary)
		dto.OwnerProfile = &cloned
	}
	if len(dto.MaintainerUserIDs) > 0 {
		maintainers := make([]sohaapi.HostedAppUserSummary, 0, len(dto.MaintainerUserIDs))
		for _, uid := range dto.MaintainerUserIDs {
			maintainers = append(maintainers, summaryOrFallback(summaries, uid))
		}
		dto.Maintainers = maintainers
	}
	if enrichment.PendingApproval != nil {
		var reqProfile *sohaapi.HostedAppUserSummary
		if summary, ok := summaries[enrichment.PendingApproval.RequesterUserID]; ok {
			cloned := domainapphosting.CloneUserSummary(summary)
			reqProfile = &cloned
		}
		apiApproval := enrichment.PendingApproval.ToAPI(reqProfile)
		dto.PendingVisibilityApproval = &apiApproval
	}
	return dto
}

func (s *Service) resolveSummariesMap(ctx context.Context, userIDs []string) (map[string]sohaapi.HostedAppUserSummary, error) {
	if s.audience == nil || len(userIDs) == 0 {
		return map[string]sohaapi.HostedAppUserSummary{}, nil
	}
	resolved, err := s.audience.ResolveUserSummaries(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return map[string]sohaapi.HostedAppUserSummary{}, nil
	}
	return resolved, nil
}

func summaryOrFallback(summaries map[string]sohaapi.HostedAppUserSummary, userID string) sohaapi.HostedAppUserSummary {
	trimmed := strings.TrimSpace(userID)
	if summary, ok := summaries[trimmed]; ok {
		return domainapphosting.CloneUserSummary(summary)
	}
	return sohaapi.HostedAppUserSummary{
		UserID:      trimmed,
		DisplayName: firstNonEmpty(trimmed, "Unknown"),
		TeamNames:   []string{},
	}
}

func (s *Service) ListPortalApps(
	ctx context.Context,
	principal domainidentity.Principal,
	params sohaapi.ListPortalHostedAppsParams,
) (sohaapi.PortalHostedAppCatalog, error) {
	if strings.TrimSpace(principal.UserID) == "" {
		return sohaapi.PortalHostedAppCatalog{}, apperrors.ErrUnauthorized
	}
	if err := s.authorize(ctx, principal, appaccess.PermIdentityPortalView); err != nil {
		return sohaapi.PortalHostedAppCatalog{}, err
	}
	normalized, err := normalizePortalListParams(params)
	if err != nil {
		return sohaapi.PortalHostedAppCatalog{}, err
	}
	effectivePrincipal, _, err := s.resolveEffectiveVisitor(ctx, principal)
	if err != nil {
		return sohaapi.PortalHostedAppCatalog{}, err
	}
	myAppsCount, err := s.countCallerMaintainableApps(ctx, principal)
	if err != nil {
		return sohaapi.PortalHostedAppCatalog{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.PortalHostedAppCatalog{}, err
	}
	settings = settings.WithDefaults()
	if !settings.Enabled {
		return sohaapi.PortalHostedAppCatalog{
			Items:       []sohaapi.PortalHostedAppItem{},
			Authors:     []sohaapi.PortalHostedAppAuthorSummary{},
			MyAppsCount: myAppsCount,
			Total:       0,
		}, nil
	}

	rawApps, err := s.repo.ListVisiblePortalApps(ctx, effectivePrincipal)
	if err != nil {
		return sohaapi.PortalHostedAppCatalog{}, err
	}
	visibleApps := filterStrictPortalVisible(rawApps, effectivePrincipal, settings)
	return s.buildPortalCatalog(ctx, visibleApps, settings, normalized, myAppsCount)
}

func normalizePortalListParams(params sohaapi.ListPortalHostedAppsParams) (sohaapi.ListPortalHostedAppsParams, error) {
	out := params
	out.Query = strings.TrimSpace(out.Query)
	if utf8.RuneCountInString(out.Query) > 200 {
		return sohaapi.ListPortalHostedAppsParams{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_query", "query must not exceed 200 characters", "搜索关键词长度不能超过 200 个字符")
	}
	if out.Kind != "" && !out.Kind.Valid() {
		return sohaapi.ListPortalHostedAppsParams{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_app_kind", "kind must be static or dynamic", "应用类型过滤参数无效")
	}
	out.AuthorUserID = strings.TrimSpace(out.AuthorUserID)
	if len(out.AuthorUserID) > 128 {
		return sohaapi.ListPortalHostedAppsParams{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_author_user_id", "authorUserId must not exceed 128 characters", "作者标识过长")
	}
	if out.Limit == 0 {
		out.Limit = 24
	}
	if out.Limit < 1 || out.Limit > 100 || out.Offset < 0 {
		return sohaapi.ListPortalHostedAppsParams{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_pagination", "limit must be 1..100 and offset >= 0", "分页参数无效")
	}
	return out, nil
}

func (s *Service) countCallerMaintainableApps(ctx context.Context, principal domainidentity.Principal) (int, error) {
	if s.authorizeAny(ctx, principal, appaccess.PermAppHostingAppsView, appaccess.PermAppHostingAppsPublish) != nil {
		return 0, nil
	}
	_, total, err := s.repo.ListApps(ctx, domainapphosting.AppListFilter{
		Scope:        sohaapi.HostedAppListScopeMine,
		MemberUserID: strings.TrimSpace(principal.UserID),
		Limit:        1,
		Offset:       0,
	})
	return total, err
}

func filterStrictPortalVisible(apps []domainapphosting.App, principal domainidentity.Principal, settings domainapphosting.Settings) []domainapphosting.App {
	visible := make([]domainapphosting.App, 0, len(apps))
	for _, app := range apps {
		if domainapphosting.VisibleInPortal(app, principal, settings) {
			visible = append(visible, app)
		}
	}
	return visible
}

func (s *Service) buildPortalCatalog(
	ctx context.Context,
	visibleApps []domainapphosting.App,
	settings domainapphosting.Settings,
	params sohaapi.ListPortalHostedAppsParams,
	myAppsCount int,
) (sohaapi.PortalHostedAppCatalog, error) {
	ownerIDs := make([]string, 0, len(visibleApps))
	visibleCountByOwner := make(map[string]int)
	for _, app := range visibleApps {
		visibleCountByOwner[app.OwnerUserID]++
		if !slices.Contains(ownerIDs, app.OwnerUserID) {
			ownerIDs = append(ownerIDs, app.OwnerUserID)
		}
	}
	summaries, err := s.resolveSummariesMap(ctx, ownerIDs)
	if err != nil {
		return sohaapi.PortalHostedAppCatalog{}, err
	}

	needle := strings.ToLower(params.Query)
	authors := buildPortalAuthorSummaries(visibleApps, visibleCountByOwner, summaries, needle, params.Kind)
	var selectedAuthor *sohaapi.PortalHostedAppAuthorSummary
	if params.AuthorUserID != "" && visibleCountByOwner[params.AuthorUserID] > 0 {
		summary := buildAuthorSummary(params.AuthorUserID, visibleCountByOwner[params.AuthorUserID], summaries)
		selectedAuthor = &summary
	}

	filtered := filterPortalCatalogApps(visibleApps, summaries, params.AuthorUserID, params.Kind, needle)
	slices.SortFunc(filtered, func(a, b domainapphosting.App) int {
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return b.UpdatedAt.Compare(a.UpdatedAt)
		}
		return cmp.Compare(a.ID, b.ID)
	})

	total := len(filtered)
	items := paginatePortalItems(filtered, settings, summaries, params.Offset, params.Limit)
	return sohaapi.PortalHostedAppCatalog{
		Items:          items,
		Authors:        authors,
		SelectedAuthor: selectedAuthor,
		MyAppsCount:    myAppsCount,
		Total:          total,
	}, nil
}

func buildPortalAuthorSummaries(
	visibleApps []domainapphosting.App,
	visibleCountByOwner map[string]int,
	summaries map[string]sohaapi.HostedAppUserSummary,
	needle string,
	kind sohaapi.HostedAppKind,
) []sohaapi.PortalHostedAppAuthorSummary {
	authorsWithMatchingApp := make(map[string]bool)
	if needle != "" {
		for _, app := range visibleApps {
			if kind != "" && app.Kind != kind {
				continue
			}
			author := summaryOrFallback(summaries, app.OwnerUserID)
			if matchesPortalAppOrAuthor(app, author, needle) {
				authorsWithMatchingApp[app.OwnerUserID] = true
			}
		}
	}

	out := make([]sohaapi.PortalHostedAppAuthorSummary, 0, len(visibleCountByOwner))
	for ownerID, count := range visibleCountByOwner {
		if count <= 0 {
			continue
		}
		author := buildAuthorSummary(ownerID, count, summaries)
		if needle != "" && !matchesAuthorSummary(author, needle) && !authorsWithMatchingApp[ownerID] {
			continue
		}
		out = append(out, author)
	}
	slices.SortFunc(out, func(a, b sohaapi.PortalHostedAppAuthorSummary) int {
		if a.VisibleAppCount != b.VisibleAppCount {
			return cmp.Compare(b.VisibleAppCount, a.VisibleAppCount)
		}
		if c := cmp.Compare(strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName)); c != 0 {
			return c
		}
		return cmp.Compare(a.UserID, b.UserID)
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func buildAuthorSummary(ownerID string, count int, summaries map[string]sohaapi.HostedAppUserSummary) sohaapi.PortalHostedAppAuthorSummary {
	s := summaryOrFallback(summaries, ownerID)
	return sohaapi.PortalHostedAppAuthorSummary{
		UserID:          s.UserID,
		DisplayName:     s.DisplayName,
		AvatarURL:       s.AvatarURL,
		TeamNames:       s.TeamNames,
		VisibleAppCount: count,
	}
}

func filterPortalCatalogApps(
	visibleApps []domainapphosting.App,
	summaries map[string]sohaapi.HostedAppUserSummary,
	authorUserID string,
	kind sohaapi.HostedAppKind,
	needle string,
) []domainapphosting.App {
	out := make([]domainapphosting.App, 0, len(visibleApps))
	for _, app := range visibleApps {
		if authorUserID != "" && app.OwnerUserID != authorUserID {
			continue
		}
		if kind != "" && app.Kind != kind {
			continue
		}
		author := summaryOrFallback(summaries, app.OwnerUserID)
		if needle != "" && !matchesPortalAppOrAuthor(app, author, needle) {
			continue
		}
		out = append(out, app)
	}
	return out
}

func matchesPortalAppOrAuthor(app domainapphosting.App, author sohaapi.HostedAppUserSummary, needle string) bool {
	if strings.Contains(strings.ToLower(app.Name), needle) || strings.Contains(strings.ToLower(app.Description), needle) {
		return true
	}
	return matchesAuthorDisplayOrTeam(author.DisplayName, author.TeamNames, needle)
}

func matchesAuthorSummary(author sohaapi.PortalHostedAppAuthorSummary, needle string) bool {
	return matchesAuthorDisplayOrTeam(author.DisplayName, author.TeamNames, needle)
}

func matchesAuthorDisplayOrTeam(displayName string, teamNames []string, needle string) bool {
	if strings.Contains(strings.ToLower(displayName), needle) {
		return true
	}
	for _, team := range teamNames {
		if strings.Contains(strings.ToLower(team), needle) {
			return true
		}
	}
	return false
}

func paginatePortalItems(
	filtered []domainapphosting.App,
	settings domainapphosting.Settings,
	summaries map[string]sohaapi.HostedAppUserSummary,
	offset, limit int,
) []sohaapi.PortalHostedAppItem {
	if offset >= len(filtered) {
		return []sohaapi.PortalHostedAppItem{}
	}
	end := min(offset+limit, len(filtered))
	slice := filtered[offset:end]
	items := make([]sohaapi.PortalHostedAppItem, 0, len(slice))
	for _, app := range slice {
		items = append(items, sohaapi.PortalHostedAppItem{
			ID:          app.ID,
			Name:        app.Name,
			Description: app.Description,
			Kind:        app.Kind,
			DNSLabel:    app.DNSLabel,
			Host:        app.Host(settings.AppsDomain),
			URL:         app.URL(settings),
			Author:      summaryOrFallback(summaries, app.OwnerUserID),
			UpdatedAt:   app.UpdatedAt.UTC(),
		})
	}
	return items
}
