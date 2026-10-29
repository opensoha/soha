package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

type admissionPreviewHandlerFixture struct {
	NetworkAccessService
	called bool
	actor  identity.Principal
	input  appnetworkaccess.AdmissionPreviewInput
}

func (s *admissionPreviewHandlerFixture) PreviewAdmission(_ context.Context, actor identity.Principal, input appnetworkaccess.AdmissionPreviewInput) (domain.PolicyPreview, error) {
	s.called, s.actor, s.input = true, actor, input
	return domain.PolicyPreview{Decision: domain.DecisionDeny}, nil
}

func TestAdmissionPreviewRejectsSubjectOverridesAndUnboundedJSON(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"target IDs", `{"deviceId":"device-1","nasBindingId":"binding-1"}`, true},
		{"subject override", `{"deviceId":"device-1","nasBindingId":"binding-1","subjectId":"foreign"}`, false},
		{"principal override", `{"deviceId":"device-1","nasBindingId":"binding-1","subjectUserId":"foreign"}`, false},
		{"trailing JSON", `{"deviceId":"device-1","nasBindingId":"binding-1"}{}`, false},
		{"null", `null`, false},
		{"oversized", `{"deviceId":"` + strings.Repeat("x", 4096) + `","nasBindingId":"binding-1"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &admissionPreviewHandlerFixture{}
			handler := NewNetworkAccessHandler(fixture, nil, nil, nil)
			out := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(out)
			c.Request = httptest.NewRequest(http.MethodPost, "/network-access/admission/preview", strings.NewReader(tc.body))
			c.Set("principal", identity.Principal{UserID: "authenticated-admin"})
			handler.PreviewAdmission(c)
			if fixture.called != tc.valid {
				t.Fatalf("service called=%t status=%d", fixture.called, out.Code)
			}
			if tc.valid {
				if out.Code != http.StatusOK || fixture.actor.UserID != "authenticated-admin" || fixture.input.DeviceID != "device-1" || fixture.input.NASBindingID != "binding-1" {
					t.Fatalf("preview lost its authenticated actor or target: %+v status=%d", fixture, out.Code)
				}
			} else if out.Code != http.StatusBadRequest {
				t.Fatalf("malformed payload status=%d", out.Code)
			}
		})
	}
}
