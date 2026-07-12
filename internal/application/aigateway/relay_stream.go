package aigateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) RelayLLMWebSocket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, writer http.ResponseWriter, clientRequest *http.Request) error {
	if !s.relayConfig.Enabled {
		return fmt.Errorf("%w: AI Gateway LLM relay is disabled", apperrors.ErrNotFound)
	}
	if normalizeRelayProviderKind(req.ProviderKind) != "openai" || strings.TrimSpace(req.Endpoint) != "realtime" {
		return fmt.Errorf("%w: realtime relay only supports OpenAI-compatible websocket requests", apperrors.ErrInvalidArgument)
	}
	if clientRequest == nil {
		return fmt.Errorf("%w: realtime relay request is required", apperrors.ErrInvalidArgument)
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayInvoke); err != nil {
		return err
	}
	if _, err := s.AuthorizeLLMRelayToken(ctx, principal, accessCtx, LLMRelayAccessRequest{
		ProviderKind: req.ProviderKind,
		SourceIP:     req.SourceIP,
	}); err != nil {
		return err
	}
	model, err := relayRealtimeRequestModel(req)
	if err != nil {
		return err
	}
	if _, err := s.AuthorizeLLMRelayToken(ctx, principal, accessCtx, LLMRelayAccessRequest{
		Model:        model,
		ProviderKind: req.ProviderKind,
		SourceIP:     req.SourceIP,
	}); err != nil {
		return err
	}
	if err := s.authorizeRelayRouteTrace(ctx, principal, accessCtx, req); err != nil {
		return err
	}
	selections, err := s.selectRelayUpstreamCandidatesForPrincipal(ctx, principal, req.ProviderKind, model)
	if err != nil {
		return err
	}
	selections = filterRelayRealtimeSelections(selections, req.ProviderKind)
	if len(selections) == 0 {
		return fmt.Errorf("%w: no active realtime relay route for model %s", apperrors.ErrNotFound, model)
	}
	requestedUpstreamID := relayRequestedUpstreamID(req)
	if requestedUpstreamID != "" {
		if err := s.authorizeRelayExplicitUpstream(ctx, principal, accessCtx, requestedUpstreamID); err != nil {
			return err
		}
		selections = filterRelaySelectionsByUpstream(selections, requestedUpstreamID)
		if len(selections) == 0 {
			return fmt.Errorf("%w: requested relay upstream is not available for model %s", apperrors.ErrNotFound, model)
		}
	}
	authorized := make([]relaySelection, 0, len(selections))
	var authErr error
	for _, selection := range selections {
		if _, err := s.AuthorizeLLMRelayToken(ctx, principal, accessCtx, LLMRelayAccessRequest{
			Model:        model,
			ProviderKind: req.ProviderKind,
			UpstreamID:   selection.upstream.ID,
			SourceIP:     req.SourceIP,
		}); err != nil {
			authErr = err
			continue
		}
		authorized = append(authorized, selection)
	}
	if len(authorized) == 0 {
		if authErr != nil {
			return authErr
		}
		return fmt.Errorf("%w: no authorized relay upstream for model %s", apperrors.ErrNotFound, model)
	}
	const stream = true
	releaseTokenConcurrency, tokenConcurrencyCode, tokenConcurrencyMessage, acquired := s.tryAcquireRelayTokenConcurrency(accessCtx, stream)
	if !acquired {
		s.recordRelayCall(ctx, principal, accessCtx, req, authorized[0], model, stream, domainaigateway.LLMCallLog{
			Status:       "rate_limited",
			HTTPStatus:   http.StatusTooManyRequests,
			ErrorCode:    tokenConcurrencyCode,
			ErrorMessage: tokenConcurrencyMessage,
			CacheStatus:  relayCacheBypass,
			CreatedAt:    time.Now().UTC(),
		})
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, tokenConcurrencyMessage)
	}
	defer releaseTokenConcurrency()
	return s.proxyRelayWebSocketWithFallback(ctx, principal, accessCtx, req, authorized, model, writer, clientRequest)
}

