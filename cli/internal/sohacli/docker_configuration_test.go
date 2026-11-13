package sohacli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const configurationUID = "3f3208c8-9161-4ea6-bf3a-d6c952288189"
const configurationRevision = "85d6f8f1-0e9c-464c-bbaa-d42a8aff157a"
const configurationConditionsJSON = `"preconditions":{"expectedUid":"` + configurationUID + `","expectedConfigurationRevision":"` + configurationRevision + `"}`

func configurationReceiptJSON(kind, id, action, key string) string {
	host := ""
	if kind == "project" {
		host = `,"hostId":"host-1"`
	}
	return `{"data":{"id":"receipt-1","requestKey":"` + key + `","action":"` + action + `","status":"committed","deleted":` + fmt.Sprint(action == "delete") + `,"createdAt":"2026-10-28T00:00:00Z","resource":{"resourceKind":"docker.` + kind + `","id":"` + id + `","uid":"` + configurationUID + `","configurationRevision":"` + configurationRevision + `"` + host + `}}}`
}

func TestDockerConfigurationTypedRequests(t *testing.T) {
	for _, test := range []struct{ resource, action, id, method, path, body string }{
		{"hosts", "register", "", "POST", "/api/v1/docker/hosts/registrations", `{"host":{"name":"registered","config":{"large":9007199254740993,"Name":1,"name":2}}}`},
		{"hosts", "update", "host-1", "PATCH", "/api/v1/docker/hosts/host-1/configuration", `{` + configurationConditionsJSON + `,"changes":[{"field":"owner","stringValue":""},{"field":"availablePortStart","integerValue":0},{"field":"config","objectValue":{}}]}`},
		{"projects", "update", "project-1", "PATCH", "/api/v1/docker/projects/project-1/configuration", `{` + configurationConditionsJSON + `,"changes":[{"field":"ttlSeconds","integerValue":0},{"field":"config","objectValue":{"large":9007199254740993}}]}`},
		{"hosts", "delete", "host-1", "POST", "/api/v1/docker/hosts/host-1/configuration-deletion", `{` + configurationConditionsJSON + `}`},
		{"projects", "delete", "project-1", "POST", "/api/v1/docker/projects/project-1/configuration-deletion", `{` + configurationConditionsJSON + `}`},
	} {
		t.Run(test.resource+"/"+test.action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != test.method || r.URL.Path != test.path || r.Header.Get("Authorization") != "Bearer profile-token" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				var raw json.RawMessage
				if json.NewDecoder(r.Body).Decode(&raw) != nil {
					t.Error("missing typed body")
				}
				if !strings.Contains(string(raw), `"idempotencyKey":"original-key"`) {
					t.Error("key missing")
				}
				if strings.Contains(test.body, "9007199254740993") && !strings.Contains(string(raw), "9007199254740993") {
					t.Error("large integer rounded")
				}
				if strings.Contains(test.body, `"integerValue":0`) && !strings.Contains(string(raw), `"integerValue":0`) {
					t.Error("explicit zero lost")
				}
				if strings.Contains(test.body, `"stringValue":""`) && !strings.Contains(string(raw), `"stringValue":""`) {
					t.Error("empty string lost")
				}
				if strings.Contains(test.body, `"objectValue":{}`) && !strings.Contains(string(raw), `"objectValue":{}`) {
					t.Error("empty map lost")
				}
				kind := strings.TrimSuffix(test.resource, "s")
				id := test.id
				if id == "" {
					id = "registered-host"
				}
				_, _ = fmt.Fprint(w, configurationReceiptJSON(kind, id, test.action, "original-key"))
			}))
			defer server.Close()
			args := []string{test.resource, test.action}
			if test.id != "" {
				args = append(args, test.id)
			}
			args = append(args, "--input", "-", "--idempotency-key", "original-key", "--yes")
			code, out, stderr := runDockerTest(t, server.URL, test.body, args...)
			if code != 0 || calls != 1 || !strings.Contains(out, "receipt-1") {
				t.Fatalf("code=%d calls=%d out=%s stderr=%s", code, calls, out, stderr)
			}
		})
	}
}

