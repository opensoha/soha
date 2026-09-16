package systemintegration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/netguard"
)

var sourceGitUserPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

// Explicit connection network policy also governs repository discovery and
// registration. Legacy connections without this policy keep their transport.
func sourceConnectionHTTPClient(ctx context.Context, item domain.Integration) (*http.Client, error) {
	config := configurationMap(item.Configuration)
	if config["git_ca_certificate"] == "" && config["git_allowed_cidrs"] == "" && config["git_allowed_endpoints"] == "" {
		return nil, nil
	}
	base, err := parseSourceGitURL(config["base_url"])
	if err != nil || base.Scheme != "https" {
		return nil, fmt.Errorf("%w: Git connection network policy requires HTTPS", apperrors.ErrInvalidArgument)
	}
	_, allowed, err := sourceGitAllowlist(config)
	if err != nil {
		return nil, err
	}
	return netguard.PinnedHTTPSClient(ctx, base.Hostname(), sourceGitPort(base), allowed, config["git_ca_certificate"])
}

// ResolveDeliveryGitAccess is called after repository authorization. The stored
// repository binding is reverified against the provider before every read.
func (s *Service) ResolveDeliveryGitAccess(ctx context.Context, repository domainapp.SourceRepository) (domainapp.GitSourceAccess, error) {
	if repository.SourceConnectionID == "" || repository.ProviderRepositoryID == "" || repository.CredentialRef != "" && repository.CredentialRef != repository.SourceConnectionID {
		return domainapp.GitSourceAccess{}, apperrors.ErrAccessDenied
	}
	item, err := s.repo.Get(ctx, repository.SourceConnectionID)
	if err != nil {
		return domainapp.GitSourceAccess{}, err
	}
	if !item.Enabled || item.Category != domain.CategorySourceControl || item.ProviderType != repository.Provider {
		return domainapp.GitSourceAccess{}, apperrors.ErrAccessDenied
	}
	access, base, allowed, err := deliveryGitPolicy(item, repository.URL)
	if err != nil {
		return domainapp.GitSourceAccess{}, err
	}
	access.Address, err = netguard.ResolveAllowedAddress(ctx, access.Host, allowed)
	if err != nil {
		return domainapp.GitSourceAccess{}, err
	}
	client, err := netguard.PinnedHTTPSClient(ctx, base.Hostname(), sourceGitPort(base), allowed, access.CertificateAuthority)
	if err != nil {
		return domainapp.GitSourceAccess{}, err
	}
	defer client.CloseIdleConnections()
	credentials, err := s.decryptCredentials(ctx, item.ID)
	if err != nil {
		return domainapp.GitSourceAccess{}, err
	}
	// Provider identity and OAuth requests obey the same endpoint/DNS/TLS policy
	// as Git, even when the clone itself is public or uses SSH.
	if normalizedGitLabAuthMode(configurationMap(item.Configuration)) == gitLabAuthModeOAuth {
		_, credentials, err = s.refreshOAuthCredentials(ctx, item, credentials, client)
		if err != nil {
			return domainapp.GitSourceAccess{}, err
		}
	}
	factory := s.adapters[item.ProviderType]
	if factory == nil {
		return domainapp.GitSourceAccess{}, apperrors.ErrAccessDenied
	}
	adapter, err := factory.Build(item, credentials, client)
	if err != nil {
		return domainapp.GitSourceAccess{}, err
	}
	reader, ok := adapter.(domainapp.SourceMetadataReader)
	if !ok {
		return domainapp.GitSourceAccess{}, apperrors.ErrAccessDenied
	}
	if err := validateSourceIdentity(ctx, reader, repository.ProviderRepositoryID, repository.URL); err != nil {
		return domainapp.GitSourceAccess{}, err
	}
	if repository.CredentialRef != "" {
		access.Credentials = credentials
	}
	return access, nil
}