func (s *Service) proxyRelayWebSocketWithFallback(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selections []relaySelection, publicModel string, writer http.ResponseWriter, clientRequest *http.Request) error {
	var lastErr error
	attempts := relayFallbackMaxAttempts(selections)
	for index, selection := range selections {
		if attempts > 0 && index >= attempts {
			break
		}
		if err := s.enforceRelayRateLimits(ctx, principal, accessCtx, req, selection, publicModel, true); err != nil {
			return err
		}
		upgraded, retryable, err := s.proxyRelayWebSocket(ctx, principal, accessCtx, req, selection, publicModel, writer, clientRequest)
		if err == nil || upgraded || !retryable || index == len(selections)-1 || ctx.Err() != nil {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (s *Service) proxyRelayWebSocket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, writer http.ResponseWriter, clientRequest *http.Request) (bool, bool, error) {
	started := time.Now().UTC()
	release, err := s.acquireRelayWebSocketUpstream(ctx, principal, accessCtx, req, selection, publicModel, started)
	if err != nil {
		return false, true, err
	}
	defer release()

	targetURL, apiKey, err := s.prepareRelayWebSocketUpstream(selection, publicModel)
	if err != nil {
		return false, false, err
	}

	dialTimeout := relayWebSocketDialTimeout(s.relayConfig)
	dialCtx, cancelDial := context.WithTimeout(ctx, dialTimeout)
	upstreamStarted := time.Now().UTC()
	upstreamConn, upstreamResp, err := relayWebSocketDialer().DialContext(dialCtx, targetURL, relayRealtimeUpstreamHeaders(req, selection.upstream, apiKey))
	cancelDial()
	if upstreamResp != nil && upstreamResp.Body != nil {
		defer func() { _ = upstreamResp.Body.Close() }()
	}
	if err != nil {
		status := http.StatusBadGateway
		errorCode := "upstream_request_failed"
		if upstreamResp != nil {
			status = upstreamResp.StatusCode
			errorCode = relayErrorCodeForStatus(upstreamResp.StatusCode)
			if errorCode == "" {
				errorCode = "upstream_ws_failed"
			}
		}
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
			Status:               relayStatusFromError(ctx, err),
			HTTPStatus:           status,
			UpstreamStatus:       status,
			ErrorCode:            errorCode,
			ErrorMessage:         redactRelayText(err.Error()),
			CacheStatus:          relayCacheBypass,
			DurationMilliseconds: time.Since(started).Milliseconds(),
			CreatedAt:            started,
		})
		s.recordRelayUpstreamFailure(ctx, principal, selection, errorCode)
		return false, true, fmt.Errorf("%w: relay realtime upstream connection failed", apperrors.ErrClusterUnready)
	}
	defer func() { _ = upstreamConn.Close() }()
	upstreamConn.SetReadLimit(relayWebSocketMessageReadLimit)

	responseHeader := http.Header{}
	if upstreamResp != nil {
		copyRelayResponseHeaders(responseHeader, upstreamResp.Header, "application/octet-stream")
	}
	writeRelayRouteTraceHeaders(responseHeader, req, selection, publicModel, true, http.StatusSwitchingProtocols, relayCacheBypass)
	upgrader := relayWebSocketUpgrader()
	clientConn, err := upgrader.Upgrade(writer, clientRequest, responseHeader)
	if err != nil {
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
			Status:               "client_cancelled",
			HTTPStatus:           http.StatusBadRequest,
			UpstreamStatus:       http.StatusSwitchingProtocols,
			ErrorCode:            "client_upgrade_failed",
			ErrorMessage:         redactRelayText(err.Error()),
			CacheStatus:          relayCacheBypass,
			DurationMilliseconds: time.Since(started).Milliseconds(),
			CreatedAt:            started,
		})
		return false, false, nil
	}
	defer func() { _ = clientConn.Close() }()
	clientConn.SetReadLimit(relayWebSocketMessageReadLimit)

	firstByteAt, clientBytes, upstreamBytes, bridgeErr := relayProxyWebSocketMessages(ctx, clientConn, upstreamConn, s.relayConfig.StreamIdleTimeout)
	status, errorCode, errorMessage := relayWebSocketBridgeOutcome(bridgeErr)
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
		Status:               status,
		HTTPStatus:           http.StatusSwitchingProtocols,
		UpstreamStatus:       http.StatusSwitchingProtocols,
		ErrorCode:            errorCode,
		ErrorMessage:         redactRelayText(errorMessage),
		InputBytes:           clientBytes,
		OutputBytes:          upstreamBytes,
		CacheStatus:          relayCacheBypass,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		CreatedAt:            started,
		TTFBMilliseconds:     relayDurationMilliseconds(started, firstByteAt),
		TTFTMilliseconds:     relayDurationMilliseconds(started, firstByteAt),
		Metadata: map[string]any{
			"upstreamConnectedAt": upstreamStarted.Format(time.RFC3339Nano),
			"transport":           "websocket",
		},
	})
	if status == "success" {
		s.recordRelayUpstreamSuccess(ctx, principal, selection)
	}
	return true, false, nil
}

