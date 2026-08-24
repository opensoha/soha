package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"go.uber.org/zap"
)

func TestBuildConfigUsesCommonProductionSemantics(t *testing.T) {
	for _, format := range []string{"json", "console"} {
		t.Run(format, func(t *testing.T) {
			config, err := buildConfig(cfgpkg.LoggerConfig{Level: "info", Format: format})
			if err != nil {
				t.Fatalf("buildConfig() error = %v", err)
			}
			if config.Encoding != format || config.Development || config.Sampling != nil {
				t.Fatalf("config encoding/development/sampling = %q/%t/%#v", config.Encoding, config.Development, config.Sampling)
			}
			if config.DisableCaller || config.DisableStacktrace {
				t.Fatalf("caller/stacktrace disabled = %t/%t", config.DisableCaller, config.DisableStacktrace)
			}
			if len(config.OutputPaths) != 1 || config.OutputPaths[0] != "stdout" || len(config.ErrorOutputPaths) != 1 || config.ErrorOutputPaths[0] != "stderr" {
				t.Fatalf("output paths = %#v / %#v", config.OutputPaths, config.ErrorOutputPaths)
			}
			if config.InitialFields["service"] != "soha" {
				t.Fatalf("service = %#v, want soha", config.InitialFields["service"])
			}
		})
	}
}

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

func TestJSONLoggerWritesStableFieldsAndMilliseconds(t *testing.T) {
	config, err := buildConfig(cfgpkg.LoggerConfig{Level: "info", Format: "json"})
	if err != nil {
		t.Fatalf("buildConfig() error = %v", err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "soha.log")
	config.OutputPaths = []string{path}
	config.ErrorOutputPaths = []string{path + ".internal"}
	logger, err := buildLogger(config)
	if err != nil {
		t.Fatalf("buildLogger() error = %v", err)
	}
	logger.Named("http").With(zap.String("request_id", "request-1")).Info("request completed",
		zap.String("event", "http.request.completed"),
		zap.Float64("latency_ms", 26.952458),
		zap.Duration("duration_ms", 1500*time.Millisecond),
	)
	if err := logger.Sync(); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("OpenRoot() error = %v", err)
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile("soha.log")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("Unmarshal() error = %v; output = %q", err, data)
	}
	assertJSONFieldOrder(t, string(data), "timestamp", "level", "request_id", "component", "latency_ms", "service", "caller", "event", "message")
	if entry["service"] != "soha" || entry["component"] != "http" || entry["event"] != "http.request.completed" {
		t.Fatalf("identity fields = %#v", entry)
	}
	if entry["level"] != "info" || entry["message"] != "request completed" || entry["duration_ms"] != float64(1500) {
		t.Fatalf("log fields = %#v", entry)
	}
	timestamp, ok := entry["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp = %#v, want string", entry["timestamp"])
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil || parsed.Location() != time.UTC {
		t.Fatalf("timestamp = %q, parsed = %v, error = %v", timestamp, parsed, err)
	}
}

func assertJSONFieldOrder(t *testing.T, output string, keys ...string) {
	t.Helper()
	previous := -1
	for _, key := range keys {
		index := strings.Index(output, `"`+key+`":`)
		if index < 0 {
			t.Fatalf("field %q missing from output %q", key, output)
		}
		if index <= previous {
			t.Fatalf("field %q is out of order in output %q", key, output)
		}
		previous = index
	}
}
