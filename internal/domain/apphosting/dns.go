package apphosting

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	maxDNSLabelLength = 63
	maxSlugLength     = 48
)

var (
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	slugPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,46}[a-z0-9])?$`)
	hex4Pattern     = regexp.MustCompile(`^[0-9a-f]{4}$`)
)

var reservedDNSLabels = map[string]struct{}{
	"auth":     {},
	"login":    {},
	"logout":   {},
	"admin":    {},
	"api":      {},
	"portal":   {},
	"static":   {},
	"assets":   {},
	"sso":      {},
	"console":  {},
	"www":      {},
	"mail":     {},
	"status":   {},
	"health":   {},
	"_soha":    {},
	"soha":     {},
	"internal": {},
	"gateway":  {},
}

func IsReservedDNSLabel(label string) bool {
	normalized := strings.ToLower(strings.TrimSpace(label))
	_, reserved := reservedDNSLabels[normalized]
	return reserved
}

func ValidateDNSLabel(label string) error {
	normalized := strings.ToLower(strings.TrimSpace(label))
	if normalized == "" || len(normalized) > maxDNSLabelLength {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_dns_label", "dnsLabel must be between 1 and 63 characters", "子域标识长度必须在 1 到 63 个字符之间")
	}
	if IsReservedDNSLabel(normalized) {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "reserved_dns_label", fmt.Sprintf("subdomain %q is reserved by the platform", normalized), fmt.Sprintf("子域标识 %q 为平台保留名称", normalized))
	}
	if !dnsLabelPattern.MatchString(normalized) {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_dns_label", "dnsLabel must contain only lowercase ASCII letters, digits, and interior hyphens", "子域标识仅允许小写字母、数字与中间连字符")
	}
	return nil
}

func ValidateExplicitSlug(slug string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(slug))
	if normalized == "" {
		return "", nil
	}
	if IsReservedDNSLabel(normalized) {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "reserved_dns_label", fmt.Sprintf("slug %q is reserved by the platform", normalized), fmt.Sprintf("标识前缀 %q 为平台保留名称", normalized))
	}
	if len(normalized) > maxSlugLength || !slugPattern.MatchString(normalized) {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_slug", "slug must be 1..48 lowercase ASCII letters, digits, or interior hyphens", "标识前缀仅允许 1 到 48 位小写字母、数字或中间连字符")
	}
	return normalized, nil
}

func SlugifyName(name string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' || r == '/':
			if builder.Len() > 0 && !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
		if builder.Len() >= maxSlugLength {
			break
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" || IsReservedDNSLabel(slug) {
		return "app"
	}
	return slug
}

func BuildDNSLabel(slug, short4Hex string) (string, error) {
	cleanSlug := strings.ToLower(strings.Trim(strings.TrimSpace(slug), "-"))
	if cleanSlug == "" || IsReservedDNSLabel(cleanSlug) {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_slug", "slug cannot be empty or reserved", "标识前缀不能为空或使用保留词")
	}
	if len(cleanSlug) > maxSlugLength {
		cleanSlug = strings.TrimRight(cleanSlug[:maxSlugLength], "-")
	}
	if !slugPattern.MatchString(cleanSlug) {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_slug", "slug contains invalid characters", "标识前缀包含非法字符")
	}
	suffix := strings.ToLower(strings.TrimSpace(short4Hex))
	if suffix == "" {
		var err error
		suffix, err = randomShort4Hex()
		if err != nil {
			return "", err
		}
	}
	if !hex4Pattern.MatchString(suffix) {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_dns_suffix", "short suffix must be 4 lowercase hex characters", "子域短码必须为 4 位十六进制字符")
	}
	label := cleanSlug + "-" + suffix
	if err := ValidateDNSLabel(label); err != nil {
		return "", err
	}
	return label, nil
}

func AllocateDNSLabel(explicitSlug, name string) (string, error) {
	slug, err := ValidateExplicitSlug(explicitSlug)
	if err != nil {
		return "", err
	}
	if slug == "" {
		slug = SlugifyName(name)
	}
	return BuildDNSLabel(slug, "")
}

func NormalizeAppsDomain(domain string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
}

func ValidateAppsDomain(domain string) error {
	normalized := NormalizeAppsDomain(domain)
	if normalized == "" || len(normalized) > 253 || !strings.Contains(normalized, ".") {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_apps_domain", "appsDomain must be a valid multi-label domain name", "托管域名后缀必须为合法的多级域名")
	}
	if net.ParseIP(normalized) != nil {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_apps_domain", "appsDomain must not be an IP address", "托管域名后缀不能为 IP 地址")
	}
	for _, part := range strings.Split(normalized, ".") {
		if !dnsLabelPattern.MatchString(part) {
			return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_apps_domain", "appsDomain contains an invalid DNS label", "托管域名后缀包含非法标签")
		}
	}
	return nil
}

// ParseHostedAppHost validates <dnsLabel>.<appsDomain>[:port] case-insensitively,
// rejecting IP literals, nested labels, and reserved platform subdomains while
// preserving an optional :port for browser redirects and local E2E testing.
func ParseHostedAppHost(rawHost, appsDomain string) (string, string, error) {
	trimmedHost := strings.TrimSpace(rawHost)
	domain := NormalizeAppsDomain(appsDomain)
	if trimmedHost == "" || len(trimmedHost) > 253 || strings.ContainsAny(trimmedHost, "/\\?#@ \t\r\n") {
		return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_host", "host is malformed", "请求 Host 格式不合法")
	}
	if domain == "" {
		return "", "", apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "apps_domain_unconfigured", "App Hosting domain is not configured", "尚未配置托管应用泛域名后缀")
	}

	hostname := trimmedHost
	port := ""
	if strings.Contains(trimmedHost, ":") {
		splitHost, splitPort, err := net.SplitHostPort(trimmedHost)
		if err != nil || splitHost == "" || splitPort == "" {
			return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_host", "host:port is malformed", "请求 Host 端口格式不合法")
		}
		portNum, err := strconv.Atoi(splitPort)
		if err != nil || portNum < 1 || portNum > 65535 {
			return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_host_port", "host port must be between 1 and 65535", "请求端口必须在 1 到 65535 之间")
		}
		hostname = splitHost
		port = strconv.Itoa(portNum)
	}

	lowerHostname := strings.ToLower(hostname)
	if net.ParseIP(lowerHostname) != nil {
		return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_host", "direct IP host access is forbidden", "禁止通过 IP 直连访问托管应用")
	}
	suffix := "." + domain
	if !strings.HasSuffix(lowerHostname, suffix) {
		return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "unknown_app_host", "host does not belong to configured appsDomain", "请求域名不属于当前托管应用域")
	}
	dnsLabel := strings.TrimSuffix(lowerHostname, suffix)
	if strings.Contains(dnsLabel, ".") {
		return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_dns_label", "nested subdomains under appsDomain are not allowed", "不支持多级嵌套应用子域")
	}
	if err := ValidateDNSLabel(dnsLabel); err != nil {
		return "", "", err
	}
	normalizedHost := lowerHostname
	if port != "" {
		normalizedHost = net.JoinHostPort(lowerHostname, port)
	}
	return dnsLabel, normalizedHost, nil
}

// ParseHostedAppHostForScheme validates the host against appsDomain and enforces
// canonical scheme ports so callers cannot redirect SSO tickets to arbitrary ports.
func ParseHostedAppHostForScheme(rawHost, appsDomain, publicScheme string) (string, string, error) {
	dnsLabel, normalizedHost, err := ParseHostedAppHost(rawHost, appsDomain)
	if err != nil {
		return "", "", err
	}
	domain := NormalizeAppsDomain(appsDomain)
	canonicalHostname := dnsLabel + "." + domain
	if !strings.Contains(normalizedHost, ":") {
		return dnsLabel, canonicalHostname, nil
	}
	_, port, splitErr := net.SplitHostPort(normalizedHost)
	if splitErr != nil {
		return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_host", "host:port is malformed", "请求 Host 端口格式不合法")
	}
	scheme := strings.ToLower(strings.TrimSpace(publicScheme))
	if scheme == "" {
		scheme = DefaultPublicScheme
	}
	if scheme == "https" {
		if port == "443" {
			return dnsLabel, canonicalHostname, nil
		}
		return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_host_port", "non-standard port is not permitted for HTTPS hosted applications", "HTTPS 托管应用不允许指定非标准端口")
	}
	if port == "80" {
		return dnsLabel, canonicalHostname, nil
	}
	if isLoopbackTestDomain(domain) {
		return dnsLabel, net.JoinHostPort(canonicalHostname, port), nil
	}
	return "", "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_host_port", "non-standard port is only permitted on local test domains", "非标准端口仅允许在本地测试域名下使用")
}

func isLoopbackTestDomain(domain string) bool {
	return strings.HasSuffix(domain, ".local") || strings.HasSuffix(domain, ".localhost") || strings.HasSuffix(domain, ".test")
}

func ValidateReturnPath(raw string) (string, error) {
	if raw == "" {
		return "/", nil
	}
	if len(raw) > 2048 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, `/\`) || hasControlRune(raw) {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_return_path", "returnPath must be a safe origin-relative path starting with /", "回跳路径必须是以单斜杠开头的站内相对路径")
	}
	return raw, nil
}

func ValidateGatewayState(raw string) (string, error) {
	if len(raw) > 2048 || hasControlRune(raw) {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_gateway_state", "gatewayState is invalid", "网关状态参数不合法")
	}
	return raw, nil
}

func hasControlRune(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func randomShort4Hex() (string, error) {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate dns suffix: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
