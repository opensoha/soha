package settings

const AssistantMeshSettingKey = "assistantMesh.organization"
const DefaultDelegationLifetimeHours = 4

type AssistantMeshSettings struct {
	DelegationLifetimeHours       int      `json:"delegationLifetimeHours"`
	Enabled                       bool     `json:"enabled"`
	OrganizationApproverUserIDs   []string `json:"organizationApproverUserIds"`
	DefaultVisibility             string   `json:"defaultVisibility"`
	ExperienceCardLifetimeDays    int      `json:"experienceCardLifetimeDays"`
	AllowPermanentExperienceCards bool     `json:"allowPermanentExperienceCards"`
}

func DefaultAssistantMeshSettings() AssistantMeshSettings {
	return AssistantMeshSettings{
		DelegationLifetimeHours:     DefaultDelegationLifetimeHours,
		OrganizationApproverUserIDs: []string{},
		DefaultVisibility:           "private", ExperienceCardLifetimeDays: 90, AllowPermanentExperienceCards: true,
	}
}
