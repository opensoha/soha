package aigatewayrelay

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/application/aigateway/relayport"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/netguard"
)

type Config struct {
	AllowInsecureUpstreamHTTP bool
	AllowPrivateUpstreamHosts bool
}

type Adapter struct {
	config  Config
	client  *http.Client
	resolve func(context.Context, string, string) ([]netip.Addr, error)
	dial    func(context.Context, string, string) (net.Conn, error)
}

var _ relayport.Transport = (*Adapter)(nil)

func New(config Config) *Adapter {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	adapter := &Adapter{
		config: config, resolve: net.DefaultResolver.LookupNetIP, dial: dialer.DialContext,
	}
	adapter.client = &http.Client{
		// Proxy is deliberately unset. HTTP and WS both use dialPinned.
		Transport: &http.Transport{
			DialContext: adapter.dialPinned, ForceAttemptHTTP2: true,
			MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return adapter
}

func (t *Adapter) ValidateURL(raw string) error {
	return t.validateURL(raw, "https", "http")
}

func (t *Adapter) validateURL(raw, secure, insecure string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%w: upstream URL is invalid", apperrors.ErrInvalidArgument)
	}
	switch parsed.Scheme {
	case secure:
	case insecure:
		if !t.config.AllowInsecureUpstreamHTTP {
			return fmt.Errorf("%w: insecure upstream transport is disabled", apperrors.ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("%w: upstream URL scheme is not supported", apperrors.ErrInvalidArgument)
	}
	if !t.config.AllowPrivateUpstreamHosts && blockedHost(parsed.Hostname()) {
		return fmt.Errorf("%w: private upstream host is not allowed", apperrors.ErrInvalidArgument)
	}
	return nil
}

func blockedHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && blockedAddress(addr)
}

func blockedAddress(addr netip.Addr) bool {
	return !addr.Unmap().IsGlobalUnicast() || netguard.BlockedOutboundIP(net.IP(addr.AsSlice()))
}

func (t *Adapter) dialPinned(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	addresses, err := t.resolveAddresses(ctx, host)
	if err != nil {
		return nil, err
	}
	// Validate all answers before attempting any connection, then use only IP literals.
	for _, addr := range addresses {
		if !addr.IsValid() || !t.config.AllowPrivateUpstreamHosts && blockedAddress(addr) {
			return nil, fmt.Errorf("%w: upstream host resolved to a disallowed address", apperrors.ErrInvalidArgument)
		}
	}
	for _, addr := range addresses {
		conn, dialErr := t.dial(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
		if ctx.Err() != nil {
			break
		}
	}
	return nil, err
}

func (t *Adapter) resolveAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	addresses, err := t.resolve(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve relay upstream: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%w: upstream host did not resolve", apperrors.ErrInvalidArgument)
	}
	return addresses, nil
}
