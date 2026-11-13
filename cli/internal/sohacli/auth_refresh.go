package sohacli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func envelopeFromAuth(p ProfileConfig, result loginResponse) credentialEnvelope {
	t := result.Data.Tokens
	value := credentialEnvelope{Version: 2, Server: canonicalServerURL(p.ServerURL), Profile: p.runtimeName, Kind: "session", ProtocolVersion: int(t.ProtocolVersion), AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, ExpiresAt: tokenExpiresAt(t.ExpiresAt, t.ExpiresIn, time.Now()), RefreshExpiresAt: t.RefreshExpiresAt, AbsoluteExpiresAt: t.SessionAbsoluteExpiresAt, SessionID: t.SessionID.String(), Generation: 1}
	if value.ProtocolVersion != 2 && value.RefreshToken != "" {
		value.LegacySessionFingerprint = legacyAuthenticationFingerprint(ProfileConfig{RefreshToken: value.RefreshToken})
	}
	return value
}
func authResultFromProfile(p ProfileConfig) loginResponse {
	var r loginResponse
	r.Data.User.UserID = p.UserID
	r.Data.User.UserName = p.UserName
	r.Data.Tokens.AccessToken = p.AccessToken
	r.Data.Tokens.RefreshToken = p.RefreshToken
	r.Data.Tokens.ExpiresAt = p.ExpiresAt
	r.Data.Tokens.TokenType = "Bearer"
	return r
}
func refreshStoredProfile(ctx context.Context, rt Runtime, name string, expected ProfileConfig, force bool) (ProfileConfig, error) {
	unlock, err := lockProfile(ctx, rt, expected.ServerURL, name)
	if err != nil {
		return ProfileConfig{}, err
	}
	defer unlock()
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return ProfileConfig{}, err
	}
	current, ok := cfg.Profiles[name]
	if !ok || !sameServerURL(current.ServerURL, expected.ServerURL) {
		return ProfileConfig{}, fmt.Errorf("profile_changed: profile changed while waiting for credential storage; retry")
	}
	current, err = hydrateProfile(rt, name, current)
	if err != nil {
		return ProfileConfig{}, err
	}
	if !sameProfileAuthentication(expected, current) {
		return ProfileConfig{}, &authenticationError{Code: "profile_changed", Message: "Profile authentication changed; retry the command or restart the MCP process with the intended identity"}
	}
	if !accessTokenExpired(current, time.Now()) && (!force || current.AccessToken != expected.AccessToken) {
		return current, nil
	}
	if current.RefreshToken == "" {
		return ProfileConfig{}, fmt.Errorf("auth_refresh_missing: access token expired and this credential cannot refresh; run soha login again")
	}
	timeout := 15 * time.Second
	if rt.HTTPTimeout > 0 && rt.HTTPTimeout < timeout {
		timeout = rt.HTTPTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := APIClient{ServerURL: current.ServerURL, Client: rt.HTTPClient, Timeout: timeout}
	var result refreshResponse
	if current.ProtocolVersion == 2 {
		if current.credential == nil {
			return ProfileConfig{}, fmt.Errorf("invalid v2 credential envelope")
		}
		value := *current.credential
		if value.PendingRefreshRequestID == "" {
			value.PendingRefreshRequestID = uuid.NewString()
			store, e := openCredentialStorage(rt, current.CredentialStore)
			if e != nil {
				return ProfileConfig{}, e
			}
			if e = store.Save(current.CredentialRef, value); e != nil {
				return ProfileConfig{}, e
			}
		}
		id, e := uuid.Parse(value.PendingRefreshRequestID)
		if e != nil {
			return ProfileConfig{}, fmt.Errorf("invalid pending refresh request")
		}
		result, err = client.CLIRefresh(requestCtx, sohaapi.CLIRefreshRequest{RefreshToken: current.RefreshToken, RefreshRequestID: id})
	} else {
		result, err = client.Refresh(requestCtx, current.RefreshToken)
	}
	if err != nil {
		return ProfileConfig{}, refreshFailure(err)
	}
	if current.ProtocolVersion == 2 {
		if err = validateV2TokenSet(result); err != nil {
			return ProfileConfig{}, err
		}
	}
	if current.ProtocolVersion == 2 && current.credential.SessionID != result.Data.Tokens.SessionID.String() {
		return ProfileConfig{}, fmt.Errorf("invalid_auth_response: refresh changed the CLI session identity")
	}
	updated, err := profileFromAuthResult(current, result, time.Now())
	if err != nil {
		return ProfileConfig{}, err
	}
	value := envelopeFromAuth(updated, result)
	value.Profile = name
	if current.ProtocolVersion != 2 {
		value.LegacySessionFingerprint = legacyAuthenticationFingerprint(current)
	}
	if current.credential != nil {
		value.Generation = current.credential.Generation + 1
	}
	mode := current.CredentialStore
	// A legacy profile is already an explicitly stored plaintext file. Migration
	// retains that storage choice; it never falls back from a failed keyring.
	if mode == "" {
		mode = "file"
	}
	if err = saveAuthenticatedProfile(ctx, rt, name, updated, value, mode, false); err != nil {
		return ProfileConfig{}, err
	}
	latest, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return ProfileConfig{}, err
	}
	return hydrateProfile(rt, name, latest.Profiles[name])
}

// A new reference/token generation may belong to the same v2 session. A new
// login or Key must never change the identity of an already-started request.
func sameProfileAuthentication(expected, current ProfileConfig) bool {
	kind := func(p ProfileConfig) string {
		if (p.CredentialKind == "" || p.CredentialKind == "legacy") && p.RefreshToken != "" {
			return "session"
		}
		return p.CredentialKind
	}
	if kind(expected) != kind(current) || expected.UserID != "" && current.UserID != expected.UserID {
		return false
	}
	if expected.ProtocolVersion == 2 || current.ProtocolVersion == 2 {
		return expected.ProtocolVersion == 2 && current.ProtocolVersion == 2 && expected.credential != nil && current.credential != nil && expected.credential.SessionID != "" && expected.credential.SessionID == current.credential.SessionID
	}
	if expected.RefreshToken != "" || current.RefreshToken != "" {
		// A private local fingerprint survives v1 refresh-token rotation.
		return expected.RefreshToken != "" && legacyAuthenticationFingerprint(expected) == legacyAuthenticationFingerprint(current)
	}
	return expected.AccessToken == current.AccessToken
}

// A plaintext v1 profile has no envelope yet. Its first refresh carries this
// fingerprint into the migrated envelope, without retaining the old secret.
func legacyAuthenticationFingerprint(p ProfileConfig) string {
	if p.credential != nil && p.credential.LegacySessionFingerprint != "" {
		return p.credential.LegacySessionFingerprint
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(p.RefreshToken)))
}

// Read under the same lock as reference switching and old-secret cleanup.
func readStableProfile(ctx context.Context, rt Runtime, name, server string) (ProfileConfig, error) {
	unlock, err := lockProfile(ctx, rt, server, name)
	if err != nil {
		return ProfileConfig{}, err
	}
	defer unlock()
	cfg, err := loadConfig(rt.ConfigPath)
	if err != nil {
		return ProfileConfig{}, err
	}
	p, ok := cfg.Profiles[name]
	if !ok || !sameServerURL(p.ServerURL, server) {
		return ProfileConfig{}, fmt.Errorf("profile_changed: retry after the profile update finishes")
	}
	return hydrateProfile(rt, name, p)
}
