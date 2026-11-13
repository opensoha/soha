package sohacli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestTokenRevokeTrailingProfileNeverUsesCurrentInstance(t *testing.T) {
	t.Setenv("SOHA_TOKEN", "")
	t.Setenv("SOHA_SERVER", "")
	for _, command := range [][]string{{"token", "revoke"}, {"service-account", "token-revoke"}} {
		t.Run(command[0], func(t *testing.T) {
			var wrong, selected atomic.Int32
			current := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { wrong.Add(1); w.WriteHeader(403) }))
			defer current.Close()
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				selected.Add(1)
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer selected-token" {
					t.Error("revocation used wrong method or credential")
				}
				writeJSON(t, w, map[string]any{"status": "ok"})
			}))
			defer target.Close()
			path := writeTestConfig(t, current.URL)
			cfg, err := loadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Profiles["selected"] = ProfileConfig{ServerURL: target.URL, AccessToken: "selected-token"}
			if err := saveConfig(path, cfg); err != nil {
				t.Fatal(err)
			}
			for _, arguments := range [][]string{
				{"owned-token", "--profile", "selected"},
				{"--profile", "selected", "owned-token"},
				{"--id", "owned-token", "--profile", "selected"},
			} {
				args := append(append([]string{}, command...), arguments...)
				var output, diagnostic bytes.Buffer
				if code := Run(context.Background(), args, Runtime{Out: &output, Err: &diagnostic, ConfigPath: path}); code != 0 {
					t.Fatalf("documented profile position rejected: %v", diagnostic.String())
				}
			}
			if wrong.Load() != 0 || selected.Load() != 3 {
				t.Fatalf("wrong/current instance requests=%d selected=%d", wrong.Load(), selected.Load())
			}
		})
	}
}

func TestTokenRevokeRejectsUnparsedArgumentsBeforeAnyRequest(t *testing.T) {
	t.Setenv("SOHA_TOKEN", "")
	t.Setenv("SOHA_SERVER", "")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	defer server.Close()
	path := writeTestConfig(t, server.URL)
	for _, command := range [][]string{{"token", "revoke"}, {"service-account", "token-revoke"}} {
		for _, arguments := range [][]string{{"owned-token", "--unknown-profile", "dev"}, {"owned-token", "extra", "--profile", "dev"}} {
			args := append(append([]string{}, command...), arguments...)
			if code := Run(context.Background(), args, Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ConfigPath: path}); code == 0 {
				t.Fatal("unparsed mutation arguments were ignored")
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid arguments reached an instance")
	}
}
