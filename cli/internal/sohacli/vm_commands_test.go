package sohacli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestVMCapacityAndBindingRequestsUseTypedProfileSurface(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer profile-token" {
			t.Error("missing profile auth")
		}
		switch r.URL.Path {
		case virtualizationPath + "/capacity/check":
			var body sohaapi.VirtualizationCapacityInput
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.StartAfterCreate == nil || *body.StartAfterCreate || body.CPU != 2 {
				t.Error("create-only capacity input lost")
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"status": "available", "connectionId": "conn", "reason": "both gates"}})
		case virtualizationPath + "/clusters/conn/capacity-policy":
			if r.Method != "PUT" {
				t.Error("wrong policy method")
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"sourceId": "source", "revision": 2}})
		case virtualizationPath + "/vms/vm-one/access-binding":
			if r.Method != "GET" {
				t.Error("wrong binding method")
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"vmId": "vm-one", "revision": 1, "username": "soha"}})
		default:
			t.Error("unexpected path", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	config := writeTestConfig(t, server.URL)
	for _, item := range []struct {
		args  []string
		input string
	}{{[]string{"compute", "capacity", "check", "--input", "-", "--profile", "dev"}, `{"connectionId":"conn","cpu":2,"memoryMiB":2048,"diskGiB":20,"startAfterCreate":false}`}, {[]string{"compute", "capacity", "policy", "set", "conn", "--input", "-", "--profile", "dev"}, `{"mode":"enforced","physicalSourceId":"source","expectedRevision":1,"cpu":{"enabled":true,"percent":400},"memory":{"enabled":false,"percent":100}}`}, {[]string{"compute", "vms", "access", "get", "vm-one", "--profile", "dev"}, ""}} {
		var out, diagnostics bytes.Buffer
		if Run(t.Context(), item.args, Runtime{In: strings.NewReader(item.input), Out: &out, Err: &diagnostics, ConfigPath: config}) != 0 {
			t.Fatal(diagnostics.String())
		}
	}
	if requests != 3 {
		t.Fatal("missing requests", requests)
	}
}

