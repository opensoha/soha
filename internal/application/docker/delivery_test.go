package docker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

func TestDeliveryComposePinsArtifactsAndRejectsUnfrozenInputs(t *testing.T) {
	image := "registry.example/api@sha256:" + strings.Repeat("a", 64)
	content := "services:\n  api:\n    image: mutable:latest\n  cache:\n    image: cache@sha256:" + strings.Repeat("b", 64) + "\n"
	compose, images, err := mapDeliveryComposeImages(content, "stable", map[string]string{"api": "api-container"}, map[string]string{"api-container": image})
	if err != nil || images["api"] != image || strings.Contains(compose, "mutable") || !strings.Contains(compose, `"name":"stable"`) {
		t.Fatalf("render = %s %v %v", compose, images, err)
	}
	for _, envFile := range []string{".env", "[.env]"} {
		if _, _, err := mapDeliveryComposeImages("services:\n  api:\n    image: old\n    env_file: "+envFile+"\n", "stable", map[string]string{"api": "api-container"}, map[string]string{"api-container": image}); err != nil {
			t.Fatalf("bundled frozen environment refused: %v", err)
		}
	}
	for _, invalid := range []string{
		"services:\n  api:\n    image: old\n    build: .\n",
		"services:\n  api:\n    image: old\n    env_file: .env.other\n",
		"include: other.yaml\nservices:\n  api:\n    image: old\n",
		"services:\n  api:\n    image: old\n  cache:\n    image: cache:latest\n",
		"services:\n  api:\n    image: old\n    environment:\n      MODE: ${MODE}\n",
		"services:\n  api:\n    image: old\n    volumes: [./data:/data]\n",
		"services:\n  api:\n    image: old\n    environment: [FROM_HOST]\n",
		"services:\n  api:\n    image: old\n    environment:\n      FROM_HOST: null\n",
		"configs:\n  config:\n    file: /host/config\nservices:\n  api:\n    image: old\n",
	} {
		if _, _, err := mapDeliveryComposeImages(invalid, "stable", map[string]string{"api": "api-container"}, map[string]string{"api-container": image}); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("accepted unfrozen input: %s (%v)", invalid, err)
		}
	}
}

func TestDeliveryDockerQueueFreezesPayloadAndRecoversOneOperation(t *testing.T) {
	repo := newMemoryDockerRepo()
	now := time.Now().UTC()
	repo.hosts["host"] = domaindocker.Host{ID: "host", Status: "online", AgentID: "agent", DockerVersion: "28", ComposeVersion: "2", LastHeartbeatAt: &now}
	repo.projects["project"] = domaindocker.Project{ID: "project", HostID: "host", Slug: "stable", ComposeContent: "services:\n  api:\n    image: old:latest\n", EnvContent: "TOKEN=private-value"}
	key, _ := keyring.NewKey("test", "docker-delivery-test-encryption-key", now.Add(-time.Hour), nil)
	keys, _ := keyring.New(key, nil)
	s := New(repo, dockerTestPermissions(), nil, WithCredentialEncryptionKeys(keys))
	p := dockerTestPrincipal()
	frozen, err := s.FreezeDeliveryProject(context.Background(), p, "host", "project")
	if err != nil {
		t.Fatal(err)
	}
	image := "registry.example/api@sha256:" + strings.Repeat("a", 64)
	prepared, err := s.PrepareDeliveryProject(context.Background(), p, frozen.Ciphertext, map[string]string{"api": "main"}, map[string]string{"main": image})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := sohaapi.DockerDeliverySnapshot{DeliveryPlanID: "plan", TargetID: "target", ApplicationID: "app", ApplicationEnvironmentID: "env", HostID: "host", ProjectID: "project", ProjectDigest: prepared.ProjectDigest, RenderedDigest: prepared.RenderedDigest}
	run := domainworkflow.Run{ID: "run", Scope: domainworkflow.ScopeDeliveryBatch}
	node := domainworkflow.NodeRun{NodeID: "plan-node", TargetID: "target", Stage: "plan"}
	ctx := domainworkflow.WithNodeExecution(context.Background(), run, node)
	first, err := s.QueueDeliveryProject(ctx, p, snapshot, prepared.Ciphertext, "validate")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.QueueDeliveryProject(ctx, p, snapshot, prepared.Ciphertext, "validate")
	if err != nil || first.ID != again.ID || len(repo.operations) != 1 {
		t.Fatalf("queue replay: %v", err)
	}
	encoded, _ := json.Marshal(repo.operations[first.ID].Payload)
	if strings.Contains(string(encoded), "private-value") || strings.Contains(string(encoded), "composeContent") {
		t.Fatal("queued plaintext configuration")
	}
	hydrated, err := s.hydrateDeliveryOperation(first)
	if err != nil || !strings.Contains(stringValue(hydrated.Payload, "composeContent"), image) || hydrated.Payload["envContent"] != "TOKEN=private-value" {
		t.Fatalf("hydration failed: %v", err)
	}
	before := repo.projects["project"]
	s.applyCallbackRuntimeState(ctx, first, "completed", map[string]any{})
	if after := repo.projects["project"]; after.LastDeployedAt != nil || after.Status != before.Status || after.ComposeContent != before.ComposeContent {
		t.Fatal("preflight changed deployed state")
	}
	changed := repo.projects["project"]
	changed.EnvContent = "TOKEN=changed"
	repo.projects["project"] = changed
	if err := s.ValidateDeliveryProject(ctx, p, snapshot, prepared.Ciphertext); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("configuration drift accepted: %v", err)
	}
	if _, err := s.CancelDeliveryProject(ctx, first.ID, "another-plan", "stop"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("foreign cancellation: %v", err)
	}
	stopped, err := s.CancelDeliveryProject(ctx, first.ID, "plan", "stop")
	if err != nil || stopped.Status != "canceled" || stopped.Result["cancellationAcknowledged"] != true {
		t.Fatalf("unclaimed cancellation: %+v %v", stopped, err)
	}
}
