package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

const (
	maxConnectorConfigBytes = 32 << 10
	maxConnectorResourceLen = 2048
	maxConnectorBytes       = 64 << 20
	maxObjectCount          = 10000
)

var (
	secretRefPattern = regexp.MustCompile(`^secret:[A-Za-z0-9][A-Za-z0-9._:/-]{0,232}$`)
	bucketPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	branchPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
)

type Validator struct {
	now func() time.Time
}

func NewValidator() *Validator {
	return &Validator{now: func() time.Time { return time.Now().UTC() }}
}

func (v *Validator) Validate(
	ctx context.Context,
	input domainknowledge.ConnectorInput,
) (domainknowledge.ConnectorValidationResult, error) {
	if err := ctx.Err(); err != nil {
		return domainknowledge.ConnectorValidationResult{}, err
	}
	if !secretRefPattern.MatchString(strings.TrimSpace(input.SecretRef)) {
		return domainknowledge.ConnectorValidationResult{}, invalidConnector("secretRef must use secret:<reference>")
	}
	raw, err := json.Marshal(input.Config)
	if err != nil {
		return domainknowledge.ConnectorValidationResult{}, invalidConnector("config must be JSON serializable")
	}
	if len(raw) == 0 || len(raw) > maxConnectorConfigBytes {
		return domainknowledge.ConnectorValidationResult{}, invalidConnector("config exceeds 32 KiB")
	}
	if containsCredentialValue(input.Config) {
		return domainknowledge.ConnectorValidationResult{}, invalidConnector("credentials must not be stored in connector config")
	}

	host, resource, warnings, err := validateConnectorConfig(input.Kind, input.Config)
	if err != nil {
		return domainknowledge.ConnectorValidationResult{}, err
	}
	hash := sha256.Sum256(raw)
	return domainknowledge.ConnectorValidationResult{
		Kind:        input.Kind,
		Valid:       true,
		Host:        host,
		Resource:    resource,
		SecretRef:   strings.TrimSpace(input.SecretRef),
		ConfigHash:  "sha256:" + hex.EncodeToString(hash[:]),
		Warnings:    warnings,
		ValidatedAt: v.now(),
	}, nil
}

func validateConnectorConfig(kind domainknowledge.SourceKind, config map[string]any) (string, string, []string, error) {
	switch kind {
	case domainknowledge.SourceKindHTTP:
		return validateHTTPConfig(config)
	case domainknowledge.SourceKindGit:
		return validateGitConfig(config)
	case domainknowledge.SourceKindObject:
		return validateObjectConfig(config)
	default:
		return "", "", nil, invalidConnector("external connector kind must be http, git, or object")
	}
}

func validateHTTPConfig(config map[string]any) (string, string, []string, error) {
	target, host, err := validateHTTPSURL(stringConfig(config, "url"), stringSliceConfig(config, "allowedHosts"))
	if err != nil {
		return "", "", nil, err
	}
	if target.RawQuery != "" || target.Fragment != "" {
		return "", "", nil, invalidConnector("HTTP url query and fragment are not allowed")
	}
	if err := validateBoundedInt(config, "maxBytes", 1, maxConnectorBytes); err != nil {
		return "", "", nil, err
	}
	return host, target.String(), []string{}, nil
}

func validateGitConfig(config map[string]any) (string, string, []string, error) {
	repository, host, err := validateHTTPSURL(stringConfig(config, "repositoryUrl"), stringSliceConfig(config, "allowedHosts"))
	if err != nil {
		return "", "", nil, err
	}
	if repository.RawQuery != "" || repository.Fragment != "" {
		return "", "", nil, invalidConnector("Git repository query and fragment are not allowed")
	}
	branch := strings.TrimSpace(stringConfig(config, "branch"))
	if branch == "" {
		branch = "main"
	}
	if !branchPattern.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "@{") {
		return "", "", nil, invalidConnector("Git branch is invalid")
	}
	if err := validateRelativePath(stringConfig(config, "path")); err != nil {
		return "", "", nil, err
	}
	if err := validateBoundedInt(config, "depth", 1, 50); err != nil {
		return "", "", nil, err
	}
	if err := validateBoundedInt(config, "maxBytes", 1, maxConnectorBytes); err != nil {
		return "", "", nil, err
	}
	return host, repository.String(), []string{}, nil
}

