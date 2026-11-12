package networkproxyruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/securefile"
)

const KernelPreflightHelp = `Usage: proxy-runtime --kernel-preflight /absolute/private-input.json
Private input (kernel-preflight/v1): engine, binary, expectedSHA256,
expectedVersion, sourceRef, expectedGOOS, expectedGOARCH, configFile,
configurationRevision, configurationHash, protectionVersion.
configurationHash is sha256:<hex> of the native rendered config FILE BYTES,
not the control-plane Configuration.Digest or image/template digest.
sourceRef is an installer-redacted opaque label or registry digest reference;
URLs, query strings and credential/userinfo references are forbidden.
Verification layer: native-parser-compatibility. This does not prove guarded
data-plane compatibility, authorization or network isolation of arbitrary code.
Use explicit stop -> install approved artifact -> read live pin -> rebind -> start.
No download, live configuration/state write, or runtime start is performed.
`

type KernelPreflightInput struct {
	SchemaVersion         string `json:"schemaVersion"`
	Engine                string `json:"engine"`
	Binary                string `json:"binary"`
	ExpectedSHA256        string `json:"expectedSHA256"`
	ExpectedVersion       string `json:"expectedVersion"`
	SourceRef             string `json:"sourceRef"`
	ExpectedGOOS          string `json:"expectedGOOS"`
	ExpectedGOARCH        string `json:"expectedGOARCH"`
	ConfigFile            string `json:"configFile"`
	ConfigurationRevision int64  `json:"configurationRevision"`
	ConfigurationHash     string `json:"configurationHash"`
	ProtectionVersion     int    `json:"protectionVersion"`
}

type KernelPreflightReport struct {
	SchemaVersion         string    `json:"schemaVersion"`
	VerificationLayer     string    `json:"verificationLayer"`
	Engine                string    `json:"engine,omitempty"`
	ActualSHA256          string    `json:"actualSHA256"`
	ActualVersion         string    `json:"actualVersion"`
	ActualGOOS            string    `json:"actualGOOS"`
	ActualGOARCH          string    `json:"actualGOARCH"`
	SourceRef             string    `json:"sourceRef,omitempty"`
	ConfigurationRevision int64     `json:"configurationRevision,omitempty"`
	ConfigurationHash     string    `json:"configurationHash"`
	ProtectionVersion     int       `json:"protectionVersion,omitempty"`
	CheckedAt             time.Time `json:"checkedAt"`
	Compatible            bool      `json:"compatible"`
	Reason                string    `json:"reason"`
}

func RunKernelPreflight(ctx context.Context, inputFile string) (report KernelPreflightReport) {
	report = KernelPreflightReport{SchemaVersion: "kernel-preflight-result/v1", VerificationLayer: "native-parser-compatibility", ActualSHA256: "unknown", ActualVersion: "unknown", ConfigurationHash: "unknown", ActualGOOS: runtime.GOOS, ActualGOARCH: runtime.GOARCH, Reason: "invalid_input"}
	defer func() { report.CheckedAt = time.Now().UTC() }()
	input, err := readKernelPreflightInput(inputFile)
	if err != nil {
		return report
	}
	report.Engine, report.SourceRef = input.Engine, input.SourceRef
	report.ConfigurationRevision, report.ProtectionVersion = input.ConfigurationRevision, input.ProtectionVersion
	if input.Engine == domain.EngineExternal {
		report.Reason = "unsupported_engine"
		return report
	}
	if input.ExpectedGOOS != runtime.GOOS || input.ExpectedGOARCH != runtime.GOARCH {
		report.Reason = "platform_mismatch"
		return report
	}
	directory, err := os.MkdirTemp("", "soha-kernel-preflight-*")
	if err != nil {
		report.Reason = "staging_failed"
		return report
	}
	defer func(path string) { _ = os.RemoveAll(path) }(directory)
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		report.Reason = "staging_failed"
		return report
	}
	binary := filepath.Join(directory, "kernel")
	report.ActualSHA256, err = stageKernelCandidate(input.Binary, binary)
	if err != nil {
		report.ActualSHA256 = "unknown"
		report.Reason = "artifact_unavailable"
		return report
	}
	if report.ActualSHA256 != input.ExpectedSHA256 {
		report.Reason = "binary_hash_mismatch"
		return report
	}
	config, err := securefile.Read(input.ConfigFile, 2<<20, true)
	if err != nil {
		report.Reason = "configuration_unavailable"
		return report
	}
	report.ConfigurationHash = fmt.Sprintf("sha256:%x", sha256.Sum256(config))
	if report.ConfigurationHash != input.ConfigurationHash {
		report.Reason = "configuration_hash_mismatch"
		return report
	}
	return checkStagedKernel(ctx, input, binary, directory, config, report)
}

