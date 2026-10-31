package sohacli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDockerIdentityRequestsProjectionAndInvalidResponses(t *testing.T) {
	const uid = "3f3208c8-9161-4ea6-bf3a-d6c952288189"
	const revision = "85d6f8f1-0e9c-464c-bbaa-d42a8aff157a"
	for _, resource := range []string{"host", "project"} {
		for _, scenario := range []string{"valid", "wrong kind", "wrong id", "missing uid", "missing revision", "missing host", "unexpected host", "malformed uuid"} {
			if resource == "host" && scenario == "missing host" || resource == "project" && scenario == "unexpected host" {
				continue
			}
			t.Run(resource+"/"+scenario, func(t *testing.T) {
				item := map[string]any{"resourceKind": "docker." + resource, "id": resource + "-1", "uid": uid, "configurationRevision": revision, "envContent": "identity-private-fixture", "config": map[string]string{"password": "identity-private-fixture"}}
				if resource == "project" {
					item["hostId"] = "host-1"
				}
				switch scenario {
				case "wrong kind":
					item["resourceKind"] = "docker.service"
				case "wrong id":
					item["id"] = "other"
				case "missing uid":
					delete(item, "uid")
				case "missing revision":
					delete(item, "configurationRevision")
				case "missing host":
					delete(item, "hostId")
				case "unexpected host":
					item["hostId"] = "other"
				case "malformed uuid":
					item["uid"] = "not-a-uuid"
				}
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != "GET" || r.URL.Path != "/api/v1/docker/"+resource+"s/"+resource+"-1/identity" || r.Header.Get("Authorization") != "Bearer profile-token" || r.Header.Get("Idempotency-Key") != "" {
						t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": item})
				}))
				defer server.Close()
				code, out, stderr := runDockerTest(t, server.URL, "", resource+"s", "identity", resource+"-1")
				if calls != 1 || strings.Contains(out+stderr, "identity-private-fixture") {
					t.Fatalf("calls=%d code=%d stderr=%s", calls, code, stderr)
				}
				if scenario != "valid" {
					if code == 0 || out != "" {
						t.Fatalf("invalid identity accepted: code=%d out=%s", code, out)
					}
					return
				}
				var result struct{ Data map[string]any }
				if err := json.Unmarshal([]byte(out), &result); err != nil || code != 0 || result.Data["uid"] != uid || result.Data["configurationRevision"] != revision {
					t.Fatalf("code=%d out=%s stderr=%s parse=%v", code, out, stderr, err)
				}
				want := 4
				if resource == "project" {
					want++
				}
				if len(result.Data) != want {
					t.Fatalf("projection exposed extra data: %s", out)
				}
			})
		}
	}
}

func TestDockerIdentityReadNeverFallsBack(t *testing.T) {
	for _, status := range []int{403, 404, 405, 501, 503, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/legacy")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `{"error":{"code":"unavailable","message":"unavailable"}}`)
			}))
			defer server.Close()
			code, out, _ := runDockerTest(t, server.URL, "", "projects", "identity", "project-1")
			if code == 0 || out != "" || calls != 1 {
				t.Fatalf("identity read fell back: code=%d calls=%d out=%s", code, calls, out)
			}
		})
	}
}

func TestDockerIdentityRejectsMutationFlagsAndInvalidIDs(t *testing.T) {
	for _, extra := range [][]string{{"--input-json", "{}"}, {"--yes"}, {"--idempotency-key", "original-key"}, {"--retry-original"}, {"--action", "restart"}, {"another-id"}} {
		t.Run(strings.Join(extra, " "), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
			defer server.Close()
			args := append([]string{"hosts", "identity", "host-1"}, extra...)
			code, _, _ := runDockerTest(t, server.URL, "", args...)
			if code == 0 || calls != 0 {
				t.Fatalf("invalid identity input reached server: code=%d calls=%d", code, calls)
			}
		})
	}
}

func TestDockerIdentityRequiresOneSafeIDAndAdvertisesHelp(t *testing.T) {
	for _, ids := range [][]string{nil, {"../other"}, {".."}, {"host-1", "host-2"}} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
		args := append([]string{"hosts", "identity"}, ids...)
		code, _, _ := runDockerTest(t, server.URL, "", args...)
		server.Close()
		if code == 0 || calls != 0 {
			t.Fatalf("invalid IDs reached server: ids=%v code=%d calls=%d", ids, code, calls)
		}
	}
	for _, sub := range dockerCommandSpec().Subcommands {
		if sub.Name == "hosts" || sub.Name == "projects" {
			if !strings.Contains(sub.Usage, "identity") {
				t.Fatalf("help lacks identity: %s", sub.Name)
			}
			found := false
			for _, word := range sub.CompletionWords {
				found = found || word == "identity"
			}
			if !found {
				t.Fatalf("completion lacks identity: %s", sub.Name)
			}
		}
	}
}

func TestDockerIdentityPreservesLegacySpaceInPublicID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v1/docker/hosts/legacy%20host/identity" {
			t.Errorf("public ID was not encoded as one segment: %s", r.URL.EscapedPath())
		}
		_, _ = fmt.Fprint(w, `{"data":{"resourceKind":"docker.host","id":"legacy host","uid":"3f3208c8-9161-4ea6-bf3a-d6c952288189","configurationRevision":"85d6f8f1-0e9c-464c-bbaa-d42a8aff157a"}}`)
	}))
	defer server.Close()
	code, out, stderr := runDockerTest(t, server.URL, "", "hosts", "identity", "legacy host")
	if code != 0 || !strings.Contains(out, "legacy host") {
		t.Fatalf("legacy ID was rejected: code=%d out=%s stderr=%s", code, out, stderr)
	}
}
