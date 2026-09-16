package providerportal

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
)

// The binary is built from soha-agent; no cross-repository internal imports are used.
//
//nolint:funlen // One Agent lifecycle verifies signature recovery, token rotation and control-plane loss in order.
func TestOutpostProtocolHTTPWithPostgres(t *testing.T) {
	binary := os.Getenv("SOHA_SSO_TEST_AGENT_BINARY")
	if binary == "" {
		t.Skip("set SOHA_SSO_TEST_AGENT_BINARY to the built Soha Agent")
	}
	f := newSSOProtocolFixture(t)
	f.publicURL = "https://soha.example.test"
	ctx := context.Background()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	ssoNoError(t, err)
	f.providers.SetOutpostSigningKey("protocol-test-key", privateKey)
	port := reserveSSOAgentPort(t)
	agentURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	renderer := os.Getenv("SOHA_SSO_TEST_PROXY_RENDERER")
	forwardAuthURL := agentURL + "/api/v1/outpost/forward-auth"
	if renderer != "" {
		forwardAuthURL = strings.Replace(forwardAuthURL, "127.0.0.1", "host.docker.internal", 1)
	}
	outpost, err := f.providers.CreateOutpost(ctx, f.principal, domainprovider.OutpostInput{Name: "Protocol test edge", Mode: "external", ForwardAuthURL: forwardAuthURL})
	ssoNoError(t, err)
	// Fixture owns every row it creates; production data is never queried or changed.
	t.Cleanup(func() { _ = f.providers.DeleteOutpost(ctx, f.principal, outpost.ID) })
	input := onboardingTestInput("proxy")
	input.Provider.Config = map[string]any{"mode": "forward_auth", "outpostId": outpost.ID, "externalHosts": []string{"app.example.test"}}
	application := f.onboard(t, input)
	sessionID := f.login(t, "outpost")
	session, err := f.providers.IssueProxySession(ctx, f.principal, domainidentity.AccessContext{TokenKind: "session_access", SessionID: sessionID})
	ssoNoError(t, err)
	agentToken := "test-agent-http-token-" + base64.RawURLEncoding.EncodeToString(publicKey)
	directory := t.TempDir()
	// #nosec G302 -- private directory needs owner traversal; it is not a file.
	ssoNoError(t, os.Chmod(directory, 0700))
	startAgent := func(trust []byte, controlToken string) func() {
		t.Helper()
		config := fmt.Sprintf("app:\n  env: development\nhttp:\n  addr: :%d\n  base_path: /api/v1\nauth:\n  bearer_token_file: %s\nlogger:\n  level: error\nkubernetes:\n  enabled: false\ncontrol_plane:\n  enabled: true\n  base_url: %s\n  bearer_token_file: %s\n  outpost:\n    enabled: true\n    agent_id: %s\n    protocol_version: v1\n    trust_key_id: protocol-test-key\n    trust_public_key: %s\n    poll_interval: 200ms\n    heartbeat_interval: 200ms\n", port, filepath.Join(directory, "agent-token"), f.server.URL, filepath.Join(directory, "control-token"), outpost.ID, base64.StdEncoding.EncodeToString(trust))
		return startSSOTestAgent(t, binary, directory, config, agentToken, controlToken)
	}
	check := func(token, cookie, mode, host string, status int) http.Header {
		return checkSSOOutpost(t, f.client, agentURL, token, cookie, mode, host, status)
	}
	badKey, _, err := ed25519.GenerateKey(rand.Reader)
	ssoNoError(t, err)
	stopBad := startAgent(badKey, outpost.Token)
	waitSSOCondition(t, "rejected configuration heartbeat", func() bool {
		view, err := f.providers.GetOutpost(ctx, f.principal, outpost.ID)
		return err == nil && view.LastHeartbeatAt != nil && view.AppliedConfigurationVersion == 0 && view.RuntimeStatus == "unavailable"
	})
	check(agentToken, session.Token, "", "app.example.test", http.StatusServiceUnavailable)
	stopBad()
	stopGood := startAgent(publicKey, outpost.Token)
	waitSSOCondition(t, "claimed and applied configuration", func() bool {
		view, err := f.providers.GetOutpost(ctx, f.principal, outpost.ID)
		return err == nil && view.RuntimeStatus == "available" && view.ConfigurationVersion == view.AppliedConfigurationVersion && view.RuntimeVersion != "" && view.ConfigurationExpiresAt != nil
	})
	headers := check(agentToken, session.Token, "", "app.example.test", http.StatusOK)
	ssoCheck(t, headers.Get("X-Soha-User-ID") == f.principal.UserID && headers.Get("X-Soha-Outpost-Token") == "", "identity/service token boundary failed")
	check("wrong-service-token", session.Token, "", "app.example.test", http.StatusUnauthorized)
	check("", session.Token, "", "app.example.test", http.StatusUnauthorized)
	check(agentToken, session.Token, "", "unregistered.example.test", http.StatusServiceUnavailable)
	input.Application.ProviderID = application.Application.ProviderID
	input.Application.Assignments = []domainportal.ApplicationAssignmentInput{{SubjectType: "user", SubjectID: f.principal.UserID, Effect: "deny"}}
	if _, err := f.applications.UpdateApplication(ctx, f.principal, application.Application.ID, input.Application); err != nil {
		t.Fatal(err)
	}
	check(agentToken, session.Token, "", "app.example.test", http.StatusForbidden)
	input.Application.Assignments[0].Effect = "allow"
	if _, err := f.applications.UpdateApplication(ctx, f.principal, application.Application.ID, input.Application); err != nil {
		t.Fatal(err)
	}
	headers = check(agentToken, "", "", "app.example.test", http.StatusFound)
	ssoCheck(t, strings.HasPrefix(headers.Get("Location"), f.publicURL+"/api/v1/provider/proxy/start?"), "remote login did not use canonical Core address")
	headers = check(agentToken, "", "?mode=nginx", "app.example.test", http.StatusUnauthorized)
	ssoCheck(t, headers.Get("X-Soha-Login-URL") != "", "NGINX response omitted login location")
	if renderer != "" {
		verifySSOProxyTemplates(t, f, renderer, application.Provider.ID, agentToken, session.Token, input.Application)
	}
	ssoNoError(t, f.users.RevokeSessionByID(ctx, sessionID))
	check(agentToken, session.Token, "", "app.example.test", http.StatusFound)
	rotated, err := f.providers.RotateOutpostToken(ctx, f.principal, outpost.ID)
	ssoNoError(t, err)
	check(agentToken, "", "", "app.example.test", http.StatusServiceUnavailable)
	stopGood()
	startAgent(publicKey, rotated.Token)
	waitSSOCondition(t, "token rotation recovery", func() bool {
		view, err := f.providers.GetOutpost(ctx, f.principal, outpost.ID)
		return err == nil && view.RuntimeStatus == "available"
	})
	f.server.Close()
	check(agentToken, "", "", "app.example.test", http.StatusServiceUnavailable)
}

