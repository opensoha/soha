package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
)

func TestDeliveryPlanManifestRevisionBinding(t *testing.T) {
	for _, field := range []string{"manifestRevision", "helmRevision"} {
		for _, test := range []struct {
			body     string
			revision int
			invalid  bool
		}{
			{body: `{}`},
			{body: `{"manifestRevision":2}`, revision: 2},
			{body: `{"manifestRevision":0}`, invalid: true},
			{body: `{"manifestRevision":-1}`, invalid: true},
			{body: `{"manifestRevision":"2"}`, invalid: true},
		} {
			test.body = strings.ReplaceAll(test.body, "manifestRevision", field)
			t.Run(test.body, func(t *testing.T) {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest("POST", "/delivery/plans", strings.NewReader(test.body))
				ctx.Request.Header.Set("Content-Type", "application/json")
				var request dto.DeliveryPlanRequest
				err := ctx.ShouldBindJSON(&request)
				if (err != nil) != test.invalid {
					t.Fatalf("binding error = %v", err)
				}
				input := deliveryPlanInputFromRequest(request)
				revision := input.ManifestRevision
				if field == "helmRevision" {
					revision = input.HelmRevision
				}
				if err == nil && revision != test.revision {
					t.Fatal("revision lost during mapping")
				}
			})
		}
	}
}
