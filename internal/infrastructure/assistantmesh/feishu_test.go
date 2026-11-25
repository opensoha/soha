package assistantmesh

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domaindirectory "github.com/opensoha/soha/internal/domain/directorysync"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type feishuDirectoryRepoStub struct {
	connection    domaindirectory.Connection
	policy        domaindirectory.Policy
	getErr        error
	userID        string
	resolveErr    error
	resolvedOpen  string
	resolvedConn  string
	resolvedLogin string
	calls         int
}

func (s *feishuDirectoryRepoStub) GetConnection(context.Context, string) (domaindirectory.Connection, domaindirectory.Policy, error) {
	return s.connection, s.policy, s.getErr
}

func (s *feishuDirectoryRepoStub) ResolveActiveFeishuOpenID(_ context.Context, connectionID, loginProviderID, openID string) (string, error) {
	s.calls++
	s.resolvedConn, s.resolvedLogin, s.resolvedOpen = connectionID, loginProviderID, openID
	return s.userID, s.resolveErr
}

type feishuProviderStub struct {
	provider domainsettings.LoginProviderSettings
	err      error
}

func (s feishuProviderStub) ResolveLoginProvider(context.Context, string) (domainsettings.LoginProviderSettings, error) {
	return s.provider, s.err
}

func TestFeishuDirectoryRequiresCurrentFeishuConnectionAndProvider(t *testing.T) {
	repo := &feishuDirectoryRepoStub{
		connection: domaindirectory.Connection{ID: "conn-1", ProviderType: domaindirectory.ProviderFeishu, LoginProviderID: "lp-1", Enabled: true},
		policy:     domaindirectory.Policy{SyncPeople: true},
		userID:     "user-1",
	}
	providers := feishuProviderStub{provider: domainsettings.LoginProviderSettings{ID: "lp-1", Type: "feishu", Enabled: true, ClientID: "cli_app"}}
	directory := NewFeishuDirectory(repo, providers, "conn-1", "cli_app")

	userID, err := directory.ResolveFeishuUser(context.Background(), "ou_open")
	if err != nil || userID != "user-1" {
		t.Fatalf("ResolveFeishuUser() = %q, %v", userID, err)
	}
	if repo.resolvedConn != "conn-1" || repo.resolvedLogin != "lp-1" || repo.resolvedOpen != "ou_open" {
		t.Fatalf("resolved (%q, %q, %q)", repo.resolvedConn, repo.resolvedLogin, repo.resolvedOpen)
	}
}

func TestFeishuDirectoryRejectsStaleBindings(t *testing.T) {
	validConnection := domaindirectory.Connection{ID: "conn-1", ProviderType: domaindirectory.ProviderFeishu, LoginProviderID: "lp-1", Enabled: true}
	validProvider := domainsettings.LoginProviderSettings{ID: "lp-1", Type: "feishu", Enabled: true, ClientID: "cli_app"}
	tests := []struct {
		name       string
		connection domaindirectory.Connection
		policy     domaindirectory.Policy
		provider   domainsettings.LoginProviderSettings
	}{
		{name: "disabled connection", connection: func() domaindirectory.Connection { c := validConnection; c.Enabled = false; return c }(), policy: domaindirectory.Policy{SyncPeople: true}, provider: validProvider},
		{name: "people sync off", connection: validConnection, policy: domaindirectory.Policy{SyncPeople: false}, provider: validProvider},
		{name: "disabled provider", connection: validConnection, policy: domaindirectory.Policy{SyncPeople: true}, provider: func() domainsettings.LoginProviderSettings { p := validProvider; p.Enabled = false; return p }()},
		{name: "app mismatch", connection: validConnection, policy: domaindirectory.Policy{SyncPeople: true}, provider: func() domainsettings.LoginProviderSettings { p := validProvider; p.ClientID = "cli_other"; return p }()},
		{name: "not feishu provider", connection: validConnection, policy: domaindirectory.Policy{SyncPeople: true}, provider: func() domainsettings.LoginProviderSettings { p := validProvider; p.Type = "oidc"; return p }()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &feishuDirectoryRepoStub{connection: tt.connection, policy: tt.policy, userID: "user-1"}
			directory := NewFeishuDirectory(repo, feishuProviderStub{provider: tt.provider}, "conn-1", "cli_app")
			if _, err := directory.ResolveFeishuUser(context.Background(), "ou_open"); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
			if repo.calls != 0 {
				t.Fatal("repository resolution must not run for stale bindings")
			}
		})
	}
}

