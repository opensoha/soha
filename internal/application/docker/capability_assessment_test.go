package docker

import (
	"context"
	"testing"
	"time"

	domaindocker "github.com/opensoha/soha/internal/domain/docker"
)

func TestCapabilityProjectAssessmentRequiresFreshPostDeploymentInventory(t *testing.T) {
	now := time.Now().UTC()
	finished := now.Add(-30 * time.Second)
	for _, test := range []struct {
		name, status, verdict string
		age                   time.Duration
		missing               bool
	}{
		{"fresh", "running", "satisfied", 5 * time.Second, false},
		{"stopped", "exited", "unsatisfied", 5 * time.Second, false},
		{"before-deploy", "running", "inconclusive", 40 * time.Second, false},
		{"stale", "running", "inconclusive", 3 * time.Minute, false},
		{"future", "running", "inconclusive", -time.Minute, false},
		{"missing", "running", "inconclusive", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newMemoryDockerRepo()
			repo.hosts["host"] = domaindocker.Host{ID: "host", Status: "online"}
			repo.projects["project"] = domaindocker.Project{ID: "project", HostID: "host"}
			repo.operations["operation"] = domaindocker.Operation{ID: "operation", HostID: "host", ProjectID: "project", OperationKind: OperationKindProjectDeploy, Status: OperationStatusCompleted, FinishedAt: &finished}
			seen := now.Add(-test.age)
			if !test.missing {
				repo.services["service"] = domaindocker.Service{ID: "service", Name: "api", HostID: "host", ProjectID: "project", Status: test.status, LastSeenAt: &seen}
			}
			service := New(repo, dockerTestPermissions(), &captureDockerOperations{})
			got, err := service.AssessProject(context.Background(), dockerTestPrincipal(), ProjectAssessmentInput{ProjectID: "project", AfterOperationID: "operation", ExpectedServices: []string{"api"}})
			if err != nil || string(got.Verdict) != test.verdict {
				t.Fatalf("assessment: %+v %v", got, err)
			}
		})
	}
}
