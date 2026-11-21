package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	plugincontracts "github.com/opensoha/soha/contracts/plugins"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/netguard"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	maxManagedMarketplaceSources = 8
	maxManagedMarketplaceURL     = 2048
)

var (
	marketplaceDNSName              = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
	globalMarketplaceIPv6           = netip.MustParsePrefix("2000::/3")
	managedMarketplaceBlockedRanges = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("168.63.129.16/32"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("3fff::/20"),
	}
)

// ManagedMarketplaceSources derives source identities without contacting the catalogs.
// Valid entries are returned even when another entry conflicts with deployment configuration.
func ManagedMarketplaceSources(urls []string, declared ...MarketplaceSource) ([]MarketplaceSource, error) {
	if len(urls) > maxManagedMarketplaceSources {
		return nil, fmt.Errorf("%w: at most %d third-party marketplace URLs are allowed", apperrors.ErrInvalidArgument, maxManagedMarketplaceSources)
	}
	ids, endpoints := map[string]bool{}, map[string]bool{}
	for _, existing := range declared {
		ids[strings.TrimSpace(existing.ID)] = true
		if canonical, err := managedMarketplaceSource(existing.URL); err == nil {
			endpoints[canonical.URL] = true
		}
	}
	sources := make([]MarketplaceSource, 0, len(urls))
	var invalid error
	for index, rawURL := range urls {
		source, err := managedMarketplaceSource(rawURL)
		if err == nil && (ids[source.ID] || endpoints[source.URL]) {
			err = fmt.Errorf("%w: source URL or identity duplicates a configured marketplace source", apperrors.ErrInvalidArgument)
		}
		if err != nil {
			invalid = errors.Join(invalid, fmt.Errorf("third-party marketplace URL %d: %w", index+1, err))
			continue
		}
		ids[source.ID], endpoints[source.URL] = true, true
		sources = append(sources, source)
	}
	return sources, invalid
}

func managedMarketplaceSource(rawURL string) (MarketplaceSource, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || len(rawURL) > maxManagedMarketplaceURL {
		return MarketplaceSource{}, fmt.Errorf("%w: invalid marketplace URL", apperrors.ErrInvalidArgument)
	}
	host, port, err := managedMarketplaceEndpoint(parsed)
	if err != nil {
		return MarketplaceSource{}, err
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(rawURL, "#") {
		return MarketplaceSource{}, fmt.Errorf("%w: marketplace catalog URL must not include a query or fragment", apperrors.ErrInvalidArgument)
	}
	parsed.Host = host
	if port != "443" {
		parsed.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.RawPath = canonicalMarketplacePath(parsed.EscapedPath())
	canonical := parsed.String()
	return MarketplaceSource{ID: fmt.Sprintf("third-party-%x", sha256.Sum256([]byte(canonical))), URL: canonical}, nil
}

func canonicalMarketplacePath(escaped string) string {
	const hex = "0123456789ABCDEF"
	var path strings.Builder
	for index := 0; index < len(escaped); index++ {
		if escaped[index] != '%' {
			path.WriteByte(escaped[index])
			continue
		}
		// EscapedPath guarantees complete hexadecimal escapes. Preserve reserved bytes.
		decoded, _ := strconv.ParseUint(escaped[index+1:index+3], 16, 8)
		character := byte(decoded)
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("-._~", rune(character)) {
			path.WriteByte(character)
		} else {
			path.WriteByte('%')
			path.WriteByte(hex[character>>4])
			path.WriteByte(hex[character&15])
		}
		index += 2
	}
	return path.String()
}

func managedMarketplaceEndpoint(parsed *url.URL) (string, string, error) {
	if parsed == nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", "", fmt.Errorf("%w: marketplace URL must use HTTPS without credentials or a fragment", apperrors.ErrInvalidArgument)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if address, err := netip.ParseAddr(host); err == nil {
		if !publicManagedMarketplaceAddress(address) {
			return "", "", fmt.Errorf("%w: marketplace URL must use a public address", apperrors.ErrInvalidArgument)
		}
		host = address.Unmap().String()
	} else if len(host) > 253 || !marketplaceDNSName.MatchString(host) || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".home.arpa") {
		return "", "", fmt.Errorf("%w: marketplace URL must use a public host", apperrors.ErrInvalidArgument)
	}
	port := parsed.Port()
	if port == "" {
		return host, "443", nil
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return "", "", fmt.Errorf("%w: invalid marketplace URL port", apperrors.ErrInvalidArgument)
	}
	return host, strconv.FormatUint(number, 10), nil
}

func publicManagedMarketplaceAddress(address netip.Addr) bool {
	if address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || netguard.BlockedOutboundIP(net.IP(address.AsSlice())) {
		return false
	}
	if address.Is6() && !globalMarketplaceIPv6.Contains(address) {
		return false
	}
	for _, prefix := range managedMarketplaceBlockedRanges {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func NewManagedMarketplaceProvider(source MarketplaceSource) (*RemoteMarketplaceProvider, error) {
	canonical, err := managedMarketplaceSource(source.URL)
	if err != nil {
		return nil, err
	}
	provider, err := NewRemoteMarketplaceProvider(canonical, newManagedMarketplaceHTTPClient())
	if err != nil {
		return nil, err
	}
	provider.managed = true
	return provider, nil
}

var managedCatalogSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileMarketplaceCatalogSchema("https://contracts.opensoha.dev/plugins/marketplace-catalog.schema.json", plugincontracts.MarketplaceCatalogSchema())
})

func compileMarketplaceCatalogSchema(location string, raw []byte) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

func validateManagedMarketplaceCatalog(raw []byte) error {
	schema, err := managedCatalogSchema()
	if err != nil {
		return err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil || schema.Validate(document) != nil {
		return fmt.Errorf("%w: invalid marketplace catalog schema", apperrors.ErrInvalidArgument)
	}
	return nil
}

type managedMarketplaceTransport struct{ transport http.RoundTripper }

func (t managedMarketplaceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if _, _, err := managedMarketplaceEndpoint(request.URL); err != nil {
		return nil, err
	}
	return t.transport.RoundTrip(request)
}

func newManagedMarketplaceHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second, ControlContext: validateManagedMarketplaceDialTarget}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: managedMarketplaceTransport{transport: &http.Transport{
			DialContext: dialer.DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 5 * time.Second, IdleConnTimeout: 30 * time.Second,
			MaxResponseHeaderBytes: 64 << 10, MaxIdleConns: 8,
		}},
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= 5 {
				return fmt.Errorf("marketplace redirect limit exceeded")
			}
			request.Header.Del("Referer")
			_, _, err := managedMarketplaceEndpoint(request.URL)
			return err
		},
	}
}

func validateManagedMarketplaceDialTarget(_ context.Context, _, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: invalid marketplace dial target", apperrors.ErrInvalidArgument)
	}
	if ip, err := netip.ParseAddr(host); err != nil || !publicManagedMarketplaceAddress(ip) {
		return fmt.Errorf("%w: marketplace dial target must be a public address", apperrors.ErrInvalidArgument)
	}
	return nil
}
