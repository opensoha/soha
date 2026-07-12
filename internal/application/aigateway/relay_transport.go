package aigateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) proxyRelayRequestWithFallback(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selections []relaySelection, publicModel string, stream bool, writer http.ResponseWriter) error {
	var lastErr error
	attempts := relayFallbackMaxAttempts(selections)
	for index, selection := range selections {
		if attempts > 0 && index >= attempts {
			break
		}
		if err := s.enforceRelayRateLimits(ctx, principal, accessCtx, req, selection, publicModel, stream); err != nil {
			return err
		}
		wrote, retryable, err := s.proxyRelayRequest(ctx, principal, accessCtx, req, selection, publicModel, stream, writer)
		if err == nil || wrote || !retryable || index == len(selections)-1 || ctx.Err() != nil {
			return err
		}
		lastErr = err
	}
	return lastErr
}

type relayHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type relayTransportRequest struct {
	request             LLMRelayHTTPRequest
	selection           relaySelection
	publicModel         string
	body                []byte
	upstreamProvider    string
	upstreamEndpoint    string
	upstreamContentType string
	cacheStatus         string
	stream              bool
	writer              http.ResponseWriter
}

type relayTransportFailure struct {
	status      string
	code        string
	message     string
	httpStatus  int
	retryable   bool
	publicError error
}

type relayTransportResult struct {
	response        *http.Response
	body            []byte
	outputBytes     int64
	bodyTruncated   bool
	upstreamStarted time.Time
	firstByteAt     time.Time
	wrote           bool
	failure         *relayTransportFailure
}

const (
	relayMaxNonStreamResponseBytes = 16 << 20
	relayStreamAuditBufferBytes    = 1 << 20
)

type relayHTTPTransport struct {
	client      relayHTTPDoer
	config      LLMRelayConfig
	credentials *relayCredentialCodec
}

func newRelayHTTPTransport(client relayHTTPDoer, config LLMRelayConfig, credentials *relayCredentialCodec) *relayHTTPTransport {
	return &relayHTTPTransport{
		client:      client,
		config:      config,
		credentials: credentials,
	}
}

func (t *relayHTTPTransport) execute(ctx context.Context, input relayTransportRequest) (relayTransportResult, error) {
	upstreamReq, cancel, err := t.buildRequest(ctx, input)
	if err != nil {
		return relayTransportResult{}, err
	}
	defer cancel()

	result := relayTransportResult{upstreamStarted: time.Now().UTC()}
	var firstByteTimer *time.Timer
	if t.config.FirstByteTimeout > 0 {
		firstByteTimer = time.AfterFunc(t.config.FirstByteTimeout, cancel)
	}
	resp, err := t.client.Do(upstreamReq)
	if firstByteTimer != nil {
		firstByteTimer.Stop()
	}
	if err != nil {
		result.failure = &relayTransportFailure{
			status:      relayStatusFromError(ctx, err),
			code:        "upstream_request_failed",
			message:     err.Error(),
			httpStatus:  http.StatusBadGateway,
			retryable:   true,
			publicError: fmt.Errorf("%w: relay upstream request failed", apperrors.ErrClusterUnready),
		}
		return result, nil
	}
	defer func() { _ = resp.Body.Close() }()
	result.response = resp

	if !input.stream && relayRetryableUpstreamStatus(resp.StatusCode) {
		result.body, _ = io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
		result.outputBytes = int64(len(result.body))
		result.failure = &relayTransportFailure{
			status:      "failure",
			code:        relayErrorCodeForStatus(resp.StatusCode),
			message:     "retryable upstream response",
			httpStatus:  resp.StatusCode,
			retryable:   true,
			publicError: fmt.Errorf("%w: relay upstream returned retryable status %d", apperrors.ErrClusterUnready, resp.StatusCode),
		}
		return result, nil
	}

	if input.stream {
		copyRelayResponseHeaders(input.writer.Header(), resp.Header, relayDefaultResponseContentType(input.request.Endpoint))
		writeRelayRouteTraceHeaders(input.writer.Header(), input.request, input.selection, input.publicModel, true, resp.StatusCode, input.cacheStatus)
		input.writer.WriteHeader(resp.StatusCode)
	}

	return t.copyResponse(ctx, input, result, cancel)
}

