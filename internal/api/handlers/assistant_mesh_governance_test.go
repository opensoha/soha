package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAssistantMeshGovernanceRequestGuards(t *testing.T) {
	handler := &AssistantMeshHandler{}
	for _, test := range []struct {
		name, body string
		handle     gin.HandlerFunc
		noStore    bool
	}{
		{"null selection", `{"assetIds":null}`, handler.TransferInheritance, false},
		{"empty selection", `{"assetIds":[]}`, handler.TransferInheritance, false},
		{"empty retirement selection", `{"assetIds":[]}`, handler.RetireInheritance, false},
		{"wrong selection type", `{"assetIds":true}`, handler.TransferInheritance, false},
		{"malformed audit", `{"inquiryId":`, handler.ReadInquiryAudit, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			test.handle(ctx)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			if test.noStore && recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("inquiry audit responses must be non-cacheable")
			}
		})
	}
}
