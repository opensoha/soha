package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const credentialTestAction = "knowledge.connector.http.fetch"
const connectorProtocolForTest = "opensoha.dev/connector-action/v1"

func credentialTestContext(value string) (context.Context, domainidentity.Principal) {
	principal := domainidentity.Principal{UserID: "fixture-user"}
	ctx := domainsecret.WithValues(context.Background(), map[string]string{"SOURCE": value})
	return domainsecret.WithExecutionContext(ctx, domainsecret.ExecutionContext{Principal: principal, Target: domainsecret.Target{Type: "capability", Ref: credentialTestAction}, References: []domainsecret.Reference{{Alias: "SOURCE", SecretID: "fixture", Version: 2, URI: "soha://secrets/fixture/versions/2"}}}), principal
}

func credentialTestRuntime(t *testing.T, protocol string, action http.HandlerFunc) (*connectorRuntimeProvider, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest" {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "actions": []map[string]any{{"name": credentialTestAction, "inputSchema": map[string]any{}, "credentialProtocol": protocol, "credentialAliases": []string{"SOURCE"}}}})
			return
		}
		calls.Add(1)
		action(w, r)
	}))
	t.Cleanup(server.Close)
	discovered, err := DiscoverConnectorRuntime(context.Background(), server.URL, server.Client(), WithConnectorRuntimeToken(uuid.NewString()))
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := discovered.(*connectorRuntimeProvider)
	if !ok {
		t.Fatal("unexpected connector provider")
	}
	return provider, &calls
}

func TestConnectorRuntimeDeliversPinnedCredentialAndMasksOutput(t *testing.T) {
	secret := credentialTestAction
	key := uuid.NewString()
	structured, _ := json.Marshal(map[string]string{key: secret})
	ctx, principal := credentialTestContext(string(structured))
	provider, calls := credentialTestRuntime(t, connectorProtocolForTest, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SchemaVersion string         `json:"schemaVersion"`
			Action        string         `json:"action"`
			Input         map[string]any `json:"input"`
			Credentials   struct {
				ExpiresAt time.Time `json:"expiresAt"`
				Entries   map[string]struct {
					Reference string `json:"reference"`
					Value     string `json:"value"`
				} `json:"entries"`
			} `json:"credentials"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		entry := body.Credentials.Entries["SOURCE"]
		if body.SchemaVersion != connectorProtocolForTest || body.Action != credentialTestAction || entry.Reference != "soha://secrets/fixture/versions/2" || entry.Value != string(structured) || time.Until(body.Credentials.ExpiresAt) <= 0 || time.Until(body.Credentials.ExpiresAt) > 10*time.Second {
			t.Error("credential protocol, pinned identity or lifetime was not preserved")
		}
		if len(body.Input) != 1 || body.Input["cursor"] != "fixture" {
			t.Error("credential material changed ordinary input")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "output": map[string]any{"echo": secret, "key": key, "raw": string(structured), secret: "fixture"}})
	})
	output, related, err := provider.InvokeTool(ctx, principal, provider.Tools()[0], map[string]any{"cursor": "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(map[string]any{"output": output, "related": related})
	if calls.Load() != 1 || strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), key) {
		t.Fatal("credential action did not complete once with protected output")
	}
}

func TestConnectorRuntimeRejectsUnboundCredentialDelivery(t *testing.T) {
	for _, scenario := range []string{"old-runtime", "no-context", "target", "principal", "access-token", "reference", "alias", "token", "insecure", "oversized", "action-name"} {
		t.Run(scenario, func(t *testing.T) {
			protocol := connectorProtocolForTest
			if scenario == "old-runtime" {
				protocol = ""
			}
			provider, calls := credentialTestRuntime(t, protocol, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true,"output":{}}`)) })
			ctx, principal := credentialTestContext(uuid.NewString())
			execution, _ := domainsecret.ExecutionContextFrom(ctx)
			switch scenario {
			case "no-context":
				ctx = domainsecret.WithValues(context.Background(), map[string]string{"SOURCE": uuid.NewString()})
			case "target":
				execution.Target.Ref = "other"
			case "action-name":
				provider.actions[0].Name = strings.Repeat("界", 129)
				execution.Target.Ref = provider.actions[0].Name
			case "principal":
				execution.Principal.UserID = "other"
			case "access-token":
				execution.Principal.AccessTokenID = "other"
			case "reference":
				execution.References[0].URI = "soha://secrets/fixture"
			case "alias":
				execution.References[0].Alias = "OTHER"
			case "token":
				provider.runtimeToken = ""
			case "insecure":
				provider.endpoint = "http://runtime.example.invalid"
			}
			if scenario != "no-context" {
				ctx = domainsecret.WithExecutionContext(ctx, execution)
			}
			input := map[string]any{}
			if scenario == "oversized" {
				input["content"] = strings.Repeat(" ", 1<<20)
			}
			_, _, err := provider.InvokeTool(ctx, principal, provider.Tools()[0], input)
			if err == nil || calls.Load() != 0 {
				t.Fatal("invalid credential delivery reached the runtime action")
			}
			if scenario == "action-name" && !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatal("overlong credential action should be rejected before transport")
			}
		})
	}
}

