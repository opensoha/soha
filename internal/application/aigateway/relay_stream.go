package aigateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	"github.com/opensoha/soha/internal/application/aigateway/relayport"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) RelayLLMWebSocket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, upgrade relayport.UpgradeWebSocket) error {
	if !s.relayConfig.Enabled {
		return fmt.Errorf("%w: AI Gateway LLM relay is disabled", apperrors.ErrNotFound)
	}
	if normalizeRelayProviderKind(req.ProviderKind) != "openai" || strings.TrimSpace(req.Endpoint) != "realtime" {
		return fmt.Errorf("%w: realtime relay only supports OpenAI-compatible websocket requests", apperrors.ErrInvalidArgument)
	}
	if upgrade == nil {
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
	return s.proxyRelayWebSocketWithFallback(ctx, principal, accessCtx, req, authorized, model, upgrade)
}

func (s *Service) proxyRelayWebSocketWithFallback(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selections []relaySelection, publicModel string, upgrade relayport.UpgradeWebSocket) error {
	var lastErr error
	attempts := relayFallbackMaxAttempts(selections)
	for index, selection := range selections {
		if attempts > 0 && index >= attempts {
			break
		}
		if err := s.enforceRelayRateLimits(ctx, principal, accessCtx, req, selection, publicModel, true); err != nil {
			return err
		}
		upgraded, retryable, err := s.proxyRelayWebSocket(ctx, principal, accessCtx, req, selection, publicModel, upgrade)
		if err == nil || upgraded || !retryable || index == len(selections)-1 || ctx.Err() != nil {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (s *Service) proxyRelayWebSocket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, upgrade relayport.UpgradeWebSocket) (bool, bool, error) {
	started := time.Now().UTC()
	release, err := s.acquireRelayWebSocketUpstream(ctx, principal, accessCtx, req, selection, publicModel, started)
	if err != nil {
		return false, true, err
	}
	defer release()

	if err := s.requireRelayTransport(); err != nil {
		return false, false, err
	}
	apiKey, err := s.decryptRelayAPIKey(selection.upstream.APIKeyCiphertext)
	if err != nil {
		return false, false, err
	}
	result, err := s.relayTransport.WebSocket(ctx, relayProviderRequest(req), relayProviderTarget(selection.upstream, selection.route), relayport.WebSocketOptions{
		APIKey: apiKey, PublicModel: publicModel,
		DialTimeout: relayWebSocketDialTimeout(s.relayConfig), IdleTimeout: s.relayConfig.StreamIdleTimeout,
		Upgrade: func(headers http.Header) (relayport.WebSocketConn, error) {
			writeRelayRouteTraceHeaders(headers, req, selection, publicModel, true, http.StatusSwitchingProtocols, relayCacheBypass)
			return upgrade(headers)
		},
	})
	if err != nil {
		return false, false, err
	}
	if result.DialError != nil {
		status := http.StatusBadGateway
		errorCode := "upstream_request_failed"
		if result.HTTPStatus != 0 {
			status = result.HTTPStatus
			errorCode = relayErrorCodeForStatus(status)
			if errorCode == "" {
				errorCode = "upstream_ws_failed"
			}
		}
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
			Status:               relayStatusFromError(ctx, result.DialError),
			HTTPStatus:           status,
			UpstreamStatus:       status,
			ErrorCode:            errorCode,
			ErrorMessage:         redactRelayText(result.DialError.Error()),
			CacheStatus:          relayCacheBypass,
			DurationMilliseconds: time.Since(started).Milliseconds(),
			CreatedAt:            started,
		})
		s.recordRelayUpstreamFailure(ctx, principal, selection, errorCode)
		return false, true, fmt.Errorf("%w: relay realtime upstream connection failed", apperrors.ErrClusterUnready)
	}
	if result.UpgradeError != nil {
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
			Status:               "client_cancelled",
			HTTPStatus:           http.StatusBadRequest,
			UpstreamStatus:       http.StatusSwitchingProtocols,
			ErrorCode:            "client_upgrade_failed",
			ErrorMessage:         redactRelayText(result.UpgradeError.Error()),
			CacheStatus:          relayCacheBypass,
			DurationMilliseconds: time.Since(started).Milliseconds(),
			CreatedAt:            started,
		})
		return false, false, nil
	}
	status, errorCode, errorMessage := relayWebSocketBridgeOutcome(result)
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
		Status:               status,
		HTTPStatus:           http.StatusSwitchingProtocols,
		UpstreamStatus:       http.StatusSwitchingProtocols,
		ErrorCode:            errorCode,
		ErrorMessage:         redactRelayText(errorMessage),
		InputBytes:           result.InputBytes,
		OutputBytes:          result.OutputBytes,
		CacheStatus:          relayCacheBypass,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		CreatedAt:            started,
		TTFBMilliseconds:     relayDurationMilliseconds(started, result.FirstByteAt),
		TTFTMilliseconds:     relayDurationMilliseconds(started, result.FirstByteAt),
		Metadata: map[string]any{
			"upstreamConnectedAt": result.UpstreamStarted.Format(time.RFC3339Nano),
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

func relayWebSocketBridgeOutcome(result relayport.WebSocketResult) (string, string, string) {
	if result.BridgeError == nil {
		return "success", "", ""
	}
	if result.ClientClosed {
		return "client_cancelled", "client_cancelled", result.BridgeError.Error()
	}
	return "failure", "realtime_ws_closed", result.BridgeError.Error()
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