func readKernelPreflightInput(path string) (KernelPreflightInput, error) {
	var input KernelPreflightInput
	if !filepath.IsAbs(path) {
		return input, errors.New("private absolute input required")
	}
	raw, err := securefile.Read(path, 16<<10, true)
	if err != nil {
		return input, errors.New("private input unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return input, errors.New("invalid input")
	}
	if err := input.validate(); err != nil {
		return KernelPreflightInput{}, err
	}
	return input, nil
}

func (input KernelPreflightInput) validate() error {
	if input.SchemaVersion != "kernel-preflight/v1" || !slices.Contains([]string{domain.EngineMihomo, domain.EngineSingBox, domain.EngineV2Ray, domain.EngineExternal}, input.Engine) {
		return errors.New("invalid schema or engine")
	}
	if !filepath.IsAbs(input.Binary) || !filepath.IsAbs(input.ConfigFile) || !configurationDigestPattern.MatchString(input.ExpectedSHA256) || !configurationDigestPattern.MatchString(input.ConfigurationHash) {
		return errors.New("invalid artifact identities")
	}
	if len(input.ExpectedVersion) > 64 || !kernelVersionPattern.MatchString(input.ExpectedVersion) || !validKernelSourceRef(input.SourceRef) {
		return errors.New("invalid redacted provenance")
	}
	if input.ConfigurationRevision < 1 || input.ProtectionVersion < 1 || input.ExpectedGOOS == "" || input.ExpectedGOARCH == "" {
		return errors.New("missing compatibility identities")
	}
	return nil
}

func stageKernelCandidate(source, destination string) (string, error) {
	file, err := openKernelArtifact(source)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700) // #nosec G304 G302 -- O_EXCL copy in the private staging directory; owner-only 0700 is required to execute the verified candidate.
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(file, maxKernelBytes+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil || n > maxKernelBytes {
		return "", errors.New("candidate staging failed")
	}
	actual := fmt.Sprintf("sha256:%x", hash.Sum(nil))
	staged, err := hashKernelArtifact(destination)
	if err != nil || staged != actual {
		return "", errors.New("candidate copy mismatch")
	}
	return actual, nil
}

func checkStagedKernel(ctx context.Context, input KernelPreflightInput, binary, directory string, config []byte, report KernelPreflightReport) KernelPreflightReport {
	if !preflightConfigLocal(input.Engine, config) {
		report.Reason = "nonlocal_configuration_unsupported"
		return report
	}
	path := filepath.Join(directory, "engine.conf")
	if err := os.WriteFile(path, config, 0600); err != nil {
		report.Reason = "staging_failed"
		return report
	}
	version, err := nativeKernelVersion(ctx, binary, input.Engine, directory)
	if err != nil {
		report.Reason = "version_probe_failed"
		if errors.Is(err, context.DeadlineExceeded) {
			report.Reason = "version_probe_timeout"
		}
		return report
	}
	report.ActualVersion = version
	if version != input.ExpectedVersion {
		report.Reason = "version_mismatch"
		return report
	}
	adapter := newBuiltinEngineAdapter(Config{Engine: input.Engine}, "", nil)
	if err := nativeKernelCommand(ctx, binary, directory, adapter.commandArgs(path, true), io.Discard); err != nil {
		report.Reason = "native_check_failed"
		if errors.Is(err, context.DeadlineExceeded) {
			report.Reason = "native_check_timeout"
		}
		return report
	}
	if actual, err := hashKernelArtifact(binary); err != nil || actual != report.ActualSHA256 {
		report.Reason = "candidate_changed"
		return report
	}
	actual, err := securefile.Read(path, 2<<20, true)
	if err != nil || fmt.Sprintf("sha256:%x", sha256.Sum256(actual)) != report.ConfigurationHash {
		report.Reason = "staged_configuration_changed"
		return report
	}
	report.Compatible, report.Reason = true, "compatible"
	return report
}

func preflightConfigLocal(engine string, raw []byte) bool {
	doc, err := decodeEngineDocument(engine, raw)
	return err == nil && !hasUnmanagedDial(doc) && !preflightExternalReference(doc)
}

func preflightExternalReference(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			if slices.Contains([]string{"proxy-providers", "rule-providers", "rule_set", "geoip", "geosite", "geox-url", "external-ui", "external-ui-url", "certificate_path", "key_path", "certificate-path", "private-key-path", "certificateFile", "keyFile", "cache_file", "cache-file"}, key) && nonemptySection(item) {
				return true
			}
			if (key == "rules" || key == "ip" || key == "domain") && preflightGeodataReference(item) {
				return true
			}
			if key == "log" && preflightExternalLog(item) {
				return true
			}
			if preflightExternalReference(item) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if preflightExternalReference(item) {
				return true
			}
		}

	}
	return false
}

func preflightGeodataReference(value any) bool {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			if preflightGeodataReference(item) {
				return true
			}
		}
	case string:
		upper := strings.ToUpper(value)
		for _, prefix := range []string{"GEOIP:", "GEOSITE:", "EXT:", "GEOIP,", "GEOSITE,", "RULE-SET,"} {
			if strings.HasPrefix(upper, prefix) {
				return true
			}
		}
	}
	return false
}
func preflightExternalLog(value any) bool {
	log, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"access", "error", "output"} {
		path, _ := log[key].(string)
		clean := filepath.Clean(path)
		if filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