func validateObjectConfig(config map[string]any) (string, string, []string, error) {
	endpoint, host, err := validateHTTPSURL(stringConfig(config, "endpoint"), stringSliceConfig(config, "allowedHosts"))
	if err != nil {
		return "", "", nil, err
	}
	if endpoint.Path != "" && endpoint.Path != "/" {
		return "", "", nil, invalidConnector("object store endpoint must not contain a path")
	}
	bucket := strings.TrimSpace(stringConfig(config, "bucket"))
	if !bucketPattern.MatchString(bucket) || net.ParseIP(bucket) != nil || strings.Contains(bucket, "..") {
		return "", "", nil, invalidConnector("object store bucket is invalid")
	}
	if err := validateRelativePath(stringConfig(config, "prefix")); err != nil {
		return "", "", nil, err
	}
	if err := validateBoundedInt(config, "maxObjects", 1, maxObjectCount); err != nil {
		return "", "", nil, err
	}
	if err := validateBoundedInt(config, "maxBytes", 1, maxConnectorBytes); err != nil {
		return "", "", nil, err
	}
	return host, bucket, []string{}, nil
}

func validateHTTPSURL(raw string, allowedHosts []string) (*url.URL, string, error) {
	if len(raw) == 0 || len(raw) > maxConnectorResourceLen {
		return nil, "", invalidConnector("connector URL is required and must be at most 2048 bytes")
	}
	target, err := url.Parse(raw)
	if err != nil {
		return nil, "", invalidConnector("connector URL is invalid")
	}
	if target.Scheme != "https" || target.User != nil || target.Hostname() == "" {
		return nil, "", invalidConnector("connector URL must use HTTPS without userinfo")
	}
	if port := target.Port(); port != "" && port != "443" {
		return nil, "", invalidConnector("connector URL port must be 443")
	}
	decodedPath, err := url.PathUnescape(target.EscapedPath())
	if err != nil || hasParentPathSegment(decodedPath) {
		return nil, "", invalidConnector("connector URL path is invalid")
	}
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if unsafeHost(host) {
		return nil, "", invalidConnector("connector host is not routable by policy")
	}
	allowed := make([]string, 0, len(allowedHosts))
	for _, item := range allowedHosts {
		item = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(item), "."))
		if item != "" && !unsafeHost(item) {
			allowed = append(allowed, item)
		}
	}
	if len(allowed) == 0 || !slices.Contains(allowed, host) {
		return nil, "", invalidConnector("connector host must be present in allowedHosts")
	}
	return target, host, nil
}

func unsafeHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast())
}

func validateRelativePath(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(value) > 1024 || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") || path.Clean(value) == ".." || strings.HasPrefix(path.Clean(value), "../") {
		return invalidConnector("connector path must be a bounded relative path")
	}
	return nil
}

func hasParentPathSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func validateBoundedInt(config map[string]any, key string, minimum, maximum int) error {
	value, ok := intConfig(config, key)
	if !ok || value < minimum || value > maximum {
		return invalidConnector(fmt.Sprintf("%s must be between %d and %d", key, minimum, maximum))
	}
	return nil
}

func intConfig(config map[string]any, key string) (int, bool) {
	switch value := config[key].(type) {
	case int:
		return value, true
	case float64:
		if value != float64(int(value)) {
			return 0, false
		}
		return int(value), true
	default:
		return 0, false
	}
}

func stringConfig(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return strings.TrimSpace(value)
}

func stringSliceConfig(config map[string]any, key string) []string {
	values, ok := config[key].([]string)
	if ok {
		return values
	}
	raw, ok := config[key].([]any)
	if !ok {
		return []string{}
	}
	values = make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok {
			values = append(values, value)
		}
	}
	return values
}

func containsCredentialValue(value any) bool {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			switch normalized {
			case "secret", "password", "token", "apikey", "authorization", "credential", "accesskey", "secretkey":
				return true
			}
			if containsCredentialValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if containsCredentialValue(child) {
				return true
			}
		}
	}
	return false
}

func invalidConnector(message string) error {
	return fmt.Errorf("%w: %s", domainknowledge.ErrInvalidInput, message)
}
