package githubplugin

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"testing"
)

func TestNewHTTPClientUsesBoundedDirectTLS(t *testing.T) {
	t.Parallel()
	client := newHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || transport.DialTLSContext != nil {
		t.Fatal("expected direct transport with standard TLS hostname verification")
	}
	if client.Timeout <= 0 || transport.ResponseHeaderTimeout <= 0 || transport.TLSHandshakeTimeout <= 0 ||
		transport.MaxResponseHeaderBytes <= 0 || transport.MaxConnsPerHost <= 0 || !transport.DisableCompression {
		t.Fatal("expected bounded requests, handshakes, headers and connections without transparent decompression")
	}
}

func TestPublicDialerPinsNumericIP(t *testing.T) {
	t.Parallel()
	var targets []string
	var lookups int
	dialer := publicDialer{
		lookup: func(ctx context.Context, network, host string) ([]netip.Addr, error) {
			if _, bounded := ctx.Deadline(); !bounded || network != "ip" || host != apiHost {
				t.Fatalf("unexpected DNS lookup: %s %s", network, host)
			}
			lookups++
			return []netip.Addr{netip.MustParseAddr("2606:50c0:8000::154"), netip.MustParseAddr("::ffff:140.82.112.5")}, nil
		},
		dial: func(_ context.Context, network, target string) (net.Conn, error) {
			if network != "tcp" {
				t.Fatalf("dial network = %q, want tcp", network)
			}
			targets = append(targets, target)
			if len(targets) == 1 {
				return nil, errors.New("first address not reachable")
			}
			connection, peer := net.Pipe()
			t.Cleanup(func() { _ = peer.Close() })
			return connection, nil
		},
	}
	connection, err := dialer.dialContext(t.Context(), "tcp", apiHost+":443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	wantTargets := []string{"[2606:50c0:8000::154]:443", "140.82.112.5:443"}
	if lookups != 1 || !reflect.DeepEqual(targets, wantTargets) {
		t.Fatalf("lookups = %d, dial targets = %v; want 1 and %v", lookups, targets, wantTargets)
	}
}

func TestPublicDialerRejectsNonpublicDNSBeforeDial(t *testing.T) {
	t.Parallel()
	for _, ip := range []string{
		"0.0.0.1", "10.0.0.1", "127.0.0.1", "169.254.169.254", "100.100.100.200", "168.63.129.16",
		"192.0.2.1", "::1", "fd00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::a00:1", "2002:a00:1::",
	} {
		t.Run(ip, func(t *testing.T) {
			t.Parallel()
			dialer := publicDialer{
				lookup: func(context.Context, string, string) ([]netip.Addr, error) {
					return []netip.Addr{netip.MustParseAddr("140.82.112.5"), netip.MustParseAddr(ip)}, nil
				},
				dial: func(context.Context, string, string) (net.Conn, error) {
					t.Fatal("mixed public and nonpublic DNS answers reached the dialer")
					return nil, nil
				},
			}
			if _, err := dialer.dialContext(t.Context(), "tcp", rawHost+":443"); !errors.Is(err, ErrEgressBlocked) {
				t.Fatalf("dialContext() error = %v, want ErrEgressBlocked", err)
			}
		})
	}
}

func TestPublicDialerRejectsOtherEndpointsBeforeDNS(t *testing.T) {
	t.Parallel()
	dialer := publicDialer{
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			t.Fatal("unexpected target reached DNS")
			return nil, nil
		},
	}
	for _, target := range []string{"example.invalid:443", "github.com:443", "api.github.com:80", "api.github.com"} {
		if _, err := dialer.dialContext(t.Context(), "tcp", target); !errors.Is(err, ErrEgressBlocked) {
			t.Errorf("dialContext(%q) error = %v, want ErrEgressBlocked", target, err)
		}
	}
}
