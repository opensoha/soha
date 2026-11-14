package runtime

import (
	"errors"
	"strings"
	"time"

	"github.com/opensoha/soha/contracts/resource"
)

const TerminalProtocol = "docker-native-terminal-v1"
const TerminalInputHeader = "X-Soha-Docker-Terminal-Input"

type TerminalInput struct {
	ExpectedContainerID string `json:"expectedContainerId"`
	ServiceName         string `json:"serviceName"`
	Shell               string `json:"shell,omitempty"`
	Width               uint16 `json:"width,omitempty"`
	Height              uint16 `json:"height,omitempty"`
	TimeoutSeconds      int64  `json:"timeoutSeconds,omitempty"`
}

func (input TerminalInput) Normalized() (TerminalInput, error) {
	if !containerIDPattern.MatchString(input.ExpectedContainerID) || !serviceNamePattern.MatchString(input.ServiceName) {
		return TerminalInput{}, errors.New("terminal requires an exact service and full container ID")
	}
	options, err := (resource.NativeTerminalOptions{Shell: input.Shell, Width: input.Width, Height: input.Height, TimeoutSeconds: input.TimeoutSeconds}).Normalized()
	if err != nil {
		return TerminalInput{}, err
	}
	input.Shell, input.Width, input.Height, input.TimeoutSeconds = options.Shell, options.Width, options.Height, options.TimeoutSeconds
	return input, nil
}

// The stream uses the same closed PTY frames and outcome semantics as native
// Pod terminals. UID verification here means host/project UID labels and CID.
type TerminalResult = resource.PodTerminalResult

type TerminalLease struct {
	Protocol        string           `json:"protocol"`
	ID              string           `json:"id"`
	Status          string           `json:"status"`
	ProjectID       string           `json:"projectId"`
	Target          ContainerBinding `json:"target"`
	Shell           string           `json:"shell"`
	CreatedAt       time.Time        `json:"createdAt"`
	TicketExpiresAt time.Time        `json:"ticketExpiresAt"`
	DeadlineAt      time.Time        `json:"deadlineAt"`
	ClosedAt        *time.Time       `json:"closedAt,omitempty"`
	MaxInputBytes   int64            `json:"maxInputBytes"`
	MaxOutputBytes  int64            `json:"maxOutputBytes"`
	Result          *TerminalResult  `json:"result,omitempty"`
}

type TerminalCreated struct {
	Session TerminalLease `json:"session"`
	Ticket  string        `json:"ticket"`
}

type TerminalRequest struct {
	Protocol   string           `json:"protocol"`
	ProjectID  string           `json:"projectId"`
	Container  ContainerBinding `json:"container"`
	Input      TerminalInput    `json:"input"`
	DeadlineAt time.Time        `json:"deadlineAt"`
}

func (input TerminalRequest) Normalized(now time.Time) (TerminalRequest, error) {
	options, err := input.Input.Normalized()
	if err != nil || input.Protocol != TerminalProtocol || input.ProjectID == "" || !SafeText(input.ProjectID, 128) || strings.TrimSpace(input.ProjectID) != input.ProjectID || input.Container.Validate() != nil || input.Container.ContainerID != options.ExpectedContainerID || input.Container.ServiceName != options.ServiceName {
		return TerminalRequest{}, errors.New("docker native terminal identity or protocol is invalid")
	}
	if !input.DeadlineAt.After(now) || input.DeadlineAt.After(now.Add(time.Duration(options.TimeoutSeconds)*time.Second)) {
		return TerminalRequest{}, errors.New("docker native terminal deadline is invalid")
	}
	input.Input = options
	return input, nil
}

type TerminalCapabilities struct {
	Protocol          string `json:"protocol"`
	NativePTY         bool   `json:"nativePty"`
	MaxTimeoutSeconds int64  `json:"maxTimeoutSeconds"`
	MaxInputBytes     int64  `json:"maxInputBytes"`
	MaxOutputBytes    int64  `json:"maxOutputBytes"`
}
