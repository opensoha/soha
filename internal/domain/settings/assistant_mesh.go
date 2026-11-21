package settings

const AssistantMeshSettingKey = "assistantMesh.organization"
const DefaultDelegationLifetimeHours = 4

type AssistantMeshSettings struct {
	DefaultProfileOpen            *bool    `json:"defaultProfileOpen,omitempty"`
	WorkPulseDefaultLifetimeHours int      `json:"workPulseDefaultLifetimeHours,omitempty"`
	WorkPulseMaxLifetimeDays      int      `json:"workPulseMaxLifetimeDays,omitempty"`
	ReceptionMinScore             *float64 `json:"receptionMinScore,omitempty"`
	InquiryPairLimitPerHour       int      `json:"inquiryPairLimitPerHour"`
	InquiryCallerLimitPerDay      int      `json:"inquiryCallerLimitPerDay"`
	InquiryMergeWindowHours       int      `json:"inquiryMergeWindowHours"`
	InquiryPendingLimit           int      `json:"inquiryPendingLimit"`
	InquiryTimeoutHours           int      `json:"inquiryTimeoutHours"`
	InquiryRetentionDays          int      `json:"inquiryRetentionDays"`
	DelegationLifetimeHours       int      `json:"delegationLifetimeHours"`
	Enabled                       bool     `json:"enabled"`
	OrganizationApproverUserIDs   []string `json:"organizationApproverUserIds"`
	DefaultVisibility             string   `json:"defaultVisibility"`
	ExperienceCardLifetimeDays    int      `json:"experienceCardLifetimeDays"`
	AllowPermanentExperienceCards bool     `json:"allowPermanentExperienceCards"`
}

func DefaultAssistantMeshSettings() AssistantMeshSettings {
	return AssistantMeshSettings{
		DefaultProfileOpen:            new(false),
		WorkPulseDefaultLifetimeHours: 24,
		WorkPulseMaxLifetimeDays:      14,
		ReceptionMinScore:             new(0.0),
		InquiryPairLimitPerHour:       5,
		InquiryCallerLimitPerDay:      30,
		InquiryMergeWindowHours:       24,
		InquiryPendingLimit:           20,
		InquiryTimeoutHours:           72,
		InquiryRetentionDays:          7,
		DelegationLifetimeHours:       DefaultDelegationLifetimeHours,
		OrganizationApproverUserIDs:   []string{},
		DefaultVisibility:             "private", ExperienceCardLifetimeDays: 90, AllowPermanentExperienceCards: true,
	}
}