func (t *relayHTTPTransport) buildRequest(ctx context.Context, input relayTransportRequest) (*http.Request, context.CancelFunc, error) {
	targetURL, err := relayEndpointURLForSelection(input.selection, input.upstreamProvider, input.upstreamEndpoint)
	if err != nil {
		return nil, func() {}, err
	}
	if err := t.validateURL(targetURL, false); err != nil {
		return nil, func() {}, err
	}
	apiKey, err := t.credentials.decrypt(input.selection.upstream.APIKeyCiphertext)
	if err != nil {
		return nil, func() {}, fmt.Errorf("%w: relay upstream API key decrypt failed", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, func() {}, fmt.Errorf("%w: relay upstream API key is not configured", apperrors.ErrInvalidArgument)
	}
	timeout := t.config.DefaultTimeout
	if input.stream {
		timeout = t.config.StreamTimeout
	}
	upstreamCtx, cancel := context.WithTimeout(ctx, timeout)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(input.body))
	if err != nil {
		cancel()
		return nil, func() {}, fmt.Errorf("build relay upstream request: %w", err)
	}
	upstreamRelayReq := input.request
	upstreamRelayReq.ProviderKind = input.upstreamProvider
	upstreamRelayReq.Endpoint = input.upstreamEndpoint
	if input.upstreamContentType != "" {
		upstreamRelayReq.Headers = upstreamRelayReq.Headers.Clone()
		upstreamRelayReq.Headers.Set("Content-Type", input.upstreamContentType)
	}
	applyRelayUpstreamHeaders(upstreamReq.Header, upstreamRelayReq, input.selection.upstream, apiKey)
	return upstreamReq, cancel, nil
}

