// Package relayport declares the AI Gateway application's provider transport.
package relayport

import (
	"context"
	"io"
	"net/http"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
)

type Transport interface {
	ValidateURL(string) error
	Prepare(Request, Target, RequestOptions) (PreparedRequest, error)
	Execute(context.Context, PreparedRequest, HTTPOptions) (HTTPResult, error)
	Probe(context.Context, Target, string, time.Duration) (domainaigateway.LLMUpstreamTestResult, error)
	WebSocket(context.Context, Request, Target, WebSocketOptions) (WebSocketResult, error)
}

type Request struct {
	ProviderKind string
	Endpoint     string
	Headers      http.Header
	Body         []byte
}

type Target struct {
	ID             string
	BaseURL        string
	ProviderKind   string
	Model          string
	DefaultHeaders map[string]any
	Metadata       map[string]any
	RouteMetadata  map[string]any
}

type RequestOptions struct {
	Stream             bool
	IncludeOpenAIUsage bool
	// ConversionProvider is chosen by application route policy.
	ConversionProvider string
	ConversionEndpoint string
}

type PreparedRequest struct {
	Request
	URL                string
	ClientProviderKind string
	DefaultHeaders     map[string]any
}

type HTTPOptions struct {
	APIKey           string
	Timeout          time.Duration
	FirstByteTimeout time.Duration
	IdleTimeout      time.Duration
	// StartStream commits the downstream response and returns its body writer.
	StartStream func(*http.Response) io.Writer
}

type HTTPFailure struct {
	Code    string
	Message string
}

type HTTPResult struct {
	Response        *http.Response
	Body            []byte
	OutputBytes     int64
	BodyTruncated   bool
	UpstreamStarted time.Time
	FirstByteAt     time.Time
	Wrote           bool
	Failure         *HTTPFailure
}

type WebSocketConn interface {
	NextReader() (int, io.Reader, error)
	NextWriter(int) (io.WriteCloser, error)
	SetReadDeadline(time.Time) error
	SetReadLimit(int64)
	Close() error
}

type UpgradeWebSocket func(http.Header) (WebSocketConn, error)

type WebSocketOptions struct {
	APIKey      string
	PublicModel string
	DialTimeout time.Duration
	IdleTimeout time.Duration
	Upgrade     UpgradeWebSocket
}

type WebSocketResult struct {
	HTTPStatus      int
	UpstreamStarted time.Time
	FirstByteAt     time.Time
	InputBytes      int64
	OutputBytes     int64
	DialError       error
	UpgradeError    error
	BridgeError     error
	ClientClosed    bool
}

func IsSuccessStatus(status int) bool {
	return status >= 200 && status < 300
}

func IsRedirectStatus(status int) bool {
	return status >= 300 && status < 400
}

func IsRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}
