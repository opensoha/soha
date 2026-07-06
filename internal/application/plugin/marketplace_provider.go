package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	defaultMarketplaceSourceID  = "static"
	maxMarketplaceCatalogBytes  = 5 << 20
	maxMarketplaceManifestBytes = 2 << 20
)

var nonPublicMarketplacePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

type MarketplaceProvider interface {
	List(context.Context, domainplugin.MarketplaceFilter) ([]domainplugin.MarketplacePlugin, error)
	Get(context.Context, domainplugin.PluginVersionRef) (domainplugin.MarketplacePlugin, error)
	FetchManifest(context.Context, domainplugin.PluginVersionRef) (ResolvedManifest, error)
}

type ResolvedManifest struct {
	Plugin          domainplugin.MarketplacePlugin
	Manifest        domainplugin.PluginManifest
	Integrity       *domainplugin.PluginIntegrity
	ChecksumStatus  string
	SignatureStatus string
	Source          string
	SourceID        string
	MarketplaceURL  string
}

type StaticMarketplaceProvider struct {
	sourceID string
	items    []domainplugin.MarketplacePlugin
}

func NewStaticMarketplaceProvider(items []domainplugin.MarketplacePlugin) *StaticMarketplaceProvider {
	return &StaticMarketplaceProvider{
		sourceID: defaultMarketplaceSourceID,
		items:    normalizeMarketplaceItems(defaultMarketplaceSourceID, "", items),
	}
}

func NewDefaultMarketplaceProvider() MarketplaceProvider {
	return NewStaticMarketplaceProvider(defaultMarketplace())
}

func (p *StaticMarketplaceProvider) List(_ context.Context, filter domainplugin.MarketplaceFilter) ([]domainplugin.MarketplacePlugin, error) {
	items := make([]domainplugin.MarketplacePlugin, 0, len(p.items))
	for _, item := range p.items {
		if matchesMarketplaceFilter(item, filter) {
			items = append(items, item)
		}
	}
	return items, nil
}

func (p *StaticMarketplaceProvider) Get(ctx context.Context, ref domainplugin.PluginVersionRef) (domainplugin.MarketplacePlugin, error) {
	if strings.TrimSpace(ref.SourceID) != "" && strings.TrimSpace(ref.SourceID) != p.sourceID {
		return domainplugin.MarketplacePlugin{}, fmt.Errorf("%w: marketplace plugin not found", apperrors.ErrNotFound)
	}
	items, err := p.List(ctx, domainplugin.MarketplaceFilter{
		SourceID: p.sourceID,
		Version:  ref.Version,
	})
	if err != nil {
		return domainplugin.MarketplacePlugin{}, err
	}
	pluginID := strings.TrimSpace(ref.PluginID)
	for _, item := range items {
		if item.ID == pluginID && marketplaceVersionMatches(item, ref.Version) {
			return item, nil
		}
	}
	return domainplugin.MarketplacePlugin{}, fmt.Errorf("%w: marketplace plugin not found", apperrors.ErrNotFound)
}

func (p *StaticMarketplaceProvider) FetchManifest(ctx context.Context, ref domainplugin.PluginVersionRef) (ResolvedManifest, error) {
	item, err := p.Get(ctx, ref)
	if err != nil {
		return ResolvedManifest{}, err
	}
	integrity := item.Manifest.Integrity
	if integrity == nil {
		integrity = &domainplugin.PluginIntegrity{}
	}
	return ResolvedManifest{
		Plugin:          item,
		Manifest:        item.Manifest,
		Integrity:       integrity,
		ChecksumStatus:  "not_provided",
		SignatureStatus: integrityStatus(item.Manifest),
		Source:          item.Source,
		SourceID:        firstNonEmpty(item.SourceID, p.sourceID),
		MarketplaceURL:  item.SourceURL,
	}, nil
}

type MarketplaceSource struct {
	ID  string
	URL string
}

type CompositeMarketplaceProvider struct {
	providers []MarketplaceProvider
}

func NewCompositeMarketplaceProvider(providers ...MarketplaceProvider) *CompositeMarketplaceProvider {
	out := make([]MarketplaceProvider, 0, len(providers))
	for _, provider := range providers {
		if provider != nil {
			out = append(out, provider)
		}
	}
	return &CompositeMarketplaceProvider{providers: out}
}