func deliveryGitPolicy(item domain.Integration, repositoryURL string) (domainapp.GitSourceAccess, *url.URL, []netip.Prefix, error) {
	config := configurationMap(item.Configuration)
	base, err := parseSourceGitURL(config["base_url"])
	if err != nil || base.Scheme != "https" {
		return domainapp.GitSourceAccess{}, nil, nil, fmt.Errorf("%w: template sources require an HTTPS source connection", apperrors.ErrInvalidArgument)
	}
	clone, err := parseSourceGitURL(repositoryURL)
	if err != nil || clone.Path == "" || clone.Path == "/" {
		return domainapp.GitSourceAccess{}, nil, nil, fmt.Errorf("%w: invalid Git clone URL", apperrors.ErrInvalidArgument)
	}
	endpoints, prefixes, err := sourceGitAllowlist(config)
	if err != nil {
		return domainapp.GitSourceAccess{}, nil, nil, err
	}
	endpoints = append(endpoints, sourceGitOrigin(base), "ssh://"+net.JoinHostPort(base.Hostname(), "22"))
	matched := false
	for _, endpoint := range endpoints {
		matched = matched || endpoint == sourceGitOrigin(clone)
	}
	if !matched {
		return domainapp.GitSourceAccess{}, nil, nil, fmt.Errorf("%w: clone endpoint is not allowed by its source connection", apperrors.ErrAccessDenied)
	}
	return domainapp.GitSourceAccess{RepositoryURL: repositoryURL, Scheme: clone.Scheme, Host: clone.Hostname(), Port: sourceGitPort(clone), CertificateAuthority: config["git_ca_certificate"]}, base, prefixes, nil
}

func sourceGitAllowlist(config map[string]string) ([]string, []netip.Prefix, error) {
	endpoints := strings.FieldsFunc(config["git_allowed_endpoints"], func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	ranges := strings.FieldsFunc(config["git_allowed_cidrs"], func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	if len(endpoints) > 32 || len(ranges) > 32 {
		return nil, nil, fmt.Errorf("%w: too many allowed Git endpoints or CIDRs", apperrors.ErrInvalidArgument)
	}
	for index, value := range endpoints {
		endpoint, err := parseSourceGitURL(value)
		if err != nil || endpoint.Path != "" && endpoint.Path != "/" || endpoint.User != nil {
			return nil, nil, fmt.Errorf("%w: allowed Git endpoints must be HTTPS or SSH origins", apperrors.ErrInvalidArgument)
		}
		endpoints[index] = sourceGitOrigin(endpoint)
	}
	prefixes := make([]netip.Prefix, 0, len(ranges))
	for _, value := range ranges {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Is4In6() {
			return nil, nil, fmt.Errorf("%w: invalid allowed Git CIDR", apperrors.ErrInvalidArgument)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return endpoints, prefixes, nil
}

func parseSourceGitURL(value string) (*url.URL, error) {
	if strings.ContainsAny(value, "\\ \t\r\n\x00") || strings.ContainsFunc(value, unicode.IsControl) {
		return nil, apperrors.ErrInvalidArgument
	}
	if strings.HasPrefix(value, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(value, "git@"), ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, apperrors.ErrInvalidArgument
		}
		value = "ssh://git@" + parts[0] + "/" + parts[1]
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Scheme != "https" && parsed.Scheme != "ssh" {
		return nil, apperrors.ErrInvalidArgument
	}
	if err := validateSourceGitAddress(parsed); err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(sourceGitPort(parsed))
	if err != nil || port < 1 || port > 65535 {
		return nil, apperrors.ErrInvalidArgument
	}
	parsed.Host = net.JoinHostPort(strings.ToLower(parsed.Hostname()), strconv.Itoa(port))
	return parsed, nil
}

func validateSourceGitAddress(parsed *url.URL) error {
	if strings.HasPrefix(parsed.Hostname(), "-") || strings.ContainsAny(parsed.Hostname(), "%@'\"`$") {
		return apperrors.ErrInvalidArgument
	}
	if parsed.User != nil {
		_, password := parsed.User.Password()
		if parsed.Scheme != "ssh" || password || !sourceGitUserPattern.MatchString(parsed.User.Username()) {
			return apperrors.ErrInvalidArgument
		}
	}
	if strings.ContainsFunc(parsed.Path, unicode.IsControl) || strings.ContainsRune(parsed.Path, '\\') {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func sourceGitPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if value.Scheme == "ssh" {
		return "22"
	}
	return "443"
}

func sourceGitOrigin(value *url.URL) string {
	return value.Scheme + "://" + net.JoinHostPort(value.Hostname(), sourceGitPort(value))
}
