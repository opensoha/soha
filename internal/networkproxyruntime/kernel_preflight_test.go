package networkproxyruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func preflightFixture(t *testing.T, engine string) (KernelPreflightInput, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "candidate")
	config := filepath.Join(dir, "live.conf")
	marker := filepath.Join(dir, "execution.log")
	version, line, versionFlag, check := "v1.19.30", "Mihomo Meta v1.19.30 linux amd64", "-v", `[ "$1" = -d ] && [ "$2" = "$PWD" ] && [ "$3" = -t ] && [ "$4" = -f ]; config="$5"`
	content := "secret: native-config-secret\nport: 0\nrules: []\n"
	if engine == domain.EngineSingBox {
		version, line, versionFlag, check = "1.14.2", "sing-box version 1.14.2", "version", `[ "$1" = check ] && [ "$2" = -c ]; config="$3"`
	}
	if engine == domain.EngineV2Ray {
		version, line, versionFlag, check = "5.53.0", "V2Ray 5.53.0 Custom", "version", `[ "$1" = test ] && [ "$2" = -format=json ] && [ "$3" = -config ]; config="$4"`
	}
	if engine != domain.EngineMihomo {
		content = `{"inbounds":[],"outbounds":[],"secret":"native-config-secret"}`
	}
	script := "#!/bin/sh\nset -eu\nif [ \"$1\" = " + quoteKernelFixture(versionFlag) + " ]; then\n echo version >> " + quoteKernelFixture(marker) + "\n echo " + quoteKernelFixture(line) + "\n exit 0\nfi\n" + check + "\ncase \"$config\" in \"$PWD\"/*) ;; *) exit 7 ;; esac\n[ \"$HOME\" = \"$PWD\" ]\n[ \"$XDG_CONFIG_HOME\" = \"$PWD\" ]\necho check >> " + quoteKernelFixture(marker) + "\necho parser-private-output >&2\necho candidate-only > \"$HOME/native-state\"\nexit 0\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	input := KernelPreflightInput{SchemaVersion: "kernel-preflight/v1", Engine: engine, Binary: binary, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(script))), ExpectedVersion: version, SourceRef: "installer-release-20261108", ExpectedGOOS: runtime.GOOS, ExpectedGOARCH: runtime.GOARCH, ConfigFile: config, ConfigurationRevision: 17, ConfigurationHash: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content))), ProtectionVersion: 3}
	return input, marker, dir
}

