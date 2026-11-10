package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
)

type dockerDeployInputService struct {
	DockerProjectService
	DockerPlanningService
	calls int
	id    string
	input domaindocker.ProjectDeployInput
}

func (s *dockerDeployInputService) DeployProject(_ context.Context, _ domainidentity.Principal, id string, input domaindocker.ProjectDeployInput) (domaindocker.Operation, error) {
	s.calls++
	s.id, s.input = id, input
	return domaindocker.Operation{ID: "operation-1", Status: "queued"}, nil
}

func (s *dockerDeployInputService) PlanProjectDeploy(_ context.Context, _ domainidentity.Principal, id string, input domaindocker.ProjectDeployInput) (domainoperation.Plan, error) {
	s.calls++
	s.id, s.input = id, input
	return domainoperation.Plan{Ready: true}, nil
}

func TestDockerProjectDeployInputBoundary(t *testing.T) {
	for _, plan := range []bool{false, true} {
		for _, test := range []struct {
			name, body, action string
			status             int
		}{
			{"legacy empty body", "", "", 200},
			{"default object", `{}`, "", 200},
			{"explicit action", `{"action":"stop"}`, "stop", 200},
			{"partial action", `{"action":"destroy",`, "", 400},
			{"malformed", `{`, "", 400},
			{"array", `[]`, "", 400},
			{"null", `null`, "", 400},
			{"wrong type", `{"action":42}`, "", 400},
			{"null action", `{"action":null}`, "", 400},
			{"empty action", `{"action":""}`, "", 400},
			{"unknown action", `{"action":"erase"}`, "", 400},
			{"uppercase property", `{"ACTION":"destroy"}`, "", 400},
			{"uppercase invalid action", `{"ACTION":"erase"}`, "", 400},
			{"case alias", `{"action":"stop","Action":"destroy"}`, "", 400},
			{"unknown field", `{"action":"stop","force":true}`, "", 400},
			{"second object", `{"action":"stop"}{}`, "", 400},
			{"invalid UTF8", "{\"action\":\"st\xffop\"}", "", 400},
			{"oversized", `{"action":"` + strings.Repeat("x", 4097) + `"}`, "", 413},
		} {
			name := "execute/" + test.name
			if plan {
				name = "plan/" + test.name
			}
			t.Run(name, func(t *testing.T) {
				service := &dockerDeployInputService{}
				handler := &DockerHandler{projects: service, planning: service}
				response := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(response)
				ctx.Params = gin.Params{{Key: "id", Value: "original-project"}}
				ctx.Request = httptest.NewRequest(http.MethodPost, "/deploy", strings.NewReader(test.body))
				ctx.Request.Header.Set("Content-Type", "application/json")
				ctx.Request.Header.Set("Idempotency-Key", "original-action-key")
				if plan {
					handler.PlanProjectDeploy(ctx)
				} else {
					handler.DeployProject(ctx)
				}
				want := test.status
				if want == 200 && !plan {
					want = 202
				}
				if response.Code != want {
					t.Fatalf("status=%d want=%d", response.Code, want)
				}
				if test.status != 200 {
					if service.calls != 0 {
						t.Fatal("invalid request reached planning or execution")
					}
					return
				}
				if service.calls != 1 || service.id != "original-project" || service.input.Action != test.action {
					t.Fatal("valid original action/target changed")
				}
				if !plan && service.input.IdempotencyKey != "original-action-key" {
					t.Fatal("original execution key lost")
				}
			})
		}
	}
}
