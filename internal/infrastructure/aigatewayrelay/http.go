package aigatewayrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/application/aigateway/relayport"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	relayMaxNonStreamResponseBytes = 16 << 20
	relayMaxModelListResponseBytes = 1 << 20
	relayStreamAuditBufferBytes    = 1 << 20
)

func (t *Adapter) Prepare(input relayport.Request, target relayport.Target, opts relayport.RequestOptions) (relayport.PreparedRequest, error) {
	request, err := prepareProviderRequest(input, target.Model, opts)
	if err != nil {
		return relayport.PreparedRequest{}, err
	}
	targetURL, err := relayEndpointURLForTarget(target, request.ProviderKind, request.Endpoint)
	if err != nil {
		return relayport.PreparedRequest{}, err
	}
	return relayport.PreparedRequest{
		Request: request, URL: targetURL, ClientProviderKind: relayport.NormalizeProvider(input.ProviderKind), DefaultHeaders: target.DefaultHeaders,
	}, nil
}

func prepareProviderRequest(req relayport.Request, model string, opts relayport.RequestOptions) (relayport.Request, error) {
	req.ProviderKind = relayport.NormalizeProvider(req.ProviderKind)
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	var err error
	switch {
	case opts.ConversionProvider != "":
		req.Body, err = relayTransformRequestBody(req.Body, model, req.ProviderKind, opts.ConversionProvider)
		req.ProviderKind, req.Endpoint = opts.ConversionProvider, opts.ConversionEndpoint
	case req.ProviderKind == "gemini":
		req.Body, err = relayGeminiRequestBody(req, model)
	case relayport.RequiresMultipart(req.Endpoint):
		var contentType string
		req.Body, contentType, err = rewriteRelayMultipartRequestBody(req.Endpoint, req.Body, req.Headers.Get("Content-Type"), model)
		if err == nil {
			req.Headers = req.Headers.Clone()
			req.Headers.Set("Content-Type", contentType)
		}
	default:
		req.Body, err = rewriteRelayRequestBody(req.Body, model, req.ProviderKind, opts.Stream, opts.IncludeOpenAIUsage)
	}
	return req, err
}

func (t *Adapter) Execute(ctx context.Context, input relayport.PreparedRequest, opts relayport.HTTPOptions) (relayport.HTTPResult, error) {
	upstreamCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	req, err := t.buildRequest(upstreamCtx, http.MethodPost, input, opts.APIKey)
	if err != nil {
		return relayport.HTTPResult{}, err
	}
	result := relayport.HTTPResult{UpstreamStarted: time.Now().UTC()}
	var firstByteTimer *time.Timer
	if opts.FirstByteTimeout > 0 {
		firstByteTimer = time.AfterFunc(opts.FirstByteTimeout, cancel)
	}
	resp, err := t.client.Do(req)
	if firstByteTimer != nil {
		firstByteTimer.Stop()
	}
	if err != nil {
		result.Failure = &relayport.HTTPFailure{Code: "upstream_request_failed", Message: err.Error()}
		return result, nil
	}
	defer func() { _ = resp.Body.Close() }()
	result.Response = resp
	if relayport.IsRedirectStatus(resp.StatusCode) || opts.StartStream == nil && relayport.IsRetryableStatus(resp.StatusCode) {
		result.Body, _ = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		result.OutputBytes = int64(len(result.Body))
		return result, nil
	}
	var writer io.Writer
	if opts.StartStream != nil {
		writer = opts.StartStream(resp)
	}
	result = readResponse(resp, result, writer, opts.IdleTimeout, cancel)
	if result.Failure == nil && writer == nil && relayport.IsSuccessStatus(resp.StatusCode) {
		body, err := relayTransformResponseBody(result.Body, input.ClientProviderKind, input.ProviderKind)
		if err != nil {
			return result, err
		}
		result.Body = body
	}
	return result, nil
}

func (t *Adapter) buildRequest(ctx context.Context, method string, input relayport.PreparedRequest, apiKey string) (*http.Request, error) {
	if err := t.ValidateURL(input.URL); err != nil {
		return nil, err
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("%w: relay upstream API key is not configured", apperrors.ErrInvalidArgument)
	}
	req, err := http.NewRequestWithContext(ctx, method, input.URL, bytes.NewReader(input.Body))
	if err != nil {
		return nil, fmt.Errorf("build relay upstream request: %w", err)
	}
	applyRelayUpstreamHeaders(req.Header, input.Request, input.DefaultHeaders, apiKey)
	return req, nil
}