func (p *CompositeMarketplaceProvider) List(ctx context.Context, filter domainplugin.MarketplaceFilter) ([]domainplugin.MarketplacePlugin, error) {
	items := []domainplugin.MarketplacePlugin{}
	for _, provider := range p.providers {
		providerItems, err := provider.List(ctx, filter)
		if err != nil {
			return nil, err
		}
		items = append(items, providerItems...)
	}
	return items, nil
}

func (p *CompositeMarketplaceProvider) Get(ctx context.Context, ref domainplugin.PluginVersionRef) (domainplugin.MarketplacePlugin, error) {
	var lastErr error
	for _, provider := range p.providers {
		item, err := provider.Get(ctx, ref)
		if err == nil {
			return item, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return domainplugin.MarketplacePlugin{}, lastErr
	}
	return domainplugin.MarketplacePlugin{}, fmt.Errorf("%w: marketplace plugin not found", apperrors.ErrNotFound)
}

func (p *CompositeMarketplaceProvider) FetchManifest(ctx context.Context, ref domainplugin.PluginVersionRef) (ResolvedManifest, error) {
	var lastErr error
	for _, provider := range p.providers {
		resolved, err := provider.FetchManifest(ctx, ref)
		if err == nil {
			return resolved, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return ResolvedManifest{}, lastErr
	}
	return ResolvedManifest{}, fmt.Errorf("%w: marketplace plugin not found", apperrors.ErrNotFound)
}

type RemoteMarketplaceProvider struct {
	sourceID   string
	catalogURL string
	client     *http.Client
}

func NewRemoteMarketplaceProvider(source MarketplaceSource, client *http.Client) (*RemoteMarketplaceProvider, error) {
	source.ID = firstNonEmpty(source.ID, "remote")
	source.URL = strings.TrimSpace(source.URL)
	if source.URL == "" {
		return nil, fmt.Errorf("%w: marketplace url is required", apperrors.ErrInvalidArgument)
	}
	if err := validateMarketplaceURL(source.URL); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &RemoteMarketplaceProvider{
		sourceID:   source.ID,
		catalogURL: source.URL,
		client:     client,
	}, nil
}

func NewAdHocRemoteMarketplaceProvider(source MarketplaceSource) (*RemoteMarketplaceProvider, error) {
	source.ID = firstNonEmpty(source.ID, "ad-hoc")
	source.URL = strings.TrimSpace(source.URL)
	if err := validateMarketplacePublicURL(source.URL); err != nil {
		return nil, err
	}
	return &RemoteMarketplaceProvider{
		sourceID:   source.ID,
		catalogURL: source.URL,
		client:     newAdHocMarketplaceHTTPClient(),
	}, nil
}

func (p *RemoteMarketplaceProvider) List(ctx context.Context, filter domainplugin.MarketplaceFilter) ([]domainplugin.MarketplacePlugin, error) {
	catalog, err := p.fetchCatalog(ctx)
	if err != nil {
		return nil, err
	}
	sourceURL := firstNonEmpty(catalog.SourceURL, p.catalogURL)
	sourceID := firstNonEmpty(catalog.SourceID, p.sourceID)
	items := normalizeMarketplaceItems(sourceID, sourceURL, catalog.Plugins)
	out := make([]domainplugin.MarketplacePlugin, 0, len(items))
	for _, item := range items {
		if matchesMarketplaceFilter(item, filter) {
			out = append(out, item)
		}
	}
	return out, nil
}

func (p *RemoteMarketplaceProvider) Get(ctx context.Context, ref domainplugin.PluginVersionRef) (domainplugin.MarketplacePlugin, error) {
	items, err := p.List(ctx, domainplugin.MarketplaceFilter{
		SourceID: ref.SourceID,
		Version:  ref.Version,
	})
	if err != nil {
		return domainplugin.MarketplacePlugin{}, err
	}
	pluginID := strings.TrimSpace(ref.PluginID)
	for _, item := range items {
		if item.ID == pluginID && marketplaceVersionMatches(item, ref.Version) {
			return item, nil
		}
	}
	return domainplugin.MarketplacePlugin{}, fmt.Errorf("%w: marketplace plugin not found", apperrors.ErrNotFound)
}

func (p *RemoteMarketplaceProvider) FetchManifest(ctx context.Context, ref domainplugin.PluginVersionRef) (ResolvedManifest, error) {
	item, err := p.Get(ctx, ref)
	if err != nil {
		return ResolvedManifest{}, err
	}
	version, ok := selectMarketplaceVersion(item, ref.Version)
	if !ok {
		return ResolvedManifest{}, fmt.Errorf("%w: marketplace plugin version not found", apperrors.ErrNotFound)
	}
	manifest := item.Manifest
	checksumStatus := "not_provided"
	integrity := &domainplugin.PluginIntegrity{
		Checksum:  version.Checksum,
		Signature: version.Signature,
		Status:    "catalog",
		Verified:  version.Signature != "",
	}
	if strings.TrimSpace(version.ManifestURL) != "" {
		manifestURL, err := resolveMarketplaceURL(p.catalogURL, version.ManifestURL)
		if err != nil {
			return ResolvedManifest{}, err
		}
		raw, err := p.getBytes(ctx, manifestURL, maxMarketplaceManifestBytes)
		if err != nil {
			return ResolvedManifest{}, err
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return ResolvedManifest{}, fmt.Errorf("%w: decode plugin manifest", apperrors.ErrInvalidArgument)
		}
		if strings.TrimSpace(version.Checksum) != "" {
			sum := checksumBytes(raw)
			if sum != strings.TrimSpace(version.Checksum) {
				return ResolvedManifest{}, fmt.Errorf("%w: manifest checksum mismatch", apperrors.ErrInvalidArgument)
			}
			checksumStatus = "verified"
		}
	}
	if integrity.Checksum == "" && manifest.Integrity != nil {
		integrity.Checksum = manifest.Integrity.Checksum
	}
	if integrity.Signature == "" && manifest.Integrity != nil {
		integrity.Signature = manifest.Integrity.Signature
		integrity.Verified = manifest.Integrity.Verified
	}
	signatureStatus := "not_provided"
	if integrity.Signature != "" {
		signatureStatus = "provided"
	}
	if integrity.Verified {
		signatureStatus = "verified"
	}
	return ResolvedManifest{
		Plugin:          item,
		Manifest:        manifest,
		Integrity:       integrity,
		ChecksumStatus:  checksumStatus,
		SignatureStatus: signatureStatus,
		Source:          firstNonEmpty(item.Source, "marketplace:"+item.ID),
		SourceID:        firstNonEmpty(item.SourceID, p.sourceID),
		MarketplaceURL:  firstNonEmpty(item.SourceURL, p.catalogURL),
	}, nil
}

func (p *RemoteMarketplaceProvider) fetchCatalog(ctx context.Context) (domainplugin.MarketplaceCatalog, error) {
	raw, err := p.getBytes(ctx, p.catalogURL, maxMarketplaceCatalogBytes)
	if err != nil {
		return domainplugin.MarketplaceCatalog{}, err
	}
	var catalog domainplugin.MarketplaceCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return domainplugin.MarketplaceCatalog{}, fmt.Errorf("%w: decode marketplace catalog", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(catalog.SchemaVersion) == "" || strings.TrimSpace(catalog.SourceID) == "" {
		return domainplugin.MarketplaceCatalog{}, fmt.Errorf("%w: marketplace catalog missing schemaVersion or sourceId", apperrors.ErrInvalidArgument)
	}
	return catalog, nil
}

func (p *RemoteMarketplaceProvider) getBytes(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	if err := validateMarketplaceURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build marketplace request", apperrors.ErrInvalidArgument)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch marketplace url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: marketplace returned status %d", apperrors.ErrInvalidArgument, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read marketplace response: %w", err)
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%w: marketplace response exceeds size limit", apperrors.ErrInvalidArgument)
	}
	return raw, nil
}

func normalizeMarketplaceItems(sourceID, sourceURL string, items []domainplugin.MarketplacePlugin) []domainplugin.MarketplacePlugin {
	out := make([]domainplugin.MarketplacePlugin, 0, len(items))
	for _, item := range items {
		if item.ID == "" && item.Manifest.ID != "" {
			item.ID = item.Manifest.ID
		}
		if item.Name == "" {
			item.Name = item.Manifest.Name
		}
		if item.Version == "" {
			item.Version = item.Manifest.Version
		}
		if item.LatestVersion == "" {
			item.LatestVersion = item.Version
		}
		if item.Publisher == "" {
			item.Publisher = item.Manifest.Publisher
		}
		if item.Type == "" {
			item.Type = item.Manifest.Type
		}
		if item.Summary == "" {
			item.Summary = item.Manifest.Description
		}
		if item.Source == "" {
			item.Source = "marketplace:" + item.ID
		}
		if item.SourceID == "" {
			item.SourceID = sourceID
		}
		if item.SourceURL == "" {
			item.SourceURL = sourceURL
		}
		if item.Compatibility == nil {
			item.Compatibility = item.Manifest.Compatibility
		}
		out = append(out, item)
	}
	return out
}

func selectMarketplaceVersion(item domainplugin.MarketplacePlugin, version string) (domainplugin.MarketplacePluginVersion, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = firstNonEmpty(item.LatestVersion, item.Version, item.Manifest.Version)
	}
	for _, candidate := range item.Versions {
		if candidate.Version == version {
			return candidate, true
		}
	}
	if version == "" || version == item.Version || version == item.Manifest.Version {
		return domainplugin.MarketplacePluginVersion{Version: version}, true
	}
	return domainplugin.MarketplacePluginVersion{}, false
}

func marketplaceVersionMatches(item domainplugin.MarketplacePlugin, version string) bool {
	_, ok := selectMarketplaceVersion(item, version)
	return ok
}

func resolveMarketplaceURL(baseURL, candidate string) (string, error) {
	parsedCandidate, err := url.Parse(strings.TrimSpace(candidate))
	if err != nil {
		return "", fmt.Errorf("%w: invalid marketplace url", apperrors.ErrInvalidArgument)
	}
	if parsedCandidate.IsAbs() {
		return parsedCandidate.String(), validateMarketplaceURL(parsedCandidate.String())
	}
	parsedBase, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("%w: invalid marketplace base url", apperrors.ErrInvalidArgument)
	}
	return parsedBase.ResolveReference(parsedCandidate).String(), nil
}

func validateMarketplaceURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%w: invalid marketplace url", apperrors.ErrInvalidArgument)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: marketplace url must not contain credentials", apperrors.ErrInvalidArgument)
	}
	switch parsed.Scheme {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("%w: marketplace url must use http or https", apperrors.ErrInvalidArgument)
	}
}

func validateMarketplacePublicURL(rawURL string) error {
	if err := validateMarketplaceURL(rawURL); err != nil {
		return err
	}
	parsed, _ := url.Parse(strings.TrimSpace(rawURL))
	host := strings.Trim(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return fmt.Errorf("%w: marketplace url host is required", apperrors.ErrInvalidArgument)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "metadata.google.internal" || strings.HasSuffix(host, ".metadata.google.internal") {
		return fmt.Errorf("%w: ad-hoc marketplace url must use a public host", apperrors.ErrInvalidArgument)
	}
	if ip := net.ParseIP(host); ip != nil {
		return validateMarketplacePublicIP(ip)
	}
	return nil
}

func newAdHocMarketplaceHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{
		Timeout:        10 * time.Second,
		ControlContext: validateMarketplaceDialTarget,
	}
	transport.DialContext = dialer.DialContext
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if req == nil || req.URL == nil {
				return fmt.Errorf("%w: invalid marketplace redirect", apperrors.ErrInvalidArgument)
			}
			return validateMarketplacePublicURL(req.URL.String())
		},
	}
}

func validateMarketplaceDialTarget(_ context.Context, _, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: invalid marketplace dial target", apperrors.ErrInvalidArgument)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: unresolved marketplace dial target", apperrors.ErrInvalidArgument)
	}
	return validateMarketplacePublicIP(ip)
}

func validateMarketplacePublicIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("%w: marketplace url resolved to an invalid address", apperrors.ErrInvalidArgument)
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return fmt.Errorf("%w: marketplace url resolved to an invalid address", apperrors.ErrInvalidArgument)
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() {
		return fmt.Errorf("%w: ad-hoc marketplace url must resolve to a public address", apperrors.ErrInvalidArgument)
	}
	for _, prefix := range nonPublicMarketplacePrefixes {
		if prefix.Contains(addr) {
			return fmt.Errorf("%w: ad-hoc marketplace url must resolve to a public address", apperrors.ErrInvalidArgument)
		}
	}
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return fmt.Errorf("%w: ad-hoc marketplace url must resolve to a public address", apperrors.ErrInvalidArgument)
	}
	return nil
}

func checksumBytes(raw []byte) string {
	hash := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(hash[:])
}
