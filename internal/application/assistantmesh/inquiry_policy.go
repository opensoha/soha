package assistantmesh

import (
	"context"
	"time"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	settings "github.com/opensoha/soha/internal/domain/settings"
)

type InquiryPolicy struct {
	Limits  domain.InquiryLimits
	Timeout time.Duration
}

func (s *Service) InquiryPolicy(ctx context.Context) (InquiryPolicy, error) {
	value, err := s.settings(ctx)
	if err != nil {
		return InquiryPolicy{}, err
	}
	defaults := settings.DefaultAssistantMeshSettings()
	return InquiryPolicy{
		Limits: domain.InquiryLimits{
			PairPerHour:  configuredValue(value.InquiryPairLimitPerHour, defaults.InquiryPairLimitPerHour),
			CallerPerDay: configuredValue(value.InquiryCallerLimitPerDay, defaults.InquiryCallerLimitPerDay),
			OwnerPending: configuredValue(value.InquiryPendingLimit, defaults.InquiryPendingLimit),
			MergeWindow:  time.Duration(configuredValue(value.InquiryMergeWindowHours, defaults.InquiryMergeWindowHours)) * time.Hour,
			Retention:    time.Duration(configuredValue(value.InquiryRetentionDays, defaults.InquiryRetentionDays)) * 24 * time.Hour,
		},
		Timeout: time.Duration(configuredValue(value.InquiryTimeoutHours, defaults.InquiryTimeoutHours)) * time.Hour,
	}, nil
}

func configuredValue(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