func waitSSOCondition(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + description)
}

func startSSOTestAgent(t *testing.T, binary, directory, config, agentToken, controlToken string) func() {
	t.Helper()
	for name, value := range map[string]string{"agent.yaml": config, "agent-token": agentToken, "control-token": controlToken} {
		ssoNoError(t, os.WriteFile(filepath.Join(directory, name), []byte(value), 0600))
	}
	command := exec.Command(binary) // #nosec G204 -- test runner explicitly supplies a locally built Agent binary.
	logPath := filepath.Join(directory, "agent.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600) // #nosec G304 -- fixed file in t.TempDir owned by this test.
	ssoNoError(t, err)
	command.Stdout, command.Stderr = logFile, logFile
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "SOHA_AGENT_") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, "GIN_MODE=release", "SOHA_AGENT_CONFIG_FILE="+filepath.Join(directory, "agent.yaml"))
	ssoNoError(t, command.Start())
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = command.Process.Kill()
			_ = command.Wait()
			_ = logFile.Close()
			if t.Failed() {
				data, _ := os.ReadFile(logPath) // #nosec G304 -- same test-owned temporary log.
				t.Log(string(data))
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func checkSSOOutpost(t *testing.T, client *http.Client, agentURL, token, cookie, mode, host string, status int) http.Header {
	t.Helper()
	request, _ := http.NewRequest("GET", agentURL+"/api/v1/outpost/forward-auth"+mode, nil)
	request.Header.Set("X-Soha-Outpost-Token", token)
	request.Header.Set("X-Forwarded-Host", host)
	request.Header.Set("X-Forwarded-Uri", "/private?from=protocol")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Method", "GET")
	request.Header.Set("X-Soha-User-ID", "forged-user")
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: "soha_proxy_session", Value: cookie})
	}
	_, headers := ssoHTTP(t, client, request, status)
	return headers
}

func reserveSSOAgentPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	ssoNoError(t, err)
	address, ok := listener.Addr().(*net.TCPAddr)
	ssoCheck(t, ok, "listener must use TCP")
	port := address.Port
	_ = listener.Close()
	return port
}
