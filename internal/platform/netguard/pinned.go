package netguard

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// ResolveAllowedAddress checks every DNS answer and returns the address to pin
// for the whole operation. An allowlist never permits metadata/link-local IPs.
func ResolveAllowedAddress(ctx context.Context, host string, allowed []netip.Prefix) (string, error) {
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return "", fmt.Errorf("source host could not be resolved")
	}
	for _, address := range addresses {
		if !AllowedSourceAddress(address, allowed) {
			return "", fmt.Errorf("source host resolves outside the connection's allowed network range")
		}
	}
	return addresses[0].Unmap().String(), nil
}

func AllowedSourceAddress(address netip.Addr, allowed []netip.Prefix) bool {
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() {
		return false
	}
	if !BlockedOutboundIP(net.IP(address.AsSlice())) {
		return true
	}
	for _, prefix := range allowed {
		if prefix.Contains(address) && (address.IsPrivate() || address.IsLoopback() && prefix.Bits() == address.BitLen()) {
			return true
		}
	}
	return false
}

// PinnedHTTPSClient is used for connection-owned source API and OAuth requests.
// It retains the original TLS hostname, rejects redirects and proxies,
// and cannot dial a second endpoint after resolving the approved address.
func PinnedHTTPSClient(ctx context.Context, host, port string, allowed []netip.Prefix, certificate string) (*http.Client, error) {
	address, err := ResolveAllowedAddress(ctx, host, allowed)
	if err != nil {
		return nil, err
	}
	return pinnedHTTPClient(host, port, address, certificate)
}

// PinnedResourceHTTPClient dials only the address obtained from an authorized
// resource, retaining the entry's HTTP/TLS hostname. Metadata and link-local
// addresses remain forbidden even when the resource reports them.
func PinnedResourceHTTPClient(host, port string, address netip.Addr) (*http.Client, error) {
	address = address.Unmap()
	if !address.IsValid() || !AllowedSourceAddress(address, []netip.Prefix{netip.PrefixFrom(address, address.BitLen())}) {
		return nil, fmt.Errorf("resource address is not an allowed probe destination")
	}
	return pinnedHTTPClient(host, port, address.Unmap().String(), "")
}

func pinnedHTTPClient(host, port, address, certificate string) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if certificate != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("system certificate roots are unavailable")
		}
		if !roots.AppendCertsFromPEM([]byte(certificate)) {
			return nil, fmt.Errorf("source connection CA certificate is invalid")
		}
		tlsConfig.RootCAs = roots
	}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig, TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout: 30 * time.Second,
		DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
			if target != net.JoinHostPort(host, port) {
				return nil, fmt.Errorf("source connection endpoint changed")
			}
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(address, port))
		},
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}, nil
}
