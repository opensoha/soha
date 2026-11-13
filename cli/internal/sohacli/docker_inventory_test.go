package sohacli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDockerInventoryRequestsAndSafeProjection(t *testing.T) {
	const secret = "inventory-private-configuration-value"
	for _, test := range []struct {
		resource, item string
		args           []string
		query          url.Values
	}{
		{"hosts", `{"id":"host 1","name":"worker","status":"ready","architecture":"amd64","endpoint":"` + secret + `","owner":"` + secret + `","config":{"agentToken":"` + secret + `"},"labels":{"private":"` + secret + `"}}`, []string{"--environment", "test", "--architecture", "amd64"}, url.Values{"environment": {"test"}, "architecture": {"amd64"}}},
		{"projects", `{"id":"project-1","hostId":"host 1","name":"api","status":"running","slug":"api","composeContent":"` + secret + `","envContent":"` + secret + `","config":{"private":"` + secret + `"}}`, []string{"--host-id", "host 1", "--source-kind", "compose"}, url.Values{"hostId": {"host 1"}, "sourceKind": {"compose"}}},
		{"services", `{"id":"service-1","hostId":"host 1","projectId":"project-1","name":"api","status":"running","containerId":"` + secret + `","image":"` + secret + `","config":{"private":"` + secret + `"}}`, []string{"--host-id", "host 1", "--project-id", "project-1"}, url.Values{"hostId": {"host 1"}, "projectId": {"project-1"}}},
	} {
		t.Run(test.resource, func(t *testing.T) {
			calls := 0
			test.query.Set("search", "api & worker")
			test.query.Set("status", "running")
			test.query.Set("page", "2")
			test.query.Set("pageSize", "3")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/docker/"+test.resource || r.Header.Get("Authorization") != "Bearer profile-token" || r.URL.Query().Encode() != test.query.Encode() {
					t.Errorf("wrong inventory request: %s %s", r.Method, r.URL.Path)
				}
				_, _ = fmt.Fprintf(w, `{"data":{"items":[%s],"total":7,"page":2,"pageSize":3,"privateExtension":"%s"}}`, test.item, secret)
			}))
			defer server.Close()
			args := append([]string{test.resource, "list", "--search", "api & worker", "--status", "running", "--page", "2", "--page-size", "3"}, test.args...)
			code, out, stderr := runDockerTest(t, server.URL, "", args...)
			var result struct {
				Data struct {
					Items               []map[string]any `json:"items"`
					Total               int              `json:"total"`
					ConfigurationHidden bool             `json:"configurationHidden"`
				} `json:"data"`
			}
			if code != 0 || calls != 1 || json.Unmarshal([]byte(out), &result) != nil || len(result.Data.Items) != 1 || result.Data.Total != 7 || !result.Data.ConfigurationHidden || strings.Contains(out+stderr, secret) {
				t.Fatalf("unsafe inventory projection: code=%d calls=%d stderr=%s", code, calls, stderr)
			}
		})
	}
}

func TestDockerInventoryRejectsInvalidAndIgnoredFlagsBeforeHTTP(t *testing.T) {
	for _, args := range [][]string{
		{"hosts", "list", "unexpected-id"}, {"hosts", "list", "--page", "0"},
		{"hosts", "list", "--page", "1000001"}, {"hosts", "list", "--page-size", "0"},
		{"hosts", "list", "--page-size", "101"}, {"hosts", "list", "--yes"},
		{"hosts", "list", "--host-id", "h"}, {"hosts", "list", "--project-id", "p"},
		{"hosts", "list", "--source-kind", "compose"}, {"projects", "list", "--architecture", "amd64"},
		{"projects", "list", "--project-id", "p"}, {"services", "list", "--environment", "test"},
		{"services", "list", "--source-kind", "compose"}, {"services", "list", "--host-id", "../outside"},
		{"services", "list", "--project-id", "bad\nvalue"}, {"hosts", "list", "--search", strings.Repeat("x", 257)},
		{"hosts", "list", "--search", "private-value\nnext"}, {"projects", "list", "--idempotency-key", "key-12345"},
	} {
		t.Run(strings.Join(args[:2], "/"), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
			defer server.Close()
			code, out, stderr := runDockerTest(t, server.URL, "", args...)
			if code == 0 || calls != 0 || strings.Contains(out+stderr, "private-value") {
				t.Fatalf("invalid list reached HTTP: code=%d calls=%d", code, calls)
			}
		})
	}
}

func TestDockerInventoryNeverFallsBackOrFollowsRedirects(t *testing.T) {
	for _, resource := range []string{"hosts", "projects", "services"} {
		for _, status := range []int{301, 307, 401, 403, 404, 503} {
			t.Run(fmt.Sprintf("%s/%d", resource, status), func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.Header().Set("Location", "/unexpected")
					w.WriteHeader(status)
					_, _ = fmt.Fprint(w, `{"error":{"code":"unavailable","message":"unavailable"}}`)
				}))
				defer server.Close()
				code, out, _ := runDockerTest(t, server.URL, "", resource, "list")
				if code == 0 || calls != 1 || out != "" {
					t.Fatalf("list followed/replayed/fell back: code=%d calls=%d", code, calls)
				}
			})
		}
	}
}

func TestDockerInventoryPageValidationAndEmptyResult(t *testing.T) {
	for _, test := range []struct {
		name, response string
		valid          bool
	}{
		{"empty", `{"data":{"items":null,"total":0,"page":1,"pageSize":1}}`, true},
		{"no-data", `{}`, false}, {"null", `{"data":null}`, false},
		{"wrong-page", `{"data":{"items":[],"total":0,"page":2,"pageSize":1}}`, false},
		{"wrong-size", `{"data":{"items":[],"total":0,"page":1,"pageSize":100}}`, false},
		{"negative-total", `{"data":{"items":[],"total":-1,"page":1,"pageSize":1}}`, false},
		{"missing-id", `{"data":{"items":[{"name":"broken"}],"total":1,"page":1,"pageSize":1}}`, false},
		{"oversize", `{"data":{"items":[{"id":"a"},{"id":"b"}],"total":2,"page":1,"pageSize":1}}`, false},
		{"invalid-id", `{"data":{"items":[{"id":"../outside"}],"total":1,"page":1,"pageSize":1}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, test.response) }))
			defer server.Close()
			code, out, stderr := runDockerTest(t, server.URL, "", "hosts", "list", "--page-size", "1")
			if (code == 0) != test.valid || !test.valid && out != "" || test.valid && !strings.Contains(out, `"items": []`) {
				t.Fatalf("page outcome wrong: code=%d stderr=%s", code, stderr)
			}
		})
	}
}

func TestDockerInventoryMetadataAdvertisesReadOnlyDiscovery(t *testing.T) {
	for _, spec := range dockerCommandSpec().Subcommands {
		if spec.Name != "hosts" && spec.Name != "projects" && spec.Name != "services" {
			continue
		}
		if !strings.Contains(spec.Usage, "list") || !strings.Contains(strings.Join(spec.CompletionWords, " "), "list") || !strings.Contains(strings.Join(spec.Examples, "\n"), "soha docker "+spec.Name+" list") {
			t.Fatalf("inventory help/completion missing for %s", spec.Name)
		}
	}
}
