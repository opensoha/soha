package sohacli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"golang.org/x/term"
)

type loginOptions struct {
	server, profile, method, login, password, store, deviceName, aiClientID, aiClientName, source string
	passwordStdin, withToken, deviceCode, noBrowser, allowHTTP, json, restart                     bool
}

func runLogin(ctx context.Context, args []string, rt Runtime) error {
	o, err := parseLoginOptions(args, rt)
	if err != nil {
		return err
	}
	if o.method == "device" && rt.DeviceLoginContext != nil {
		var stop context.CancelFunc
		ctx, stop = rt.DeviceLoginContext(ctx)
		defer stop()
	}
	server, err := validateLoginServer(o.server, o.allowHTTP)
	if err != nil {
		return err
	}
	o.server = server
	preflight, err := lockConfig(ctx, rt)
	if err != nil {
		return err
	}
	_, err = loadConfig(rt.ConfigPath)
	preflight()
	if err != nil {
		return err
	}
	store, err := openCredentialStorage(rt, o.store)
	if err != nil {
		return err
	}
	if o.method != "device" {
		if err = store.Probe(server, o.profile); err != nil {
			return err
		}
	}
	client := APIClient{ServerURL: server, Client: rt.HTTPClient, Timeout: rt.HTTPTimeout}
	p := ProfileConfig{ServerURL: server, AIClientID: o.aiClientID, AIClientName: o.aiClientName, Source: o.source, InsecureHTTP: o.allowHTTP, runtimeName: o.profile}
	var value credentialEnvelope
	var display string
	var clearPending func() error
	switch o.method {
	case "key":
		token, e := readCredentialStdin(rt, "Key")
		if e != nil {
			return e
		}
		contextClient := client
		contextClient.Token = token
		identity, e := contextClient.AuthContext(ctx)
		if e != nil {
			return authRequestFailure(e)
		}
		if e := validateAuthContext(identity); e != nil {
			return e
		}
		if identity.GovernedAI != nil {
			if identity.GovernedAI.Mode != "gateway_only" || identity.GovernedAI.AIClientID == "" || identity.GovernedAI.SkillID == "" {
				return fmt.Errorf("invalid_auth_response: invalid governed AI binding")
			}
			if o.aiClientID != "" && o.aiClientID != identity.GovernedAI.AIClientID {
				return fmt.Errorf("governed_ai_identity_mismatch: --ai-client-id differs from this Key's server binding")
			}
			p.AIClientID, p.SkillID = identity.GovernedAI.AIClientID, identity.GovernedAI.SkillID
		}
		kind := string(identity.CredentialKind)
		if kind != "personal_access_token" && kind != "service_account_token" {
			return fmt.Errorf("credential_kind_mismatch: --with-token accepts a personal or service account Key; use device login for sessions")
		}
		value = credentialEnvelope{Version: 2, Server: server, Profile: o.profile, Kind: kind, AccessToken: token, Generation: 1}
		if identity.ExpiresAt != nil {
			value.ExpiresAt = *identity.ExpiresAt
		}
		p.UserID = identity.SubjectID
		p.UserName = identity.DisplayName
		display = identity.DisplayName
	case "device", "password", "legacy-password":
		var result loginResponse
		if o.method == "device" {
			result, clearPending, err = loginWithDevice(ctx, rt, client, &store, o)
		} else {
			if o.method == "password" {
				if err = requireLoginMethod(ctx, client, "password"); err != nil {
					return err
				}
			}
			password, e := loginPassword(rt, o)
			if e != nil {
				return e
			}
			if o.method == "legacy-password" {
				result, err = client.Login(ctx, o.login, password)
			} else {
				result, err = client.CLIPassword(ctx, o.login, password)
			}
		}
		if err != nil {
			return authRequestFailure(err)
		}
		if result.Data.Tokens.AccessToken == "" {
			return fmt.Errorf("invalid_auth_response: server did not return an access token")
		}
		if o.method != "legacy-password" && int(result.Data.Tokens.ProtocolVersion) != 2 {
			return fmt.Errorf("unsupported_auth_protocol: expected a v2 CLI session")
		}
		if int(result.Data.Tokens.ProtocolVersion) == 2 {
			if e := validateV2TokenSet(result); e != nil {
				return e
			}
		}
		p, err = profileFromAuthResult(p, result, time.Now())
		if err != nil {
			return err
		}
		value = envelopeFromAuth(p, result)
		display = result.Data.User.UserName
	default:
		return fmt.Errorf("unsupported login method")
	}
	unlock, err := lockProfile(ctx, rt, server, o.profile)
	if err == nil {
		err = saveAuthenticatedProfile(ctx, rt, o.profile, p, value, store.mode, true)
		unlock()
	}
	if err != nil {
		var uncertain *credentialSaveUncertain
		if errors.As(err, &uncertain) {
			return err
		}
		if value.Kind == "session" {
			cleanupErr := revokeLoginSession(client, value)
			if cleanupErr != nil {
				_, _ = fmt.Fprintln(rt.Err, "Credential save failed and session revocation could not be confirmed; revoke the CLI session in your Web profile.")
			} else if clearPending != nil {
				_ = clearPending()
			}
		}
		return err
	}
	temporarySessionRevoked := false
	if store.mode == "ephemeral" && value.Kind == "session" {
		if err = revokeLoginSession(client, value); err != nil {
			return fmt.Errorf("ephemeral_cleanup_unconfirmed: temporary session revocation could not be confirmed; review your sessions in the Web profile")
		}
		temporarySessionRevoked = true
	}
	if clearPending != nil {
		if err = clearPending(); err != nil {
			return fmt.Errorf("login credentials saved; pending-login cleanup failed: %w", err)
		}
	}
	saved := store.mode != "ephemeral"
	if o.json {
		result := map[string]any{"authenticated": saved, "verified": true, "server": server, "profile": o.profile, "credentialKind": value.Kind, "credentialSource": store.mode, "displayName": display, "persisted": saved}
		if temporarySessionRevoked {
			result["temporarySessionRevoked"] = true
		}
		return writePrettyJSON(rt.Out, result)
	}
	if !saved {
		if temporarySessionRevoked {
			_, err = fmt.Fprintf(rt.Out, "Authentication verified as %s; the temporary session was revoked and no credentials were saved. Log in with keyring or file storage for subsequent invocations.\n", terminalLabel(display))
			return err
		}
		_, err = fmt.Fprintf(rt.Out, "Authentication verified as %s; ephemeral credentials were not saved. Use SOHA_SERVER and SOHA_TOKEN for subsequent invocations.\n", terminalLabel(display))
		return err
	}
	_, err = fmt.Fprintf(rt.Out, "Logged in to %s as %s (profile %s; credential store %s)\n", server, terminalLabel(display), terminalLabel(o.profile), store.mode)
	return err
}