func TestDockerConfigurationRejectsAmbiguousInputBeforeHTTP(t *testing.T) {
	valid := `{` + configurationConditionsJSON + `,"changes":[{"field":"owner","stringValue":""}]}`
	for _, body := range []string{
		strings.Replace(valid, `"changes":`, `"Changes":`, 1),
		strings.Replace(valid, `"changes":`, `"changes":[],"Changes":`, 1),
		strings.Replace(valid, `"expectedUid":`, `"ExpectedUid":`, 1),
		strings.Replace(valid, `"field":`, `"Field":`, 1),
		strings.Replace(valid, `"changes":`, `"changes":[],"changes":`, 1),
		strings.Replace(valid, `"stringValue":""`, `"stringValue":null`, 1),
		strings.Replace(valid, `"stringValue":""`, `"stringValue":"","unexpected":1`, 1),
		strings.Replace(valid, configurationUID, "00000000-0000-0000-0000-000000000000", 1),
		strings.Replace(valid, configurationRevision, "not-a-uuid", 1),
		strings.TrimSuffix(valid, "}") + `,"idempotencyKey":"different-key"}`,
		valid + `{}`, `[]`, `{"changes":[]}`, `{"config":"` + strings.Repeat("a", 2<<20) + `"}`,
	} {
		t.Run(fmt.Sprint(len(body))+"/"+fmt.Sprint(strings.Contains(body, "null")), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
			defer server.Close()
			code, out, _ := runDockerTest(t, server.URL, body, "hosts", "update", "host-1", "--input", "-", "--idempotency-key", "original-key", "--yes")
			if code == 0 || calls != 0 || out != "" {
				t.Fatalf("invalid input reached HTTP: code=%d calls=%d", code, calls)
			}
		})
	}
}

func TestDockerConfigurationUnknownNeverReplaysOrFallsBack(t *testing.T) {
	for _, status := range []int{403, 404, 405, 409, 501, 503, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/legacy")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `{"error":{"code":"unknown","message":"failed"}}`)
			}))
			defer server.Close()
			body := `{` + configurationConditionsJSON + `}`
			code, out, stderr := runDockerTest(t, server.URL, body, "projects", "delete", "project-1", "--input", "-", "--idempotency-key", "original-key", "--yes")
			if code == 0 || out != "" || calls != 1 || !strings.Contains(stderr, "configuration-requests get original-key") {
				t.Fatalf("unsafe retry code=%d calls=%d stderr=%s", code, calls, stderr)
			}
		})
	}
}

func TestDockerConfigurationReceiptBindingAndLookup(t *testing.T) {
	for _, change := range []string{"valid", "wrong-key", "wrong-kind", "wrong-id", "missing-uid", "wrong-action", "wrong-deleted"} {
		t.Run(change, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := configurationReceiptJSON("project", "project-1", "delete", "original-key")
				switch change {
				case "wrong-key":
					body = strings.Replace(body, "original-key", "another-key", 1)
				case "wrong-kind":
					body = strings.Replace(body, "docker.project", "docker.service", 1)
				case "wrong-id":
					body = strings.Replace(body, "project-1", "other-project", 1)
				case "missing-uid":
					body = strings.Replace(body, configurationUID, "00000000-0000-0000-0000-000000000000", 1)
				case "wrong-action":
					body = strings.Replace(body, `"action":"delete"`, `"action":"update"`, 1)
				case "wrong-deleted":
					body = strings.Replace(body, `"deleted":true`, `"deleted":false`, 1)
				}
				_, _ = fmt.Fprint(w, body)
			}))
			defer server.Close()
			code, out, _ := runDockerTest(t, server.URL, `{`+configurationConditionsJSON+`}`, "projects", "delete", "project-1", "--input", "-", "--idempotency-key", "original-key", "--yes")
			if (code == 0) != (change == "valid") || change != "valid" && out != "" {
				t.Fatalf("unbound receipt accepted code=%d out=%s", code, out)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/docker/configuration-requests/original-key" {
			t.Error("lookup changed endpoint")
		}
		_, _ = fmt.Fprint(w, configurationReceiptJSON("host", "deleted-host", "delete", "original-key"))
	}))
	defer server.Close()
	code, out, stderr := runDockerTest(t, server.URL, "", "configuration-requests", "get", "original-key")
	if code != 0 || !strings.Contains(out, "deleted-host") {
		t.Fatalf("lookup failed %d %s", code, stderr)
	}
}

func TestDockerConfigurationRequiresConfirmationAndExactFlags(t *testing.T) {
	for _, extra := range [][]string{nil, {"--retry-original"}, {"--input-json", "{}"}, {"--action", "destroy"}, {"--idempotency-key", "bad/key"}} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
		args := append([]string{"hosts", "delete", "host-1", "--input", "-", "--idempotency-key", "original-key"}, extra...)
		code, _, _ := runDockerTest(t, server.URL, `{`+configurationConditionsJSON+`}`, args...)
		server.Close()
		if code == 0 || calls != 0 {
			t.Fatalf("unconfirmed/invalid call reached server: %v", extra)
		}
	}
}
