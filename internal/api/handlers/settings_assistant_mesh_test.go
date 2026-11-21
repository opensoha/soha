package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAssistantMeshSettingsRequest(t *testing.T) {
	const valid = `{"enabled":false,"organizationApproverUserIds":[],"defaultVisibility":"private","experienceCardLifetimeDays":90,"allowPermanentExperienceCards":true}`
	handler := NewSettingsHandler(stubSettingsService{}, nil)
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"explicit false", valid, http.StatusOK},
		{"valid optional governance", strings.Replace(valid, "{", `{"inquiryRetentionDays":14,`, 1), http.StatusOK},
		{"explicit false and zero", strings.Replace(valid, "{", `{"defaultProfileOpen":false,"receptionMinScore":0,`, 1), http.StatusOK},
		{"null default profile", strings.Replace(valid, "{", `{"defaultProfileOpen":null,`, 1), http.StatusBadRequest},
		{"null reception score", strings.Replace(valid, "{", `{"receptionMinScore":null,`, 1), http.StatusBadRequest},
		{"fractional pulse hours", strings.Replace(valid, "{", `{"workPulseDefaultLifetimeHours":2.5,`, 1), http.StatusBadRequest},
		{"zero pulse maximum", strings.Replace(valid, "{", `{"workPulseMaxLifetimeDays":0,`, 1), http.StatusBadRequest},
		{"explicit null retention", strings.Replace(valid, "{", `{"inquiryRetentionDays":null,`, 1), http.StatusBadRequest},
		{"explicit zero limit", strings.Replace(valid, "{", `{"inquiryPendingLimit":0,`, 1), http.StatusBadRequest},
		{"missing enabled", strings.Replace(valid, `"enabled":false,`, "", 1), http.StatusBadRequest},
		{"null reviewers", strings.Replace(valid, "[]", "null", 1), http.StatusBadRequest},
		{"unknown field", strings.Replace(valid, "{", `{"approvalsEnabled":false,`, 1), http.StatusBadRequest},
		{"trailing object", valid + "{}", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPut, "/settings/ai/assistant-mesh", strings.NewReader(test.body))
			handler.UpdateAssistantMeshSettings(ctx)
			if recorder.Code != test.status {
				t.Fatalf("status = %d want %d: %s", recorder.Code, test.status, recorder.Body.String())
			}
		})
	}
}