func quoteKernelFixture(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
func writePreflightInput(t *testing.T, input KernelPreflightInput) string {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKernelPreflightBuiltinCompatibilityAndLiveFilesUnchanged(t *testing.T) {
	for _, engine := range []string{domain.EngineMihomo, domain.EngineSingBox, domain.EngineV2Ray} {
		t.Run(engine, func(t *testing.T) {
			input, marker, dir := preflightFixture(t, engine)
			before, err := os.ReadFile(input.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			info, _ := os.Stat(input.ConfigFile)
			report := RunKernelPreflight(context.Background(), writePreflightInput(t, input))
			if !report.Compatible || report.Reason != "compatible" || report.ActualSHA256 != input.ExpectedSHA256 || report.ActualVersion != input.ExpectedVersion || report.ConfigurationHash != input.ConfigurationHash || report.ConfigurationRevision != 17 || report.ProtectionVersion != 3 || report.VerificationLayer != "native-parser-compatibility" {
				t.Fatalf("report %+v", report)
			}
			after, _ := os.ReadFile(input.ConfigFile)
			nowInfo, _ := os.Stat(input.ConfigFile)
			if string(before) != string(after) || !info.ModTime().Equal(nowInfo.ModTime()) || info.Mode() != nowInfo.Mode() {
				t.Fatal("live configuration changed")
			}
			if _, err := os.Stat(filepath.Join(dir, "native-state")); !os.IsNotExist(err) {
				t.Fatal("native parser wrote into live artifact directory")
			}
			calls, _ := os.ReadFile(marker)
			if string(calls) != "version\ncheck\n" {
				t.Fatalf("native commands %q", calls)
			}
			raw, _ := json.Marshal(report)
			if strings.Contains(string(raw), "native-config-secret") || strings.Contains(string(raw), "parser-private-output") || strings.Contains(string(raw), input.Binary) || strings.Contains(string(raw), input.ConfigFile) {
				t.Fatal("report exposed private material")
			}
		})
	}
}

func TestKernelPreflightRejectsUnapprovedInputsBeforeExecution(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		mutate       func(*KernelPreflightInput)
	}{
		{"binary hash", "binary_hash_mismatch", func(i *KernelPreflightInput) { i.ExpectedSHA256 = "sha256:" + strings.Repeat("0", 64) }},
		{"config hash", "configuration_hash_mismatch", func(i *KernelPreflightInput) { i.ConfigurationHash = "sha256:" + strings.Repeat("0", 64) }},
		{"platform", "platform_mismatch", func(i *KernelPreflightInput) { i.ExpectedGOARCH = "other" }},
		{"secret provenance", "invalid_input", func(i *KernelPreflightInput) {
			i.SourceRef = "https://user:provenance-secret@example.invalid?token=private"
		}},
		{"missing hash", "invalid_input", func(i *KernelPreflightInput) { i.ExpectedSHA256 = "" }},
		{"external", "unsupported_engine", func(i *KernelPreflightInput) { i.Engine = domain.EngineExternal }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, marker, _ := preflightFixture(t, domain.EngineMihomo)
			test.mutate(&input)
			report := RunKernelPreflight(context.Background(), writePreflightInput(t, input))
			if report.Compatible || report.Reason != test.reason {
				t.Fatalf("report %+v", report)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("unapproved input executed")
			}
			raw, _ := json.Marshal(report)
			if strings.Contains(string(raw), "provenance-secret") {
				t.Fatal("invalid input leaked")
			}
		})
	}
}

func TestKernelPreflightVersionMismatchTimeoutAndNativeFailureAreRedacted(t *testing.T) {
	for _, mode := range []string{"wrong-version", "timeout", "bad-parser", "large-output"} {
		t.Run(mode, func(t *testing.T) {
			input, _, _ := preflightFixture(t, domain.EngineMihomo)
			ctx := context.Background()
			reason := "version_mismatch"
			switch mode {
			case "wrong-version":
				input.ExpectedVersion = "v9.0.0"
			case "timeout":
				ctxWithDeadline, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
				defer cancel()
				ctx = ctxWithDeadline
				reason = "version_probe_timeout"
				replacePreflightScript(t, &input, "#!/bin/sh\necho secret-timeout >&2\nsleep 20\n")
			case "bad-parser":
				reason = "native_check_failed"
				replacePreflightScript(t, &input, "#!/bin/sh\nif [ \"$1\" = -v ]; then echo 'Mihomo Meta v1.19.30'; exit 0; fi\necho native-failure-secret\necho native-failure-secret >&2\nexit 5\n")
			case "large-output":
				reason = "version_probe_failed"
				replacePreflightScript(t, &input, "#!/bin/sh\necho 'Mihomo Meta v1.19.30'\nhead -c 5000 /dev/zero\n")
			}
			report := RunKernelPreflight(ctx, writePreflightInput(t, input))
			if report.Compatible || report.Reason != reason {
				t.Fatalf("report %+v", report)
			}
			raw, _ := json.Marshal(report)
			if strings.Contains(string(raw), "secret-timeout") || strings.Contains(string(raw), "native-failure-secret") {
				t.Fatal("native output escaped redaction")
			}
		})
	}
}

func replacePreflightScript(t *testing.T, input *KernelPreflightInput, script string) {
	t.Helper()
	if err := os.WriteFile(input.Binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	input.ExpectedSHA256 = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(script)))
}

