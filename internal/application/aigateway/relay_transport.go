package aigateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/application/aigateway/relayport"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) proxyRelayRequestWithFallback(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selections []relaySelection, publicModel string, stream bool, writer http.ResponseWriter) (relayExecutionReceipt, error) {
	var lastErr error
	attempts := relayFallbackMaxAttempts(selections)
	for index, selection := range selections {
		if attempts > 0 && index >= attempts {
			break
		}
		if err := s.enforceRelayRateLimits(ctx, principal, accessCtx, req, selection, publicModel, stream); err != nil {
			return relayExecutionReceipt{}, err
		}
		receipt, wrote, retryable, err := s.proxyRelayRequest(ctx, principal, accessCtx, req, selection, publicModel, stream, writer)
		if err == nil || wrote || !retryable || index == len(selections)-1 || ctx.Err() != nil {
			return receipt, err
		}
		lastErr = err
	}
	return relayExecutionReceipt{}, lastErr
}

type relayTransportFailure struct {
	status      string
	code        string
	message     string
	httpStatus  int
	retryable   bool
	publicError error
}

func (s *Service) recordRelayConcurrencyLimit(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, body []byte, cacheStatus string, started time.Time) {
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, domainaigateway.LLMCallLog{
		Status:               "rate_limited",
		HTTPStatus:           http.StatusTooManyRequests,
		ErrorCode:            "upstream_concurrency_limited",
		ErrorMessage:         "relay upstream concurrency limit exceeded",
		InputBytes:           int64(len(body)),
		CacheStatus:          cacheStatus,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		CreatedAt:            started,
	})
}

func (s *Service) finishRelayTransportFailure(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, input []byte, cacheStatus string, started time.Time, result relayport.HTTPResult) (bool, bool, error) {
	failure := relayHTTPFailureForResult(ctx, stream, result)
	item := domainaigateway.LLMCallLog{
		Status:               failure.status,
		HTTPStatus:           failure.httpStatus,
		ErrorCode:            failure.code,
		ErrorMessage:         redactRelayText(failure.message),
		InputBytes:           int64(len(input)),
		OutputBytes:          result.OutputBytes,
		CacheStatus:          cacheStatus,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		CreatedAt:            started,
	}
	if result.Response != nil {
		item = relayCallLogFromResponse(req, result.Response, started, result.UpstreamStarted, result.FirstByteAt, result.Body, int64(len(input)), failure.status, failure.code, failure.message, cacheStatus)
		item.OutputBytes = result.OutputBytes
	}
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, item)
	if failure.code != "client_cancelled" {
		s.recordRelayUpstreamFailure(ctx, principal, selection, failure.code)
	}
	return result.Wrote, failure.retryable, failure.publicError
}

func (s *Service) finishRelayTransportResponse(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, input []byte, cacheAttempt relayResponseCacheAttempt, cacheStatus string, started time.Time, result relayport.HTTPResult, writer http.ResponseWriter) bool {
	status := "success"
	if !relayport.IsSuccessStatus(result.Response.StatusCode) {
		status = "failure"
	}
	responseBody := result.Body
	if !stream {
		if status == "success" {
			cacheStatus = s.storeRelayResponseCache(ctx, cacheAttempt, result.Response, responseBody)
		}
		relayport.CopyResponseHeaders(writer.Header(), result.Response.Header, relayport.ResponseContentType(req.Endpoint))
		writeRelayRouteTraceHeaders(writer.Header(), req, selection, publicModel, false, result.Response.StatusCode, cacheStatus)
		writer.WriteHeader(result.Response.StatusCode)
		if _, err := writer.Write(responseBody); err != nil {
			s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, false, relayCallLogFromResponse(req, result.Response, started, result.UpstreamStarted, result.FirstByteAt, responseBody, int64(len(input)), "client_cancelled", "client_cancelled", err.Error(), cacheStatus))
			return false
		}
	}
	item := relayCallLogFromResponse(req, result.Response, started, result.UpstreamStarted, result.FirstByteAt, responseBody, int64(len(input)), status, "", "", cacheStatus)
	item.OutputBytes = result.OutputBytes
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, item)
	if status == "success" {
		s.recordRelayUpstreamSuccess(ctx, principal, selection)
	}
	return status == "success" && ctx.Err() == nil
}

