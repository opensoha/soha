package settings

const AssistantMeshSettingKey = "assistantMesh.organization"

type AssistantMeshSettings struct {
	Enabled                       bool     `json:"enabled"`
	OrganizationApproverUserIDs   []string `json:"organizationApproverUserIds"`
	DefaultVisibility             string   `json:"defaultVisibility"`
	ExperienceCardLifetimeDays    int      `json:"experienceCardLifetimeDays"`
	AllowPermanentExperienceCards bool     `json:"allowPermanentExperienceCards"`
}

func DefaultAssistantMeshSettings() AssistantMeshSettings {
	return AssistantMeshSettings{
		OrganizationApproverUserIDs: []string{},
		DefaultVisibility:           "private", ExperienceCardLifetimeDays: 90, AllowPermanentExperienceCards: true,
	}
}
