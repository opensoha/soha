package personalbrowser

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha/internal/infrastructure/companionartifact"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Fetcher struct{ client *http.Client }

func NewFetcher() *Fetcher {
	transport := companionartifact.NewPublicTransport()
	transport.MaxResponseHeaderBytes = 32 << 10
	return &Fetcher{client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (f *Fetcher) Fetch(ctx context.Context, input guest.BrowserNetworkRequest) (guest.BrowserNetworkResponse, error) {
	var result guest.BrowserNetworkResponse
	if input.Validate() != nil {
		return result, apperrors.ErrInvalidArgument
	}
	body, _ := base64.StdEncoding.DecodeString(input.BodyBase64)
	req, err := http.NewRequestWithContext(ctx, input.Method, input.URL, bytes.NewReader(body))
	if err != nil {
		return result, apperrors.ErrInvalidArgument
	}
	for name, value := range input.Headers {
		switch http.CanonicalHeaderKey(name) {
		case "Accept", "Accept-Language", "User-Agent", "Cookie", "Authorization", "Origin", "Referer", "Content-Type", "Cache-Control", "If-None-Match", "If-Modified-Since", "Range":
			req.Header.Set(name, value)
		}
	}
	response, err := f.client.Do(req)
	if err != nil {
		return result, apperrors.ErrServiceUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return result, apperrors.ErrServiceUnavailable
	}
	result.Status, result.BodyBase64 = response.StatusCode, base64.StdEncoding.EncodeToString(raw)
	for name, values := range response.Header {
		switch strings.ToLower(name) {
		case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "content-length", "alt-svc":
			continue
		}
		for _, value := range values {
			result.Headers = append(result.Headers, guest.BrowserNetworkHeader{Name: name, Value: value})
		}
	}
	if result.Validate() != nil {
		return guest.BrowserNetworkResponse{}, apperrors.ErrServiceUnavailable
	}
	return result, nil
}