func (t *relayHTTPTransport) copyResponse(ctx context.Context, input relayTransportRequest, result relayTransportResult, cancel context.CancelFunc) (relayTransportResult, error) {
	var idleTimer *time.Timer
	if input.stream && t.config.StreamIdleTimeout > 0 {
		idleTimer = time.AfterFunc(t.config.StreamIdleTimeout, cancel)
		defer idleTimer.Stop()
	}
	var output []byte
	buf := make([]byte, 32*1024)
	for {
		n, readErr := result.response.Body.Read(buf)
		if n > 0 {
			if idleTimer != nil {
				idleTimer.Reset(t.config.StreamIdleTimeout)
			}
			if result.firstByteAt.IsZero() {
				result.firstByteAt = time.Now().UTC()
			}
			chunk := buf[:n]
			result.outputBytes += int64(n)
			if input.stream {
				output, result.bodyTruncated = appendRelayTail(output, chunk, relayStreamAuditBufferBytes)
			} else if len(output)+len(chunk) > relayMaxNonStreamResponseBytes {
				remaining := relayMaxNonStreamResponseBytes - len(output)
				if remaining > 0 {
					output = append(output, chunk[:remaining]...)
				}
				result.body = output
				result.bodyTruncated = true
				result.failure = &relayTransportFailure{
					status:      "failure",
					code:        "upstream_response_too_large",
					message:     "relay upstream response exceeds the configured limit",
					httpStatus:  http.StatusBadGateway,
					retryable:   true,
					publicError: fmt.Errorf("%w: relay upstream response is too large", apperrors.ErrClusterUnready),
				}
				return result, nil
			} else {
				output = append(output, chunk...)
			}
			if input.stream {
				if _, err := input.writer.Write(chunk); err != nil {
					result.body = output
					result.wrote = true
					result.failure = &relayTransportFailure{
						status:      "client_cancelled",
						code:        "client_cancelled",
						message:     err.Error(),
						httpStatus:  result.response.StatusCode,
						publicError: nil,
					}
					return result, nil
				}
				if flusher, ok := input.writer.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			result.body = output
			result.wrote = input.stream && !result.firstByteAt.IsZero()
			result.failure = &relayTransportFailure{
				status:      relayStatusFromError(ctx, readErr),
				code:        "upstream_read_failed",
				message:     readErr.Error(),
				httpStatus:  result.response.StatusCode,
				retryable:   !input.stream,
				publicError: nil,
			}
			if !input.stream {
				result.failure.publicError = fmt.Errorf("%w: relay upstream read failed", apperrors.ErrClusterUnready)
			}
			return result, nil
		}
	}
	result.body = output
	result.wrote = input.stream
	return result, nil
}

func appendRelayTail(dst, chunk []byte, limit int) ([]byte, bool) {
	if limit <= 0 {
		return nil, len(dst) > 0 || len(chunk) > 0
	}
	if len(chunk) >= limit {
		return append(dst[:0], chunk[len(chunk)-limit:]...), true
	}
	overflow := len(dst) + len(chunk) - limit
	truncated := overflow > 0
	if overflow > 0 {
		copy(dst, dst[overflow:])
		dst = dst[:len(dst)-overflow]
	}
	return append(dst, chunk...), truncated
}

func (s *Service) proxyRelayRequest(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, writer http.ResponseWriter) (bool, bool, error) {
	started := time.Now().UTC()
	body, upstreamProvider, upstreamEndpoint, upstreamContentType, err := relayTransformUpstreamRequest(req, selection, stream, s.relayConfig.IncludeUsageForOpenAIStream)
	if err != nil {
		return false, false, err
	}
	cacheAttempt := s.relayResponseCacheAttempt(accessCtx, req, selection, publicModel, stream, body)
	if served, err := s.writeRelayCachedResponse(ctx, principal, accessCtx, req, selection, publicModel, stream, cacheAttempt, started, writer); served || err != nil {
		return served, false, err
	}
	cacheStatus := cacheAttempt.statusOnMiss()
	release, acquired := s.tryAcquireRelayUpstreamConcurrency(selection.upstream)
	if !acquired {
		s.recordRelayConcurrencyLimit(ctx, principal, accessCtx, req, selection, publicModel, stream, body, cacheStatus, started)
		return false, true, fmt.Errorf("%w: relay upstream concurrency limit exceeded", apperrors.ErrAccessDenied)
	}
	defer release()
	result, err := s.relayTransportComponent().execute(ctx, relayTransportRequest{
		request:             req,
		selection:           selection,
		publicModel:         publicModel,
		body:                body,
		upstreamProvider:    upstreamProvider,
		upstreamEndpoint:    upstreamEndpoint,
		upstreamContentType: upstreamContentType,
		cacheStatus:         cacheStatus,
		stream:              stream,
		writer:              writer,
	})
	if err != nil {
		return false, false, err
	}
	if result.failure != nil {
		return s.finishRelayTransportFailure(ctx, principal, accessCtx, req, selection, publicModel, stream, body, cacheStatus, started, result)
	}
	return s.finishRelayTransportSuccess(ctx, principal, accessCtx, req, selection, publicModel, stream, body, cacheAttempt, cacheStatus, started, result, writer)
}

func (s *Service) relayTransportComponent() *relayHTTPTransport {
	if s.relayTransport != nil {
		return s.relayTransport
	}
	return newRelayHTTPTransport(normalizedHTTPClient(s.httpClient), s.relayConfig, s.relayCredentialCodec())
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

func (s *Service) finishRelayTransportFailure(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, input []byte, cacheStatus string, started time.Time, result relayTransportResult) (bool, bool, error) {
	failure := result.failure
	item := domainaigateway.LLMCallLog{
		Status:               failure.status,
		HTTPStatus:           failure.httpStatus,
		ErrorCode:            failure.code,
		ErrorMessage:         redactRelayText(failure.message),
		InputBytes:           int64(len(input)),
		OutputBytes:          result.outputBytes,
		CacheStatus:          cacheStatus,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		CreatedAt:            started,
	}
	if result.response != nil {
		item = relayCallLogFromResponse(req, result.response, started, result.upstreamStarted, result.firstByteAt, result.body, int64(len(input)), failure.status, failure.code, failure.message, cacheStatus)
		item.OutputBytes = result.outputBytes
	}
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, item)
	if failure.code != "client_cancelled" {
		s.recordRelayUpstreamFailure(ctx, principal, selection, failure.code)
	}
	return result.wrote, failure.retryable, failure.publicError
}

func (s *Service) finishRelayTransportSuccess(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, input []byte, cacheAttempt relayResponseCacheAttempt, cacheStatus string, started time.Time, result relayTransportResult, writer http.ResponseWriter) (bool, bool, error) {
	status := "success"
	if result.response.StatusCode >= http.StatusBadRequest {
		status = "failure"
	}
	responseBody := result.body
	var err error
	if !stream {
		responseBody, err = relayTransformSuccessfulResponse(req, selection, false, status, result.body)
	}
	if err != nil {
		item := relayCallLogFromResponse(req, result.response, started, result.upstreamStarted, result.firstByteAt, result.body, int64(len(input)), "failure", "relay_transform_failed", err.Error(), cacheStatus)
		item.OutputBytes = result.outputBytes
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, item)
		return false, false, err
	}
	if !stream {
		if status == "success" {
			cacheStatus = s.storeRelayResponseCache(ctx, cacheAttempt, result.response, responseBody)
		}
		copyRelayResponseHeaders(writer.Header(), result.response.Header, relayDefaultResponseContentType(req.Endpoint))
		writeRelayRouteTraceHeaders(writer.Header(), req, selection, publicModel, false, result.response.StatusCode, cacheStatus)
		writer.WriteHeader(result.response.StatusCode)
		if _, err := writer.Write(responseBody); err != nil {
			s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, false, relayCallLogFromResponse(req, result.response, started, result.upstreamStarted, result.firstByteAt, responseBody, int64(len(input)), "client_cancelled", "client_cancelled", err.Error(), cacheStatus))
			return true, false, nil
		}
	}
	item := relayCallLogFromResponse(req, result.response, started, result.upstreamStarted, result.firstByteAt, responseBody, int64(len(input)), status, "", "", cacheStatus)
	item.OutputBytes = result.outputBytes
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, item)
	if result.response.StatusCode < http.StatusBadRequest {
		s.recordRelayUpstreamSuccess(ctx, principal, selection)
	}
	return true, false, nil
}

