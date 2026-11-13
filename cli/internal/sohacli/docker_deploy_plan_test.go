package sohacli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func dockerDeployTestPlan() sohaapi.OperationalPlan {
	return sohaapi.OperationalPlan{Ready: true, Capability: "docker.projects.deploy.trigger", Target: "project-1", DockerProjectDeploy: &sohaapi.DockerProjectDeployPreconditions{
		HostID:  "host-1",
		Host:    sohaapi.DockerConfigurationPreconditions{ExpectedUID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), ExpectedConfigurationRevision: uuid.MustParse("22222222-2222-4222-8222-222222222222")},
		Project: sohaapi.DockerConfigurationPreconditions{ExpectedUID: uuid.MustParse("33333333-3333-4333-8333-333333333333"), ExpectedConfigurationRevision: uuid.MustParse("44444444-4444-4444-8444-444444444444")},
	}}
}

func TestDockerDeployRefusesUnboundPlanAndNeverReplansConflict(t *testing.T) {
	for _, scenario := range []string{"missing", "wrong-target", "wrong-capability", "zero-identity", "conflict"} {
		t.Run(scenario, func(t *testing.T) {
			plans, writes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/docker/projects/project-1/deploy/plan" {
					plans++
					plan := dockerDeployTestPlan()
					switch scenario {
					case "missing":
						plan.DockerProjectDeploy = nil
					case "wrong-target":
						plan.Target = "another-project"
					case "wrong-capability":
						plan.Capability = "another-capability"
					case "zero-identity":
						plan.DockerProjectDeploy.Host.ExpectedUID = uuid.Nil
					}
					_ = json.NewEncoder(w).Encode(sohaapi.OperationalPlanEnvelope{Data: plan})
					return
				}
				writes++
				w.WriteHeader(http.StatusConflict)
				_, _ = fmt.Fprint(w, `{"error":{"code":"conflict","message":"reviewed configuration changed"}}`)
			}))
			defer server.Close()
			code, _, _ := runDockerTest(t, server.URL, "", "projects", "deploy", "project-1", "--idempotency-key", "original-1", "--yes")
			wantWrites := 0
			if scenario == "conflict" {
				wantWrites = 1
			}
			if code == 0 || plans != 1 || writes != wantWrites {
				t.Fatalf("code=%d plans=%d writes=%d", code, plans, writes)
			}
		})
	}
}

func TestTUIDockerDeployFreezesReviewedBinding(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(fmt.Sprint(bound), func(t *testing.T) {
			writes := 0
			preview := dockerDeployTestPlan()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/docker/projects/project-1/deploy/plan" {
					plan := preview
					if !bound {
						plan.DockerProjectDeploy = nil
					}
					_ = json.NewEncoder(w).Encode(sohaapi.OperationalPlanEnvelope{Data: plan})
					return
				}
				writes++
				var body sohaapi.DockerProjectDeployInput
				if r.Method != "POST" || r.URL.Path != "/api/v1/docker/projects/project-1/deploy" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Preconditions == nil || *body.Preconditions != *preview.DockerProjectDeploy || body.Action != "restart" {
					t.Error("TUI did not submit the exact reviewed target/input")
				}
				_, _ = fmt.Fprint(w, `{"data":{"id":"original-operation"}}`)
			}))
			defer server.Close()
			api := tuiAPI{client: APIClient{ServerURL: server.URL}}
			row := tuiRow{id: "project-1"}
			plan, err := api.prepareWrite(t.Context(), tuiScope{tab: 1, section: "projects"}, &row, `{"action":"deploy","intent":"restart"}`)
			if !bound {
				if err == nil || plan != nil || writes != 0 {
					t.Fatal("TUI accepted an unbound plan")
				}
				return
			}
			if err != nil || writes != 0 {
				t.Fatalf("preview: %v", err)
			}
			row.id = "another-project"
			if _, err := plan.submit(t.Context()); err != nil || writes != 1 {
				t.Fatalf("submit: %v writes=%d", err, writes)
			}
		})
	}
}
