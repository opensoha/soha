package outpostruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type HTTPClient struct {
	baseURL, executorID, token string
	client                     *http.Client
}

func NewHTTPClient(baseURL, executorID, token string) (*HTTPClient, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid Outpost control plane URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && (u.Scheme != "http" || u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return nil, fmt.Errorf("outpost executor requires HTTPS outside loopback")
	}
	if executorID == "" || len(token) < 32 {
		return nil, fmt.Errorf("outpost executor identity and dedicated credential are required")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/api/v1") {
		u.Path += "/api/v1"
	}
	u.Path += "/identity/outpost-runtime-executor"
	return &HTTPClient{baseURL: u.String(), executorID: executorID, token: token, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *HTTPClient) Claim(ctx context.Context) (delivery.ExecutionTask, error) {
	return c.request(ctx, http.MethodPost, "/claim", map[string]string{"executorId": c.executorID})
}

func (c *HTTPClient) Status(ctx context.Context, taskID string) (delivery.ExecutionTask, error) {
	return c.request(ctx, http.MethodGet, "/status?taskId="+url.QueryEscape(taskID), nil)
}

func (c *HTTPClient) Callback(ctx context.Context, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	return c.request(ctx, http.MethodPost, "/callback", map[string]any{"callbackToken": input.CallbackToken, "status": input.Status, "payload": input.Payload})
}

func (c *HTTPClient) request(ctx context.Context, method, path string, input any) (delivery.ExecutionTask, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return delivery.ExecutionTask{}, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		return delivery.ExecutionTask{}, fmt.Errorf("outpost executor control plane unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNoContent {
		return delivery.ExecutionTask{}, apperrors.ErrNotFound
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return delivery.ExecutionTask{}, fmt.Errorf("outpost executor request rejected (HTTP %d)", response.StatusCode)
	}
	var envelope struct {
		Data delivery.ExecutionTask `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope); err != nil {
		return delivery.ExecutionTask{}, fmt.Errorf("invalid Outpost executor response")
	}
	if envelope.Data.ID == "" {
		return delivery.ExecutionTask{}, fmt.Errorf("missing Outpost execution task")
	}
	return envelope.Data, nil
}