func relayTransformSuccessfulResponse(req LLMRelayHTTPRequest, selection relaySelection, stream bool, status string, body []byte) ([]byte, error) {
	plan, err := relayRequestTransformPlan(req, selection, stream)
	if err != nil || status != "success" || !plan.enabled {
		return body, err
	}
	return relayTransformResponseBody(body, plan)
}

func relayEndpointURLForSelection(selection relaySelection, providerKind, endpoint string) (string, error) {
	return relayEndpointURLForUpstream(selection.upstream, selection.route, providerKind, endpoint)
}

func relayRealtimeEndpointURLForSelection(selection relaySelection, publicModel string) (string, error) {
	targetURL, err := relayEndpointURLForSelection(selection, "openai", "realtime")
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: relay realtime upstream URL is invalid", apperrors.ErrInvalidArgument)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("%w: relay realtime upstream URL scheme is not supported", apperrors.ErrInvalidArgument)
	}
	query := parsed.Query()
	query.Set("model", strings.TrimSpace(firstNonEmpty(selection.route.UpstreamModel, publicModel)))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func relayEndpointURLForUpstream(upstream domainaigateway.LLMUpstream, route domainaigateway.LLMModelRoute, providerKind, endpoint string) (string, error) {
	providerKind = normalizeRelayProviderKind(providerKind)
	if providerKind == "azure-openai" {
		return azureOpenAIEndpointURL(
			upstream.BaseURL,
			route.UpstreamModel,
			endpoint,
			upstream.Metadata,
			route.Metadata,
		)
	}
	if providerKind == "gemini" {
		return geminiEndpointURL(
			upstream.BaseURL,
			route.UpstreamModel,
			endpoint,
			upstream.Metadata,
			route.Metadata,
		)
	}
	if providerKind == "cohere" {
		return cohereEndpointURL(upstream.BaseURL, endpoint)
	}
	return relayEndpointURL(upstream.BaseURL, providerKind, endpoint)
}

func relayEndpointURL(baseURL, providerKind, endpoint string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	switch providerKind {
	case "anthropic":
		if strings.HasSuffix(baseURL, "/v1") {
			return baseURL + anthropicEndpointPath(endpoint), nil
		}
		return baseURL + "/v1" + anthropicEndpointPath(endpoint), nil
	default:
		if strings.HasSuffix(baseURL, "/v1") {
			return baseURL + openAIEndpointPath(endpoint), nil
		}
		return baseURL + "/v1" + openAIEndpointPath(endpoint), nil
	}
}

