package sohacli

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func runToken(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return fmt.Errorf("token requires a subcommand: list, create, revoke, rotate, or rotation-status")
	}
	switch args[0] {
	case "list":
		return runTokenList(ctx, args[1:], rt)
	case "create":
		return runTokenCreate(ctx, args[1:], rt)
	case "rotate":
		return runTokenRotate(ctx, args[1:], rt, "personal_access_token")
	case "rotation-status":
		return runTokenRotationStatus(ctx, args[1:], rt)
	case "revoke":
		return runTokenRevoke(ctx, args[1:], rt)
	default:
		return fmt.Errorf("unknown token command %q", args[0])
	}
}

func runTokenList(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("token list", args, rt)
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
	items, err := gatewayClient(rt, profile).ListPersonalAccessTokens(ctx, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writeStructuredOutput(rt.Out, formatValue, sanitizeCLIValue(items))
}

func runTokenCreate(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("token create", args, rt)
	profileFlag := fs.String("profile", "", "profile name")
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
	created, err := gatewayClient(rt, profile).CreatePersonalAccessToken(ctx, payload, gatewayHeaders(profile, "", "", "", ""))
	if err != nil {
		return err
	}
	return writePrettyJSON(rt.Out, sanitizeCreatedToken(created))
}

func runTokenRevoke(ctx context.Context, args []string, rt Runtime) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("token revoke", rest, rt)
	profileFlag := fs.String("profile", "", "profile name")
	tokenID := fs.String("id", "", "token id")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	ids := append(leading, fs.Args()...)
	if len(ids) > 1 {
		return usageError{message: "token revoke accepts one token id"}
	}
	positional := ""
	if len(ids) == 1 {
		positional = ids[0]
	}
	id := firstNonEmptyString(*tokenID, positional)
	if id == "" {
		return fmt.Errorf("token revoke requires a token id")
	}
	_, _, profile, err := loadRuntimeProfile(ctx, rt, *profileFlag)
	if err != nil {
		return err
	}
	if err := gatewayClient(rt, profile).RevokePersonalAccessToken(ctx, id, gatewayHeaders(profile, "", "", "", "")); err != nil {
		return err
	}
	_, err = fmt.Fprintf(rt.Out, "Revoked token %s\n", id)
	return err
}
