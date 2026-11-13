package sohacli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
)

func dockerReadFixture(action string) protocol.ReadResult {
	project, _ := protocol.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	result := protocol.ReadResult{Protocol: protocol.ReadProtocol, ProjectID: "project-1", Action: action,
		Container: protocol.ContainerBinding{ProjectBinding: project, ServiceName: "api", ContainerID: strings.Repeat("a", 64)},
		Complete:  true, Volumes: []protocol.Volume{}, Files: []protocol.FileEntry{}}
	switch action {
	case "logs", "files.read":
		result.Content, result.Encoding = "中文\x1b]52;c;clipboard\a\r\npassword=authorized-file-value", "utf-8"
		result.SizeBytes, result.ReturnedBytes = int64(len(result.Content)), int64(len(result.Content))
	}
	if strings.HasPrefix(action, "files.") {
		result.Target = "/data"
	}
	return result
}

func TestDockerVersionedReadsPreserveRequestsAndExactStructuredContent(t *testing.T) {
	for _, test := range []struct {
		resource, action, protocolAction, suffix, query string
		flags                                           []string
	}{
		{"logs", "get", "logs", "logs", "limitBytes=1024&serviceName=api&sinceSeconds=60&tailLines=12", []string{"--tail-lines", "12", "--since-seconds", "60", "--limit-bytes", "1024"}},
		{"volumes", "list", "volumes", "volumes", "serviceName=api", nil},
		{"files", "list", "files.list", "files", "limit=12&serviceName=api&target=%2Fdata", []string{"--target", "/data", "--path", ".", "--limit", "12"}},
		{"files", "read", "files.read", "file", "limitBytes=1024&serviceName=api&target=%2Fdata", []string{"--target", "/data", "--limit-bytes", "1024"}},
	} {
		for _, format := range []string{"json", "yaml"} {
			t.Run(test.protocolAction+"/"+format, func(t *testing.T) {
				calls := 0
				result := dockerReadFixture(test.protocolAction)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != "GET" || r.URL.Path != "/api/v1/docker/projects/project-1/runtime/v1/"+test.suffix || r.URL.RawQuery != test.query || r.Header.Get("Authorization") != "Bearer profile-token" || r.ContentLength > 0 {
						t.Errorf("unexpected versioned read request: %s %s", r.Method, r.URL)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": result})
				}))
				defer server.Close()
				args := append([]string{test.resource, test.action, "--project-id", "project-1", "--service", "api", "--output", format}, test.flags...)
				code, out, stderr := runDockerTest(t, server.URL, "", args...)
				if code != 0 || calls != 1 || strings.ContainsAny(out, "\x1b\a\r") {
					t.Fatalf("code=%d calls=%d stderr=%s; content must stay structured", code, calls, stderr)
				}
				if format == "json" {
					var envelope struct {
						Data protocol.ReadResult `json:"data"`
					}
					if json.Unmarshal([]byte(out), &envelope) != nil || envelope.Data.Content != result.Content || envelope.Data.ReturnedBytes != int64(len(result.Content)) {
						t.Fatal("authorized UTF-8 content or byte count changed")
					}
				} else if result.Content != "" && (!strings.Contains(out, "中文") || !strings.Contains(out, "authorized-file-value") || !strings.Contains(out, `\x1b`)) {
					t.Fatal("YAML lost exact authorized content or control quoting")
				}
			})
		}
	}
}

