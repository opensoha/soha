package radiusadapter

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/opensoha/soha/internal/networkprotocol"
)

type ExecutionResult struct {
	Status     string
	ReasonCode string
}

type Executor func(context.Context, networkprotocol.NASSessionCommand) ExecutionResult

func RadclientExecutor(cfg Config) Executor {
	return func(ctx context.Context, command networkprotocol.NASSessionCommand) ExecutionResult {
		args, input := radclientRequest(cfg, command)
		process := exec.CommandContext(ctx, cfg.RadclientPath, args...) // #nosec G204 -- configured executable and typed arguments; no shell is used.
		process.Stdin = strings.NewReader(input)
		output := &limitedOutput{limit: 64 << 10}
		process.Stdout, process.Stderr = output, output
		err := process.Run()
		text := strings.ToLower(output.String())
		if ctx.Err() != nil || strings.Contains(text, "no response from server") {
			return ExecutionResult{Status: "timed-out", ReasonCode: "radius_timeout"}
		}
		if strings.Contains(text, "coa-ack") || strings.Contains(text, "disconnect-ack") {
			return ExecutionResult{Status: "applied", ReasonCode: command.Action + "_applied"}
		}
		if strings.Contains(text, "unsupported-service") || strings.Contains(text, "unsupported-attribute") {
			return ExecutionResult{Status: "unsupported", ReasonCode: "radius_action_unsupported"}
		}
		if strings.Contains(text, "coa-nak") || strings.Contains(text, "disconnect-nak") {
			return ExecutionResult{Status: "rejected", ReasonCode: command.Action + "_rejected"}
		}
		if err != nil {
			return ExecutionResult{Status: "rejected", ReasonCode: "radclient_failed"}
		}
		return ExecutionResult{Status: "rejected", ReasonCode: "radius_unexpected_response"}
	}
}

func radclientRequest(cfg Config, command networkprotocol.NASSessionCommand) ([]string, string) {
	timeout := strconv.FormatFloat(cfg.CommandTimeout.Seconds(), 'f', 3, 64)
	args := []string{"-b", "-r", "1", "-t", timeout, "-S", cfg.SecretFile, cfg.NASTarget, command.Action}
	var input strings.Builder
	stringAttribute(&input, "NAS-Identifier", command.NASID)
	stringAttribute(&input, "User-Name", command.SubjectID)
	stringAttribute(&input, "Calling-Station-Id", command.DeviceID)
	stringAttribute(&input, "Class", command.SessionID)
	if command.Action == "coa" && command.RadiusAttributes != nil {
		if command.RadiusAttributes.VLANID != 0 {
			stringAttribute(&input, "Tunnel-Type", "VLAN")
			stringAttribute(&input, "Tunnel-Medium-Type", "IEEE-802")
			integerAttribute(&input, "Tunnel-Private-Group-Id", command.RadiusAttributes.VLANID)
		}
		if command.RadiusAttributes.FilterID != "" {
			stringAttribute(&input, "Filter-Id", command.RadiusAttributes.FilterID)
		}
		integerAttribute(&input, "Session-Timeout", command.RadiusAttributes.SessionTimeoutSeconds)
	}
	return args, input.String()
}

func stringAttribute(target *strings.Builder, name, value string) {
	fmt.Fprintf(target, "%s = %s\n", name, strconv.QuoteToASCII(value))
}

func integerAttribute(target *strings.Builder, name string, value int) {
	fmt.Fprintf(target, "%s = %d\n", name, value)
}

type limitedOutput struct {
	data  []byte
	limit int
}

func (w *limitedOutput) Write(value []byte) (int, error) {
	remaining := w.limit - len(w.data)
	if remaining > 0 {
		if remaining > len(value) {
			remaining = len(value)
		}
		w.data = append(w.data, value[:remaining]...)
	}
	return len(value), nil
}

func (w *limitedOutput) String() string { return string(w.data) }
