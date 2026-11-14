package resource

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"
)

const (
	PodTerminalProtocolVersion            = "1"
	NativePodTerminalDefaultTimeout int64 = 300
	NativePodTerminalMaxTimeout     int64 = 900
	NativePodTerminalMaxOutputBytes int64 = 32 << 20
	NativePodTerminalMaxInputBytes  int64 = 1 << 20
	NativeTerminalMaxFrameBytes           = 128 << 10
	NativeTerminalMaxChunkBytes           = 64 << 10
	NativeTerminalOutputChunkBytes        = 16 << 10
	NativePodTerminalTicketHeader         = "X-Soha-Terminal-Ticket"
	NativeTerminalMaxDimension            = 1000
)

type PodTerminalInput struct {
	ExpectedUID    string `json:"expectedUid"`
	Container      string `json:"container,omitempty"`
	Shell          string `json:"shell,omitempty"`
	Width          uint16 `json:"width,omitempty"`
	Height         uint16 `json:"height,omitempty"`
	TimeoutSeconds int64  `json:"timeoutSeconds,omitempty"`
}

func (input PodTerminalInput) Normalized() (PodTerminalInput, error) {
	if _, err := (PodArgvInput{ExpectedUID: input.ExpectedUID, Container: input.Container, Argv: []string{"terminal"}}).Normalized(); err != nil {
		return PodTerminalInput{}, err
	}
	options, err := (NativeTerminalOptions{Shell: input.Shell, Width: input.Width, Height: input.Height, TimeoutSeconds: input.TimeoutSeconds}).Normalized()
	if err != nil {
		return PodTerminalInput{}, err
	}
	input.Shell, input.Width, input.Height, input.TimeoutSeconds = options.Shell, options.Width, options.Height, options.TimeoutSeconds
	return input, nil
}

type NativeTerminalSize struct {
	Width  uint16
	Height uint16
}
type NativeTerminalSizeQueue interface {
	Next(context.Context) *NativeTerminalSize
}

type PodTerminalResult struct {
	Status                 string `json:"status"`
	ExitCode               *int64 `json:"exitCode,omitempty"`
	InputBytes             int64  `json:"inputBytes"`
	OutputBytes            int64  `json:"outputBytes"`
	UIDVerifiedBeforeStart bool   `json:"uidVerifiedBeforeStart"`
	TargetChanged          bool   `json:"targetChanged"`
	ReasonCode             string `json:"reasonCode,omitempty"`
}

func (result PodTerminalResult) Validate() error {
	if result.InputBytes < 0 || result.InputBytes > NativePodTerminalMaxInputBytes || result.OutputBytes < 0 || result.OutputBytes > NativePodTerminalMaxOutputBytes || result.TargetChanged && result.Status != "unknown" {
		return fmt.Errorf("invalid terminal result bounds or target")
	}
	if result.ExitCode != nil && (*result.ExitCode < 0 || *result.ExitCode > 4294967295) {
		return fmt.Errorf("invalid terminal exit code")
	}
	if !validNativeTerminalReason(result.ReasonCode) {
		return fmt.Errorf("invalid terminal result reason")
	}
	switch result.Status {
	case "succeeded":
		if result.ExitCode == nil || *result.ExitCode != 0 || !result.UIDVerifiedBeforeStart {
			return fmt.Errorf("successful terminal requires a verified target and exit code zero")
		}
	case "failed":
		if result.ExitCode == nil || *result.ExitCode == 0 || !result.UIDVerifiedBeforeStart {
			return fmt.Errorf("failed terminal requires a verified target and a nonzero exit code")
		}
	case "timed_out", "canceled", "transport_lost", "unknown", "input_limit", "output_limit":
		if result.ExitCode != nil {
			return fmt.Errorf("uncertain terminal result cannot assert an exit code")
		}
	default:
		return fmt.Errorf("invalid terminal result status")
	}
	return nil
}

type PodTerminalLease struct {
	ID              string             `json:"id"`
	Status          string             `json:"status"`
	ClusterID       string             `json:"clusterId"`
	Target          PodExecutionTarget `json:"target"`
	Shell           string             `json:"shell"`
	CreatedAt       time.Time          `json:"createdAt"`
	TicketExpiresAt time.Time          `json:"ticketExpiresAt"`
	DeadlineAt      time.Time          `json:"deadlineAt"`
	ClosedAt        *time.Time         `json:"closedAt,omitempty"`
	MaxInputBytes   int64              `json:"maxInputBytes"`
	MaxOutputBytes  int64              `json:"maxOutputBytes"`
	Result          *PodTerminalResult `json:"result,omitempty"`
}

type PodTerminalCreated struct {
	Session PodTerminalLease `json:"session"`
	Ticket  string           `json:"ticket"`
}

// NativeTerminalFrame contains no bearer or ticket. Output is one merged PTY
// stream. Ready acknowledges the authorized WebSocket, not process readiness.
type NativeTerminalFrame struct {
	Type   string             `json:"type"`
	Data   string             `json:"data,omitempty"`
	Width  uint16             `json:"width,omitempty"`
	Height uint16             `json:"height,omitempty"`
	Result *PodTerminalResult `json:"result,omitempty"`
	Code   string             `json:"code,omitempty"`
}

func (frame NativeTerminalFrame) Validate(fromClient bool) error {
	if fromClient {
		switch frame.Type {
		case "input":
			return frame.validateData()
		case "resize":
			if frame.Width > 0 && frame.Width <= NativeTerminalMaxDimension && frame.Height > 0 && frame.Height <= NativeTerminalMaxDimension && frame.Data == "" && frame.Result == nil && frame.Code == "" {
				return nil
			}
		case "eof":
			if frame.emptyFields() {
				return nil
			}
		}
	} else {
		switch frame.Type {
		case "output":
			return frame.validateData()
		case "ready":
			if frame.emptyFields() {
				return nil
			}
		case "exit":
			if frame.Data == "" && frame.Width == 0 && frame.Height == 0 && frame.Code == "" && frame.Result != nil {
				return frame.Result.Validate()
			}
		case "error":
			if frame.Data == "" && frame.Width == 0 && frame.Height == 0 && frame.Result == nil && validNativeTerminalError(frame.Code) {
				return nil
			}
		}
	}
	return fmt.Errorf("invalid terminal frame direction or fields")
}

func (frame NativeTerminalFrame) validateData() error {
	if frame.Width != 0 || frame.Height != 0 || frame.Result != nil || frame.Code != "" || frame.Data == "" || len(frame.Data) > base64.StdEncoding.EncodedLen(NativeTerminalMaxChunkBytes) {
		return fmt.Errorf("invalid terminal data frame")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(frame.Data)
	if err != nil || len(data) == 0 || len(data) > NativeTerminalMaxChunkBytes || base64.StdEncoding.EncodeToString(data) != frame.Data {
		return fmt.Errorf("terminal data must be bounded canonical base64")
	}
	return nil
}

func (frame NativeTerminalFrame) emptyFields() bool {
	return frame.Data == "" && frame.Width == 0 && frame.Height == 0 && frame.Result == nil && frame.Code == ""
}

func validNativeTerminalError(code string) bool {
	switch code {
	case "invalid_frame", "input_limit", "output_limit", "lease_expired", "authorization_revoked", "target_changed", "transport_lost", "unsupported":
		return true
	default:
		return false
	}
}

func validNativeTerminalReason(code string) bool {
	switch code {
	case "", "process_exited", "deadline_exceeded", "request_canceled", "transport_lost", "target_changed", "target_verification_unavailable", "input_limit", "output_limit":
		return true
	default:
		return false
	}
}
