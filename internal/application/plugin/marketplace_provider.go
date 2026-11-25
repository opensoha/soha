package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	maxMarketplaceCatalogBytes  = 5 << 20
	maxMarketplaceManifestBytes = 2 << 20
	marketplaceSourceTimeout    = 10 * time.Second
)

var errMarketplaceReleaseSuspended = apperrors.NewBusiness(apperrors.ErrConflict, "plugin_release_suspended",
	"This plugin release is suspended.", "此插件版本已暂停分发。")

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

type marketplaceSourceLister interface {
	listWithSources(context.Context, domainplugin.MarketplaceFilter) (sohaapi.MarketplacePluginListEnvelope, error)
}

type ResolvedManifest struct {
	Plugin          domainplugin.MarketplacePlugin
	Manifest        domainplugin.PluginManifest
	Integrity       *domainplugin.PluginIntegrity
	RawChecksum     string
	rawManifest     []byte
	ChecksumStatus  string
	SignatureStatus string
	Source          string
	SourceID        string
	MarketplaceURL  string
	SourceInfo      *domainplugin.PluginSourceInfo
	Package         *domainplugin.PluginPackageDescriptor
}

type MarketplaceSource struct {
	ID       string
	URL      string
	Official bool
	Legacy   bool
}

type CompositeMarketplaceProvider struct {
	providers []MarketplaceProvider
}

func configuredMarketplaceProviderForURL(provider MarketplaceProvider, rawURL string) (MarketplaceProvider, bool) {
	// Only URLs already present in the server-side provider graph bypass ad-hoc SSRF checks.
	rawURL = strings.TrimSpace(rawURL)
	switch candidate := provider.(type) {
	case *RemoteMarketplaceProvider:
		return candidate, rawURL != "" && rawURL == candidate.catalogURL
	case *CompositeMarketplaceProvider:
		for _, child := range candidate.providers {
			if matched, ok := configuredMarketplaceProviderForURL(child, rawURL); ok {
				return matched, true
			}
		}
	}
	return nil, false
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
	result, err := p.listWithSources(ctx, filter)
	return result.Items, err
}

func (p *CompositeMarketplaceProvider) listWithSources(ctx context.Context, filter domainplugin.MarketplaceFilter) (sohaapi.MarketplacePluginListEnvelope, error) {
	ctx, cancel := context.WithTimeout(ctx, marketplaceSourceTimeout)
	defer cancel()
	type catalogResult struct {
		list sohaapi.MarketplacePluginListEnvelope
		err  error
	}
	providers := make([]MarketplaceProvider, 0, len(p.providers))
	for _, provider := range p.providers {
		if visibleMarketplaceProvider(provider) && marketplaceProviderMatchesSource(provider, filter.SourceID) {
			providers = append(providers, provider)
		}
	}
	catalogs := make([]catalogResult, len(providers))
	var group errgroup.Group
	for index, provider := range providers {
		group.Go(func() error {
			catalogs[index].list, catalogs[index].err = listMarketplaceProvider(ctx, provider, filter)
			return nil
		})
	}
	_ = group.Wait()

	result := sohaapi.MarketplacePluginListEnvelope{Items: []domainplugin.MarketplacePlugin{}}
	var firstErr error
	var available bool
	for _, catalog := range catalogs {
		if catalog.list.Meta != nil {
			if result.Meta == nil {
				result.Meta = &sohaapi.MarketplacePluginListMeta{}
			}
			result.Meta.Sources = append(result.Meta.Sources, catalog.list.Meta.Sources...)
		}
		if catalog.err == nil {
			available = true
			result.Items = append(result.Items, catalog.list.Items...)
		} else if firstErr == nil {
			firstErr = catalog.err
		}
	}
	if !available && firstErr != nil {
		result.Items = nil
		return result, firstErr
	}
	return result, nil
}

func listMarketplaceProvider(ctx context.Context, provider MarketplaceProvider, filter domainplugin.MarketplaceFilter) (sohaapi.MarketplacePluginListEnvelope, error) {
	if lister, ok := provider.(marketplaceSourceLister); ok {
		return lister.listWithSources(ctx, filter)
	}
	items, err := provider.List(ctx, filter)
	return sohaapi.MarketplacePluginListEnvelope{Items: items}, err
}

func marketplaceProviderMatchesSource(provider MarketplaceProvider, sourceID string) bool {
	if sourceID == "" {
		return true
	}
	switch candidate := provider.(type) {
	case *RemoteMarketplaceProvider:
		return sourceID == candidate.sourceID
	case *CompositeMarketplaceProvider:
		return slices.ContainsFunc(candidate.providers, func(child MarketplaceProvider) bool {
			return marketplaceProviderMatchesSource(child, sourceID)
		})
	default:
		return true
	}
}

func visibleMarketplaceProvider(provider MarketplaceProvider) bool {
	remote, ok := provider.(*RemoteMarketplaceProvider)
	return !ok || !remote.legacy
}

