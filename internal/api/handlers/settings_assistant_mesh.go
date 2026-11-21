package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

func (h *SettingsHandler) GetAssistantMeshSettings(c *gin.Context) {
	item, err := h.ai.GetAssistantMeshSettings(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) UpdateAssistantMeshSettings(c *gin.Context) {
	var input struct {
		DelegationLifetimeHours       *int      `json:"delegationLifetimeHours"`
		Enabled                       *bool     `json:"enabled"`
		OrganizationApproverUserIDs   *[]string `json:"organizationApproverUserIds"`
		DefaultVisibility             *string   `json:"defaultVisibility"`
		ExperienceCardLifetimeDays    *int      `json:"experienceCardLifetimeDays"`
		AllowPermanentExperienceCards *bool     `json:"allowPermanentExperienceCards"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&input) != nil || decoder.Decode(&extra) != io.EOF ||
		input.Enabled == nil || input.OrganizationApproverUserIDs == nil || input.DefaultVisibility == nil ||
		input.ExperienceCardLifetimeDays == nil || input.AllowPermanentExperienceCards == nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid Assistant Mesh settings payload")
		return
	}
	var delegationHours int
	if input.DelegationLifetimeHours != nil {
		delegationHours = *input.DelegationLifetimeHours
		if delegationHours < 1 || delegationHours > 72 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "delegation lifetime must be between 1 and 72 hours")
			return
		}
	}
	item, err := h.ai.UpdateAssistantMeshSettings(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domainsettings.AssistantMeshSettings{
		DelegationLifetimeHours: delegationHours,
		Enabled:                 *input.Enabled, OrganizationApproverUserIDs: *input.OrganizationApproverUserIDs,
		DefaultVisibility: *input.DefaultVisibility, ExperienceCardLifetimeDays: *input.ExperienceCardLifetimeDays,
		AllowPermanentExperienceCards: *input.AllowPermanentExperienceCards,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