func azureOpenAIEndpointURL(baseURL, deployment, endpoint string, upstreamMetadata, routeMetadata map[string]any) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	values := azureOpenAIConfigValues(upstreamMetadata, routeMetadata)
	apiStyle := strings.ToLower(strings.TrimSpace(gatewayFirstString(values, "apiStyle", "api_style", "style", "mode")))
	apiVersion := strings.TrimSpace(gatewayFirstString(values, "apiVersion", "api_version", "azureApiVersion", "azure_api_version"))
	if apiStyle == "" {
		apiStyle = "v1"
		if apiVersion != "" && azureOpenAIEndpointSupportsDeployment(endpoint) {
			apiStyle = "deployment"
		}
	}
	switch apiStyle {
	case "deployment", "deployments", "versioned", "api-version", "api_version":
		if azureOpenAIEndpointSupportsDeployment(endpoint) {
			return azureOpenAIDeploymentEndpointURL(baseURL, deployment, endpoint, apiVersion, values)
		}
	}
	return azureOpenAIV1EndpointURL(baseURL, endpoint), nil
}

func azureOpenAIConfigValues(upstreamMetadata, routeMetadata map[string]any) map[string]any {
	out := copyMap(gatewayConditionValues(upstreamMetadata, "azureOpenAI", "azure_openai", "azure"))
	for key, value := range gatewayConditionValues(routeMetadata, "azureOpenAI", "azure_openai", "azure") {
		out[key] = value
	}
	return out
}

func geminiEndpointURL(baseURL, model, endpoint string, upstreamMetadata, routeMetadata map[string]any) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	values := geminiConfigValues(upstreamMetadata, routeMetadata)
	apiVersion := strings.Trim(strings.TrimSpace(gatewayFirstString(values, "apiVersion", "api_version", "version")), "/")
	if apiVersion == "" {
		apiVersion = "v1beta"
	}
	resourceBaseURL := geminiResourceBaseURL(baseURL, apiVersion)
	switch endpoint {
	case "models":
		return resourceBaseURL + "/models", nil
	case "interactions":
		return resourceBaseURL + "/interactions", nil
	case "generateContent", "streamGenerateContent":
		model = strings.TrimSpace(strings.TrimPrefix(model, "models/"))
		if model == "" {
			return "", fmt.Errorf("%w: Gemini relay upstream model is required", apperrors.ErrInvalidArgument)
		}
		targetURL := resourceBaseURL + "/models/" + url.PathEscape(model) + ":" + endpoint
		if endpoint == "streamGenerateContent" {
			targetURL += "?alt=sse"
		}
		return targetURL, nil
	default:
		return "", fmt.Errorf("%w: Gemini relay endpoint %s is not supported", apperrors.ErrInvalidArgument, endpoint)
	}
}

func geminiResourceBaseURL(baseURL, apiVersion string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	apiVersion = strings.Trim(apiVersion, "/")
	if strings.HasSuffix(lower, "/"+strings.ToLower(apiVersion)) {
		return baseURL
	}
	return baseURL + "/" + apiVersion
}

func geminiConfigValues(upstreamMetadata, routeMetadata map[string]any) map[string]any {
	out := copyMap(gatewayConditionValues(upstreamMetadata, "gemini", "googleAI", "google_ai"))
	for key, value := range gatewayConditionValues(routeMetadata, "gemini", "googleAI", "google_ai") {
		out[key] = value
	}
	return out
}

func cohereEndpointURL(baseURL, endpoint string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	switch endpoint {
	case "models":
		baseURL = trimURLSuffixFold(baseURL, "/v2")
		if strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
			return baseURL + "/models", nil
		}
		return baseURL + "/v1/models", nil
	case "rerank":
		baseURL = trimURLSuffixFold(baseURL, "/v1")
		if strings.HasSuffix(strings.ToLower(baseURL), "/v2") {
			return baseURL + "/rerank", nil
		}
		return baseURL + "/v2/rerank", nil
	default:
		return "", fmt.Errorf("%w: Cohere relay endpoint %s is not supported", apperrors.ErrInvalidArgument, endpoint)
	}
}

func trimURLSuffixFold(value, suffix string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if strings.HasSuffix(strings.ToLower(value), strings.ToLower(suffix)) {
		return value[:len(value)-len(suffix)]
	}
	return value
}

func azureOpenAIEndpointSupportsDeployment(endpoint string) bool {
	switch endpoint {
	case "chat/completions", "embeddings", "images/generations", "images/edits", "images/variations", "audio/speech", "audio/transcriptions", "audio/translations":
		return true
	default:
		return false
	}
}

func azureOpenAIV1EndpointURL(baseURL, endpoint string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	switch {
	case strings.HasSuffix(lower, "/openai/v1"):
		return baseURL + openAIEndpointPath(endpoint)
	case strings.HasSuffix(lower, "/openai"):
		return baseURL + "/v1" + openAIEndpointPath(endpoint)
	default:
		return baseURL + "/openai/v1" + openAIEndpointPath(endpoint)
	}
}