func revokeLoginSession(client APIClient, value credentialEnvelope) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if value.ProtocolVersion == 2 {
		return client.CLILogout(ctx, value.RefreshToken)
	}
	return client.doJSON(ctx, http.MethodPost, "/api/v1/auth/logout", value.AccessToken, nil, sohaapi.RefreshRequest{RefreshToken: value.RefreshToken}, nil)
}
func parseLoginOptions(args []string, rt Runtime) (loginOptions, error) {
	o := loginOptions{}
	fs := newRuntimeFlagSet("login", args, rt)
	fs.StringVar(&o.server, "server", defaultServerFromEnv(), "Soha server URL")
	fs.StringVar(&o.profile, "profile", defaultProfile, "profile name")
	fs.StringVar(&o.method, "method", "device", "device, password, key or explicit legacy-password")
	fs.StringVar(&o.login, "login", env("SOHA_LOGIN"), "local password login name")
	fs.StringVar(&o.password, "password", env("SOHA_PASSWORD"), "password (prefer --password-stdin to avoid shell history)")
	fs.BoolVar(&o.passwordStdin, "password-stdin", false, "read password from stdin")
	fs.BoolVar(&o.withToken, "with-token", false, "import a personal or service account Key from stdin")
	fs.BoolVar(&o.deviceCode, "device-code", false, "explicitly select device login, including non-interactive terminals")
	fs.BoolVar(&o.noBrowser, "no-browser", false, "display the device link without opening a browser")
	fs.StringVar(&o.store, "credential-store", "auto", "auto, keyring, file or ephemeral; auto never falls back to plaintext")
	fs.StringVar(&o.deviceName, "device-name", "Terminal", "self-reported device label")
	fs.BoolVar(&o.allowHTTP, "allow-insecure-http", false, "allow an explicitly trusted non-loopback HTTP server")
	fs.BoolVar(&o.restart, "restart", false, "cancel a recoverable pending device request before starting again")
	fs.BoolVar(&o.json, "json", false, "print only the final authentication result as JSON")
	fs.StringVar(&o.aiClientID, "ai-client-id", env("SOHA_AI_CLIENT_ID"), "AI client ID")
	fs.StringVar(&o.aiClientName, "ai-client", env("SOHA_AI_CLIENT"), "AI client display name")
	fs.StringVar(&o.source, "source", "soha", "request source label")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("login does not accept positional arguments")
	}
	explicitMethod := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "method" {
			explicitMethod = true
		}
	})
	passwordSelected := o.login != "" || o.password != "" || o.passwordStdin
	if !explicitMethod {
		if o.withToken {
			o.method = "key"
		} else if passwordSelected {
			o.method = "password"
		}
	}
	if o.withToken && o.method != "key" || o.deviceCode && o.method != "device" || o.noBrowser && o.method != "device" || passwordSelected && (o.method == "device" || o.method == "key") {
		return o, fmt.Errorf("device, password and Key login options are mutually exclusive")
	}
	if o.passwordStdin && o.password != "" {
		return o, fmt.Errorf("--password-stdin cannot be combined with --password or SOHA_PASSWORD")
	}
	switch o.method {
	case "device":
		if !terminalInput(rt) && !o.deviceCode && !o.noBrowser {
			return o, fmt.Errorf("non_interactive_login: use --device-code --no-browser, --with-token, or --method password --password-stdin")
		}
	case "password", "legacy-password":
		if o.login == "" {
			return o, fmt.Errorf("--login is required for password login")
		}
	case "key":
		if !o.withToken {
			return o, fmt.Errorf("key login requires --with-token and stdin")
		}
	default:
		return o, fmt.Errorf("invalid --method; use device, password, key or legacy-password")
	}
	o.profile = profileName(o.profile)
	return o, nil
}
func terminalInput(rt Runtime) bool {
	f, ok := rt.In.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
func readCredentialStdin(rt Runtime, label string) (string, error) {
	if terminalInput(rt) {
		_, _ = fmt.Fprintf(rt.Err, "%s: ", label)
		return readHiddenLine(rt, false)
	}
	raw, err := io.ReadAll(io.LimitReader(rt.In, 65537))
	if err != nil {
		return "", fmt.Errorf("failed to read credential from stdin")
	}
	if len(raw) > 65536 {
		return "", fmt.Errorf("credential input exceeds the size limit")
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("stdin must contain one non-empty credential")
	}
	return value, nil
}
func loginPassword(rt Runtime, o loginOptions) (string, error) {
	if o.password != "" {
		return o.password, nil
	}
	if o.passwordStdin {
		return readCredentialStdin(rt, "Password")
	}
	if !terminalInput(rt) {
		return "", fmt.Errorf("password_required: use --password-stdin for non-interactive login")
	}
	_, _ = fmt.Fprint(rt.Err, "Password: ")
	return readHiddenLine(rt, false)
}
func requireLoginMethod(ctx context.Context, client APIClient, method string) error {
	_, err := cliAuthOptionsFor(ctx, client, method)
	return err
}
func cliAuthOptionsFor(ctx context.Context, client APIClient, method string) (sohaapi.CLIAuthOptions, error) {
	options, err := client.CLIAuthOptions(ctx)
	if err != nil {
		var status *httpStatusError
		if errors.As(err, &status) && (status.StatusCode == 404 || status.StatusCode == 405) {
			return options, fmt.Errorf("cli_auth_unsupported: upgrade this server or explicitly use --method legacy-password; Key import requires /auth/context")
		}
		return options, authRequestFailure(err)
	}
	if int(options.ProtocolVersion) != 2 {
		return options, fmt.Errorf("unsupported_auth_protocol: this CLI requires authentication protocol v2")
	}
	for _, supported := range options.SupportedMethods {
		if string(supported) == method {
			return options, nil
		}
	}
	return options, fmt.Errorf("cli_auth_disabled: this instance does not allow the requested login method; use a Key or contact the administrator")
}
func authRequestFailure(err error) error {
	if err == nil {
		return nil
	}
	var known *authenticationError
	if errors.As(err, &known) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	code, message := "server_unavailable", "Authentication server is unavailable; retry when it is reachable"
	var status *httpStatusError
	var transport net.Error
	if !errors.As(err, &status) && !errors.As(err, &transport) {
		return err
	}
	if errors.As(err, &status) {
		switch status.Code {
		case "access_denied", "authorization_expired", "authorization_cancelled", "expired_token", "device_authorization_denied", "device_authorization_cancelled", "device_authorization_closed", "device_already_claimed", "invalid_grant", "code_attempts_exceeded", "interactive_auth_required", "cli_auth_disabled", "refresh_recovery_expired", "refresh_reuse_detected", "refresh_replay_detected", "confirmation_expired", "device_capacity_limited", "token_rotation_completed", "rotation_request_conflict":
			code = status.Code
		default:
			if status.StatusCode == 401 {
				code = "credential_invalid"
			} else if status.StatusCode == 403 {
				code = "permission_denied"
			} else if status.StatusCode == 429 {
				code = "rate_limited"
			} else if status.StatusCode < 500 {
				code = "authentication_rejected"
			}
		}
		message = map[string]string{"token_rotation_completed": "This rotation already completed; query rotation-status with the original request ID using an independent credential", "rotation_request_conflict": "This request ID belongs to another rotation; query the original outcome", "interactive_auth_required": "Complete browser authentication using device login", "cli_auth_disabled": "CLI session login is disabled on this instance; use a Key or contact the administrator", "credential_invalid": "The server rejected this credential", "permission_denied": "This identity does not have permission for the requested action", "access_denied": "The device request was denied", "device_authorization_denied": "The device request was denied", "device_authorization_cancelled": "The device request was cancelled; start a new login", "expired_token": "The device request expired; start a new login", "refresh_recovery_expired": "The response recovery window expired; log in again and review your CLI sessions", "refresh_reuse_detected": "This session was revoked after refresh credential reuse; log in again", "refresh_replay_detected": "This session was revoked after refresh credential reuse; log in again"}[code] // #nosec G101 -- Public error messages only, never credential values.
		if message == "" {
			message = "Authentication could not be completed; review the request status and retry"
		}
	}
	return &authenticationError{Code: code, Message: message, cause: err}
}
func openDeviceBrowser(ctx context.Context, rt Runtime, uri string) error {
	if rt.OpenBrowser != nil {
		return rt.OpenBrowser(uri)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", uri) // #nosec G204 -- Fixed executable; validated HTTP(S) verification URI is one argument, never a shell.
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32.exe", "url.dll,FileProtocolHandler", uri) // #nosec G204 -- Fixed executable; validated HTTP(S) verification URI is one argument, never a shell.
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", uri) // #nosec G204 -- Fixed executable; validated HTTP(S) verification URI is one argument, never a shell.
	}
	return cmd.Run()
}
func newDeviceVerifier() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
func validateVerificationURL(uri, origin string, allowHTTP bool) error {
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid_verification_uri")
	}
	fragment, err := url.ParseQuery(u.Fragment)
	if err != nil || len(fragment) != 1 || !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(fragment.Get("handle")) {
		return fmt.Errorf("invalid_verification_uri")
	}
	u.Fragment = ""
	if !sameServerURL(u.String(), strings.TrimRight(origin, "/")+"/auth/device") {
		return fmt.Errorf("server_mismatch: verification link does not match this server's advertised verification origin")
	}
	_, err = validateLoginServer(u.String(), allowHTTP)
	return err
}

func validateV2TokenSet(result loginResponse) error {
	t := result.Data.Tokens
	if t.ProtocolVersion != 2 || t.CredentialKind != "session" || t.AccessToken == "" || t.RefreshToken == "" || t.SessionID == uuid.Nil || t.RefreshExpiresAt == nil || t.SessionAbsoluteExpiresAt == nil || !t.ExpiresAt.After(time.Now()) || !t.RefreshExpiresAt.After(time.Now()) || t.RefreshExpiresAt.After(*t.SessionAbsoluteExpiresAt) || t.ExpiresAt.After(*t.RefreshExpiresAt) {
		return fmt.Errorf("invalid_auth_response: incomplete or inconsistent v2 session metadata")
	}
	return nil
}