func TestFeishuSenderSendsFixedEnvelopeAndNeutralisesMentions(t *testing.T) {
	requestID := "heya-" + strings.Repeat("a", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != feishuActionPath {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer runtime-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var envelope feishuActionEnvelope
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Errorf("decode envelope: %v", err)
		}
		if envelope.Action != "feishu.message.send_text" || envelope.Input.ReceiveIDType != "open_id" || envelope.Input.ReceiveID != "ou_user" {
			t.Errorf("envelope = %#v", envelope)
		}
		if envelope.RequestID != requestID || envelope.Event.ID != requestID || envelope.Event.Type != "assistant_mesh_delivery" || envelope.Event.Source != "feishu" {
			t.Errorf("event envelope = %#v", envelope)
		}
		if envelope.Event.Payload["appId"] != "cli_app" {
			t.Errorf("appId = %#v", envelope.Event.Payload["appId"])
		}
		if _, err := time.Parse(time.RFC3339, envelope.Event.OccurredAt); err != nil {
			t.Errorf("occurredAt = %q", envelope.Event.OccurredAt)
		}
		if strings.Contains(envelope.Input.Text, "<at") || !strings.Contains(envelope.Input.Text, "<\u200b") {
			t.Errorf("text = %q", envelope.Input.Text)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	sender, err := NewFeishuSender(server.URL, "runtime-token", "cli_app")
	if err != nil {
		t.Fatal(err)
	}
	retryable, err := sender.SendFeishuText(context.Background(), "open_id", "ou_user", requestID, `hello <at user_id="all">everyone</at>`)
	if err != nil || retryable {
		t.Fatalf("SendFeishuText() = %v, %v", retryable, err)
	}
}

func TestFeishuSenderClassifiesFailuresWithoutLeakingBody(t *testing.T) {
	requestID := "heya-" + strings.Repeat("b", 32)
	tests := []struct {
		name      string
		status    int
		body      string
		retryable bool
	}{
		{name: "known 4xx", status: http.StatusBadRequest, body: `{"ok":false,"error":{"code":"bad_request","message":"private-body-marker","retryable":false}}`},
		{name: "explicit retry", status: http.StatusBadRequest, body: `{"ok":false,"error":{"code":"temporary","message":"private-body-marker","retryable":true}}`, retryable: true},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{}`, retryable: true},
		{name: "server error", status: http.StatusServiceUnavailable, body: `{"ok":false}`, retryable: true},
		{name: "decode error", status: http.StatusOK, body: `{`, retryable: true},
		{name: "ok false", status: http.StatusOK, body: `{"ok":false,"error":{"code":"rejected","retryable":false}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			sender, err := NewFeishuSender(server.URL, "runtime-token", "cli_app")
			if err != nil {
				t.Fatal(err)
			}
			retryable, err := sender.SendFeishuText(context.Background(), "chat_id", "oc_chat", requestID, "hello")
			if err == nil || retryable != tt.retryable {
				t.Fatalf("SendFeishuText() = %v, %v; want retryable %v", retryable, err, tt.retryable)
			}
			if strings.Contains(err.Error(), "private-body-marker") {
				t.Fatalf("error leaked response body: %v", err)
			}
		})
	}
}

func TestFeishuSenderRejectsRedirects(t *testing.T) {
	var followed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		followed.Add(1)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	sender, err := NewFeishuSender(redirect.URL, "runtime-token", "cli_app")
	if err != nil {
		t.Fatal(err)
	}
	retryable, err := sender.SendFeishuText(context.Background(), "open_id", "ou_user", "heya-"+strings.Repeat("c", 32), "hello")
	if err == nil || retryable {
		t.Fatalf("SendFeishuText() = %v, %v; want non-retryable redirect error", retryable, err)
	}
	if followed.Load() != 0 {
		t.Fatal("redirect target was followed")
	}
}

func TestNewFeishuSenderValidatesEndpoint(t *testing.T) {
	tests := []string{
		"http://example.com",
		"https://user:pass@example.com",
		"https://example.com/?query=1",
		"https://example.com/#fragment",
		"ftp://example.com",
	}
	for _, endpoint := range tests {
		if _, err := NewFeishuSender(endpoint, "token", "cli_app"); err == nil {
			t.Fatalf("NewFeishuSender(%q) error = nil", endpoint)
		}
	}
	if _, err := NewFeishuSender("https://example.com", "token", "cli_app"); err != nil {
		t.Fatalf("valid endpoint rejected: %v", err)
	}
}

func TestFeishuSenderValidatesInput(t *testing.T) {
	sender, err := NewFeishuSender("https://example.com", "token", "cli_app")
	if err != nil {
		t.Fatal(err)
	}
	requestID := "heya-" + strings.Repeat("d", 32)
	for _, tt := range []struct{ idType, recipient, requestID string }{
		{"user_id", "u_1", requestID},
		{"open_id", "oc_chat", requestID},
		{"chat_id", "ou_user", requestID},
		{"chat_id", "oc_chat", "heya-ABC"},
	} {
		if _, err := sender.SendFeishuText(context.Background(), tt.idType, tt.recipient, tt.requestID, "hello"); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("SendFeishuText(%q, %q, %q) error = %v", tt.idType, tt.recipient, tt.requestID, err)
		}
	}
}