func azureOpenAIDeploymentEndpointURL(baseURL, deployment, endpoint, apiVersion string, values map[string]any) (string, error) {
	deployment = strings.TrimSpace(firstNonEmpty(
		gatewayFirstString(values, "deployment", "deploymentID", "deploymentId", "deploymentName"),
		deployment,
	))
	if deployment == "" {
		return "", fmt.Errorf("%w: Azure OpenAI deployment is required", apperrors.ErrInvalidArgument)
	}
	apiVersion = strings.TrimSpace(apiVersion)
	if apiVersion == "" {
		return "", fmt.Errorf("%w: Azure OpenAI apiVersion is required for deployment style", apperrors.ErrInvalidArgument)
	}
	resourceBaseURL := azureOpenAIResourceBaseURL(baseURL)
	targetURL := resourceBaseURL + "/openai/deployments/" + url.PathEscape(deployment) + openAIEndpointPath(endpoint)
	return appendRelayQuery(targetURL, "api-version", apiVersion), nil
}

func azureOpenAIResourceBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	switch {
	case strings.HasSuffix(lower, "/openai/v1"):
		return strings.TrimRight(baseURL[:len(baseURL)-len("/openai/v1")], "/")
	case strings.HasSuffix(lower, "/openai"):
		return strings.TrimRight(baseURL[:len(baseURL)-len("/openai")], "/")
	default:
		return baseURL
	}
}

func appendRelayQuery(targetURL, key, value string) string {
	separator := "?"
	if strings.Contains(targetURL, "?") {
		separator = "&"
	}
	return targetURL + separator + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}

func relayRetryableUpstreamStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status >= 500
}

func relayErrorCodeForStatus(status int) string {
	switch {
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

func openAIEndpointPath(endpoint string) string {
	switch endpoint {
	case "models":
		return "/models"
	case "realtime":
		return "/realtime"
	case "responses":
		return "/responses"
	case "embeddings":
		return "/embeddings"
	case "images/generations":
		return "/images/generations"
	case "images/edits":
		return "/images/edits"
	case "images/variations":
		return "/images/variations"
	case "audio/speech":
		return "/audio/speech"
	case "audio/transcriptions":
		return "/audio/transcriptions"
	case "audio/translations":
		return "/audio/translations"
	default:
		return "/chat/completions"
	}
}

func anthropicEndpointPath(endpoint string) string {
	switch endpoint {
	case "models":
		return "/models"
	default:
		return "/messages"
	}
}

func (s *Service) testRelayUpstream(ctx context.Context, upstream domainaigateway.LLMUpstream) (domainaigateway.LLMUpstreamTestResult, error) {
	checkedAt := time.Now().UTC()
	providerKind := normalizeRelayProviderKind(upstream.ProviderKind)
	if providerKind == "" {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: upstream provider kind is invalid", apperrors.ErrInvalidArgument)
	}
	targetURL, err := relayEndpointURLForUpstream(upstream, domainaigateway.LLMModelRoute{}, providerKind, "models")
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	if err := s.validateRelayUpstreamURL(targetURL); err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	apiKey, err := s.decryptRelayAPIKey(upstream.APIKeyCiphertext)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	if strings.TrimSpace(apiKey) == "" {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: relay upstream API key is not configured", apperrors.ErrInvalidArgument)
	}
	timeout := s.relayConfig.DefaultTimeout
	if upstream.TimeoutSeconds > 0 {
		timeout = time.Duration(upstream.TimeoutSeconds) * time.Second
	}
	upstreamCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("build relay upstream test request: %w", err)
	}
	applyRelayUpstreamHeaders(req.Header, LLMRelayHTTPRequest{ProviderKind: providerKind, Endpoint: "models", Headers: http.Header{}}, upstream, apiKey)
	started := time.Now().UTC()
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: relay upstream test failed", apperrors.ErrClusterUnready)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	status := "success"
	if resp.StatusCode >= 400 {
		status = "failure"
	}
	return domainaigateway.LLMUpstreamTestResult{
		UpstreamID:   upstream.ID,
		ProviderKind: providerKind,
		Status:       status,
		HTTPStatus:   resp.StatusCode,
		DurationMs:   time.Since(started).Milliseconds(),
		CheckedAt:    checkedAt,
	}, nil
}

func applyRelayUpstreamHeaders(headers http.Header, req LLMRelayHTTPRequest, upstream domainaigateway.LLMUpstream, apiKey string) {
	contentType := "application/json"
	if relayEndpointRequiresMultipart(req.Endpoint) {
		contentType = strings.TrimSpace(req.Headers.Get("Content-Type"))
	}
	headers.Set("Accept", firstNonEmpty(req.Headers.Get("Accept"), relayDefaultUpstreamAccept(req.Endpoint)))
	for key, value := range upstream.DefaultHeaders {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" || isSensitiveRelayHeader(name) {
			continue
		}
		headers.Set(name, fmt.Sprint(value))
	}
	headers.Set("Content-Type", firstNonEmpty(contentType, "application/json"))
	if req.ProviderKind == "anthropic" {
		headers.Set("x-api-key", apiKey)
		headers.Set("anthropic-version", firstNonEmpty(req.Headers.Get("anthropic-version"), headers.Get("anthropic-version"), "2023-06-01"))
		if beta := req.Headers.Get("anthropic-beta"); beta != "" {
			headers.Set("anthropic-beta", beta)
		}
		return
	}
	if normalizeRelayProviderKind(req.ProviderKind) == "azure-openai" {
		headers.Set("api-key", apiKey)
		return
	}
	if normalizeRelayProviderKind(req.ProviderKind) == "gemini" {
		headers.Set("x-goog-api-key", apiKey)
		return
	}
	headers.Set("Authorization", "Bearer "+apiKey)
	if organization := req.Headers.Get("OpenAI-Organization"); organization != "" && relayProviderUsesOpenAIWireProtocol(req.ProviderKind) {
		headers.Set("OpenAI-Organization", organization)
	}
}

