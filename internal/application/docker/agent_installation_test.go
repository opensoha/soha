package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type dockerInstallAccessURL string

func (value dockerInstallAccessURL) AccessURL() string { return string(value) }

func TestHostAgentInstallationUsesOneTimeHostBoundCredentials(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Name: "runtime-1", Status: "pending"}
	legacyToken := "legacy-runner-token-32-characters-minimum"
	service := newDockerInstallTestService(t, repo, legacyToken)

	installation, ticket := createHostAgentInstallation(t, service, repo)
	script := downloadHostAgentInstaller(t, service, ticket)
	assertHostAgentInstaller(t, script, legacyToken)

	enrollment := decodeDockerEnrollmentRequest(t, script)
	credentials, err := service.ExchangeHostAgentEnrollment(context.Background(), installation.OperationID, enrollment)
	if err != nil {
		t.Fatalf("ExchangeHostAgentEnrollment() error = %v", err)
	}
	assertHostAgentCredentials(t, service, repo, installation, credentials)
	if _, err := service.ExchangeHostAgentEnrollment(context.Background(), installation.OperationID, enrollment); !errors.Is(err, ErrHostAgentEnrollmentConsumed) {
		t.Fatalf("replayed enrollment error = %v, want consumed", err)
	}
	if _, err := service.RenderHostAgentInstallation(context.Background(), ticket+"-invalid"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("invalid ticket error = %v, want not found", err)
	}
}

func createHostAgentInstallation(t *testing.T, service *Service, repo *memoryDockerRepo) (domaindocker.HostAgentInstallation, string) {
	t.Helper()
	installation, err := service.CreateHostAgentInstallation(context.Background(), dockerTestPrincipal(), "host-1")
	if err != nil {
		t.Fatalf("CreateHostAgentInstallation() error = %v", err)
	}
	operation := repo.operations[installation.OperationID]
	state := repo.installations[installation.OperationID]
	if operation.OperationKind != OperationKindHostSync || operation.HostID != "host-1" || len(operation.Payload) != 0 {
		t.Fatalf("installation operation = %#v", operation)
	}
	if state.DownloadTokenHash == "" || strings.Contains(installation.ScriptURL, state.DownloadTokenHash) {
		t.Fatalf("download ticket was not stored as a non-reversible hash")
	}

	scriptURL, err := url.Parse(installation.ScriptURL)
	if err != nil {
		t.Fatalf("parse script URL: %v", err)
	}
	return installation, path.Base(path.Dir(scriptURL.Path))
}

func downloadHostAgentInstaller(t *testing.T, service *Service, ticket string) []byte {
	t.Helper()
	type renderResult struct {
		script []byte
		err    error
	}
	results := make(chan renderResult, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			script, renderErr := service.RenderHostAgentInstallation(context.Background(), ticket)
			results <- renderResult{script: script, err: renderErr}
		}()
	}
	wg.Wait()
	close(results)

	var script []byte
	successes := 0
	expired := 0
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			script = result.script
		case errors.Is(result.err, ErrHostAgentInstallationExpired):
			expired++
		default:
			t.Fatalf("RenderHostAgentInstallation() error = %v", result.err)
		}
	}
	if successes != 1 || expired != 1 {
		t.Fatalf("concurrent installer downloads = %d success, %d expired", successes, expired)
	}
	return script
}

func assertHostAgentInstaller(t *testing.T, script []byte, legacyToken string) {
	t.Helper()
	if strings.Contains(string(script), legacyToken) || !strings.Contains(string(script), "systemctl enable --now soha-agent") {
		t.Fatalf("rendered installer leaked the legacy token or missed service start")
	}
	config := decodeDockerAgentConfig(t, script)
	if !strings.Contains(config, "bearer_token_file: "+hostAgentAuthPath) ||
		!strings.Contains(config, "bearer_token_file: "+hostRuntimeAuthPath) ||
		!strings.Contains(config, "provider_kinds: []") || strings.Contains(config, "bearer_token:") {
		t.Fatalf("installer config does not use scoped token files: %s", config)
	}
}

