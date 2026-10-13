package aigateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/redaction"
)

const connectorActionProtocol = "opensoha.dev/connector-action/v1"

var connectorCredentialAlias = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
var connectorCredentialID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type connectorCredential struct {
	Reference string `json:"reference"`
	Value     string `json:"value"`
}

func (p *connectorRuntimeProvider) actionBody(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, []string, error) {
	if input == nil {
		input = map[string]any{}
	}
	values := domainsecret.ValuesFromContext(ctx)
	if len(values) == 0 {
		return map[string]any{"input": input}, nil, nil
	}
	var action connectorRuntimeAction
	for _, candidate := range p.actions {
		if candidate.Name == tool.Name {
			action = candidate
			break
		}
	}
	if action.CredentialProtocol != connectorActionProtocol {
		return nil, nil, fmt.Errorf("%w: connector runtime does not support per-call credential delivery", apperrors.ErrUnsupportedOperation)
	}
	if utf8.RuneCountInString(tool.Name) > 128 {
		return nil, nil, fmt.Errorf("%w: credential connector action name exceeds 128 characters", apperrors.ErrInvalidArgument)
	}
	if p.runtimeToken == "" || !credentialRuntimeEndpoint(p.endpoint) {
		return nil, nil, fmt.Errorf("%w: credential delivery requires authenticated HTTPS or loopback transport", apperrors.ErrUnsupportedOperation)
	}
	execution, ok := domainsecret.ExecutionContextFrom(ctx)
	if !ok || execution.Target.Type != "capability" || execution.Target.Ref != tool.Name || !sameConnectorCredentialPrincipal(execution.Principal, principal) {
		return nil, nil, fmt.Errorf("%w: pinned credential execution context does not match the invocation", apperrors.ErrAccessDenied)
	}
	entries, err := connectorCredentialEntries(values, execution.References, action.CredentialAliases)
	if err != nil {
		return nil, nil, err
	}
	needles, err := connectorCredentialStrings(values)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 2*time.Minute {
		return nil, nil, fmt.Errorf("%w: credential invocation needs a bounded lifetime", apperrors.ErrInvalidArgument)
	}
	return map[string]any{"schemaVersion": connectorActionProtocol, "action": tool.Name, "input": input, "credentials": map[string]any{"expiresAt": deadline.UTC().Format(time.RFC3339Nano), "entries": entries}}, needles, nil
}

func sameConnectorCredentialPrincipal(bound, caller domainidentity.Principal) bool {
	return caller.UserID != "" && bound.UserID == caller.UserID && bound.AccessTokenID == caller.AccessTokenID
}

func connectorCredentialEntries(values map[string]string, refs []domainsecret.Reference, aliases []string) (map[string]connectorCredential, error) {
	invalid := fmt.Errorf("%w: invalid pinned connector credential aliases or material", apperrors.ErrInvalidArgument)
	if len(values) > 16 || len(refs) != len(values) || len(aliases) == 0 || len(aliases) > 16 {
		return nil, invalid
	}
	allowed := map[string]bool{}
	for _, alias := range aliases {
		if !connectorCredentialAlias.MatchString(alias) || allowed[alias] {
			return nil, invalid
		}
		allowed[alias] = true
	}
	entries := map[string]connectorCredential{}
	for _, ref := range refs {
		value, exists := values[ref.Alias]
		if !exists || !allowed[ref.Alias] || entries[ref.Alias].Reference != "" || len(value) == 0 || len(value) > 65536 || !connectorCredentialID.MatchString(ref.SecretID) || ref.Version < 1 || len(ref.URI) > 512 || ref.URI != fmt.Sprintf("soha://secrets/%s/versions/%d", ref.SecretID, ref.Version) {
			return nil, invalid
		}
		entries[ref.Alias] = connectorCredential{Reference: ref.URI, Value: value}
	}
	return entries, nil
}

func credentialRuntimeEndpoint(raw string) bool {
	target, err := url.Parse(raw)
	if err != nil || target.User != nil || target.RawQuery != "" || target.Fragment != "" || target.Hostname() == "" {
		return false
	}
	if target.Scheme == "https" {
		return true
	}
	host := strings.ToLower(target.Hostname())
	return target.Scheme == "http" && (host == "localhost" || net.ParseIP(host).IsLoopback())
}

func connectorActionTransportError(ctx context.Context, err error, needles []string) error {
	if len(needles) == 0 {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("%w: credential connector request or response failed", apperrors.ErrInvalidArgument)
}

func connectorCredentialStrings(values map[string]string) ([]string, error) {
	set := map[string]bool{}
	for _, value := range values {
		if err := collectConnectorCredentialStrings(value, set, 0); err != nil {
			return nil, err
		}
		var structured any
		if json.Unmarshal([]byte(value), &structured) == nil {
			if err := collectConnectorCredentialStrings(structured, set, 0); err != nil {
				return nil, err
			}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	slices.SortFunc(out, func(a, b string) int { return len(b) - len(a) })
	return out, nil
}

func collectConnectorCredentialStrings(value any, set map[string]bool, depth int) error {
	if depth > 16 || len(set) > 64 {
		return fmt.Errorf("%w: connector credential material exceeds bounded structure", apperrors.ErrInvalidArgument)
	}
	switch item := value.(type) {
	case string:
		if item != "" {
			if !set[item] && len(set) >= 64 {
				return fmt.Errorf("%w: connector credential material exceeds bounded structure", apperrors.ErrInvalidArgument)
			}
			set[item] = true
		}
	case []any:
		for _, child := range item {
			if err := collectConnectorCredentialStrings(child, set, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, child := range item {
			if err := collectConnectorCredentialStrings(key, set, depth+1); err != nil {
				return err
			}
			if err := collectConnectorCredentialStrings(child, set, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func redactConnectorCredentialOutput(value any, needles []string) any {
	value = redaction.Value(value)
	switch item := value.(type) {
	case string:
		for _, needle := range needles {
			item = strings.ReplaceAll(item, needle, redaction.Redacted)
		}
		return item
	case []any:
		for index, child := range item {
			item[index] = redactConnectorCredentialOutput(child, needles)
		}
	case map[string]any:
		out := make(map[string]any, len(item))
		for key, child := range item {
			for _, needle := range needles {
				key = strings.ReplaceAll(key, needle, redaction.Redacted)
			}
			out[key] = redactConnectorCredentialOutput(child, needles)
		}
		return out
	}
	return value
}