func TestKernelPreflightRejectsUnsafeFilesAndUnknownFields(t *testing.T) {
	input, marker, _ := preflightFixture(t, domain.EngineMihomo)
	link := filepath.Join(t.TempDir(), "candidate-link")
	if err := os.Symlink(input.Binary, link); err != nil {
		t.Fatal(err)
	}
	input.Binary = link
	if report := RunKernelPreflight(context.Background(), writePreflightInput(t, input)); report.Reason != "artifact_unavailable" {
		t.Fatalf("symlink report %+v", report)
	}
	input, _, _ = preflightFixture(t, domain.EngineMihomo)
	if err := os.Chmod(input.Binary, 0777); err != nil {
		t.Fatal(err)
	}
	if report := RunKernelPreflight(context.Background(), writePreflightInput(t, input)); report.Reason != "artifact_unavailable" {
		t.Fatalf("unsafe mode report %+v", report)
	}
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":"kernel-preflight/v1","unknown":"secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if report := RunKernelPreflight(context.Background(), path); report.Reason != "invalid_input" {
		t.Fatal("unknown field accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("unsafe candidate executed")
	}
}

func TestKernelArtifactDiagnosticsMeasuredAndDriftFailsClosed(t *testing.T) {
	input, _, _ := preflightFixture(t, domain.EngineMihomo)
	e := newEngine(Config{Engine: domain.EngineMihomo, EngineBinary: input.Binary, KernelSourceRef: input.SourceRef}, "")
	e.measureKernelArtifact(context.Background())
	diagnostics := e.kernelArtifactDiagnostics()
	raw, _ := json.Marshal(diagnostics)
	if !strings.Contains(string(raw), input.ExpectedSHA256) || !strings.Contains(string(raw), input.ExpectedVersion) || !strings.Contains(string(raw), "installer-owned-local-binary") {
		t.Fatalf("diagnostics %s", raw)
	}
	if err := os.WriteFile(input.Binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if e.verifyKernelArtifactDrift() == nil {
		t.Fatal("changed running artifact remained healthy")
	}
	raw, _ = json.Marshal(e.kernelArtifactDiagnostics())
	if strings.Contains(string(raw), input.ExpectedSHA256) || !strings.Contains(string(raw), "unknown") {
		t.Fatalf("stale artifact claimed %s", raw)
	}
}

func TestKernelPreflightPrivateBoundsAndNonlocalConfig(t *testing.T) {
	input, marker, _ := preflightFixture(t, domain.EngineMihomo)
	path := writePreflightInput(t, input)
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if report := RunKernelPreflight(context.Background(), path); report.Reason != "invalid_input" {
		t.Fatalf("public input accepted %+v", report)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", (16<<10)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if report := RunKernelPreflight(context.Background(), path); report.Reason != "invalid_input" {
		t.Fatal("oversized input accepted")
	}
	content := []byte(strings.Repeat("x", (2<<20)+1))
	if err := os.WriteFile(input.ConfigFile, content, 0600); err != nil {
		t.Fatal(err)
	}
	input.ConfigurationHash = fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	if report := RunKernelPreflight(context.Background(), writePreflightInput(t, input)); report.Reason != "configuration_unavailable" {
		t.Fatalf("oversized config accepted %+v", report)
	}
	content = []byte("rules: [GEOIP,CN,DIRECT]\nproxy-providers: {remote: {url: https://private.invalid}}\n")
	if err := os.WriteFile(input.ConfigFile, content, 0600); err != nil {
		t.Fatal(err)
	}
	input.ConfigurationHash = fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	if report := RunKernelPreflight(context.Background(), writePreflightInput(t, input)); report.Reason != "nonlocal_configuration_unsupported" {
		t.Fatalf("remote bootstrap accepted %+v", report)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("unsafe configuration executed native binary")
	}
}
