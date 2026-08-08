package cluster

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type staticAccessURL string

func (value staticAccessURL) AccessURL() string { return string(value) }

func TestAgentInstallationRendersReverseSessionManifestAndInvalidatesTicket(t *testing.T) {
	repo := &stubRepository{}
	service := newTestService(t, repo)
	service.SetAccessURLResolver(staticAccessURL("https://soha.example.com"))

	cluster, err := service.Register(context.Background(), domainidentity.Principal{}, domaincluster.RegisterInput{
		Name: "private-k3s", ConnectionMode: domaincluster.ConnectionModeAgent,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got := metadataString(repo.connection.Metadata, "transport"); got != agentReverseSessionTransport {
		t.Fatalf("transport = %q, want %q", got, agentReverseSessionTransport)
	}
	token := metadataString(repo.connection.Metadata, "token")
	if len(token) < 32 {
		t.Fatalf("generated Agent token is too short: %d", len(token))
	}

	installation, err := service.CreateAgentInstallation(context.Background(), domainidentity.Principal{}, cluster.ID)
	if err != nil {
		t.Fatalf("CreateAgentInstallation() error = %v", err)
	}
	if !strings.HasPrefix(installation.Command, "kubectl apply -f https://soha.example.com/api/v1/kubernetes/agent-installations/") {
		t.Fatalf("command = %q", installation.Command)
	}
	ticket := strings.TrimSuffix(strings.TrimPrefix(installation.ManifestURL, "https://soha.example.com/api/v1/kubernetes/agent-installations/"), "/manifest.yaml")
	manifest, err := service.RenderAgentInstallation(context.Background(), ticket)
	if err != nil {
		t.Fatalf("RenderAgentInstallation() error = %v", err)
	}
	text := string(manifest)
	for _, expected := range []string{"kind: Deployment", "enabled: true", "kubeconfig: \"\"", "base_url: https://soha.example.com", agentImage} {
		if !strings.Contains(text, expected) {
			t.Fatalf("manifest does not contain %q", expected)
		}
	}
	if strings.Contains(text, "kind: Service\n") || strings.Contains(text, "nodePort:") {
		t.Fatalf("reverse Agent manifest unexpectedly exposes a Kubernetes Service:\n%s", text)
	}
	if err := service.AuthenticateAgentSession(context.Background(), cluster.ID, "wrong-token"); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("AuthenticateAgentSession(wrong) error = %v", err)
	}
	if err := service.AuthenticateAgentSession(context.Background(), cluster.ID, token); err != nil {
		t.Fatalf("AuthenticateAgentSession() error = %v", err)
	}
	if _, err := service.RenderAgentInstallation(context.Background(), ticket); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("RenderAgentInstallation(after session) error = %v, want not found", err)
	}
}

func TestAgentInstallationRejectsExpiredTicket(t *testing.T) {
	repo := &stubRepository{}
	service := newTestService(t, repo)
	service.SetAccessURLResolver(staticAccessURL("http://soha.internal"))
	cluster, err := service.Register(context.Background(), domainidentity.Principal{}, domaincluster.RegisterInput{
		Name: "private-k3s", ConnectionMode: domaincluster.ConnectionModeAgent,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	installation, err := service.CreateAgentInstallation(context.Background(), domainidentity.Principal{}, cluster.ID)
	if err != nil {
		t.Fatalf("CreateAgentInstallation() error = %v", err)
	}
	repo.connection.Metadata["install_ticket_expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
	ticket := strings.TrimSuffix(strings.TrimPrefix(installation.ManifestURL, "http://soha.internal/api/v1/kubernetes/agent-installations/"), "/manifest.yaml")
	if _, err := service.RenderAgentInstallation(context.Background(), ticket); !errors.Is(err, ErrAgentInstallationExpired) {
		t.Fatalf("RenderAgentInstallation() error = %v, want expired", err)
	}
}

func TestAgentInstallationMigratesLegacyHTTPConnection(t *testing.T) {
	repo := &stubRepository{connection: domaincluster.Connection{
		Summary: domaincluster.Summary{
			ID: "legacy-agent", Name: "legacy-agent", ConnectionMode: domaincluster.ConnectionModeAgent,
		},
		CredentialType: "bearer",
		SourceType:     "agent",
		SourceRef:      "http://10.0.0.4:31666",
		Metadata: map[string]any{
			"transport": "http",
			"endpoint":  "http://10.0.0.4:31666",
			"token":     "legacy-token",
		},
	}}
	service := newTestService(t, repo)
	service.SetAccessURLResolver(staticAccessURL("http://soha.example.com"))

	installation, err := service.CreateAgentInstallation(context.Background(), domainidentity.Principal{}, "legacy-agent")
	if err != nil {
		t.Fatalf("CreateAgentInstallation() error = %v", err)
	}
	if installation.Command == "" {
		t.Fatal("installation command is empty")
	}
	if got := metadataString(repo.connection.Metadata, "transport"); got != agentReverseSessionTransport {
		t.Fatalf("transport = %q, want %q", got, agentReverseSessionTransport)
	}
	if got := metadataString(repo.connection.Metadata, "endpoint"); got != "" {
		t.Fatalf("endpoint = %q, want empty", got)
	}
	if got := metadataString(repo.connection.Metadata, "token"); got == "" || got == "legacy-token" {
		t.Fatalf("token was not rotated during migration")
	}
	if repo.connection.SourceRef != agentReverseSessionTransport {
		t.Fatalf("sourceRef = %q, want %q", repo.connection.SourceRef, agentReverseSessionTransport)
	}
}
