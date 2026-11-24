package githubplugin

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/opensoha/soha/internal/platform/netguard"
)

const (
	apiHost        = "api.github.com"
	rawHost        = "raw.githubusercontent.com"
	releaseHost    = "release-assets.githubusercontent.com"
	objectsHost    = "objects.githubusercontent.com"
	dialTimeout    = 5 * time.Second
	requestTimeout = 10 * time.Second
)

var (
	globalIPv6 = netip.MustParsePrefix("2000::/3")
	// Supplement the shared guard with other non-public or transition ranges.
	localBlockedPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("168.63.129.16/32"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("3fff::/20"),
	}
)

func newHTTPClient() *http.Client {
	networkDialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	dialer := publicDialer{lookup: net.DefaultResolver.LookupNetIP, dial: networkDialer.DialContext}
	protocols := &http.Protocols{}
	protocols.SetHTTP1(true) // Bound concurrent requests using the per-host connection cap.
	return &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			DialContext:            dialer.dialContext,
			TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout:    5 * time.Second,
			ResponseHeaderTimeout:  5 * time.Second,
			IdleConnTimeout:        30 * time.Second,
			MaxResponseHeaderBytes: 64 << 10,
			MaxConnsPerHost:        8,
			MaxIdleConnsPerHost:    2,
			DisableCompression:     true,
			Protocols:              protocols,
			// No proxy: the dialer must see the request's original destination.
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type publicDialer struct {
	lookup func(context.Context, string, string) ([]netip.Addr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func (dialer publicDialer) dialContext(ctx context.Context, network, target string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil || port != "443" || (host != apiHost && host != rawHost && !isReleaseAssetHost(host)) {
		return nil, ErrEgressBlocked
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	addresses, err := dialer.lookup(dialCtx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, requestFailure(dialCtx, err)
	}
	for _, address := range addresses {
		if !isPublicAddress(address) {
			return nil, ErrEgressBlocked
		}
	}
	for _, address := range addresses {
		if dialCtx.Err() != nil {
			return nil, requestFailure(dialCtx, nil)
		}
		// TLS still verifies the fixed request hostname; only the TCP dial uses the IP.
		connection, err := dialer.dial(dialCtx, network, net.JoinHostPort(address.Unmap().String(), port))
		if err == nil {
			return connection, nil
		}
	}
	return nil, requestFailure(dialCtx, nil)
}

func isReleaseAssetHost(host string) bool {
	return host == releaseHost || host == objectsHost
}

func isPublicAddress(address netip.Addr) bool {
	if address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || netguard.BlockedOutboundIP(net.IP(address.AsSlice())) {
		return false
	}
	if address.Is6() && !globalIPv6.Contains(address) {
		return false
	}
	for _, prefix := range localBlockedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