func TestConnectorCredentialMaterialBoundedBeforeSending(t *testing.T) {
	values := make([]string, 64)
	for index := range values {
		values[index] = uuid.NewString()
	}
	material, _ := json.Marshal(values)
	for _, item := range []struct{ name, value string }{
		{"bytes", strings.Repeat("界", 22000)},
		{"material", string(material)},
		{"depth", strings.Repeat("[", 17) + `"value"` + strings.Repeat("]", 17)},
	} {
		t.Run(item.name, func(t *testing.T) {
			provider, calls := credentialTestRuntime(t, connectorProtocolForTest, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true,"output":{}}`)) })
			ctx, principal := credentialTestContext(item.value)
			_, _, err := provider.InvokeTool(ctx, principal, provider.Tools()[0], nil)
			if !errors.Is(err, apperrors.ErrInvalidArgument) || calls.Load() != 0 {
				t.Fatal("unbounded credential material reached the runtime")
			}
		})
	}
}

func TestConnectorRuntimeCredentialNilInputIsObject(t *testing.T) {
	provider, _ := credentialTestRuntime(t, connectorProtocolForTest, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if _, ok := body["input"].(map[string]any); !ok {
			t.Error("empty action input must be an object in the credential protocol")
		}
		_, _ = w.Write([]byte(`{"ok":true,"output":{}}`))
	})
	ctx, principal := credentialTestContext(uuid.NewString())
	if _, _, err := provider.InvokeTool(ctx, principal, provider.Tools()[0], nil); err != nil {
		t.Fatal(err)
	}
}

func TestConnectorCredentialErrorsAndRedirectsDoNotExposeMaterial(t *testing.T) {
	for _, mode := range []string{"http-error", "action-error", "redirect", "over-limit"} {
		t.Run(mode, func(t *testing.T) {
			secret := uuid.NewString()
			var followed atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { followed.Add(1); _, _ = fmt.Fprint(w, secret) }))
			t.Cleanup(destination.Close)
			provider, _ := credentialTestRuntime(t, connectorProtocolForTest, func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "http-error":
					http.Error(w, secret, http.StatusForbidden)
				case "action-error":
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": secret})
				case "redirect":
					http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
				case "over-limit":
					_, _ = fmt.Fprint(w, `{"ok":true,"output":{}}`+strings.Repeat(" ", 1<<20))
				}
			})
			ctx, principal := credentialTestContext(secret)
			_, _, err := provider.InvokeTool(ctx, principal, domainaigateway.ToolCapability{Name: credentialTestAction}, map[string]any{})
			if err == nil || !errors.Is(err, apperrors.ErrInvalidArgument) || strings.Contains(err.Error(), secret) || followed.Load() != 0 {
				t.Fatal("credential error or redirect disclosed material")
			}
		})
	}
}
