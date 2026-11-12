package proxysource

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/infrastructure/companionartifact"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Options are installer owned. Private CIDRs never originate in source API input.
type Options struct {
	PrivateAllowlist []netip.Prefix
	Protected        func(context.Context, netip.Addr) bool
	ProtectedHost    func(context.Context, string) bool
}
type Fetcher struct {
	client        *http.Client
	protectedHost func(context.Context, string) bool
}

func NewFetcher(options Options) (*Fetcher, error) {
	if options.Protected == nil || options.ProtectedHost == nil {
		return nil, domain.Invalid("protection_required", "options")
	}
	transport := companionartifact.NewPublicTransport()
	transport.DisableCompression = true
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, domain.Invalid("source_denied", "url")
		}
		if options.ProtectedHost(ctx, strings.TrimSuffix(strings.ToLower(host), ".")) {
			return nil, domain.Invalid("source_denied", "url")
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, domain.Invalid("source_unavailable", "url")
		}
		for _, ip := range addresses {
			if !allowedAddress(ctx, ip, options) {
				return nil, domain.Invalid("source_denied", "url")
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].Unmap().String(), port))
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return domain.Invalid("source_denied", "url") }

	return &Fetcher{client: client, protectedHost: options.ProtectedHost}, nil
}
func allowedAddress(ctx context.Context, ip netip.Addr, options Options) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || options.Protected(ctx, ip) {
		return false
	}
	for _, raw := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	if ip.IsPrivate() {
		for _, prefix := range options.PrivateAllowlist {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	return true
}
func validateURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") {
		return domain.Invalid("source_denied", "url")
	}
	return nil
}
func (f *Fetcher) Fetch(ctx context.Context, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || validateURL(u) != nil {
		return "", domain.Invalid("source_denied", "url")
	}
	if f.protectedHost(ctx, strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")) {
		return "", domain.Invalid("source_denied", "url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", domain.Invalid("source_denied", "url")
	}
	response, err := f.client.Do(req)
	if err != nil {
		return "", domain.Invalid("source_unavailable", "url")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.ContentLength > domain.MaxSourceBytes || response.Header.Get("Content-Encoding") != "" {
		return "", domain.Invalid("invalid_source_response", "url")
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, domain.MaxSourceBytes+1))
	if err != nil {
		return "", domain.Invalid("source_unavailable", "url")
	}
	if len(b) > domain.MaxSourceBytes {
		return "", domain.Invalid("source_limit", "content")
	}
	return string(b), nil
}
