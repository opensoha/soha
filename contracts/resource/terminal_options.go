package resource

import "fmt"

// NativeTerminalOptions contains only shared PTY limits and shell selection.
// Target identity and authorization remain with the owning resource protocol.
type NativeTerminalOptions struct {
	Shell          string
	Width, Height  uint16
	TimeoutSeconds int64
}

func (input NativeTerminalOptions) Normalized() (NativeTerminalOptions, error) {
	switch input.Shell {
	case "", "sh", "/bin/sh":
		input.Shell = "/bin/sh"
	case "bash", "/bin/bash":
		input.Shell = "/bin/bash"
	default:
		return NativeTerminalOptions{}, fmt.Errorf("terminal shell must be /bin/sh or /bin/bash")
	}
	if input.TimeoutSeconds < 0 || input.TimeoutSeconds > NativePodTerminalMaxTimeout {
		return NativeTerminalOptions{}, fmt.Errorf("terminal timeout is outside supported bounds")
	}
	if input.Width > NativeTerminalMaxDimension || input.Height > NativeTerminalMaxDimension {
		return NativeTerminalOptions{}, fmt.Errorf("terminal dimensions are outside supported bounds")
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = NativePodTerminalDefaultTimeout
	}
	if input.Width == 0 {
		input.Width = 80
	}
	if input.Height == 0 {
		input.Height = 24
	}
	return input, nil
}
