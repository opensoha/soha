package networkproxyruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/opensoha/soha/internal/platform/securefile"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func TestApplyRestoresRunningConfiguration(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		readback bool
		reason   string
	}{
		{name: "start failure", reason: "start_failed"},
		{name: "readback changed", readback: true, reason: "readback_failed"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			stateDir := t.TempDir()
			configFile := filepath.Join(stateDir, "engine.yaml")
			old := []byte("port: 7890\n")
			if err := os.WriteFile(configFile, old, 0600); err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(stateDir, "fake-mihomo")
			// #nosec G306 -- executable fixture in a private test directory, containing no secrets.
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$1\" = \"-t\" ]; then exit 0; fi\nexec sleep 60\n"), 0700); err != nil {
				t.Fatal(err)
			}
			var newConfigProbes atomic.Int32
			controller := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				content, err := securefile.Read(configFile, 2<<20, true)
				if err != nil {
					writer.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if strings.Contains(string(content), "port: 7891") {
					newConfigProbes.Add(1)
					if !scenario.readback {
						writer.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					if err := os.WriteFile(configFile, []byte("tampered"), 0600); err != nil {
						t.Errorf("tamper config: %v", err)
					}
				}
				_, _ = fmt.Fprint(writer, `{"version":"test"}`)
			}))
			defer controller.Close()
			config := Config{Engine: domain.EngineMihomo, EngineBinary: binary, ConfigFile: configFile,
				StateDir: stateDir, ControllerURL: controller.URL}
			engine := newEngine(config, "a-secret-for-tests")
			defer engine.stop()
			if err := engine.start(context.Background()); err != nil {
				t.Fatalf("start old engine: %v", err)
			}
			result := engine.apply(context.Background(), domain.Configuration{Revision: 2, Engine: domain.EngineMihomo,
				Enabled: true, Content: "port: 7891\n"})
			if result.Status != "rolled-back" || result.ReasonCode != scenario.reason {
				t.Fatalf("apply result = %+v", result)
			}
			if newConfigProbes.Load() == 0 {
				t.Fatal("new configuration was never probed")
			}
			got, err := securefile.Read(configFile, 2<<20, true)
			if err != nil || string(got) != string(old) {
				t.Fatalf("restored configuration = %q, %v", got, err)
			}
			if err := engine.health(context.Background()); err != nil {
				t.Fatalf("old engine did not recover: %v", err)
			}
		})
	}
}

func TestRunnerRestartsExitedEngine(t *testing.T) {
	stateDir := t.TempDir()
	configFile := filepath.Join(stateDir, "engine.yaml")
	if err := os.WriteFile(configFile, []byte("port: 7890\n"), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(stateDir, "fake-mihomo")
	// #nosec G306 -- executable fixture in a private test directory, containing no secrets.
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	controller := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(writer, `{"version":"test"}`)
	}))
	defer controller.Close()
	config := Config{Engine: domain.EngineMihomo, EngineBinary: binary, ConfigFile: configFile,
		StateDir: stateDir, ControllerURL: controller.URL}
	runner := &Runner{enabled: true, engine: newEngine(config, "a-secret-for-tests")}
	defer runner.engine.stop()
	runner.ensureRunning(context.Background())
	if runner.engine.process == nil {
		t.Fatal("engine did not start")
	}
	firstPID := runner.engine.process.Process.Pid
	if err := runner.engine.process.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runner.engine.wait:
		runner.engine.wait <- err
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not exit")
	}
	runner.ensureRunning(context.Background())
	if runner.engine.process == nil || runner.engine.process.Process.Pid == firstPID {
		t.Fatal("runner did not restart the exited engine")
	}
}
