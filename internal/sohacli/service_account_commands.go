package sohacli

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func runServiceAccount(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("service-account requires a subcommand: list, create, token-list, token-create, token-revoke, token-rotate, or rotation-status")
	}
	switch args[0] {
	case "list":
		return runServiceAccountList(ctx, args[1:], rt)
	case "create":
		return runServiceAccountCreate(ctx, args[1:], rt)
	case "token-list":
		return runServiceAccountTokenList(ctx, args[1:], rt)
	case "token-create":
		return runServiceAccountTokenCreate(ctx, args[1:], rt)
	case "token-rotate":
		return runTokenRotate(ctx, args[1:], rt, "service_account_token")
	case "rotation-status":
		return runTokenRotationStatus(ctx, args[1:], rt)
	case "token-revoke":
		return runServiceAccountTokenRevoke(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown service-account command %q", args[0])
	}
}

func runServiceAccountList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("service-account list", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	format := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	formatValue, err := normalizeOutputFormat(*format, "json", "yaml")
	if err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	items, err := gatewayClient(rt, profile).ListServiceAccounts(ctx, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, formatValue, sanitizeCLIValue(items))
}

func runServiceAccountTokenList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("service-account token-list", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	format := fs.String("output", "json", "output format: json or yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	formatValue, err := normalizeOutputFormat(*format, "json", "yaml")
	if err != nil {
		return err
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	items, err := gatewayClient(rt, profile).ListServiceAccountTokens(ctx, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, formatValue, sanitizeCLIValue(items))
}

func runServiceAccountCreate(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("service-account create", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	id := fs.String("id", "", "service account id")
	name := fs.String("name", "", "service account name")
	description := fs.String("description", "", "service account description")
	status := fs.String("status", "active", "service account status")
	roleIDs := fs.String("role-ids", "", "comma-separated role ids")
	teamIDs := fs.String("team-ids", "", "comma-separated team ids")
	scopeGrantIDs := fs.String("scope-grant-ids", "", "comma-separated scope grant ids")
	metadata := fs.String("metadata-json", "", "metadata JSON object")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("--name is required")
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"id":            strings.TrimSpace(*id),
		"name":          strings.TrimSpace(*name),
		"description":   strings.TrimSpace(*description),
		"status":        strings.TrimSpace(*status),
		"roleIds":       splitCSV(*roleIDs),
		"teamIds":       splitCSV(*teamIDs),
		"scopeGrantIds": splitCSV(*scopeGrantIDs),
	}
	if *metadata != "" {
		parsed, err := parseJSONObject([]byte(*metadata))
		if err != nil {
			return fmt.Errorf("invalid --metadata-json: %w", err)
		}
		payload["metadata"] = parsed
	}
	item, err := gatewayClient(rt, profile).CreateServiceAccount(ctx, payload, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCLIValue(item))
}

func runServiceAccountTokenCreate(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("service-account token-create", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
	serviceAccountID := fs.String("service-account-id", "", "service account id")
	name := fs.String("name", "", "token name")
	scopes := fs.String("scopes", "", "comma-separated token scopes")
	permissionKeys := fs.String("permission-keys", "", "comma-separated permission keys")
	expiresAt := fs.String("expires-at", "", "RFC3339 expiration time")
	metadata := fs.String("metadata-json", "", "metadata JSON object")
	governed := fs.Bool("governed-ai", false, "restrict this Key to a registered AI client, known skill and governed Gateway endpoints")
	clientID := fs.String("ai-client-id", "", "registered AI client ID for --governed-ai")
	skillID := fs.String("skill-id", "", "known skill ID for --governed-ai")
	if err := fs.Parse(args); err != nil {
		return err
	}
	saID := firstNonEmptyString(*serviceAccountID, fs.Arg(0))
	if saID == "" {
		return fmt.Errorf("service-account token-create requires --service-account-id or positional id")
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("--name is required")
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"name":           strings.TrimSpace(*name),
		"scopes":         splitCSV(*scopes),
		"permissionKeys": splitCSV(*permissionKeys),
	}
	expiry, err := keyExpiry(*expiresAt)
	if err != nil {
		return err
	}
	payload["expiresAt"] = expiry.Format(time.RFC3339)

	if *metadata != "" {
		parsed, err := parseJSONObject([]byte(*metadata))
		if err != nil {
			return fmt.Errorf("invalid --metadata-json: %w", err)
		}
		payload["metadata"] = parsed
	}
	if err := applyGovernedCredentialInput(payload, *governed, *clientID, *skillID); err != nil {
		return err
	}
	created, err := gatewayClient(rt, profile).CreateServiceAccountToken(ctx, saID, payload, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCreatedToken(created))
}

func runServiceAccountTokenRevoke(ctx context.Context, args []string, rt Runtime) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("service-account token-revoke", rest, rt)
	profileFlag := fs.String("profile", "", "profile name")
	tokenID := fs.String("id", "", "token id")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	if len(ids) > 1 {
		return usageError{message: "service-account token-revoke accepts one token id"}
	}
	positional := ""
	if len(ids) == 1 {
		positional = ids[0]
	}
	id := firstNonEmptyString(*tokenID, positional)
	if id == "" {
		return fmt.Errorf("service-account token-revoke requires a token id")
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	if err := gatewayClient(rt, profile).RevokeServiceAccountToken(ctx, id, gatewayHeaders(profile, "", "", "", "")); err != nil {
		return err
	}
	_, err = fmt.Fprintf(rt.Out, "Revoked service account token %s\n", id)
	return err
}
