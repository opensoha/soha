package observabilityprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appobservability "github.com/opensoha/soha/internal/application/observability"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

const maxResponseBytes = 8 << 20

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"), netip.MustParsePrefix("::1/128"), netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"), netip.MustParsePrefix("ff00::/8"),
}

type LogClient struct {
	external *http.Client
	managed  *http.Client
}

func NewLogClient() *LogClient {
	return &LogClient{external: publicHTTPClient(), managed: noRedirectClient()}
}

func (c *LogClient) ValidateConfig(runtime appobservability.ProviderRuntime, _ map[string]any) error {
	if runtime.ProtocolVersion != "v1" {
		return fmt.Errorf("unsupported provider protocol %q", runtime.ProtocolVersion)
	}
	if strings.TrimSpace(runtime.Action) == "" || strings.TrimSpace(runtime.Runtime.ActionPath) == "" {
		return fmt.Errorf("provider action mapping is required")
	}
	endpoint, err := url.Parse(strings.TrimSpace(runtime.Runtime.Endpoint))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return fmt.Errorf("valid provider http endpoint is required")
	}
	actionPath, err := url.Parse(strings.TrimSpace(runtime.Runtime.ActionPath))
	if err != nil || actionPath.Host != "" || actionPath.Scheme != "" || !strings.HasPrefix(actionPath.Path, "/") {
		return fmt.Errorf("provider actionPath must be an absolute path on the configured endpoint")
	}
	if runtime.Runtime.Mode == sohaapi.ExternalHTTP {
		if endpoint.Scheme != "https" {
			return fmt.Errorf("external provider endpoint must use https")
		}
		return validatePublicHost(endpoint.Hostname())
	}
	if runtime.Runtime.Mode != sohaapi.ManagedContainer {
		return fmt.Errorf("unsupported provider runtime mode %q", runtime.Runtime.Mode)
	}
	return nil
}

func (c *LogClient) Search(ctx context.Context, runtime appobservability.ProviderRuntime, sourceID string, config map[string]any, query telemetry.LogSearchQuery) (telemetry.LogSearchResult, error) {
	if err := c.ValidateConfig(runtime, config); err != nil {
		return telemetry.LogSearchResult{}, err
	}
	endpoint, err := actionURL(runtime)
	if err != nil {
		return telemetry.LogSearchResult{}, err
	}
	credentials := map[string]string{}
	cleanConfig := make(map[string]any, len(config))
	for key, value := range config {
		if key == "credentials" {
			if items, ok := value.(map[string]string); ok {
				credentials = items
			}
			continue
		}
		cleanConfig[key] = value
	}
	payload, err := json.Marshal(struct {
		ProtocolVersion string                   `json:"protocolVersion"`
		ProviderKey     string                   `json:"providerKey"`
		SourceID        string                   `json:"sourceId"`
		Config          map[string]any           `json:"config"`
		Credentials     map[string]string        `json:"credentials,omitempty"`
		Query           telemetry.LogSearchQuery `json:"query"`
	}{runtime.ProtocolVersion, runtime.ProviderKey, sourceID, cleanConfig, credentials, query})
	if err != nil {
		return telemetry.LogSearchResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return telemetry.LogSearchResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	client := c.external
	if runtime.Runtime.Mode == sohaapi.ManagedContainer {
		client = c.managed
	}
	response, err := client.Do(request)
	if err != nil {
		return telemetry.LogSearchResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return telemetry.LogSearchResult{}, fmt.Errorf("provider returned http status %d", response.StatusCode)
	}
	var result telemetry.LogSearchResult
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes+1))
	if err := decoder.Decode(&result); err != nil {
		return telemetry.LogSearchResult{}, fmt.Errorf("decode provider response: %w", err)
	}
	if result.SourceID == "" {
		result.SourceID = sourceID
	}
	return result, nil
}

func actionURL(runtime appobservability.ProviderRuntime) (string, error) {
	base, err := url.Parse(strings.TrimSpace(runtime.Runtime.Endpoint))
	if err != nil {
		return "", err
	}
	path := strings.ReplaceAll(runtime.Runtime.ActionPath, "{action}", url.PathEscape(runtime.Action))
	reference, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(reference).String(), nil
}

func noRedirectClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func publicHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		return validatePublicHost(host)
	}}
	transport.DialContext = dialer.DialContext
	return &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func validatePublicHost(host string) error {
	host = strings.Trim(strings.ToLower(host), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" {
		return fmt.Errorf("external provider endpoint must use a public host")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok || !addr.Unmap().IsGlobalUnicast() {
		return fmt.Errorf("external provider endpoint must use a public address")
	}
	addr = addr.Unmap()
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return fmt.Errorf("external provider endpoint must use a public address")
		}
	}
	return nil
}