func (s *Service) acquireRelayWebSocketUpstream(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, started time.Time) (func(), error) {
	release, acquired := s.tryAcquireRelayUpstreamConcurrency(selection.upstream)
	if acquired {
		return release, nil
	}
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
		Status:               "rate_limited",
		HTTPStatus:           http.StatusTooManyRequests,
		ErrorCode:            "upstream_concurrency_limited",
		ErrorMessage:         "relay upstream concurrency limit exceeded",
		CacheStatus:          relayCacheBypass,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		CreatedAt:            started,
	})
	return nil, fmt.Errorf("%w: relay upstream concurrency limit exceeded", apperrors.ErrAccessDenied)
}

func relayWebSocketBridgeOutcome(err error) (string, string, string) {
	if err == nil {
		return "success", "", ""
	}
	if errors.Is(err, context.Canceled) || relayWebSocketCloseError(err) {
		return "client_cancelled", "client_cancelled", err.Error()
	}
	return "failure", "realtime_ws_closed", err.Error()
}

func (s *Service) prepareRelayWebSocketUpstream(selection relaySelection, publicModel string) (string, string, error) {
	targetURL, err := relayRealtimeEndpointURLForSelection(selection, publicModel)
	if err != nil {
		return "", "", err
	}
	if err := s.validateRelayWebSocketUpstreamURL(targetURL); err != nil {
		return "", "", err
	}
	apiKey, err := s.decryptRelayAPIKey(selection.upstream.APIKeyCiphertext)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(apiKey) == "" {
		return "", "", fmt.Errorf("%w: relay upstream API key is not configured", apperrors.ErrInvalidArgument)
	}
	return targetURL, apiKey, nil
}

func relayWebSocketDialTimeout(config LLMRelayConfig) time.Duration {
	timeout := config.StreamTimeout
	if timeout <= 0 {
		timeout = config.DefaultTimeout
	}
	if config.FirstByteTimeout > 0 && (timeout <= 0 || config.FirstByteTimeout < timeout) {
		return config.FirstByteTimeout
	}
	return timeout
}

func relayWebSocketHopHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "connection", "upgrade", "sec-websocket-key", "sec-websocket-accept", "sec-websocket-version", "sec-websocket-protocol", "sec-websocket-extensions":
		return true
	default:
		return false
	}
}

func relayWebSocketDialer() *websocket.Dialer {
	dialer := *websocket.DefaultDialer
	return &dialer
}

func relayWebSocketUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return relayAllowWebSocketOrigin(r)
		},
	}
}

const relayWebSocketMessageReadLimit = 8 << 20

func relayAllowWebSocketOrigin(r *http.Request) bool {
	if r == nil || len(r.Header.Values("Origin")) > 1 {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	return relayIsLocalHost(parsed.Hostname()) && relayIsLocalHost(relayHostName(r.Host))
}

func relayHostName(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(hostport, "[]")
}

func relayIsLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

type relayWebSocketProxyResult struct {
	firstByteAt   time.Time
	clientBytes   int64
	upstreamBytes int64
	err           error
}

func relayProxyWebSocketMessages(ctx context.Context, clientConn, upstreamConn *websocket.Conn, upstreamIdleTimeout time.Duration) (time.Time, int64, int64, error) {
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

func relayCopyWebSocketMessages(ctx context.Context, dst, src *websocket.Conn, clientToUpstream bool, idleTimeout time.Duration, results chan<- relayWebSocketProxyResult, cancel context.CancelFunc, closeBoth func()) {
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

func (s *Service) validateRelayWebSocketUpstreamURL(rawURL string) error {
	return s.relayTransportComponent().validateURL(rawURL, true)
}

func relayRealtimeUpstreamHeaders(req LLMRelayHTTPRequest, upstream domainaigateway.LLMUpstream, apiKey string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	for key, value := range upstream.DefaultHeaders {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" || isSensitiveRelayHeader(name) || relayWebSocketHopHeader(name) {
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
