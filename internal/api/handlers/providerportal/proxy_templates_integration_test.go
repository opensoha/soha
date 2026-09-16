package providerportal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"go.yaml.in/yaml/v3"
)

// Uses Web's actual templates with the real Core and Agent already running.
// Kubernetes admission and product-specific UI merging remain deployment checks.
func verifySSOProxyTemplates(t *testing.T, f *ssoProtocolFixture, renderer, providerID, agentToken, sessionToken string, applicationInput domainportal.ApplicationInput) {
	t.Helper()
	type businessResponse struct {
		Headers http.Header
		Body    string
		Method  string
	}
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = json.NewEncoder(w).Encode(businessResponse{Headers: r.Header, Body: string(body), Method: r.Method})
	}))
	listener, err := net.Listen("tcp", "0.0.0.0:0") // #nosec G102 -- ephemeral test upstream must be reachable from Docker containers.
	ssoNoError(t, err)
	_ = upstream.Listener.Close()
	upstream.Listener = listener
	upstream.Start()
	defer upstream.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	ssoCheck(t, ok, "upstream listener must use TCP")
	upstreamURL := fmt.Sprintf("http://host.docker.internal:%d", address.Port)
	templates, applicationID := renderSSOProxyTemplates(t, f, renderer, providerID, upstreamURL)
	verifySSOProxyManifests(t, templates)
	for _, target := range []string{"nginx-standalone", "nginx-proxy-manager", "traefik-standalone", "caddy-standalone"} {
		t.Run(target, func(t *testing.T) {
			content := strings.ReplaceAll(templates[target], "REPLACE_WITH_AGENT_HTTP_TOKEN", agentToken)
			directory := t.TempDir()
			configPath := filepath.Join(directory, "proxy.conf")
			var image, mount string
			var args []string
			switch target {
			case "nginx-standalone", "nginx-proxy-manager":
				if target == "nginx-proxy-manager" {
					content = "server { listen 80; server_name app.example.test;\n" + content + "\n}"
				}
				content = "events {}\nhttp { access_log off;\n" + content + "\n}\n"
				image, mount = "nginx:1.29-alpine", "/etc/nginx/nginx.conf"
			case "traefik-standalone":
				image, mount = "traefik:v3.5.0", "/etc/traefik/dynamic.yaml"
				args = []string{"--entrypoints.web.address=:80", "--providers.file.filename=" + mount, "--log.level=ERROR"}
			case "caddy-standalone":
				// TLS termination is deployment-specific; this fixture uses an HTTP listener.
				content = "{\n admin off\n}\n" + strings.Replace(content, "app.example.test {", "http://app.example.test {", 1)
				image, mount = "caddy:2.10.2-alpine", "/etc/caddy/Caddyfile"
			}
			ssoNoError(t, os.WriteFile(configPath, []byte(content), 0600))
			if strings.HasPrefix(target, "nginx-") {
				ssoDocker(t, "run", "--rm", "--add-host", "host.docker.internal:host-gateway", "-v", configPath+":"+mount+":ro", image, "nginx", "-t")
			}
			if target == "caddy-standalone" {
				ssoDocker(t, "run", "--rm", "--add-host", "host.docker.internal:host-gateway", "-v", configPath+":"+mount+":ro", image, "caddy", "validate", "--config", mount, "--adapter", "caddyfile")
			}
			run := []string{"run", "--rm", "-d", "--add-host", "host.docker.internal:host-gateway", "-p", "127.0.0.1::80", "-v", configPath + ":" + mount + ":ro", image}
			id := strings.TrimSpace(ssoDocker(t, append(run, args...)...))
			defer ssoDocker(t, "stop", "-t", "1", id)
			address := strings.TrimSpace(ssoDocker(t, "port", id, "80/tcp"))
			makeRequest := func(cookie string) *http.Request {
				request, _ := http.NewRequest("POST", "http://"+address+"/private?from=proxy", strings.NewReader("business-body-bypasses-core"))
				request.Host = "app.example.test"
				request.Header.Set("X-Soha-User-ID", "forged-user")
				request.Header.Set("X-Soha-Email", "forged@example.test")
				request.Header.Set("X-Auth-Request-Email", "forged@example.test")
				request.Header.Set("X-Soha-Projects", "forged-project")
				request.Header.Set("X-Soha-Tags", "forged-tag")
				request.Header.Set("X-Soha-Session-Token", "forged-session")
				request.Header.Set("X-Soha-Outpost-Token", "forged-service-token")
				request.Header.Set("X-Forwarded-Host", "attacker.example.test")
				if cookie != "" {
					request.AddCookie(&http.Cookie{Name: "soha_proxy_session", Value: cookie})
				}
				return request
			}
			waitSSOCondition(t, "proxy configuration load", func() bool {
				response, err := f.client.Do(makeRequest(""))
				if err != nil {
					return false
				}
				_ = response.Body.Close()
				return response.StatusCode == http.StatusFound
			})
			_, headers := ssoHTTP(t, f.client, makeRequest(""), http.StatusFound)
			ssoCheck(t, strings.HasPrefix(headers.Get("Location"), f.publicURL+"/api/v1/provider/proxy/start?"), "proxy changed canonical login location")
			body, _ := ssoHTTP(t, f.client, makeRequest(sessionToken), http.StatusOK)
			var received businessResponse
			ssoNoError(t, json.Unmarshal(body, &received))
			ssoCheck(t, received.Method == "POST" && received.Body == "business-body-bypasses-core" && received.Headers.Get("X-Soha-User-ID") == f.principal.UserID && received.Headers.Get("X-Auth-Request-Email") == f.principal.Email, "proxy lost business body or leaked forged identity/internal credentials")
			for _, name := range []string{"X-Soha-Email", "X-Soha-Projects", "X-Soha-Tags", "X-Soha-Outpost-Token", "X-Soha-Session-Token"} {
				ssoCheck(t, received.Headers.Get(name) == "", "proxy forwarded unverified header %s", name)
			}
			applicationInput.Assignments[0].Effect = "deny"
			if _, err := f.applications.UpdateApplication(context.Background(), f.principal, applicationID, applicationInput); err != nil {
				t.Fatal(err)
			}
			ssoHTTP(t, f.client, makeRequest(sessionToken), http.StatusForbidden)
			applicationInput.Assignments[0].Effect = "allow"
			if _, err := f.applications.UpdateApplication(context.Background(), f.principal, applicationID, applicationInput); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func verifySSOProxyManifests(t *testing.T, templates map[string]string) {
	t.Helper()
	directory := t.TempDir()
	for _, target := range []string{"nginx-ingress", "traefik-ingress", "traefik-compose"} {
		t.Run(target+"-syntax", func(t *testing.T) {
			content := templates[target]
			if target == "traefik-compose" {
				var compose map[string]any
				ssoNoError(t, yaml.Unmarshal([]byte(content), &compose))
				services, ok := compose["services"].(map[string]any)
				ssoCheck(t, ok, "Compose services must be an object")
				application, ok := services["protected-application"].(map[string]any)
				ssoCheck(t, ok, "protected application service must be an object")
				application["image"] = "nginx:1.29-alpine"
				data, err := yaml.Marshal(compose)
				ssoNoError(t, err)
				path := filepath.Join(directory, "compose.yaml")
				ssoNoError(t, os.WriteFile(path, data, 0600))
				ssoDocker(t, "compose", "-f", path, "config", "--quiet")
				return
			}
			validator := os.Getenv("SOHA_SSO_TEST_KUBECONFORM")
			if validator == "" {
				t.Skip("set SOHA_SSO_TEST_KUBECONFORM for Kubernetes schema checks")
			}
			args := []string{"-strict", "-summary", "-kubernetes-version", "1.34.1", "-schema-location", "default"}
			if target == "traefik-ingress" {
				schema := os.Getenv("SOHA_SSO_TEST_TRAEFIK_SCHEMA")
				if schema == "" {
					t.Skip("set SOHA_SSO_TEST_TRAEFIK_SCHEMA to Traefik's Middleware JSON schema")
				}
				args = append(args, "-schema-location", schema)
			}
			command := exec.Command(validator, append(args, "-")...) // #nosec G204 -- local validator explicitly supplied by the test runner.
			command.Stdin = strings.NewReader(content)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%s validation: %v; %s", target, err, output)
			}
		})
	}
}

func ssoDocker(t *testing.T, arguments ...string) string {
	t.Helper()
	command := exec.Command("docker", arguments...)
	output, err := command.CombinedOutput()
	ssoCheck(t, err == nil, "Docker %s failed: %v; %s", arguments[0], err, output)
	return string(output)
}

func renderSSOProxyTemplates(t *testing.T, f *ssoProtocolFixture, renderer, providerID, upstreamURL string) (map[string]string, string) {
	t.Helper()
	provider, err := f.providers.GetProvider(context.Background(), f.principal, providerID)
	ssoNoError(t, err)
	provider.Config["upstreamUrl"] = upstreamURL
	provider.Config["headerMappings"] = map[string]string{"email": "X-Auth-Request-Email"}
	provider, err = f.providers.UpdateProvider(context.Background(), f.principal, provider.ID, domainprovider.ProviderInput{ApplicationID: provider.ApplicationID, Name: provider.Name, Type: provider.Type, Status: provider.Status, Enabled: provider.Enabled, Config: provider.Config})
	ssoNoError(t, err)
	setup, err := f.providers.GetProviderSetup(context.Background(), f.principal, provider.ID, f.publicURL)
	ssoNoError(t, err)
	payload, err := json.Marshal(map[string]any{"provider": provider, "setup": setup})
	ssoNoError(t, err)
	command := exec.Command("node", "--experimental-strip-types", renderer)
	command.Stdin = bytes.NewReader(payload)
	output, err := command.Output()
	ssoCheck(t, err == nil, "render Web proxy templates: %v", err)
	var templates map[string]string
	if err := json.Unmarshal(output, &templates); err != nil || len(templates) != 7 {
		t.Fatalf("render seven templates: %v", err)
	}
	return templates, provider.ApplicationID
}