func copyRelayResponseHeaders(dst, src http.Header, defaultContentType string) {
	for _, name := range []string{
		"Content-Type",
		"Cache-Control",
		"X-Request-Id",
		"Openai-Request-Id",
		"Request-Id",
		"Anthropic-Organization-Id",
	} {
		for _, value := range src.Values(name) {
			if strings.TrimSpace(value) != "" {
				dst.Add(name, value)
			}
		}
	}
	if dst.Get("Content-Type") == "" {
		dst.Set("Content-Type", defaultContentType)
	}
}

func relayDefaultUpstreamAccept(endpoint string) string {
	if strings.TrimSpace(endpoint) == "audio/speech" {
		return "*/*"
	}
	return "application/json"
}

func relayDefaultResponseContentType(endpoint string) string {
	if strings.TrimSpace(endpoint) == "audio/speech" {
		return "application/octet-stream"
	}
	return "application/json"
}

func (s *Service) validateRelayUpstreamURL(rawURL string) error {
	return s.relayTransportComponent().validateURL(rawURL, false)
}

func (t *relayHTTPTransport) validateURL(rawURL string, websocket bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		kind := "base URL"
		if websocket {
			kind = "websocket URL"
		}
		return fmt.Errorf("%w: upstream %s is invalid", apperrors.ErrInvalidArgument, kind)
	}
	insecureScheme := parsed.Scheme == "http" || parsed.Scheme == "ws"
	if insecureScheme && !t.config.AllowInsecureUpstreamHTTP {
		if websocket {
			return fmt.Errorf("%w: insecure upstream websocket is disabled", apperrors.ErrInvalidArgument)
		}
		return fmt.Errorf("%w: insecure upstream HTTP is disabled", apperrors.ErrInvalidArgument)
	}
	validScheme := parsed.Scheme == "https" || parsed.Scheme == "http"
	if websocket {
		validScheme = parsed.Scheme == "wss" || parsed.Scheme == "ws"
	}
	if !validScheme {
		if websocket {
			return fmt.Errorf("%w: upstream websocket URL scheme is not supported", apperrors.ErrInvalidArgument)
		}
		return fmt.Errorf("%w: upstream URL scheme is not supported", apperrors.ErrInvalidArgument)
	}
	if !t.config.AllowPrivateUpstreamHosts && relayHostBlocked(parsed.Hostname()) {
		return fmt.Errorf("%w: private upstream host is not allowed", apperrors.ErrInvalidArgument)
	}
	return nil
}

func relayHostBlocked(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsPrivate() || addr.IsUnspecified()
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
