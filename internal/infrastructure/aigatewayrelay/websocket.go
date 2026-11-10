package aigatewayrelay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/opensoha/soha/internal/application/aigateway/relayport"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type relayWebSocketProxyResult struct {
	firstByteAt   time.Time
	clientBytes   int64
	upstreamBytes int64
	err           error
}

func relayProxyWebSocketMessages(ctx context.Context, clientConn, upstreamConn relayport.WebSocketConn, upstreamIdleTimeout time.Duration) (time.Time, int64, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan relayWebSocketProxyResult, 2)
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = clientConn.Close()
			_ = upstreamConn.Close()
		})
	}
	go relayCopyWebSocketMessages(ctx, upstreamConn, clientConn, true, 0, results, cancel, closeBoth)
	go relayCopyWebSocketMessages(ctx, clientConn, upstreamConn, false, upstreamIdleTimeout, results, cancel, closeBoth)

	var firstByteAt time.Time
	var clientBytes int64
	var upstreamBytes int64
	var firstErr error
	ctxDone := ctx.Done()
	for completed := 0; completed < 2; {
		select {
		case <-ctxDone:
			closeBoth()
			ctxDone = nil
		case result := <-results:
			completed++
			if !result.firstByteAt.IsZero() && firstByteAt.IsZero() {
				firstByteAt = result.firstByteAt
			}
			clientBytes += result.clientBytes
			upstreamBytes += result.upstreamBytes
			if firstErr == nil && result.err != nil {
				firstErr = result.err
			}
		}
	}
	return firstByteAt, clientBytes, upstreamBytes, firstErr
}

func relayCopyWebSocketMessages(ctx context.Context, dst, src relayport.WebSocketConn, clientToUpstream bool, idleTimeout time.Duration, results chan<- relayWebSocketProxyResult, cancel context.CancelFunc, closeBoth func()) {
	result := relayWebSocketProxyResult{}
	defer func() {
		cancel()
		closeBoth()
		results <- result
	}()
	for {
		if idleTimeout > 0 {
			_ = src.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		messageType, reader, err := src.NextReader()
		if err != nil {
			result.err = err
			return
		}
		writer, err := dst.NextWriter(messageType)
		if err != nil {
			result.err = err
			return
		}
		count, copyErr := io.Copy(writer, reader)
		closeErr := writer.Close()
		if clientToUpstream {
			result.clientBytes += count
		} else {
			result.upstreamBytes += count
			if count > 0 && result.firstByteAt.IsZero() {
				result.firstByteAt = time.Now().UTC()
			}
		}
		if copyErr != nil {
			result.err = copyErr
			return
		}
		if closeErr != nil {
			result.err = closeErr
			return
		}
		select {
		case <-ctx.Done():
			result.err = ctx.Err()
			return
		default:
		}
	}
}

func relayWebSocketCloseError(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, websocket.ErrCloseSent) ||
		websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure)
}

func relayWebSocketHandshakeHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "host", "connection", "upgrade", "sec-websocket-key", "sec-websocket-accept", "sec-websocket-version", "sec-websocket-protocol", "sec-websocket-extensions":
		return true
	default:
		return false
	}
}
func relayRealtimeUpstreamHeaders(req relayport.Request, defaultHeaders map[string]any, apiKey string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	for key, value := range defaultHeaders {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" || relayport.SensitiveHeader(name) || relayWebSocketHandshakeHeader(name) {
			continue
		}
		headers.Set(name, fmt.Sprint(value))
	}
	if organization := req.Headers.Get("OpenAI-Organization"); organization != "" {
		headers.Set("OpenAI-Organization", organization)
	}
	if beta := req.Headers.Get("OpenAI-Beta"); beta != "" {
		headers.Set("OpenAI-Beta", beta)
	}
	return headers
}

const relayWebSocketMessageReadLimit = 8 << 20

func (t *Adapter) WebSocket(ctx context.Context, req relayport.Request, target relayport.Target, opts relayport.WebSocketOptions) (relayport.WebSocketResult, error) {
	targetURL, err := relayRealtimeEndpointURL(target, opts.PublicModel)
	if err != nil {
		return relayport.WebSocketResult{}, err
	}
	if err := t.validateURL(targetURL, "wss", "ws"); err != nil {
		return relayport.WebSocketResult{}, err
	}
	if strings.TrimSpace(opts.APIKey) == "" {
		return relayport.WebSocketResult{}, fmt.Errorf("%w: relay upstream API key is not configured", apperrors.ErrInvalidArgument)
	}
	dialCtx, cancel := context.WithTimeout(ctx, opts.DialTimeout)
	result := relayport.WebSocketResult{UpstreamStarted: time.Now().UTC()}
	dialer := websocket.Dialer{NetDialContext: t.dialPinned, HandshakeTimeout: 45 * time.Second}
	upstream, resp, err := dialer.DialContext(dialCtx, targetURL, relayRealtimeUpstreamHeaders(req, target.DefaultHeaders, opts.APIKey))
	cancel()
	if resp != nil {
		result.HTTPStatus = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	if err != nil {
		result.DialError = err
		return result, nil
	}
	defer func() { _ = upstream.Close() }()
	upstream.SetReadLimit(relayWebSocketMessageReadLimit)
	headers := make(http.Header)
	if resp != nil {
		relayport.CopyResponseHeaders(headers, resp.Header, "application/octet-stream")
	}
	client, err := opts.Upgrade(headers)
	if err != nil {
		result.UpgradeError = err
		return result, nil
	}
	defer func() { _ = client.Close() }()
	client.SetReadLimit(relayWebSocketMessageReadLimit)
	result.FirstByteAt, result.InputBytes, result.OutputBytes, result.BridgeError =
		relayProxyWebSocketMessages(ctx, client, upstream, opts.IdleTimeout)
	result.ClientClosed = ctx.Err() != nil || relayWebSocketCloseError(result.BridgeError)
	return result, nil
}
