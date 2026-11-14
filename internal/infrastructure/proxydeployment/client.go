package proxydeployment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type HTTPClient struct {
	baseURL, executorID, token string
	client                     *http.Client
	retryWait                  func(context.Context, time.Duration) bool
}

func NewHTTPClient(baseURL, executorID, token string) (*HTTPClient, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid Proxy control plane URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && (u.Scheme != "http" || u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return nil, fmt.Errorf("proxy executor requires HTTPS outside loopback")
	}
	if executorID == "" || len(token) < 32 {
		return nil, fmt.Errorf("proxy executor identity and dedicated credential are required")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/api/v1") {
		u.Path += "/api/v1"
	}
	u.Path += "/network/proxy-deployment-executor"
	return &HTTPClient{baseURL: u.String(), executorID: executorID, token: token, retryWait: wait, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
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
	encoded, err := json.Marshal(input)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	if input == nil {
		encoded = nil
	}
	// Bound retries even for the idle claim loop, whose parent may have no deadline.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := c.requestWithRateLimit(ctx, method, path, encoded)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNoContent {
		return delivery.ExecutionTask{}, apperrors.ErrNotFound
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return delivery.ExecutionTask{}, fmt.Errorf("proxy executor request rejected (HTTP %d)", response.StatusCode)
	}
	var envelope struct {
		Data delivery.ExecutionTask `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope); err != nil {
		return delivery.ExecutionTask{}, fmt.Errorf("invalid Proxy executor response")
	}
	if envelope.Data.ID == "" {
		return delivery.ExecutionTask{}, fmt.Errorf("missing Proxy execution task")
	}
	return envelope.Data, nil
}

// Only an explicit 429 proves the request was rejected before execution. Never
// replay a network error or another HTTP error, especially for claim/callback.
func (c *HTTPClient) requestWithRateLimit(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	backoff := time.Second
	for {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")
		response, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("proxy executor control plane unavailable")
		}
		if response.StatusCode != http.StatusTooManyRequests {
			return response, nil
		}
		delay := rateLimitDelay(response.Header.Get("Retry-After"), time.Now(), backoff)
		_ = response.Body.Close()
		if !c.retryWait(ctx, delay) {
			return nil, ctx.Err()
		}
		backoff = min(backoff*2, time.Minute)
	}
}

func rateLimitDelay(value string, now time.Time, fallback time.Duration) time.Duration {
	delay := fallback
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		// Clamp before conversion to avoid duration overflow.
		delay = time.Duration(max(int64(1), min(seconds, int64(60)))) * time.Second
	} else if date, err := http.ParseTime(value); err == nil {
		delay = date.Sub(now)
	}
	return max(time.Second, min(delay, time.Minute))
}
