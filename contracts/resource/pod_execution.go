package resource

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	PodExecutionProtocolVersion        = "1"
	PodExecDefaultTimeoutSeconds int64 = 10
	PodExecMaxTimeoutSeconds     int64 = 300
	PodExecDefaultOutputBytes    int64 = 64 << 10
	PodArgvMaxOutputBytes        int64 = 1 << 20
	PodExecMaxStdinBytes               = 64 << 10
)

// PodArgvInput never adds a shell. A caller may explicitly select a shell in
// argv, under the same exec authorization and immutable execution receipt.
type PodArgvInput struct {
	ExpectedUID    string   `json:"expectedUid"`
	Container      string   `json:"container,omitempty"`
	Argv           []string `json:"argv"`
	StdinBase64    string   `json:"stdinBase64,omitempty"`
	TimeoutSeconds int64    `json:"timeoutSeconds,omitempty"`
	MaxOutputBytes int64    `json:"maxOutputBytes,omitempty"`
}

func (input PodArgvInput) Normalized() (PodArgvInput, error) {
	if input.ExpectedUID == "" || len(input.ExpectedUID) > 256 || strings.IndexFunc(input.ExpectedUID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return PodArgvInput{}, fmt.Errorf("a reviewed Pod UID of at most 256 bytes without whitespace or controls is required")
	}
	if input.Container != "" && len(validation.IsDNS1123Label(input.Container)) > 0 {
		return PodArgvInput{}, fmt.Errorf("container must be one valid Kubernetes container name")
	}
	if len(input.Argv) < 1 || len(input.Argv) > 64 || strings.TrimSpace(input.Argv[0]) == "" {
		return PodArgvInput{}, fmt.Errorf("argv requires one executable and at most 64 arguments")
	}
	total := 0
	for _, arg := range input.Argv {
		if len(arg) > 8192 || strings.ContainsRune(arg, 0) {
			return PodArgvInput{}, fmt.Errorf("argv values must be bounded and contain no NUL")
		}
		total += len(arg)
		if total > 32768 {
			return PodArgvInput{}, fmt.Errorf("argv exceeds the 32768 byte budget")
		}
	}
	if input.TimeoutSeconds < 0 || input.TimeoutSeconds > PodExecMaxTimeoutSeconds || input.MaxOutputBytes < 0 || input.MaxOutputBytes > PodArgvMaxOutputBytes {
		return PodArgvInput{}, fmt.Errorf("exec timeout or combined output budget is outside the supported bounds")
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = PodExecDefaultTimeoutSeconds
	}
	if input.MaxOutputBytes == 0 {
		input.MaxOutputBytes = PodExecDefaultOutputBytes
	}
	if len(input.StdinBase64) > base64.StdEncoding.EncodedLen(PodExecMaxStdinBytes) {
		return PodArgvInput{}, fmt.Errorf("stdin exceeds the 65536 byte budget")
	}
	stdin, err := base64.StdEncoding.Strict().DecodeString(input.StdinBase64)
	if err != nil || len(stdin) > PodExecMaxStdinBytes || base64.StdEncoding.EncodeToString(stdin) != input.StdinBase64 {
		return PodArgvInput{}, fmt.Errorf("stdin must be bounded standard base64")
	}
	input.StdinBase64 = base64.StdEncoding.EncodeToString(stdin)
	input.Argv = append([]string(nil), input.Argv...)
	return input, nil
}

type PodExecutionOutput struct {
	Status                 string `json:"status"`
	ExitCode               *int64 `json:"exitCode,omitempty"`
	Stdout                 string `json:"stdout"`
	Stderr                 string `json:"stderr"`
	StdoutEncoding         string `json:"stdoutEncoding"`
	StderrEncoding         string `json:"stderrEncoding"`
	StdoutBytes            int64  `json:"stdoutBytes"`
	StderrBytes            int64  `json:"stderrBytes"`
	MaxOutputBytes         int64  `json:"maxOutputBytes"`
	StdoutTruncated        bool   `json:"stdoutTruncated"`
	StderrTruncated        bool   `json:"stderrTruncated"`
	UIDVerifiedBeforeStart bool   `json:"uidVerifiedBeforeStart"`
	TargetChanged          bool   `json:"targetChanged"`
	ReasonCode             string `json:"reasonCode,omitempty"`
}

