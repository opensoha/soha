package networkaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type SiteRadiusProfileLookup func(context.Context, string, string) (domain.SiteProfileBinding, error)

func ResolveNASRadiusProfile(ctx context.Context, decision AccessNetworkDecision, siteID, profile string, lookup SiteRadiusProfileLookup) (*domain.SiteProfileBinding, error) {
	current, err := lookup(ctx, siteID, profile)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return nil, err
	}
	var siteProfile *domain.SiteProfileBinding
	if err == nil {
		if current.SiteID != siteID || current.AccessProfile != profile {
			return nil, fmt.Errorf("site profile binding does not match authorization")
		}
		siteProfile = &current
	}
	return domain.NASRadiusProfile(decision.AccessNetworkID, siteID, profile, NASNetworkAttributes(decision), siteProfile), nil
}

func CheckAccessNetworkProfile(ctx context.Context, decision AccessNetworkDecision, method, siteID string, lookup SiteRadiusProfileLookup) (AccessNetworkDecision, error) {
	if !decision.Allowed {
		return decision, nil
	}
	profile, _ := NASInitialProfile(decision, strings.ReplaceAll(method, "_", "-"))
	mapping, err := ResolveNASRadiusProfile(ctx, decision, siteID, profile, lookup)
	if err != nil {
		return AccessNetworkDecision{}, err
	}
	if mapping == nil {
		decision.Allowed, decision.ReasonCode, decision.AccessProfile = false, "site_profile_binding_unavailable", ""
	} else {
		decision.AccessProfile = profile
	}
	return decision, nil
}
