package apphosting

import (
	"slices"
	"strings"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func NormalizeVisibilityInput(input *sohaapi.HostedAppVisibilityInput, settings Settings) (sohaapi.HostedAppVisibilityScope, error) {
	if input == nil {
		return sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate}, nil
	}
	if !input.Mode.Valid() {
		return sohaapi.HostedAppVisibilityScope{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_visibility_mode", "visibility.mode must be private, selected, or organization", "可见范围模式必须为 private、selected 或 organization")
	}
	switch input.Mode {
	case sohaapi.HostedAppVisibilityModePrivate:
		if len(input.UserIDs) > 0 || len(input.TeamIDs) > 0 {
			return sohaapi.HostedAppVisibilityScope{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_private_visibility", "private visibility must not specify userIds or teamIds", "仅自己可见模式不能附带指定用户或部门")
		}
		return sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate}, nil
	case sohaapi.HostedAppVisibilityModeOrganization:
		if settings.EffectiveOrganizationPolicy() == sohaapi.AppHostingOrganizationVisibilityPolicyDisabled {
			return sohaapi.HostedAppVisibilityScope{}, apperrors.NewBusiness(apperrors.ErrAccessDenied, "organization_visibility_disabled", "organization-wide visibility is disabled by platform policy", "平台管理员当前已禁用全公司公开可见范围")
		}
		if len(input.UserIDs) > 0 || len(input.TeamIDs) > 0 {
			return sohaapi.HostedAppVisibilityScope{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_organization_visibility", "organization visibility must not specify userIds or teamIds", "全公司可见模式不能附带指定用户或部门")
		}
		return sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeOrganization}, nil
	case sohaapi.HostedAppVisibilityModeSelected:
		users, err := sanitizeIdentifierSlice(input.UserIDs, "userIds")
		if err != nil {
			return sohaapi.HostedAppVisibilityScope{}, err
		}
		teams, err := sanitizeIdentifierSlice(input.TeamIDs, "teamIds")
		if err != nil {
			return sohaapi.HostedAppVisibilityScope{}, err
		}
		if len(users) == 0 && len(teams) == 0 {
			return sohaapi.HostedAppVisibilityScope{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "empty_selected_visibility", "selected visibility requires at least one userId or teamId", "指定范围可见至少需要选择一个同事或部门")
		}
		return sohaapi.HostedAppVisibilityScope{
			Mode:    sohaapi.HostedAppVisibilityModeSelected,
			UserIDs: users,
			TeamIDs: teams,
		}, nil
	default:
		return sohaapi.HostedAppVisibilityScope{}, apperrors.ErrInvalidArgument
	}
}

func CloneVisibilityScope(scope sohaapi.HostedAppVisibilityScope) sohaapi.HostedAppVisibilityScope {
	mode := scope.Mode
	if !mode.Valid() {
		mode = sohaapi.HostedAppVisibilityModePrivate
	}
	if mode != sohaapi.HostedAppVisibilityModeSelected {
		return sohaapi.HostedAppVisibilityScope{Mode: mode}
	}
	var users, teams []string
	if len(scope.UserIDs) > 0 {
		users = slices.Clone(scope.UserIDs)
	}
	if len(scope.TeamIDs) > 0 {
		teams = slices.Clone(scope.TeamIDs)
	}
	return sohaapi.HostedAppVisibilityScope{
		Mode:    mode,
		UserIDs: users,
		TeamIDs: teams,
	}
}

func ScopesEqual(a, b sohaapi.HostedAppVisibilityScope) bool {
	ca := CloneVisibilityScope(a)
	cb := CloneVisibilityScope(b)
	return ca.Mode == cb.Mode && slices.Equal(ca.UserIDs, cb.UserIDs) && slices.Equal(ca.TeamIDs, cb.TeamIDs)
}

// AccessAllowed evaluates whether an authenticated principal satisfies the
// hosted application's visibility audience rule (independent of online/platform state).
func AccessAllowed(app App, principal domainidentity.Principal) bool {
	userID := strings.TrimSpace(principal.UserID)
	if userID == "" {
		return false
	}
	if app.OwnerUserID != "" && userID == app.OwnerUserID {
		return true
	}
	switch app.Visibility.Mode {
	case sohaapi.HostedAppVisibilityModePrivate:
		return false
	case sohaapi.HostedAppVisibilityModeSelected:
		if slices.Contains(app.Visibility.UserIDs, userID) {
			return true
		}
		for _, visitorTeam := range principal.Teams {
			trimmed := strings.TrimSpace(visitorTeam)
			if trimmed != "" && slices.Contains(app.Visibility.TeamIDs, trimmed) {
				return true
			}
		}
		return false
	case sohaapi.HostedAppVisibilityModeOrganization:
		return true
	default:
		return false
	}
}

// VisibleInPortal reports whether a hosted application appears in the Colleague Works
// portal catalog for the given principal: must be online with an active revision,
// non-private (even for the owner), and permitted by AccessAllowed.
func VisibleInPortal(app App, principal domainidentity.Principal, settings Settings) bool {
	if !settings.Enabled || app.Status != sohaapi.HostedAppStatusOnline || strings.TrimSpace(app.ActiveRevisionID) == "" {
		return false
	}
	if app.Visibility.Mode == sohaapi.HostedAppVisibilityModePrivate {
		return false
	}
	return AccessAllowed(app, principal)
}

// CanAccess evaluates whether an authenticated visitor principal is permitted to
// access a currently online hosted application under active platform settings.
func CanAccess(principal domainidentity.Principal, app App, settings Settings) bool {
	if !settings.Enabled || app.Status != sohaapi.HostedAppStatusOnline || strings.TrimSpace(app.ActiveRevisionID) == "" {
		return false
	}
	return AccessAllowed(app, principal)
}

func sanitizeIdentifierSlice(values []string, field string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || len(trimmed) > 128 {
			return nil, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_audience_id", field+" entries must be 1..128 non-empty characters", field+" 包含空值或超长标识")
		}
		if !slices.Contains(out, trimmed) {
			out = append(out, trimmed)
		}
	}
	slices.Sort(out)
	return out, nil
}