func relayErrorCodeForStatus(status int) string {
	switch {
	case relayport.IsRedirectStatus(status):
		return "upstream_redirect_blocked"
	case status == http.StatusTooManyRequests:
		return "upstream_429"
	case status >= 500:
		return "upstream_5xx"
	case status >= 400:
		return "upstream_4xx"
	default:
		return ""
	}
}

func (s *Service) proxyRelayRequest(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, writer http.ResponseWriter) (relayExecutionReceipt, bool, bool, error) {
	started := time.Now().UTC()
	prepared, err := s.prepareRelayRequest(req, selection, stream)
	if err != nil {
		return relayExecutionReceipt{}, false, false, err
	}
	body := prepared.Body
	cacheAttempt := s.relayResponseCacheAttempt(accessCtx, req, selection, publicModel, stream, body, prepared.URL)
	if served, written := s.writeRelayCachedResponse(ctx, principal, accessCtx, req, selection, publicModel, stream, cacheAttempt, started, writer); served {
		if written && ctx.Err() == nil {
			return relayExecutionReceipt{Selection: selection, Succeeded: true, CacheHit: true}, true, false, nil
		}
		return relayExecutionReceipt{}, true, false, nil
	}
	cacheStatus := cacheAttempt.statusOnMiss()
	release, acquired := s.tryAcquireRelayUpstreamConcurrency(selection.upstream)
	if !acquired {
		s.recordRelayConcurrencyLimit(ctx, principal, accessCtx, req, selection, publicModel, stream, body, cacheStatus, started)
		return relayExecutionReceipt{}, false, true, fmt.Errorf("%w: relay upstream concurrency limit exceeded", apperrors.ErrAccessDenied)
	}
	defer release()
	opts, err := s.relayHTTPOptions(req, selection, publicModel, stream, writer, cacheStatus)
	if err != nil {
		return relayExecutionReceipt{}, false, false, err
	}
	result, err := s.relayTransport.Execute(ctx, prepared, opts)
	if err != nil {
		if result.Response != nil {
			item := relayCallLogFromResponse(req, result.Response, started, result.UpstreamStarted, result.FirstByteAt, result.Body, int64(len(body)), "failure", "relay_transform_failed", err.Error(), cacheStatus)
			item.OutputBytes = result.OutputBytes
			s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, item)
		}
		return relayExecutionReceipt{}, false, false, err
	}
	completionErr := completeRelayWorkbenchStream(ctx, req.Endpoint, writer, &result)
	if result.Failure != nil || relayport.IsRedirectStatus(result.Response.StatusCode) || !stream && relayport.IsRetryableStatus(result.Response.StatusCode) {
		wrote, retryable, err := s.finishRelayTransportFailure(ctx, principal, accessCtx, req, selection, publicModel, stream, body, cacheStatus, started, result)
		if completionErr != nil {
			err = completionErr
		}
		return relayExecutionReceipt{}, wrote, retryable, err
	}
	if s.finishRelayTransportResponse(ctx, principal, accessCtx, req, selection, publicModel, stream, body, cacheAttempt, cacheStatus, started, result, writer) {
		return relayExecutionReceipt{Selection: selection, Succeeded: true}, true, false, nil
	}
	return relayExecutionReceipt{}, true, false, nil
}

func completeRelayWorkbenchStream(ctx context.Context, endpoint string, writer http.ResponseWriter, result *relayport.HTTPResult) error {
	workbench, ok := writer.(*workbenchRelayStreamWriter)
	if !ok {
		return nil
	}
	err := ctx.Err()
	if err == nil && result.Failure == nil && result.Response != nil && relayport.IsSuccessStatus(result.Response.StatusCode) {
		err = workbench.complete(ctx, endpoint)
	}
	if err == nil {
		return nil
	}
	code := "upstream_incomplete_stream"
	if ctx.Err() != nil || errors.Is(err, ErrWorkbenchRelayStreamStopped) {
		code = "client_cancelled"
	}
	result.Failure = &relayport.HTTPFailure{Code: code, Message: err.Error()}
	return err
}

