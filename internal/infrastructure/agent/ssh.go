package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
)

type VMSSHExecutor struct {
	Client func(context.Context, string) (*Client, error)
}

func (executor VMSSHExecutor) Capability(ctx context.Context, id string) (sohaapi.AgentSSHCapability, error) {
	client, err := executor.Client(ctx, id)
	if err != nil {
		return sohaapi.AgentSSHCapability{}, err
	}
	var payload struct {
		Data sohaapi.AgentSSHCapability `json:"data"`
	}
	err = client.request(ctx, http.MethodGet, "/api/v1/ssh/capability", nil, &payload)
	return payload.Data, err
}

func (executor VMSSHExecutor) OpenSession(ctx context.Context, id string, input sohaapi.AgentSSHSessionRequest) (appvirtualization.SSHChannel, error) {
	client, err := executor.Client(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = client.request(ctx, http.MethodPost, "/api/v1/ssh/sessions", input, nil); err != nil {
		return nil, err
	}
	endpoint, err := client.websocketEndpoint("/api/v1/ssh/sessions/" + url.PathEscape(input.SessionID) + "/stream")
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+client.token)
	conn, response, err := client.wsDialer.DialContext(ctx, endpoint, headers)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("agent SSH stream unavailable: %w", err)
	}
	conn.SetReadLimit(128 * 1024)
	return &sshChannel{connection: conn}, nil
}

type sshChannel struct {
	connection *websocket.Conn
	write      sync.Mutex
}

func (channel *sshChannel) ReadFrame() (sohaapi.VirtualMachineSSHFrame, error) {
	var frame sohaapi.VirtualMachineSSHFrame
	err := channel.connection.ReadJSON(&frame)
	if err == nil && (len(frame.Data) > 87384 || len(frame.Message) > 1024 || (frame.Type != "output" && frame.Type != "error" && frame.Type != "close")) {
		return frame, fmt.Errorf("invalid Agent SSH frame")
	}
	if err == nil && frame.Type == "output" {
		if frame.DataEncoding == "base64" {
			data, decodeErr := base64.StdEncoding.DecodeString(frame.Data)
			if decodeErr != nil || len(data) > 65536 {
				return frame, fmt.Errorf("invalid Agent SSH frame")
			}
		} else if (frame.DataEncoding != "" && frame.DataEncoding != "utf8") || len(frame.Data) > 65536 {
			return frame, fmt.Errorf("invalid Agent SSH frame")
		}
	}
	if err == nil && frame.ExitCode != nil && (frame.Type != "close" || *frame.ExitCode < 0 || *frame.ExitCode > 255) {
		return frame, fmt.Errorf("invalid Agent SSH exit status")
	}
	return frame, err
}
func (channel *sshChannel) WriteFrame(frame sohaapi.VirtualMachineSSHFrame) error {
	channel.write.Lock()
	defer channel.write.Unlock()
	if err := channel.connection.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return channel.connection.WriteJSON(frame)
}
func (channel *sshChannel) Close() error { return channel.connection.Close() }