func (p *CompositeMarketplaceProvider) Get(ctx context.Context, ref domainplugin.PluginVersionRef) (domainplugin.MarketplacePlugin, error) {
	var lastErr error
	for _, provider := range p.providers {
		if !visibleMarketplaceProvider(provider) || !marketplaceProviderMatchesSource(provider, ref.SourceID) {
			continue
		}
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
		if !visibleMarketplaceProvider(provider) || !marketplaceProviderMatchesSource(provider, ref.SourceID) {
			continue
		}
		resolved, err := provider.FetchManifest(ctx, ref)
		if err == nil || errors.Is(err, errMarketplaceReleaseSuspended) || errors.Is(err, errMarketplaceRepositoryPreviewRequired) {
			return resolved, err
		}
		lastErr = err
	}
	if lastErr != nil {
		return ResolvedManifest{}, lastErr
	}
	return ResolvedManifest{}, fmt.Errorf("%w: marketplace plugin not found", apperrors.ErrNotFound)
}

type RemoteMarketplaceProvider struct {
	official   bool
	managed    bool
	legacy     bool
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
		client = &http.Client{Timeout: marketplaceSourceTimeout}
	}
	return &RemoteMarketplaceProvider{
		sourceID:   source.ID,
		official:   source.Official,
		legacy:     source.Legacy,
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
		official:   source.Official,
		catalogURL: source.URL,
		client:     newAdHocMarketplaceHTTPClient(),
	}, nil
}

func (p *RemoteMarketplaceProvider) List(ctx context.Context, filter domainplugin.MarketplaceFilter) ([]domainplugin.MarketplacePlugin, error) {
	if !marketplaceProviderMatchesSource(p, filter.SourceID) {
		return []domainplugin.MarketplacePlugin{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, marketplaceSourceTimeout)
	defer cancel()
	items, err := p.fetchMarketplaceItems(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domainplugin.MarketplacePlugin, 0, len(items))
	for _, item := range items {
		item.SourceInfo.Official = p.official
		if matchesMarketplaceFilter(item, filter) {
			out = append(out, item)
		}
	}
	return out, nil
}

func (p *RemoteMarketplaceProvider) listWithSources(ctx context.Context, filter domainplugin.MarketplaceFilter) (sohaapi.MarketplacePluginListEnvelope, error) {
	items, err := p.List(ctx, filter)
	result := sohaapi.MarketplacePluginListEnvelope{Items: items}
	if !marketplaceProviderMatchesSource(p, filter.SourceID) {
		return result, err
	}
	status := sohaapi.MarketplaceSourceStatus{SourceID: p.sourceID, Status: sohaapi.MarketplaceSourceStatusAvailable}
	if err != nil {
		status.Status = sohaapi.MarketplaceSourceStatusUnavailable
		status.Code = "source_unavailable"
	}
	result.Meta = &sohaapi.MarketplacePluginListMeta{Sources: []sohaapi.MarketplaceSourceStatus{status}}
	return result, err
}

func (p *RemoteMarketplaceProvider) Get(ctx context.Context, ref domainplugin.PluginVersionRef) (domainplugin.MarketplacePlugin, error) {
	return p.get(ctx, ref, false)
}

func (p *RemoteMarketplaceProvider) get(ctx context.Context, ref domainplugin.PluginVersionRef, includeUnversionedRepository bool) (domainplugin.MarketplacePlugin, error) {
	items, err := p.List(ctx, domainplugin.MarketplaceFilter{SourceID: ref.SourceID})
	if err != nil {
		return domainplugin.MarketplacePlugin{}, err
	}
	pluginID := strings.TrimSpace(ref.PluginID)
	for _, item := range items {
		if item.ID == pluginID && ((includeUnversionedRepository && item.RepositoryURL != "") || marketplaceVersionMatches(item, ref.Version)) {
			return item, nil
		}
	}
	return domainplugin.MarketplacePlugin{}, fmt.Errorf("%w: marketplace plugin not found", apperrors.ErrNotFound)
}

func (p *RemoteMarketplaceProvider) FetchManifest(ctx context.Context, ref domainplugin.PluginVersionRef) (ResolvedManifest, error) {
	item, err := p.get(ctx, ref, true)
	if err != nil {
		return ResolvedManifest{}, err
	}
	if item.RepositoryURL != "" {
		return ResolvedManifest{}, errMarketplaceRepositoryPreviewRequired
	}
	version, ok := selectMarketplaceVersion(item, ref.Version)
	if !ok {
		return ResolvedManifest{}, fmt.Errorf("%w: marketplace plugin version not found", apperrors.ErrNotFound)
	}
	if item.Suspended || version.Suspended {
		return ResolvedManifest{}, errMarketplaceReleaseSuspended
	}
	manifest := item.Manifest
	rawChecksum := ""
	checksumStatus := "not_provided"
	integrity := &domainplugin.PluginIntegrity{
		Checksum:  version.Checksum,
		Signature: version.Signature,
		Status:    "catalog",
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
		manifest = domainplugin.PluginManifest{}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return ResolvedManifest{}, fmt.Errorf("%w: decode plugin manifest", apperrors.ErrInvalidArgument)
		}
		rawChecksum = checksumBytes(raw)
		if strings.TrimSpace(version.Checksum) != "" {
			if rawChecksum != strings.TrimSpace(version.Checksum) {
				return ResolvedManifest{}, fmt.Errorf("%w: manifest checksum mismatch", apperrors.ErrInvalidArgument)
			}
			checksumStatus = "verified"
		}
	}
	if manifest.ID != item.ID || manifest.Version != version.Version {
		return ResolvedManifest{}, fmt.Errorf("%w: manifest identity does not match catalog release", apperrors.ErrInvalidArgument)
	}
	if integrity.Checksum == "" && manifest.Integrity != nil {
		integrity.Checksum = manifest.Integrity.Checksum
	}
	if integrity.Signature == "" && manifest.Integrity != nil {
		integrity.Signature = manifest.Integrity.Signature
	}
	manifest = withoutManifestTrustClaims(manifest)
	signatureStatus := "not_provided"
	if integrity.Signature != "" {
		signatureStatus = "declared"
	}
	return ResolvedManifest{
		Plugin:          item,
		Manifest:        manifest,
		Integrity:       integrity,
		RawChecksum:     rawChecksum,
		ChecksumStatus:  checksumStatus,
		SignatureStatus: signatureStatus,
		Source:          firstNonEmpty(item.Source, "marketplace:"+item.ID),
		SourceID:        firstNonEmpty(item.SourceID, p.sourceID),
		MarketplaceURL:  firstNonEmpty(item.SourceURL, p.catalogURL),
		SourceInfo:      marketplaceReleaseSource(item.SourceInfo, version, rawChecksum, checksumStatus == "verified"),
		Package:         version.Package,
	}, nil
}

