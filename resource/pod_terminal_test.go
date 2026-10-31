package resource

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNativeTerminalInputBoundsAndDefaults(t *testing.T) {
	input, err := (PodTerminalInput{ExpectedUID: "reviewed"}).Normalized()
	if err != nil || input.Shell != "/bin/sh" || input.Width != 80 || input.Height != 24 || input.TimeoutSeconds != 300 {
		t.Fatalf("defaults=%+v err=%v", input, err)
	}
	for name, input := range map[string]PodTerminalInput{
		"uid": {}, "shell": {ExpectedUID: "uid", Shell: "sh -c id"}, "width": {ExpectedUID: "uid", Width: 1001}, "height": {ExpectedUID: "uid", Height: 65535}, "timeout": {ExpectedUID: "uid", TimeoutSeconds: 901}, "negative": {ExpectedUID: "uid", TimeoutSeconds: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := input.Normalized(); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}

func TestNativeTerminalFrameDirectionAndBounds(t *testing.T) {
	zero, one := int64(0), int64(1)
	valid := PodTerminalResult{Status: "succeeded", ExitCode: &zero, UIDVerifiedBeforeStart: true}
	for _, test := range []struct {
		name          string
		frame         NativeTerminalFrame
		client, valid bool
	}{
		{"binary", NativeTerminalFrame{Type: "input", Data: base64.StdEncoding.EncodeToString([]byte{0, 255})}, true, true},
		{"direction", NativeTerminalFrame{Type: "input", Data: "AA=="}, false, false},
		{"resize", NativeTerminalFrame{Type: "resize", Width: 1000, Height: 1000}, true, true},
		{"resize-over", NativeTerminalFrame{Type: "resize", Width: 1001, Height: 1}, true, false},
		{"noncanonical", NativeTerminalFrame{Type: "input", Data: "AB=="}, true, false},
		{"over-budget", NativeTerminalFrame{Type: "input", Data: base64.StdEncoding.EncodeToString(make([]byte, NativeTerminalMaxChunkBytes+1))}, true, false},
		{"result", NativeTerminalFrame{Type: "exit", Result: &valid}, false, true},
		{"error", NativeTerminalFrame{Type: "error", Code: "authorization_revoked"}, false, true},
		{"provider-error", NativeTerminalFrame{Type: "error", Code: "private provider content"}, false, false},
		{"mixed", NativeTerminalFrame{Type: "ready", Data: "AA=="}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.frame.Validate(test.client); (err == nil) != test.valid {
				t.Fatalf("err=%v", err)
			}
		})
	}
	for name, result := range map[string]PodTerminalResult{
		"failed-zero":        {Status: "failed", ExitCode: &zero, UIDVerifiedBeforeStart: true},
		"uncertain-exit":     {Status: "unknown", ExitCode: &one},
		"unverified-success": {Status: "succeeded", ExitCode: &zero},
		"target-success":     {Status: "succeeded", ExitCode: &zero, UIDVerifiedBeforeStart: true, TargetChanged: true},
		"unbounded-reason":   {Status: "unknown", ReasonCode: strings.Repeat("p", 128<<10)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := result.Validate(); err == nil {
				t.Fatal("accepted contradictory result")
			}
		})
	}
}
