package identityprovider

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type authorizationRepo struct {
	*memoryRepo
	signHook       func()
	applicationErr error
}

func (r *authorizationRepo) GetProviderApplication(ctx context.Context, id string) (domainportal.Application, error) {
	if r.applicationErr != nil {
		return domainportal.Application{}, r.applicationErr
	}
	return r.memoryRepo.GetProviderApplication(ctx, id)
}
func (r *authorizationRepo) GetActiveSigningKey(ctx context.Context, id string) (domainprovider.SigningKey, error) {
	if r.signHook != nil {
		r.signHook()
	}
	return r.memoryRepo.GetActiveSigningKey(ctx, id)
}

type authorizationUsers struct {
	memoryUsers
	teams      []string
	sessionErr error
	disabled   bool
}

func (u *authorizationUsers) ListTeams(context.Context, string) ([]string, error) {
	return u.teams, nil
}
func (u *authorizationUsers) GetAuthzState(ctx context.Context, id string) (domainidentity.AuthzState, error) {
	state, err := u.memoryUsers.GetAuthzState(ctx, id)
	if u.disabled {
		state.Status = "disabled"
	}
	return state, err
}
func (u *authorizationUsers) GetAuthSessionByID(ctx context.Context, id string) (domainidentity.Session, error) {
	if u.sessionErr != nil {
		return domainidentity.Session{}, u.sessionErr
	}
	return u.memoryUsers.GetAuthSessionByID(ctx, id)
}

