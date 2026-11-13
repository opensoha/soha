package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"go.uber.org/zap"
)

func TestBuildConfigRejectsInvalidSettings(t *testing.T) {
	for _, config := range []cfgpkg.LoggerConfig{
		{Level: "verbose", Format: "json"},
		{Level: "info", Format: "text"},
	} {
		if _, err := buildConfig(config); err == nil {
			t.Fatalf("buildConfig(%#v) error = nil", config)
		}
	}
}

func TestJSONLoggerWritesCanonicalFieldsInOrder(t *testing.T) {
	config, err := buildConfig(cfgpkg.LoggerConfig{Level: "info", Format: "json"})
	if err != nil {
		t.Fatalf("buildConfig() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "agent.log")
	config.OutputPaths = []string{path}
	config.ErrorOutputPaths = []string{path + ".internal"}
	log, err := buildLogger(config)
	if err != nil {
		t.Fatalf("buildLogger() error = %v", err)
	}
	log.Named("http").With(zap.String("request_id", "request-1")).Info("http request completed",
		zap.String("event", "http.request.completed"),
		zap.Float64("latency_ms", 26.952458),
		zap.Duration("duration_ms", 1500*time.Millisecond),
	)
	if err := log.Sync(); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("Unmarshal() error = %v; output = %q", err, data)
	}
	assertJSONFieldOrder(t, string(data), "timestamp", "level", "request_id", "component", "latency_ms", "service", "caller", "event", "message")
	if entry["service"] != "soha-agent" || entry["component"] != "http" || entry["duration_ms"] != float64(1500) {
		t.Fatalf("log fields = %#v", entry)
	}
}

func assertJSONFieldOrder(t *testing.T, output string, keys ...string) {
	t.Helper()
	previous := -1
	for _, key := range keys {
		index := strings.Index(output, `"`+key+`":`)
		if index < 0 || index <= previous {
			t.Fatalf("field %q missing or out of order in output %q", key, output)
		}
		previous = index
	}
}
