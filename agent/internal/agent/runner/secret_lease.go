package runner

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type secretValuesContextKey struct{}

var secretEnvironmentAliasPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func validSecretEnvironmentAlias(alias string) bool {
	if !secretEnvironmentAliasPattern.MatchString(alias) {
		return false
	}
	switch alias {
	case "BASH_ENV", "BASHOPTS", "CDPATH", "ENV", "GLOBIGNORE", "HOME", "IFS", "PATH", "SHELL", "SHELLOPTS":
		return false
	}
	return !strings.HasPrefix(alias, "LD_") && !strings.HasPrefix(alias, "DYLD_")
}

func (r *Runner) redeemSecretLease(ctx context.Context, grant *sohaapi.SecretLeaseGrant, agentID string) (context.Context, error) {
	if grant == nil {
		return ctx, nil
	}
	if strings.TrimSpace(grant.ID) == "" || strings.TrimSpace(grant.Token) == "" || strings.TrimSpace(agentID) == "" {
		return ctx, fmt.Errorf("invalid secret lease grant")
	}
	redemption, err := r.apiClient().RedeemSecretLease(ctx, grant.ID, sohaapi.RedeemSecretLeaseParams{
		XSohaSecretLeaseToken: sohaapi.SecretLeaseToken(grant.Token),
		XSohaAgentID:          sohaapi.SecretLeaseAgentID(agentID),
	})
	if err != nil {
		return ctx, fmt.Errorf("redeem secret lease: %w", err)
	}
	if redemption.LeaseID != grant.ID || !redemption.ExpiresAt.After(time.Now().UTC()) {
		return ctx, fmt.Errorf("invalid secret lease redemption")
	}
	values := make(map[string]string, len(redemption.Values))
	for alias, value := range redemption.Values {
		alias = strings.TrimSpace(alias)
		if !validSecretEnvironmentAlias(alias) {
			return ctx, fmt.Errorf("invalid secret lease alias")
		}
		values[alias] = value
	}
	return context.WithValue(ctx, secretValuesContextKey{}, values), nil
}

func secretEnvironment(ctx context.Context) []string {
	values, _ := ctx.Value(secretValuesContextKey{}).(map[string]string)
	if len(values) == 0 {
		return nil
	}
	aliases := make([]string, 0, len(values))
	for alias := range values {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	aliasSet := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		aliasSet[alias] = struct{}{}
	}
	environment := make([]string, 0, len(os.Environ())+len(aliases))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, overridden := aliasSet[name]; !overridden {
			environment = append(environment, entry)
		}
	}
	for _, alias := range aliases {
		environment = append(environment, alias+"="+values[alias])
	}
	return environment
}

func redactResolvedSecretValues(ctx context.Context, value any) any {
	values, _ := ctx.Value(secretValuesContextKey{}).(map[string]string)
	if len(values) == 0 {
		return value
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = redactResolvedSecretValues(ctx, item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			redacted, _ := redactResolvedSecretValues(ctx, item).(map[string]any)
			out = append(out, redacted)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactResolvedSecretValues(ctx, item))
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactResolvedSecretText(values, item))
		}
		return out
	case string:
		return redactResolvedSecretText(values, typed)
	default:
		return typed
	}
}

func redactResolvedSecretText(values map[string]string, text string) string {
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	// Registry errors can contain individual credentials rather than the full JSON secret.
	for _, value := range buildpacksRegistrySecrets(values["REGISTRY_AUTH"]) {
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}