func (s *Service) prepareRelayRequest(req LLMRelayHTTPRequest, selection relaySelection, stream bool) (relayport.PreparedRequest, error) {
	if err := s.requireRelayTransport(); err != nil {
		return relayport.PreparedRequest{}, err
	}
	plan, err := relayRequestTransformPlan(req, selection, stream)
	if err != nil {
		return relayport.PreparedRequest{}, err
	}
	return s.relayTransport.Prepare(relayProviderRequest(req), relayProviderTarget(selection.upstream, selection.route), relayport.RequestOptions{
		Stream: stream, IncludeOpenAIUsage: s.relayConfig.IncludeUsageForOpenAIStream,
		ConversionProvider: plan.upstreamProvider, ConversionEndpoint: plan.upstreamEndpoint,
	})
}

func relayProviderRequest(req LLMRelayHTTPRequest) relayport.Request {
	return relayport.Request{ProviderKind: req.ProviderKind, Endpoint: req.Endpoint, Headers: req.Headers, Body: req.Body}
}

func relayProviderTarget(upstream domainaigateway.LLMUpstream, route domainaigateway.LLMModelRoute) relayport.Target {
	return relayport.Target{
		ID: upstream.ID, BaseURL: upstream.BaseURL, ProviderKind: upstream.ProviderKind, Model: route.UpstreamModel,
		DefaultHeaders: upstream.DefaultHeaders, Metadata: upstream.Metadata, RouteMetadata: route.Metadata,
	}
}

func (s *Service) relayHTTPOptions(req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, writer http.ResponseWriter, cacheStatus string) (relayport.HTTPOptions, error) {
	apiKey, err := s.decryptRelayAPIKey(selection.upstream.APIKeyCiphertext)
	if err != nil {
		return relayport.HTTPOptions{}, fmt.Errorf("%w: relay upstream API key decrypt failed", apperrors.ErrInvalidArgument)
	}
	opts := relayport.HTTPOptions{
		APIKey: apiKey, Timeout: s.relayConfig.DefaultTimeout,
	}
	if stream {
		opts.Timeout = s.relayConfig.StreamTimeout
		opts.FirstByteTimeout = s.relayConfig.FirstByteTimeout
		opts.IdleTimeout = s.relayConfig.StreamIdleTimeout
		opts.StartStream = func(resp *http.Response) io.Writer {
			relayport.CopyResponseHeaders(writer.Header(), resp.Header, relayport.ResponseContentType(req.Endpoint))
			writeRelayRouteTraceHeaders(writer.Header(), req, selection, publicModel, true, resp.StatusCode, cacheStatus)
			writer.WriteHeader(resp.StatusCode)
			return writer
		}
	}
	return opts, nil
}

func relayHTTPFailureForResult(ctx context.Context, stream bool, result relayport.HTTPResult) relayTransportFailure {
	status := http.StatusBadGateway
	if result.Response != nil {
		status = result.Response.StatusCode
	}
	failure := relayTransportFailure{status: "failure", httpStatus: status, retryable: true}
	if result.Failure == nil {
		failure.code = relayErrorCodeForStatus(status)
		failure.message = "retryable upstream response"
		failure.publicError = fmt.Errorf("%w: relay upstream returned retryable status %d", apperrors.ErrClusterUnready, status)
		return failure
	}
	failure.code = result.Failure.Code
	failure.message = result.Failure.Message
	switch failure.code {
	case "client_cancelled":
		failure.status, failure.retryable = "client_cancelled", false
	case "upstream_incomplete_stream":
		failure.retryable = false
	case "upstream_read_failed":
		failure.status, failure.retryable = relayStatusFromError(ctx, nil), !stream
		if !stream {
			failure.publicError = fmt.Errorf("%w: relay upstream read failed", apperrors.ErrClusterUnready)
		}
	case "upstream_response_too_large":
		failure.httpStatus = http.StatusBadGateway
		failure.publicError = fmt.Errorf("%w: relay upstream response is too large", apperrors.ErrClusterUnready)
	default:
		failure.status = relayStatusFromError(ctx, nil)
		failure.publicError = fmt.Errorf("%w: relay upstream request failed", apperrors.ErrClusterUnready)
	}
	return failure
}

func (s *Service) testRelayUpstream(ctx context.Context, upstream domainaigateway.LLMUpstream) (domainaigateway.LLMUpstreamTestResult, error) {
	if err := s.requireRelayTransport(); err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	apiKey, err := s.decryptRelayAPIKey(upstream.APIKeyCiphertext)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	timeout := s.relayConfig.DefaultTimeout
	if upstream.TimeoutSeconds > 0 {
		timeout = time.Duration(upstream.TimeoutSeconds) * time.Second
	}
	return s.relayTransport.Probe(ctx, relayProviderTarget(upstream, domainaigateway.LLMModelRoute{}), apiKey, timeout)
}