func assertHostAgentCredentials(t *testing.T, service *Service, repo *memoryDockerRepo, installation domaindocker.HostAgentInstallation, credentials sohaapi.DockerHostAgentCredentials) {
	t.Helper()
	if credentials.HostID != "host-1" || credentials.OperationID != installation.OperationID || credentials.AgentID != "host-1" {
		t.Fatalf("credentials identity = %#v", credentials)
	}
	if len(credentials.AgentBearerToken) < 32 || len(credentials.RuntimeBearerToken) < 32 || credentials.AgentBearerToken == credentials.RuntimeBearerToken {
		t.Fatalf("credentials are missing or reused")
	}
	stored := repo.installations[installation.OperationID]
	if stored.RuntimeTokenHash == credentials.RuntimeBearerToken || strings.Contains(stored.AgentTokenCiphertext, credentials.AgentBearerToken) {
		t.Fatalf("stored credentials contain plaintext secrets")
	}
	authorization, err := service.AuthenticateHostAgent(context.Background(), credentials.RuntimeBearerToken)
	if err != nil || authorization.HostID != "host-1" || authorization.AgentID != "host-1" {
		t.Fatalf("AuthenticateHostAgent() = %#v, %v", authorization, err)
	}
	agentToken, err := service.hostAgentBearerToken(context.Background(), "host-1")
	if err != nil || agentToken != credentials.AgentBearerToken {
		t.Fatalf("hostAgentBearerToken() returned the wrong Agent-local credential")
	}
}

func TestHostAgentReinstallRevokesPreviousRuntimeCredential(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Name: "runtime-1", Status: "online"}
	service := newDockerInstallTestService(t, repo, "legacy-runner-token-32-characters-minimum")

	first := enrollDockerInstall(t, service, repo, "host-1")
	second := enrollDockerInstall(t, service, repo, "host-1")
	if _, err := service.AuthenticateHostAgent(context.Background(), first.RuntimeBearerToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("previous runtime credential error = %v, want unauthorized", err)
	}
	if authorization, err := service.AuthenticateHostAgent(context.Background(), second.RuntimeBearerToken); err != nil || authorization.HostID != "host-1" {
		t.Fatalf("replacement runtime credential authorization = %#v, %v", authorization, err)
	}
}

func newDockerInstallTestService(t *testing.T, repo *memoryDockerRepo, legacyToken string) *Service {
	t.Helper()
	permissions := appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{
		"admin": {appaccess.ManagedActionPermission(appaccess.PermDockerHostsManage, "update")},
	}})
	key, err := keyring.NewKey("docker-install-test", "docker-install-test-encryption-key", time.Now().UTC().Add(-time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return New(repo, permissions, nil,
		WithAccessURLResolver(dockerInstallAccessURL("https://soha.example.com")),
		WithRuntimeBearerToken(legacyToken),
		WithCredentialEncryptionKeys(keys),
	)
}

func enrollDockerInstall(t *testing.T, service *Service, repo *memoryDockerRepo, hostID string) sohaapi.DockerHostAgentCredentials {
	t.Helper()
	installation, err := service.CreateHostAgentInstallation(context.Background(), dockerTestPrincipal(), hostID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(installation.ScriptURL)
	if err != nil {
		t.Fatal(err)
	}
	script, err := service.RenderHostAgentInstallation(context.Background(), path.Base(path.Dir(parsed.Path)))
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := service.ExchangeHostAgentEnrollment(context.Background(), installation.OperationID, decodeDockerEnrollmentRequest(t, script))
	if err != nil {
		t.Fatal(err)
	}
	if repo.installations[installation.OperationID].RevokedAt != nil {
		t.Fatal("new installation was unexpectedly revoked")
	}
	return credentials
}

func decodeDockerEnrollmentRequest(t *testing.T, script []byte) sohaapi.DockerHostAgentEnrollmentRequest {
	t.Helper()
	match := regexp.MustCompile(`printf '%s' '([^']+)' \| base64 -d > "\$work_dir/enrollment\.json"`).FindSubmatch(script)
	if len(match) != 2 {
		t.Fatalf("installer did not contain an enrollment request")
	}
	raw, err := base64.StdEncoding.DecodeString(string(match[1]))
	if err != nil {
		t.Fatalf("decode enrollment request: %v", err)
	}
	var request sohaapi.DockerHostAgentEnrollmentRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatalf("unmarshal enrollment request: %v", err)
	}
	return request
}

func decodeDockerAgentConfig(t *testing.T, script []byte) string {
	t.Helper()
	match := regexp.MustCompile(`printf '%s' '([^']+)' \| base64 -d \|`).FindSubmatch(script)
	if len(match) != 2 {
		t.Fatalf("installer did not contain the Agent config")
	}
	raw, err := base64.StdEncoding.DecodeString(string(match[1]))
	if err != nil {
		t.Fatalf("decode Agent config: %v", err)
	}
	return string(raw)
}