func TestDockerVersionedReadRejectsResponseIdentityBoundsAndInvalidUTF8(t *testing.T) {
	for name, change := range map[string]func(*protocol.ReadResult){
		"project":        func(r *protocol.ReadResult) { r.ProjectID = "other" },
		"service":        func(r *protocol.ReadResult) { r.Container.ServiceName = "other" },
		"namespace":      func(r *protocol.ReadResult) { r.Container.Namespace = "caller" },
		"short CID":      func(r *protocol.ReadResult) { r.Container.ContainerID = "short" },
		"action":         func(r *protocol.ReadResult) { r.Action = "logs" },
		"target":         func(r *protocol.ReadResult) { r.Target = "/other" },
		"path":           func(r *protocol.ReadResult) { r.Path = "other" },
		"completeness":   func(r *protocol.ReadResult) { r.Truncated = true },
		"returned bytes": func(r *protocol.ReadResult) { r.ReturnedBytes++ },
		"source size":    func(r *protocol.ReadResult) { r.SizeBytes-- },
		"limit": func(r *protocol.ReadResult) {
			r.Content = strings.Repeat("a", 1025)
			r.SizeBytes = 1025
			r.ReturnedBytes = 1025
		},
		"missing arrays": func(r *protocol.ReadResult) { r.Files = nil },
	} {
		t.Run(name, func(t *testing.T) {
			result := dockerReadFixture("files.read")
			change(&result)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": result})
			}))
			defer server.Close()
			code, out, stderr := runDockerTest(t, server.URL, "", "files", "read", "--project-id", "project-1", "--service", "api", "--target", "/data", "--limit-bytes", "1024")
			if code == 0 || out != "" || !strings.Contains(stderr, "runtime_read_response_invalid") || strings.Contains(stderr, "authorized-file-value") {
				t.Fatalf("invalid response accepted or leaked: code=%d stderr=%s", code, stderr)
			}
		})
	}
	for _, response := range []string{`{"data":{"content":"` + string([]byte{0xff}) + `"}}`, `{"data":{},"unknown":"private-value"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
		code, out, stderr := runDockerTest(t, server.URL, "", "volumes", "list", "--project-id", "project-1", "--service", "api")
		server.Close()
		if code == 0 || out != "" || !strings.Contains(stderr, "runtime_read_response_invalid") || strings.Contains(stderr, "private-value") {
			t.Fatal("noncanonical response accepted or leaked")
		}
	}
}

func TestDockerVersionedReadRejectsInvalidFlagsBeforeHTTP(t *testing.T) {
	for _, flags := range [][]string{
		{"--service", ""}, {"--project-id", "../outside"}, {"--project-id", "a\nb"}, {"--service", "api;id"},
		{"--target", "/"}, {"--target", "/data/"}, {"--path", "../outside"}, {"--path", `a\b`},
		{"--path", "/outside"}, {"--path", "a\nfile"}, {"--tail-lines", "1"}, {"--limit", "10"},
		{"--limit-bytes", "1048577"}, {"--limit-bytes", "-1"}, {"--request-timeout", "999ms"},
		{"--request-timeout", "121s"}, {"--output", "text"}, {"positional-id"},
	} {
		t.Run(strings.Join(flags, "_"), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer server.Close()
			args := append([]string{"files", "read", "--project-id", "project-1", "--service", "api", "--target", "/data"}, flags...)
			code, _, stderr := runDockerTest(t, server.URL, "", args...)
			if code == 0 || calls != 0 {
				t.Fatalf("bad request reached HTTP: code=%d calls=%d stderr=%s", code, calls, stderr)
			}
		})
	}
}

func TestDockerVersionedReadNeverFallsBackOrFollowsRedirects(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusNotImplemented, http.StatusForbidden, http.StatusTemporaryRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/legacy-compose-path")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `{"error":{"code":"unsupported","message":"unsupported"}}`)
			}))
			defer server.Close()
			code, out, stderr := runDockerTest(t, server.URL, "", "volumes", "list", "--project-id", "project-1", "--service", "api")
			if code == 0 || calls != 1 || out != "" {
				t.Fatalf("fallback or redirect: code=%d calls=%d stderr=%s", code, calls, stderr)
			}
		})
	}
}

func TestDockerVersionedReadGovernedCredentialsCannotFallBackToREST(t *testing.T) {
	for _, prefix := range []string{"soha_gpat_", "soha_gsat_"} {
		t.Run(prefix, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer server.Close()
			config := writeTestConfigWithProfile(t, ProfileConfig{ServerURL: server.URL, AccessToken: prefix + strings.Repeat("a", 40)})
			var out, errs bytes.Buffer
			code := Run(t.Context(), []string{"docker", "volumes", "list", "--project-id", "project-1", "--service", "api", "--profile", "dev"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &errs, ConfigPath: config})
			if code == 0 || calls != 0 || !strings.Contains(errs.String(), "governed_ai_unsupported") {
				t.Fatalf("governed credential used REST: %d %d %s", code, calls, errs.String())
			}
		})
	}
}

func TestDockerVersionedReadMetadataAdvertisesCommands(t *testing.T) {
	for family, actions := range map[string][]string{"logs": {"get"}, "volumes": {"list"}, "files": {"list", "read"}} {
		found := false
		for _, spec := range dockerCommandSpec().Subcommands {
			if spec.Name != family {
				continue
			}
			found = true
			for _, action := range actions {
				if !strings.Contains(spec.Usage, action) || !strings.Contains(strings.Join(spec.CompletionWords, " "), action) || !strings.Contains(strings.Join(spec.Examples, "\n"), "soha docker "+family+" "+action) {
					t.Fatalf("help/completion/examples missing: %s %s", family, action)
				}
			}
		}
		if !found {
			t.Fatal("missing command family", family)
		}
	}
}