func authorizationFixture(t *testing.T) (*Service, *authorizationRepo, *authorizationUsers, domainprovider.AuthorizeResult) {
	t.Helper()
	r := &authorizationRepo{memoryRepo: newMemoryRepo(t)}
	u := &authorizationUsers{teams: []string{"team-1"}}
	r.client.AllowedGrantTypes = []string{"authorization_code", "refresh_token"}
	r.client.AllowedScopes = append(r.client.AllowedScopes, "offline_access")
	r.app.Assignments = []domainportal.ApplicationAssignment{
		{SubjectType: domainportal.AssignmentSubjectUser, SubjectID: "user-1", Effect: domainportal.AssignmentEffectAllow},
		{SubjectType: domainportal.AssignmentSubjectUser, SubjectID: "user-2", Effect: domainportal.AssignmentEffectAllow},
	}
	s := New(r, u, nil, nil, "test-encryption-key-32-bytes-long")
	a, err := s.Authorize(context.Background(), "https://soha.example", u.principal(), domainprovider.AuthorizeInput{
		ResponseType: "code", ClientID: "client-1", RedirectURI: "https://app.example/callback", Scope: "openid offline_access",
		CodeChallenge: pkceChallenge("verifier"), CodeChallengeMethod: "S256", PlatformSessionID: "platform-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, r, u, a
}
func exchangeAuthorization(t *testing.T, s *Service, a domainprovider.AuthorizeResult) domainprovider.TokenResponse {
	t.Helper()
	tokens, err := s.Token(context.Background(), "https://soha.example", authorizationCodeInput(a))
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}
func authorizationCodeInput(a domainprovider.AuthorizeResult) domainprovider.TokenInput {
	return domainprovider.TokenInput{GrantType: "authorization_code", Code: a.Code, RedirectURI: "https://app.example/callback", ClientID: "client-1", ClientSecret: "secret-1", CodeVerifier: "verifier"}
}
func refreshInput(token string) domainprovider.TokenInput {
	return domainprovider.TokenInput{GrantType: "refresh_token", RefreshToken: token, ClientID: "client-1", ClientSecret: "secret-1"}
}

func TestCurrentAuthorizationRevocationMatrix(t *testing.T) {
	tests := []struct {
		name   string
		revoke func(*authorizationRepo, *authorizationUsers)
	}{
		{"application disabled", func(r *authorizationRepo, _ *authorizationUsers) { r.app.Status = "disabled" }},
		{"assignment removed", func(r *authorizationRepo, _ *authorizationUsers) { r.app.Assignments = r.app.Assignments[1:] }},
		{"team removed", func(r *authorizationRepo, u *authorizationUsers) {
			r.app.Assignments = []domainportal.ApplicationAssignment{{SubjectType: domainportal.AssignmentSubjectTeam, SubjectID: "team-1", Effect: domainportal.AssignmentEffectAllow}}
			u.teams = nil
		}},
		{"MFA strengthened", func(r *authorizationRepo, _ *authorizationUsers) {
			r.app.Metadata = map[string]any{"accessPolicy": map[string]any{"requireMfa": true}}
		}},
		{"platform session revoked", func(_ *authorizationRepo, u *authorizationUsers) { u.revokedSessionID = "platform-1" }},
		{"user disabled", func(_ *authorizationRepo, u *authorizationUsers) { u.disabled = true }},
		{"provider disabled", func(r *authorizationRepo, _ *authorizationUsers) { r.provider.Enabled = false }},
		{"client disabled", func(r *authorizationRepo, _ *authorizationUsers) { r.client.Status = "disabled" }},
		{"application query failed", func(r *authorizationRepo, _ *authorizationUsers) {
			r.applicationErr = errors.New("database unavailable")
		}},
		{"session query failed", func(_ *authorizationRepo, u *authorizationUsers) { u.sessionErr = errors.New("database unavailable") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, r, u, a := authorizationFixture(t)
			tokens := exchangeAuthorization(t, s, a)
			// Obtain a second unconsumed code before changing authorization.
			a, err := s.Authorize(context.Background(), "https://soha.example", u.principal(), domainprovider.AuthorizeInput{ResponseType: "code", ClientID: "client-1", RedirectURI: "https://app.example/callback", Scope: "openid", CodeChallenge: pkceChallenge("verifier"), CodeChallengeMethod: "S256", PlatformSessionID: "platform-1"})
			if err != nil {
				t.Fatal(err)
			}
			tt.revoke(r, u)
			if _, err := s.Token(context.Background(), "https://soha.example", authorizationCodeInput(a)); err == nil {
				t.Fatal("code exchange allowed revoked authorization")
			} else if tt.name == "MFA strengthened" && (!errors.Is(err, apperrors.ErrUnauthorized) || !errors.Is(err, apperrors.ErrMFARequired)) {
				t.Fatalf("code MFA error must retain invalid_grant and MFA cause: %v", err)
			}
			if _, err := s.UserInfo(context.Background(), "https://soha.example", tokens.AccessToken); err == nil {
				t.Fatal("userinfo allowed revoked authorization")
			}
			info, err := s.Introspect(context.Background(), "https://soha.example", tokens.AccessToken, domainprovider.ClientAuthInput{ClientID: "client-1", ClientSecret: "secret-1"})
			if info.Active || (err != nil && tt.name != "client disabled") {
				t.Fatalf("introspection active=%v err=%v", info.Active, err)
			}
			refreshed, err := s.Token(context.Background(), "https://soha.example", refreshInput(tokens.RefreshToken))
			if tt.name == "MFA strengthened" && (!errors.Is(err, apperrors.ErrUnauthorized) || !errors.Is(err, apperrors.ErrMFARequired)) {
				t.Fatalf("noninteractive MFA error must retain invalid_grant and MFA cause: %v", err)
			}
			if err == nil || refreshed.AccessToken != "" || refreshed.RefreshToken != "" {
				t.Fatal("refresh delivered tokens after revocation")
			}
		})
	}
}

func TestOIDCBackChannelUsesStoredBrowserSource(t *testing.T) {
	const issuer = "https://soha.example"
	s, r, u, legacyCode := authorizationFixture(t)
	legacyTokens := exchangeAuthorization(t, s, legacyCode)
	r.app.Metadata = map[string]any{"accessPolicy": domainportal.ApplicationPolicyConditions{AllowedCIDRs: []string{"192.0.2.0/24"}}}
	ctxWithSource := func(source string) context.Context {
		return requestctx.WithMetadata(context.Background(), requestctx.Metadata{SourceIP: source})
	}
	browser := ctxWithSource("::ffff:192.0.2.20")
	rp := ctxWithSource("198.51.100.7")
	client := domainprovider.ClientAuthInput{ClientID: "client-1", ClientSecret: "secret-1"}
	authorize := domainprovider.AuthorizeInput{
		ResponseType: "code", ClientID: client.ClientID, RedirectURI: "https://app.example/callback", Scope: "openid offline_access",
		CodeChallenge: pkceChallenge("verifier"), CodeChallengeMethod: "S256", PlatformSessionID: "platform-1",
	}
	if _, err := s.Authorize(rp, issuer, u.principal(), authorize); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("out-of-range browser = %v", err)
	}
	code, err := s.Authorize(browser, issuer, u.principal(), authorize)
	ssoNoError(t, err)
	tokens, err := s.Token(rp, issuer, authorizationCodeInput(code))
	ssoNoError(t, err)
	for _, session := range r.sessions {
		if session.AuthorizationSourceIP != "" && session.AuthorizationSourceIP != "192.0.2.20" {
			t.Fatalf("saved source = %q", session.AuthorizationSourceIP)
		}
	}
	info, err := s.Introspect(rp, issuer, tokens.AccessToken, client)
	if err != nil || !info.Active {
		t.Fatalf("introspect from RP = %+v %v", info, err)
	}
	tokens, err = s.Token(rp, issuer, refreshInput(tokens.RefreshToken))
	ssoNoError(t, err)
	if _, err := s.UserInfo(rp, issuer, tokens.AccessToken); err != nil {
		t.Fatalf("userinfo from RP: %v", err)
	}

	// An allowed address on the token/introspection request cannot establish
	// historical browser proof or override the current CIDR policy.
	info, err = s.Introspect(browser, issuer, legacyTokens.AccessToken, client)
	if err != nil || info.Active {
		t.Fatalf("historical session = %+v %v", info, err)
	}
	r.app.Metadata = map[string]any{"accessPolicy": domainportal.ApplicationPolicyConditions{AllowedCIDRs: []string{"198.51.100.0/24"}}}
	info, err = s.Introspect(rp, issuer, tokens.AccessToken, client)
	if err != nil || info.Active {
		t.Fatalf("tightened policy introspection = %+v %v", info, err)
	}
	if _, err := s.Token(rp, issuer, refreshInput(tokens.RefreshToken)); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("tightened policy refresh = %v", err)
	}
}

