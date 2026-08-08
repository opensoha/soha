package companionartifact

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Fetcher struct {
	client *http.Client
}

func NewFetcher() *Fetcher {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("split companion package address: %w", err)
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolve companion package host: %w", err)
			}
			for _, address := range addresses {
				if !publicAddress(address) {
					return nil, fmt.Errorf("%w: companion package host resolves to a non-public address", apperrors.ErrInvalidArgument)
				}
			}
			if len(addresses) == 0 {
				return nil, fmt.Errorf("%w: companion package host did not resolve", apperrors.ErrInvalidArgument)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Minute}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many companion package redirects")
		}
		return validatePackageURL(req.URL.String())
	}
	return &Fetcher{client: client}
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string, maxBytes int64) (io.ReadCloser, error) {
	if err := validatePackageURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build companion package request", apperrors.ErrInvalidArgument)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch companion package: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: companion package returned status %d", apperrors.ErrInvalidArgument, resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: companion package exceeds size limit", apperrors.ErrInvalidArgument)
	}
	return &limitedReadCloser{Reader: io.LimitReader(resp.Body, maxBytes+1), closer: resp.Body, max: maxBytes}, nil
}

type limitedReadCloser struct {
	io.Reader
	closer io.Closer
	max    int64
}

func (r *limitedReadCloser) Close() error {
	return r.closer.Close()
}

func validatePackageURL(raw string) error {
	if strings.TrimSpace(raw) != raw || len(raw) > 2048 {
		return fmt.Errorf("%w: invalid companion package url", apperrors.ErrInvalidArgument)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%w: companion package url must be absolute HTTPS without credentials or fragment", apperrors.ErrInvalidArgument)
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		address, ok := netip.AddrFromSlice(ip)
		if !ok || !publicAddress(address.Unmap()) {
			return fmt.Errorf("%w: companion package url must use a public host", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}
