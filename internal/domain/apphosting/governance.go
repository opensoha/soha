package apphosting

import (
	"slices"
	"strings"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const DefaultAppDataWarningMiB = 200

func (s Settings) EffectiveGovernance() sohaapi.AppHostingGovernance {
	if s.Governance != nil {
		result := *s.Governance
		result.QuotaOverrides = append([]sohaapi.AppHostingQuotaOverride{}, result.QuotaOverrides...)
		return result
	}
	return sohaapi.AppHostingGovernance{
		MaxDynamicAppsPerOwner: min(s.WithDefaults().MaxAppsPerOwner, 5),
		DataCapacityWarningMiB: DefaultAppDataWarningMiB,
		QuotaOverrides:         []sohaapi.AppHostingQuotaOverride{},
		RuntimeLimits: sohaapi.AppHostingRuntimeLimits{
			CPUMilli: 500, MemoryMiB: 512, MaxReplicas: 2, ScaleDownSeconds: 600,
		},
	}
}

func (s Settings) OwnerQuota(kind sohaapi.HostedAppKind, teams, roles []string) int32 {
	rules := s.EffectiveGovernance()
	defaultQuota := s.WithDefaults().MaxAppsPerOwner
	if kind == sohaapi.HostedAppKindDynamic {
		defaultQuota = rules.MaxDynamicAppsPerOwner
	}
	var limit int32
	for _, rule := range rules.QuotaOverrides {
		members := teams
		if rule.Scope == sohaapi.AppHostingQuotaOverrideScopeRole {
			members = roles
		}
		if !slices.Contains(members, rule.ID) {
			continue
		}
		quota := rule.MaxStaticApps
		if kind == sohaapi.HostedAppKindDynamic {
			quota = rule.MaxDynamicApps
		}
		if quota > 0 && (limit == 0 || quota < limit) {
			limit = quota
		}
	}
	if limit == 0 {
		return defaultQuota
	}
	return limit
}

func (s Settings) validateGovernance() error {
	if s.Governance == nil {
		return nil
	}
	rules := s.Governance
	if rules.MaxDynamicAppsPerOwner < 1 || rules.MaxDynamicAppsPerOwner > 1000 {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_dynamic_quota", "dynamic app quota must be between 1 and 1000", "动态应用额度必须在 1 到 1000 之间")
	}
	if !validRuntimeLimits(rules.RuntimeLimits) {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_runtime_limits", "application runtime limits are outside the supported range", "应用运行规格超出允许范围")
	}
	if rules.DataCapacityWarningMiB < 1 || rules.DataCapacityWarningMiB > 1048576 {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_data_capacity_threshold", "app data warning threshold must be between 1 and 1048576 MiB", "应用数据库告警阈值必须在 1 到 1048576 MiB 之间")
	}
	return validateOwnerQuotaOverrides(rules.QuotaOverrides)
}

func validRuntimeLimits(limits sohaapi.AppHostingRuntimeLimits) bool {
	return limits.CPUMilli >= 50 && limits.CPUMilli <= 4000 &&
		limits.MemoryMiB >= 64 && limits.MemoryMiB <= 4096 &&
		limits.MaxReplicas >= 1 && limits.MaxReplicas <= 10 &&
		limits.ScaleDownSeconds >= 60 && limits.ScaleDownSeconds <= 3600
}

func validateOwnerQuotaOverrides(rules []sohaapi.AppHostingQuotaOverride) error {
	if len(rules) > 100 {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "too_many_quota_overrides", "at most 100 quota overrides are supported", "最多支持 100 条额度覆盖")
	}
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		key := string(rule.Scope) + ":" + rule.ID
		if !rule.Scope.Valid() || !validQuotaOverride(rule) || seen[key] {
			return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_quota_override", "quota overrides require a unique team or role and at least one valid quota", "额度覆盖需要唯一的部门或角色及至少一项有效额度")
		}
		seen[key] = true
	}
	return nil
}

func validQuotaOverride(rule sohaapi.AppHostingQuotaOverride) bool {
	return rule.ID != "" && strings.TrimSpace(rule.ID) == rule.ID && len(rule.ID) <= 128 &&
		rule.MaxStaticApps >= 0 && rule.MaxStaticApps <= 1000 &&
		rule.MaxDynamicApps >= 0 && rule.MaxDynamicApps <= 1000 &&
		(rule.MaxStaticApps > 0 || rule.MaxDynamicApps > 0)
}
