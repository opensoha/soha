package handlers

import (
	"github.com/gin-gonic/gin"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManifestClaimHeaderRequiresCurrentPrivateConfirmation(t *testing.T) {
	for _, mode := range []string{"confirmed", "missing", "payload", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			provider := dd.ManifestAdmissionProviderPrefix + "manifest_agent_v3.one"
			task := dd.ExecutionTask{ID: "claimed", TaskKind: "manifest_preflight", ProviderKind: provider}
			if mode == "confirmed" {
				task.ManifestAdmissionValidated = true
			}
			if mode == "payload" {
				task.Payload = map[string]any{"manifestAdmissionValidated": true, "admissionVersion": "1"}
			}
			if mode == "legacy" {
				provider = "manifest_agent_v3.one"
				task.ProviderKind = provider
			}
			service := &stubDeliveryRunnerService{item: task}
			h := NewDeliveryHandlerWithServices(DeliveryServices{Runner: service, Agents: helmClaimAuthenticator{}}, legacyRunnerKeyring("runner-token"))
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/claim", strings.NewReader(`{"agentId":"one","providerKinds":["`+provider+`"]}`))
			c.Request.Header.Set("Authorization", "Bearer cluster-token")
			c.Request.Header.Set("Content-Type", "application/json")
			h.ClaimExecutionTask(c)
			if (w.Header().Get(dd.ManifestAdmissionHeader) == "1") != (mode == "confirmed") {
				t.Fatal("admission header came from unconfirmed state")
			}
			accepted := mode == "confirmed" || mode == "legacy"
			if (w.Code == http.StatusAccepted) != accepted {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