func TestCurrentAuthorizationRefreshSigningRace(t *testing.T) {
	for _, grant := range []string{"authorization_code", "refresh_token"} {
		t.Run(grant, func(t *testing.T) {
			for _, revoke := range []string{"application", "session"} {
				t.Run(revoke, func(t *testing.T) {
					s, r, u, a := authorizationFixture(t)
					input := authorizationCodeInput(a)
					if grant == "refresh_token" {
						tokens := exchangeAuthorization(t, s, a)
						input = refreshInput(tokens.RefreshToken)
					}
					signing, resume := make(chan struct{}), make(chan struct{})
					r.signHook = func() { close(signing); <-resume }
					done := make(chan error, 1)
					go func() {
						response, err := s.Token(context.Background(), "https://soha.example", input)
						if response.AccessToken != "" || response.RefreshToken != "" {
							done <- errors.New("revoked refresh returned credentials")
							return
						}
						done <- err
					}()
					select {
					case <-signing:
					case <-time.After(5 * time.Second):
						t.Fatal("refresh never reached signer")
					}
					if revoke == "application" {
						r.app.Status = "disabled"
					} else {
						u.revokedSessionID = "platform-1"
					}
					close(resume)
					if err := <-done; err == nil {
						t.Fatal("refresh signed during revocation was allowed")
					}
					for _, session := range r.sessions {
						if session.RevokedAt == nil {
							t.Fatal("failed refresh left live session")
						}
					}
				})
			}
		})
	}
}

func TestCurrentAuthorizationRefreshSigningFailureRevokesSession(t *testing.T) {
	s, r, _, a := authorizationFixture(t)
	tokens := exchangeAuthorization(t, s, a)
	r.key.EncryptedPrivateKey = "invalid"
	_, err := s.Token(context.Background(), "https://soha.example", refreshInput(tokens.RefreshToken))
	if err == nil {
		t.Fatal("invalid signing key succeeded")
	}
	for _, session := range r.sessions {
		if session.RevokedAt == nil {
			t.Fatal("signing failure left live session")
		}
	}
}

func TestCurrentAuthorizationSessionLookupFailure(t *testing.T) {
	s, _, u, _ := authorizationFixture(t)
	u.sessionErr = errors.New("database unavailable")
	if _, err := s.currentApplicationAccess(context.Background(), "user-1", domainportal.Application{Status: "enabled"}, "platform-1"); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("got %v", err)
	}
}

type authorizationSAMLRuntime struct {
	fakeSAMLProviderRuntime
	signHook func()
}

func (r authorizationSAMLRuntime) SignResponse(material SAMLSigningMaterial, input SAMLResponseInput) ([]byte, error) {
	r.signHook()
	return r.fakeSAMLProviderRuntime.SignResponse(material, input)
}
func TestCurrentAuthorizationSAMLSigningRevocation(t *testing.T) {
	for _, revoke := range []string{"application", "session", "provider"} {
		t.Run(revoke, func(t *testing.T) {
			s, r, u, _ := authorizationFixture(t)
			r.provider.Type = domainprovider.ProviderTypeSAML
			key, err := keyring.NewKey("active", "test-encryption-key-32-bytes-long", time.Now(), nil)
			if err != nil {
				t.Fatal(err)
			}
			s.encryptionKeys, err = keyring.New(key, nil)
			if err != nil {
				t.Fatal(err)
			}
			r.samlKey, err = s.generateSAMLSigningKey(r.provider.ID, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			s.saml = authorizationSAMLRuntime{signHook: func() {
				switch revoke {
				case "application":
					r.app.Status = "disabled"
				case "session":
					u.revokedSessionID = "platform-1"
				case "provider":
					r.provider.Enabled = false
				}
			}}
			result, err := s.SAMLSSO(context.Background(), "https://soha.example", r.provider.ID, "platform-1", u.principal(), SAMLRequestInput{})
			if err == nil || len(result.HTML) != 0 {
				t.Fatal("SAML delivered assertion after signing raced with revocation")
			}
		})
	}
}

func TestCurrentAuthorizationRevocationStillAllowsTokenRevoke(t *testing.T) {
	s, r, _, a := authorizationFixture(t)
	tokens := exchangeAuthorization(t, s, a)
	r.app.Status = "disabled"
	if err := s.Revoke(context.Background(), "https://soha.example", tokens.AccessToken, domainprovider.ClientAuthInput{ClientID: "client-1", ClientSecret: "secret-1"}); err != nil {
		t.Fatal(err)
	}
	r.app.Status = "enabled"
	info, err := s.Introspect(context.Background(), "https://soha.example", tokens.AccessToken, domainprovider.ClientAuthInput{ClientID: "client-1", ClientSecret: "secret-1"})
	if err != nil || info.Active {
		t.Fatalf("revoked access token revived after app re-enabled: active=%v err=%v", info.Active, err)
	}
}