func relayFallbackMaxAttempts(selections []relaySelection) int {
	if len(selections) == 0 {
		return 0
	}
	values := selections[0].route.FallbackPolicy
	attempts := firstNonZeroInt(
		intFromAny(values["maxAttempts"]),
		intFromAny(values["max_attempts"]),
		intFromAny(values["attempts"]),
	)
	if attempts <= 0 {
		return 0
	}
	if attempts > len(selections) {
		return len(selections)
	}
	return attempts
}

func (s *Service) tryAcquireRelayTokenConcurrency(accessCtx domainidentity.AccessContext, stream bool) (func(), string, string, bool) {
	tokenKey := relayRateLimitTokenClientID(accessCtx)
	releases := make([]func(), 0, 2)
	if limit := relayTokenConcurrencyLimit(accessCtx.Metadata); limit > 0 {
		release, ok := s.tryAcquireRelayConcurrency(tokenKey+":requests", limit)
		if !ok {
			return func() {}, "token_concurrency_limited", "relay token concurrency limit exceeded", false
		}
		releases = append(releases, release)
	}
	if stream {
		if limit := relayTokenStreamConcurrencyLimit(accessCtx.Metadata); limit > 0 {
			release, ok := s.tryAcquireRelayConcurrency(tokenKey+":streams", limit)
			if !ok {
				releaseRelayConcurrencyAll(releases)
				return func() {}, "token_stream_concurrency_limited", "relay token stream concurrency limit exceeded", false
			}
			releases = append(releases, release)
		}
	}
	return func() {
		releaseRelayConcurrencyAll(releases)
	}, "", "", true
}

func relayTokenConcurrencyLimit(metadata map[string]any) int {
	values := gatewayConditionValues(metadata, "concurrency", "concurrencyLimit", "concurrencyLimits", "limits")
	limit, _ := gatewayFirstPositiveInt(values,
		"maxConcurrentRequests",
		"maxConcurrency",
		"concurrentRequests",
		"concurrency",
		"requestConcurrency",
		"maxParallelRequests",
	)
	return limit
}

func relayTokenStreamConcurrencyLimit(metadata map[string]any) int {
	values := gatewayConditionValues(metadata, "concurrency", "concurrencyLimit", "concurrencyLimits", "streamConcurrency", "streamConcurrencyLimit", "limits")
	limit, _ := gatewayFirstPositiveInt(values,
		"maxConcurrentStreamingRequests",
		"maxConcurrentStreams",
		"maxStreamConcurrency",
		"streamConcurrency",
		"concurrentStreams",
		"streamingConcurrency",
	)
	return limit
}

func releaseRelayConcurrencyAll(releases []func()) {
	for i := len(releases) - 1; i >= 0; i-- {
		if releases[i] != nil {
			releases[i]()
		}
	}
}

func (s *Service) tryAcquireRelayUpstreamConcurrency(upstream domainaigateway.LLMUpstream) (func(), bool) {
	limit := upstream.MaxConcurrency
	if s == nil || limit <= 0 {
		return func() {}, true
	}
	key := strings.TrimSpace(upstream.ID)
	if key == "" {
		key = strings.TrimSpace(upstream.Name)
	}
	if key == "" {
		return func() {}, true
	}
	return s.tryAcquireRelayConcurrency("upstream:"+key, limit)
}

func (s *Service) tryAcquireRelayConcurrency(key string, limit int) (func(), bool) {
	key = strings.TrimSpace(key)
	if s == nil || limit <= 0 || key == "" {
		return func() {}, true
	}
	s.relayConcurrencyMu.Lock()
	if s.relayConcurrency == nil {
		s.relayConcurrency = map[string]int{}
	}
	if s.relayConcurrency[key] >= limit {
		s.relayConcurrencyMu.Unlock()
		return func() {}, false
	}
	s.relayConcurrency[key]++
	s.relayConcurrencyMu.Unlock()
	released := false
	return func() {
		s.relayConcurrencyMu.Lock()
		defer s.relayConcurrencyMu.Unlock()
		if released {
			return
		}
		released = true
		if s.relayConcurrency[key] <= 1 {
			delete(s.relayConcurrency, key)
			return
		}
		s.relayConcurrency[key]--
	}, true
}
