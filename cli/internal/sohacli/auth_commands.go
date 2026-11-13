package sohacli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type authStatusError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
type authStatusView struct {
	Authenticated            *bool            `json:"authenticated"`
	Verification             string           `json:"verification"`
	Server                   string           `json:"server"`
	Profile                  string           `json:"profile"`
	CredentialSource         string           `json:"credentialSource"`
	CredentialKind           string           `json:"credentialKind"`
	SubjectType              string           `json:"subjectType"`
	SubjectID                string           `json:"subjectId"`
	DisplayName              string           `json:"displayName"`
	ExpiresAt                *time.Time       `json:"expiresAt"`
	RefreshIdleExpiresAt     *time.Time       `json:"refreshIdleExpiresAt"`
	SessionAbsoluteExpiresAt *time.Time       `json:"sessionAbsoluteExpiresAt"`
	PermissionSummary        []string         `json:"permissionSummary"`
	Error                    *authStatusError `json:"error"`
}

func runAuth(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return usageError{message: "usage: soha auth <status|sessions|revoke-session|mfa>"}
	}
	switch args[0] {
	case "status":
		return runAuthStatus(ctx, args[1:], rt)
	case "mfa":
		return runAuthMFA(ctx, args[1:], rt)
	case "sessions":
		fs := newRuntimeFlagSet("auth sessions", args[1:], rt)
		name := fs.String("profile", "", "profile name")
		limit := fs.Int("limit", 20, "page size 1..100")
		cursor := fs.String("cursor", "", "page cursor")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		_, _, p, err := loadRuntimeProfile(ctx, rt, *name)
		if err != nil {
			return err
		}
		result, err := gatewayClient(rt, p).CLISessions(ctx, *limit, *cursor)
		if err != nil {
			return authRequestFailure(err)
		}
		return writePrettyJSON(rt.Out, result)
	case "revoke-session":
		fs := newRuntimeFlagSet("auth revoke-session", args[1:], rt)
		name := fs.String("profile", "", "profile name")
		id := fs.String("id", "", "CLI session ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" {
			return usageError{message: "--id is required"}
		}
		_, _, p, err := loadRuntimeProfile(ctx, rt, *name)
		if err != nil {
			return err
		}
		if err = gatewayClient(rt, p).RevokeCLISession(ctx, *id); err != nil {
			return authRequestFailure(err)
		}
		_, err = fmt.Fprintln(rt.Out, "CLI session revoked.")
		return err
	default:
		return usageError{message: "unknown auth command; use status, sessions, revoke-session or mfa"}
	}
}
func runWhoami(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("whoami", args, rt)
	name := fs.String("profile", "", "profile name")
	jsonOutput := fs.Bool("json", false, "print server-confirmed identity as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, _, p, err := loadRuntimeProfile(ctx, rt, *name)
	if err != nil {
		return err
	}
	identity, err := gatewayClient(rt, p).AuthContext(ctx)
	if err != nil {
		return authRequestFailure(err)
	}
	if err = validateAuthContext(identity); err != nil {
		return err
	}
	if *jsonOutput {
		return writePrettyJSON(rt.Out, identity)
	}
	_, err = fmt.Fprintf(rt.Out, "%s (%s:%s) · %s\n", terminalLabel(identity.DisplayName), identity.SubjectType, terminalLabel(identity.SubjectID), identity.CredentialKind)
	return err
}
func validateAuthContext(identity sohaapi.CLIAuthContext) error {
	validKind := identity.CredentialKind == "session" || identity.CredentialKind == "personal_access_token" || identity.CredentialKind == "service_account_token"
	if !validKind || identity.SubjectID == "" || (identity.SubjectType != "user" && identity.SubjectType != "service_account") {
		return fmt.Errorf("invalid_auth_response: server did not confirm a supported credential and subject")
	}
	return nil
}
func terminalLabel(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r >= 127 && r <= 159 {
			return -1
		}
		return r
	}, value)
}
func statusMetadata(rt Runtime, requested string) (string, ProfileConfig, error) {
	token, server := env("SOHA_TOKEN"), normalizeServerURL(env("SOHA_SERVER"))
	if token != "" && server != "" {
		if !sameServerURL(server, server) {
			return "", ProfileConfig{}, fmt.Errorf("server_mismatch: invalid environment server")
		}
		return profileName(requested), ProfileConfig{ServerURL: server, CredentialStore: "environment", CredentialKind: "unknown"}, nil
	}
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return "", ProfileConfig{}, err
	}
	name := profileName(firstNonEmptyString(requested, cfg.CurrentProfile))
	p, ok := cfg.Profiles[name]
	if token != "" {
		p.ServerURL = firstNonEmptyString(server, p.ServerURL, defaultServerURL)
		p.CredentialStore = "environment"
		p.CredentialKind = "unknown"
		p.UserID = ""
		p.UserName = ""
		p.ExpiresAt = time.Time{}
		return name, p, nil
	}
	if !ok {
		return name, p, &authenticationError{Code: "credential_missing", Message: "No credentials are configured; run soha login"}
	}
	if server != "" && !sameServerURL(server, p.ServerURL) {
		return name, p, &authenticationError{Code: "server_mismatch", Message: "The selected profile belongs to another server"}
	}
	if p.AccessToken == "" && p.CredentialRef == "" {
		return name, p, &authenticationError{Code: "credential_missing", Message: "This profile has no stored credentials; run soha login"}
	}
	if p.CredentialStore == "" {
		p.CredentialStore = "file"
	}
	if p.CredentialKind == "" {
		p.CredentialKind = "legacy"
	}
	return name, p, nil
}
func runAuthStatus(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("auth status", args, rt)
	name := fs.String("profile", "", "profile name")
	offline := fs.Bool("offline", false, "show cached metadata without reading secrets or contacting the server")
	jsonOutput := fs.Bool("json", false, "print a credential-free JSON status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolved, p, err := statusMetadata(rt, *name)
	view := authStatusView{Verification: "cached", Profile: resolved, Server: p.ServerURL, CredentialSource: p.CredentialStore, CredentialKind: p.CredentialKind, SubjectType: "unknown", SubjectID: p.UserID, DisplayName: p.UserName, PermissionSummary: []string{}}
	if !p.ExpiresAt.IsZero() {
		view.ExpiresAt = &p.ExpiresAt
	}
	if err == nil && !*offline {
		view.Verification = "online"
		_, _, active, e := loadRuntimeProfile(ctx, rt, *name)
		if e == nil {
			identity, queryErr := gatewayClient(rt, active).AuthContext(ctx)
			e = queryErr
			if e == nil {
				e = validateAuthContext(identity)
			}
			if e == nil {
				yes := true
				view.Authenticated = &yes
				view.CredentialKind = string(identity.CredentialKind)
				view.SubjectType = string(identity.SubjectType)
				view.SubjectID = identity.SubjectID
				view.DisplayName = identity.DisplayName
				view.ExpiresAt = identity.ExpiresAt
				view.RefreshIdleExpiresAt = identity.RefreshExpiresAt
				view.SessionAbsoluteExpiresAt = identity.SessionAbsoluteExpiresAt
				view.PermissionSummary = identity.PermissionKeys
			}
		}
		err = e
	}
	if err != nil {
		err = authRequestFailure(err)
		code, message, retryable := "credential_invalid", "Authentication could not be verified", false
		var classified *authenticationError
		if errors.As(err, &classified) {
			code = classified.Code
			message = classified.Message
			retryable = code == "server_unavailable" || code == "auth_refresh_unavailable" || code == "store_busy"
		}
		view.Error = &authStatusError{Code: code, Message: message, Retryable: retryable}
		if code == "credential_invalid" || code == "auth_refresh_rejected" {
			no := false
			view.Authenticated = &no
		}
	}
	if *jsonOutput {
		if e := writePrettyJSON(rt.Out, view); e != nil {
			return e
		}
	} else {
		state := "unverified (cached metadata)"
		if view.Authenticated != nil {
			if *view.Authenticated {
				state = "authenticated"
			} else {
				state = "rejected"
			}
		}
		if _, e := fmt.Fprintf(rt.Out, "%s · %s · profile %s · %s/%s\n", state, view.Server, view.Profile, view.CredentialSource, view.CredentialKind); e != nil {
			return e
		}
		if view.Error != nil {
			_, _ = fmt.Fprintf(rt.Err, "%s: %s\n", view.Error.Code, view.Error.Message)
		}
	}
	return err
}
func runLogout(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("logout", args, rt)
	requested := fs.String("profile", "", "profile name")
	localOnly := fs.Bool("local-only", false, "remove only stored local credentials")
	revoke := fs.Bool("revoke", false, "also revoke the selected Key on the server")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *localOnly && *revoke {
		return usageError{message: "--local-only and --revoke are mutually exclusive"}
	}
	if env("SOHA_TOKEN") != "" && !*localOnly {
		return fmt.Errorf("environment_credentials_active: unset SOHA_TOKEN to end environment authentication; --local-only only clears a stored profile")
	}
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	name := profileName(firstNonEmptyString(*requested, cfg.CurrentProfile))
	metadata, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("credential_missing: profile is not configured")
	}
	lockedServer := metadata.ServerURL
	if !*localOnly && env("SOHA_SERVER") != "" && !sameServerURL(metadata.ServerURL, env("SOHA_SERVER")) {
		return &authenticationError{Code: "server_mismatch", Message: "This profile belongs to another server"}
	}
	unlock, err := lockProfile(ctx, rt, metadata.ServerURL, name)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err = loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	metadata, ok = cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile_changed: profile was removed")
	}
	if !sameServerURL(lockedServer, metadata.ServerURL) {
		return fmt.Errorf("profile_changed: retry logout after the profile update")
	}
	p := metadata
	if !*localOnly {
		p, err = hydrateProfile(rt, name, metadata)
		if err != nil {
			return err
		}
		client := APIClient{ServerURL: p.ServerURL, Token: p.AccessToken, Client: rt.HTTPClient, Timeout: rt.HTTPTimeout}
		switch {
		case p.ProtocolVersion == 2 && p.RefreshToken != "":
			err = client.CLILogout(ctx, p.RefreshToken)
		case p.CredentialKind == "session" || p.CredentialKind == "" && p.RefreshToken != "":
			err = client.doJSON(ctx, http.MethodPost, "/api/v1/auth/logout", p.AccessToken, nil, sohaapi.RefreshRequest{RefreshToken: p.RefreshToken}, nil)
		case *revoke:
			var identity sohaapi.CLIAuthContext
			identity, err = client.AuthContext(ctx)
			if err == nil {
				if identity.TokenID == "" {
					err = fmt.Errorf("credential_kind_mismatch: the server did not return a Key ID")
				} else if identity.CredentialKind == "personal_access_token" {
					err = client.RevokePersonalAccessToken(ctx, identity.TokenID, nil)
				} else if identity.CredentialKind == "service_account_token" {
					err = client.RevokeServiceAccountToken(ctx, identity.TokenID, nil)
				} else {
					err = fmt.Errorf("credential_kind_mismatch: --revoke requires a personal or service account Key")
				}
			}
		}
		if err != nil {
			return authRequestFailure(err)
		}
	}
	if err = clearLocalAuthentication(ctx, rt, name, metadata); err != nil {
		return err
	}
	if env("SOHA_TOKEN") != "" {
		_, _ = fmt.Fprintln(rt.Err, "Stored profile cleared; SOHA_TOKEN still supplies environment authentication.")
	}
	_, err = fmt.Fprintf(rt.Out, "Cleared authentication for profile %s.\n", name)
	return err
}
func clearLocalAuthentication(ctx context.Context, rt Runtime, name string, expected ProfileConfig) error {
	unlock, err := lockConfig(ctx, rt)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return err
	}
	p, ok := cfg.Profiles[name]
	if !ok {
		return nil
	}
	if p.CredentialRef != expected.CredentialRef || !sameServerURL(p.ServerURL, expected.ServerURL) {
		return fmt.Errorf("profile_changed: retry logout for the current profile")
	}
	ref, mode := p.CredentialRef, p.CredentialStore
	p.AccessToken = ""
	p.RefreshToken = ""
	p.CredentialRef = ""
	p.CredentialStore = ""
	p.CredentialKind = ""
	p.ProtocolVersion = 0
	p.ExpiresAt = time.Time{}
	p.UserID = ""
	p.UserName = ""
	cfg.Profiles[name] = p
	if err = saveConfig(rt.ConfigPath, cfg); err != nil {
		return err
	}
	if ref != "" {
		store, e := openCredentialStorage(rt, mode)
		if e != nil {
			return e
		}
		return store.Delete(ref)
	}
	return nil
}