func DecodePodExecutionStream(value, encoding string) ([]byte, error) {
	switch encoding {
	case "utf-8":
		if len(value) > int(PodArgvMaxOutputBytes) || !utf8.ValidString(value) {
			return nil, fmt.Errorf("invalid or oversized UTF-8 process output")
		}
		return []byte(value), nil
	case "base64":
		if len(value) > base64.StdEncoding.EncodedLen(int(PodArgvMaxOutputBytes)) {
			return nil, fmt.Errorf("oversized process output")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(value)
		if err != nil || int64(len(data)) > PodArgvMaxOutputBytes || base64.StdEncoding.EncodeToString(data) != value {
			return nil, fmt.Errorf("invalid base64 process output")
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported process output encoding")
	}
}

type PodExecutionTarget struct {
	Namespace string    `json:"namespace"`
	PodName   string    `json:"podName"`
	UID       string    `json:"uid"`
	Container string    `json:"container"`
	CheckedAt time.Time `json:"checkedAt"`
}

type PodArgvResult struct {
	Target PodExecutionTarget `json:"target"`
	Output PodExecutionOutput `json:"output"`
}

// Validate rejects a response from a mismatched or incompatible execution
// adapter before its data can enter a durable receipt.
func (result PodArgvResult) Validate(namespace, name string, input PodArgvInput) error {
	target, output := result.Target, result.Output
	if target.Namespace != namespace || target.PodName != name || target.UID != input.ExpectedUID || target.CheckedAt.IsZero() || len(validation.IsDNS1123Label(target.Container)) > 0 || input.Container != "" && target.Container != input.Container {
		return fmt.Errorf("pod execution response target does not match")
	}
	if err := output.Validate(input.MaxOutputBytes); err != nil {
		return err
	}
	return nil
}

func (output PodExecutionOutput) Validate(maximum int64) error {
	if maximum < 1 || maximum > PodArgvMaxOutputBytes || output.MaxOutputBytes != maximum || !output.UIDVerifiedBeforeStart || output.TargetChanged && output.Status != "unknown" {
		return fmt.Errorf("invalid Pod execution output bounds or identity")
	}
	switch output.Status {
	case "succeeded":
		if output.ExitCode == nil || *output.ExitCode != 0 {
			return fmt.Errorf("successful Pod execution requires exit code zero")
		}
	case "failed":
		if output.ExitCode == nil {
			return fmt.Errorf("failed process requires a known exit code")
		}
	case "timed_out", "canceled", "transport_lost", "unknown":
	default:
		return fmt.Errorf("invalid Pod execution status")
	}
	if output.ExitCode != nil && (*output.ExitCode < 0 || *output.ExitCode > 4294967295) {
		return fmt.Errorf("invalid process exit code")
	}
	stdout, err := DecodePodExecutionStream(output.Stdout, output.StdoutEncoding)
	if err != nil {
		return err
	}
	stderr, err := DecodePodExecutionStream(output.Stderr, output.StderrEncoding)
	if err != nil {
		return err
	}
	if int64(len(stdout)+len(stderr)) > maximum || !validPodStreamCount(output.StdoutBytes, len(stdout), output.StdoutTruncated) || !validPodStreamCount(output.StderrBytes, len(stderr), output.StderrTruncated) {
		return fmt.Errorf("inconsistent Pod execution output counters")
	}
	return nil
}

func validPodStreamCount(observed int64, retained int, truncated bool) bool {
	if truncated {
		return observed > int64(retained)
	}
	return observed == int64(retained)
}

type PodExecutionReceipt struct {
	ID              string              `json:"id"`
	Status          string              `json:"status"`
	ClusterID       string              `json:"clusterId"`
	Namespace       string              `json:"namespace"`
	PodName         string              `json:"podName"`
	ExpectedUID     string              `json:"expectedUid"`
	Container       string              `json:"container"`
	CreatedAt       time.Time           `json:"createdAt"`
	DeadlineAt      time.Time           `json:"deadlineAt"`
	ExpiresAt       time.Time           `json:"expiresAt"`
	FinishedAt      *time.Time          `json:"finishedAt,omitempty"`
	OutputAvailable bool                `json:"outputAvailable"`
	Output          *PodExecutionOutput `json:"output,omitempty"`
}

type AgentPodExecutionCapabilities struct {
	TerminalVersion        string `json:"terminalVersion,omitempty"`
	MaxSessionSeconds      int64  `json:"maxSessionSeconds,omitempty"`
	MaxTerminalInputBytes  int64  `json:"maxTerminalInputBytes,omitempty"`
	MaxTerminalOutputBytes int64  `json:"maxTerminalOutputBytes,omitempty"`
	ProtocolVersion        string `json:"protocolVersion"`
	ArgvVersion            string `json:"argvVersion"`
	Ready                  bool   `json:"ready"`
	MaxTimeoutSeconds      int64  `json:"maxTimeoutSeconds"`
	MaxOutputBytes         int64  `json:"maxOutputBytes"`
}