func (t *Adapter) Probe(ctx context.Context, target relayport.Target, apiKey string, timeout time.Duration) (domainaigateway.LLMUpstreamTestResult, error) {
	checkedAt := time.Now().UTC()
	providerKind := relayport.NormalizeProvider(target.ProviderKind)
	if providerKind == "" {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: upstream provider kind is invalid", apperrors.ErrInvalidArgument)
	}
	targetURL, err := relayEndpointURLForTarget(target, providerKind, "models")
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	upstreamCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := t.buildRequest(upstreamCtx, http.MethodGet, relayport.PreparedRequest{
		Request: relayport.Request{ProviderKind: providerKind, Endpoint: "models"}, URL: targetURL, DefaultHeaders: target.DefaultHeaders,
	}, apiKey)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	started := time.Now().UTC()
	resp, err := t.client.Do(req)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: relay upstream test failed", apperrors.ErrClusterUnready)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, relayMaxModelListResponseBytes))
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("read relay upstream model list: %w", err)
	}
	status := "failure"
	var models []string
	if relayport.IsSuccessStatus(resp.StatusCode) {
		status = "success"
		models = relayModelsFromResponse(body)
	}
	return domainaigateway.LLMUpstreamTestResult{
		UpstreamID: target.ID, ProviderKind: providerKind, Status: status, HTTPStatus: resp.StatusCode,
		DurationMs: time.Since(started).Milliseconds(), ModelCount: len(models), Models: models, CheckedAt: checkedAt,
	}, nil
}

func readResponse(resp *http.Response, result relayport.HTTPResult, writer io.Writer, idleTimeout time.Duration, cancel context.CancelFunc) relayport.HTTPResult {
	var idleTimer *time.Timer
	if writer != nil && idleTimeout > 0 {
		idleTimer = time.AfterFunc(idleTimeout, cancel)
		defer idleTimer.Stop()
	}
	result.Wrote = writer != nil
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if idleTimer != nil {
				idleTimer.Reset(idleTimeout)
			}
			if result.FirstByteAt.IsZero() {
				result.FirstByteAt = time.Now().UTC()
			}
			result.OutputBytes += int64(n)
			if failure := collectResponseChunk(&result, writer, buf[:n]); failure != nil {
				result.Failure = failure
				return result
			}
		}
		if err == io.EOF {
			return result
		}
		if err != nil {
			result.Failure = &relayport.HTTPFailure{Code: "upstream_read_failed", Message: err.Error()}
			return result
		}
	}
}

func collectResponseChunk(result *relayport.HTTPResult, writer io.Writer, chunk []byte) *relayport.HTTPFailure {
	if writer == nil {
		remaining := relayMaxNonStreamResponseBytes - len(result.Body)
		if len(chunk) > remaining {
			result.Body = append(result.Body, chunk[:remaining]...)
			result.BodyTruncated = true
			return &relayport.HTTPFailure{Code: "upstream_response_too_large", Message: "relay upstream response exceeds the configured limit"}
		}
		result.Body = append(result.Body, chunk...)
		return nil
	}
	body, truncated := appendRelayTail(result.Body, chunk, relayStreamAuditBufferBytes)
	result.Body, result.BodyTruncated = body, result.BodyTruncated || truncated
	if _, err := writer.Write(chunk); err != nil {
		return &relayport.HTTPFailure{Code: "client_cancelled", Message: err.Error()}
	}
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func appendRelayTail(dst, chunk []byte, limit int) ([]byte, bool) {
	if len(chunk) >= limit {
		return append(dst[:0], chunk[len(chunk)-limit:]...), len(dst)+len(chunk) > limit
	}
	overflow := len(dst) + len(chunk) - limit
	if overflow > 0 {
		copy(dst, dst[overflow:])
		dst = dst[:len(dst)-overflow]
	}
	return append(dst, chunk...), overflow > 0
}

func relayModelsFromResponse(body []byte) []string {
	var payload struct {
		Data   []struct{ ID, Name, Model string } `json:"data"`
		Models []struct{ ID, Name, Model string } `json:"models"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	models := make([]string, 0, len(payload.Data)+len(payload.Models))
	for _, item := range append(payload.Data, payload.Models...) {
		model := strings.TrimSpace(strings.TrimPrefix(firstNonEmpty(item.ID, item.Name, item.Model), "models/"))
		if model != "" {
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return slices.Compact(models)
}