func (p *RemoteMarketplaceProvider) fetchMarketplaceItems(ctx context.Context) ([]domainplugin.MarketplacePlugin, error) {
	raw, err := p.getBytes(ctx, p.catalogURL, maxMarketplaceCatalogBytes)
	if err != nil {
		return nil, err
	}
	var header struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("%w: decode marketplace catalog", apperrors.ErrInvalidArgument)
	}
	if header.SchemaVersion == marketplaceRepositoriesVersion {
		return p.decodeMarketplaceRepositories(raw)
	}
	if p.managed {
		if err := validateManagedMarketplaceCatalog(raw); err != nil {
			return nil, err
		}
	}
	var catalog domainplugin.MarketplaceCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, fmt.Errorf("%w: decode marketplace catalog", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(catalog.SchemaVersion) == "" || strings.TrimSpace(catalog.SourceID) == "" {
		return nil, fmt.Errorf("%w: marketplace catalog missing schemaVersion or sourceId", apperrors.ErrInvalidArgument)
	}
	return normalizeMarketplaceItems(p.sourceID, p.catalogURL, catalog.Plugins), nil
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
	defer func() { _ = resp.Body.Close() }()
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
		item.SourceID = sourceID
		item.SourceURL = sourceURL
		item.RepositoryURL = ""
		item.SourceInfo = &domainplugin.PluginSourceInfo{Kind: domainplugin.SourceKindMarketplace, CanonicalSource: sourceURL, SourceID: sourceID}
		item.Verified = false
		item.Manifest = withoutManifestTrustClaims(item.Manifest)
		if item.PublisherInfo != nil {
			publisher := *item.PublisherInfo
			publisher.Verified = false
			if publisher.VerificationLevel == "official" || publisher.VerificationLevel == "verified" {
				publisher.VerificationLevel = "community"
			}
			item.PublisherInfo = &publisher
		}
		if item.Compatibility == nil {
			item.Compatibility = item.Manifest.Compatibility
		}
		out = append(out, item)
	}
	return out
}

func marketplaceReleaseSource(source *domainplugin.PluginSourceInfo, version domainplugin.MarketplacePluginVersion, checksum string, verified bool) *domainplugin.PluginSourceInfo {
	info := *source
	commit := version.CommitSha
	if commit == "" && info.Official && verified {
		// Released catalog schemas can carry the official release commit in metadata.
		commit, _ = version.Metadata["commitSha"].(string)
	}
	if repositoryCommitSHA.MatchString(commit) {
		info.CommitSha = commit
	}
	info.RawManifestChecksum = checksum
	return &info
}

func withoutManifestTrustClaims(manifest domainplugin.PluginManifest) domainplugin.PluginManifest {
	if manifest.Integrity != nil {
		integrity := *manifest.Integrity
		integrity.Verified = false
		integrity.Status = "not_provided"
		if strings.TrimSpace(integrity.Signature) != "" {
			integrity.Status = "declared"
		}
		manifest.Integrity = &integrity
	}
	return manifest
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
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport := base.Clone()
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