func TestVMShellPreservesBytesAndCancelsBlockedInput(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	defer func() { _ = writer.Close() }()
	var cleanup atomic.Bool
	data := []byte{0xe4, 0xb8, 0xad, 0xff, 0x1b}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer profile-token" {
			t.Error("stream/profile auth missing")
		}
		switch r.URL.Path {
		case virtualizationPath + "/vms/vm-one/access-binding":
			writeJSON(t, w, map[string]any{"data": map[string]any{"vmId": "vm-one", "revision": 3}})
		case virtualizationPath + "/vms/vm-one/ssh-sessions":
			var request sohaapi.VirtualMachineSSHSessionInput
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request.BindingRevision != 3 || request.Cols != 80 || request.Rows != 24 {
				t.Error("wrong frozen terminal request")
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": "session-one", "vmId": "vm-one", "bindingRevision": 3, "status": "pending", "expiresAt": time.Now().Add(time.Minute)}})
		case virtualizationPath + "/ssh-sessions/session-one/stream":
			if r.URL.RawQuery != "" {
				t.Error("bearer copied into query")
			}
			upgrader := websocket.Upgrader{}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = conn.Close() }()
			_ = conn.WriteJSON(sohaapi.VirtualMachineSSHFrame{Type: "output", DataEncoding: "base64", Data: base64.StdEncoding.EncodeToString(data)})
			code := 0
			_ = conn.WriteJSON(sohaapi.VirtualMachineSSHFrame{Type: "close", ExitCode: &code})
		case virtualizationPath + "/ssh-sessions/session-one":
			if r.Method != "DELETE" {
				t.Error("cleanup is not durable close")
			}
			cleanup.Store(true)
			writeJSON(t, w, map[string]any{"data": map[string]any{"status": "closed"}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var out, diagnostics bytes.Buffer
	code := Run(ctx, []string{"compute", "vms", "shell", "vm-one", "--profile", "dev"}, Runtime{In: input, Out: &out, Err: &diagnostics, ConfigPath: writeTestConfig(t, server.URL)})
	if code != 0 || !bytes.Equal(out.Bytes(), data) || !cleanup.Load() || ctx.Err() != nil {
		t.Fatalf("shell code=%d cleanup=%v error=%s", code, cleanup.Load(), diagnostics.String())
	}
	if _, err := writer.Write([]byte("still-owned")); err != nil {
		t.Fatal("shell closed caller stdin", err)
	}
}

func TestSSHSecretInputsRejectMixedFlagsAndKeepMaterialWriteOnly(t *testing.T) {
	const material = "PRIVATE-KEY-TASK51-CANARY"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/secrets/generate-ssh-key" {
			var request sohaapi.SecretGenerateSSHKeyRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			if len(request.Bindings) != 1 || request.Bindings[0].Purpose != "vm_cloud_init" {
				t.Error("purpose lost")
			}
		} else {
			var request sohaapi.SecretCreateRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request.Kind != "ssh_keypair" || request.Value != material || request.SSHPassphrase != "passphrase-canary" || request.Bindings[0].Purpose != "vm_auth" {
				t.Error("typed secret material lost")
			}
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"id": "key-one", "kind": "ssh_keypair", "name": "key", "scopeType": "workspace", "scopeId": "default", "status": "active", "currentVersion": 1, "value": material, "sshPassphrase": "passphrase-canary"}})
	}))
	defer server.Close()
	config := writeTestConfig(t, server.URL)
	input := `{"name":"key","kind":"ssh_keypair","scopeType":"workspace","scopeId":"default","value":"` + material + `","sshPassphrase":"passphrase-canary","bindings":[{"targetType":"vm","targetRef":"vm-one","purpose":"vm_auth"}]}`
	for _, args := range [][]string{{"secret", "create", "--input", "-", "--profile", "dev"}, {"secret", "generate-ssh-key", "--name", "key", "--binding", "vm_create:vm_cloud_init=conn", "--profile", "dev"}} {
		var out, diagnostics bytes.Buffer
		if Run(t.Context(), args, Runtime{In: strings.NewReader(input), Out: &out, Err: &diagnostics, ConfigPath: config}) != 0 {
			t.Fatal(diagnostics.String())
		}
		if strings.Contains(out.String()+diagnostics.String(), material) || strings.Contains(out.String()+diagnostics.String(), "passphrase-canary") {
			t.Fatal("secret output leaked material")
		}
	}
	for _, args := range [][]string{{"secret", "create", "--input", "-", "--kind", "ssh_password"}, {"secret", "rotate", "key-one", "--input", "-", "--vault-mount", "secret"}} {
		var diagnostics bytes.Buffer
		if Run(t.Context(), args, Runtime{In: strings.NewReader(input), Out: &bytes.Buffer{}, Err: &diagnostics}) != 1 || !strings.Contains(diagnostics.String(), "cannot be combined") {
			t.Fatal("mixed flags silently ignored", diagnostics.String())
		}
	}
	if _, err := parseSecretBindings([]string{"vm=vm-one"}); err == nil {
		t.Fatal("VM binding without purpose accepted")
	}
}

func TestSSHSecretReferenceLookupUsesVisibleFixedVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/secrets/key/references" || r.Method != "GET" {
			t.Error("wrong reference endpoint")
		}
		writeJSON(t, w, map[string]any{"items": []any{map[string]any{"targetId": "vm-one", "vmId": "vm-one", "usage": "login_binding", "version": 1, "status": "active"}}})
	}))
	defer server.Close()
	var out, diagnostics bytes.Buffer
	if Run(t.Context(), []string{"secret", "references", "key", "--profile", "dev"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &diagnostics, ConfigPath: writeTestConfig(t, server.URL)}) != 0 || !strings.Contains(out.String(), "login_binding") {
		t.Fatal(diagnostics.String(), out.String())
	}
}

func TestSSHJSONInputRejectsOversizeFilesAndStdinWithoutEchoingValues(t *testing.T) {
	value := "PRIVATE-CANARY" + strings.Repeat(" ", 10<<20)
	file := filepath.Join(t.TempDir(), "credential.json")
	if err := os.WriteFile(file, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, "-"} {
		var request sohaapi.SecretCreateRequest
		err := readTypedSecretInput(Runtime{In: strings.NewReader(value)}, path, &request)
		if err == nil || strings.Contains(err.Error(), "PRIVATE-CANARY") {
			t.Fatal("oversized secret input accepted or echoed", err)
		}
	}
}
