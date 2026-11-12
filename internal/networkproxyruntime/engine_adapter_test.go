package networkproxyruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func TestBuiltinEngineAdapterCompatibility(t *testing.T) {
	for _, test := range []struct {
		engine, content, renderedMarker string
		check, run                      []string
		close                           bool
	}{
		{domain.EngineMihomo, "mixed-port: 7890\nrules: []\n", "external-controller: 127.0.0.1:9090", []string{"-d", ".", "-t", "-f", "engine.conf"}, []string{"-d", ".", "-f", "engine.conf"}, true},
		{domain.EngineSingBox, `{"inbounds":[]}`, `"external_controller":"127.0.0.1:9090"`, []string{"check", "-c", "engine.conf"}, []string{"run", "-c", "engine.conf"}, true},
		{domain.EngineV2Ray, `{"inbounds":[]}`, `"services":["StatsService"]`, []string{"test", "-format=json", "-config", "engine.conf"}, []string{"run", "-format=json", "-config", "engine.conf"}, false},
	} {
		t.Run(test.engine, func(t *testing.T) {
			e := newEngine(Config{Engine: test.engine, EngineBinary: "operator-binary", ControllerURL: "http://127.0.0.1:9090"}, "operator-controller-secret")
			for _, command := range []struct {
				check bool
				want  []string
			}{{true, test.check}, {false, test.run}} {
				got := e.command("engine.conf", command.check).Args
				want := append([]string{"operator-binary"}, command.want...)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("argv = %v; want %v", got, want)
				}
			}
			raw, err := e.render(test.content)
			if err != nil || !strings.Contains(string(raw), test.renderedMarker) {
				t.Fatalf("render = %s, %v", raw, err)
			}
			if e.adapter.supportsClose() != test.close {
				t.Fatal("close support changed")
			}
			if !test.close {
				status, err := e.close(context.Background(), "connection")
				if status != "failed" || err == nil {
					t.Fatal("unsupported close accepted")
				}
			}
			if _, err := e.render("invalid: ["); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestBuiltinClashAdapterObservation(t *testing.T) {
	for _, engineID := range []string{domain.EngineMihomo, domain.EngineSingBox} {
		t.Run(engineID, func(t *testing.T) {
			failing := ""
			server := clashObservationFixture(t, &failing)
			defer server.Close()
			e := newEngine(Config{Engine: engineID, ControllerURL: server.URL}, "operator-secret")
			sample, connections := e.adapter.observe(context.Background())
			if sample.Health != "healthy" || sample.EngineVersion != "fixture-version" || sample.UploadTotal != 123 || sample.DownloadTotal != 456 || sample.ActiveConnections == nil || *sample.ActiveConnections != 0 || connections == nil {
				t.Fatalf("unexpected sample %+v, connections %v", sample, connections)
			}
			if slices.Contains(sample.Capabilities, "protection_rules_readback") != (engineID == domain.EngineMihomo) {
				t.Fatal("rules readback support changed")
			}
			if status, err := e.close(context.Background(), "connection"); status != "closed" || err != nil {
				t.Fatalf("close = %s, %v", status, err)
			}
			for _, test := range []struct{ path, reason, version string }{{"/version", "controller_unavailable", "unknown"}, {"/connections", "traffic_unavailable", "fixture-version"}} {
				failing = test.path
				sample, connections = e.adapter.observe(context.Background())
				if sample.Health != "degraded" || sample.ReasonCode != test.reason || sample.EngineVersion != test.version || len(sample.Capabilities) != 0 || connections != nil {
					t.Fatalf("failure sample %+v", sample)
				}
			}
		})
	}
}

func TestBuiltinV2RayAdapterObservation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX script")
	}
	binary := filepath.Join(t.TempDir(), "v2ray-fixture")
	fixture := "#!/bin/sh\ncase \"$1\" in\nversion) echo 'V2Ray 5.fixture' ;;\napi) echo '{\"stat\":[{\"name\":\"inbound>>>proxy>>>traffic>>>uplink\",\"value\":\"123\"},{\"name\":\"inbound>>>proxy>>>traffic>>>downlink\",\"value\":\"456\"}]}' ;;\n*) exit 1 ;;\nesac\n"
	if err := os.WriteFile(binary, []byte(fixture), 0700); err != nil { // #nosec G306 -- owner-only POSIX executable fixture in t.TempDir.
		t.Fatal(err)
	}
	e := newEngine(Config{Engine: domain.EngineV2Ray, EngineBinary: binary, ControllerURL: "http://127.0.0.1:9090"}, "")
	sample, connections := e.adapter.observe(context.Background())
	if sample.Health != "healthy" || sample.EngineVersion != "5.fixture" || sample.UploadTotal != 123 || sample.DownloadTotal != 456 || sample.ActiveConnections != nil || connections != nil || !reflect.DeepEqual(sample.Capabilities, []string{"traffic", "protected_destinations_v1", "guarded_dns_tcp_v1"}) {
		t.Fatalf("unexpected sample %+v", sample)
	}
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	sample, _ = e.adapter.observe(context.Background())
	if sample.Health != "degraded" || sample.ReasonCode != "stats_unavailable" || len(sample.Capabilities) != 0 {
		t.Fatalf("unexpected failure sample %+v", sample)
	}
}

func clashObservationFixture(t *testing.T, failing *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer operator-secret" {
			t.Error("controller secret missing")
		}
		if r.URL.Path == *failing {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/version":
			_, _ = fmt.Fprint(w, `{"version":"fixture-version"}`)
		case "/connections":
			_, _ = fmt.Fprint(w, `{"uploadTotal":123,"downloadTotal":456,"connections":[]}`)
		case "/connections/connection":
			if r.Method != http.MethodDelete {
				t.Error("unexpected close method")
			}
			_, _ = fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected controller path %s", r.URL.Path)
		}
	}))
}
