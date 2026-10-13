package aigateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	appaccess "github.com/opensoha/soha/internal/application/access"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkbenchImageRouteEditAndOwnerAudit(t *testing.T) {
	const picture = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+XvlkAAAAASUVORK5CYII="
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		calls++
		if calls == 1 {
			if r.URL.Path != "/v1/images/generations" {
				t.Error(r.URL.Path)
			}
			body := decodeRelayTestJSON(t, r.Body)
			if body["model"] != "gpt-upstream" || body["response_format"] != "b64_json" {
				t.Error("model/b64 routing")
			}
		}
		if calls == 2 {
			if r.URL.Path != "/v1/images/edits" {
				t.Error(r.URL.Path)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil { // #nosec G120 -- handler body is capped at 1 MiB by MaxBytesReader above.
				t.Error(err)
			}
			file, _, err := r.FormFile("image")
			if err != nil {
				t.Error(err)
			} else {
				defer func() {
					if err := file.Close(); err != nil {
						t.Error(err)
					}
				}()
				data, _ := io.ReadAll(file)
				want, _ := base64.StdEncoding.DecodeString(picture)
				if string(data) != string(want) {
					t.Error("reference image changed")
				}
			}
			if r.FormValue("model") != "gpt-upstream" {
				t.Error("edit model routing")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"`+picture+`"}]}`)
	}))
	defer server.Close()
	repo := relayRepoForUpstream(t, server.URL, "openai", "image-fixture-key")
	service := newRelayRuntimeTestService(repo, server.Client())
	service.permissions = appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse, appaccess.PermAIGatewayRelayInvoke}}})
	principal := relayTestPrincipal()
	principal.PermissionKeys = []string{appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse, appaccess.PermAIGatewayRelayInvoke}
	// A chat-only route must never be guessed as an image route.
	if _, err := service.InvokeWorkbenchImage(context.Background(), principal, WorkbenchImageRequest{Prompt: "white robot", SessionID: "owned-session"}); err == nil || calls != 0 {
		t.Fatal("guessed image capability")
	}
	repo.routes[0].Metadata = map[string]any{"imageEndpoints": []string{"images/generations", "images/edits"}}
	first, err := service.InvokeWorkbenchImage(context.Background(), principal, WorkbenchImageRequest{Prompt: "white robot", SessionID: "owned-session"})
	if err != nil || len(first) == 0 {
		t.Fatal(err)
	}
	_, err = service.InvokeWorkbenchImage(context.Background(), principal, WorkbenchImageRequest{Prompt: "blue hair", Image: "data:image/png;base64," + picture, SessionID: "owned-session"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(repo.logs()) != 2 {
		t.Fatalf("calls=%d logs=%d", calls, len(repo.logs()))
	}
}

func TestWorkbenchGPTImageOmitsLegacyFormat(t *testing.T) {
	body, _, err := workbenchImageBody("public-avatar", "gpt-image-1.5", WorkbenchImageRequest{Prompt: "robot"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if _, present := fields["response_format"]; present {
		t.Fatal("GPT Image rejects response_format")
	}
}
