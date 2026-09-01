package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

func TestBrowserHandoffCreatesIndependentSingleUseBrowserSession(t *testing.T) {
	service, repo, principal, access, application := newBrowserHandoffTestService(t)

	handoff, err := service.CreateBrowserHandoff(context.Background(), principal, access, application, "https://soha.example")
	if err != nil {
		t.Fatal(err)
	}
	if remaining := time.Until(handoff.ExpiresAt); remaining < 55*time.Second || remaining > 61*time.Second {
		t.Fatalf("handoff TTL = %s", remaining)
	}
	inspected, err := service.GetBrowserHandoff(context.Background(), handoff.ID)
	if err != nil || inspected.Application.ID != application.ID || inspected.AccountName != principal.UserName {
		t.Fatalf("GetBrowserHandoff() = %#v, %v", inspected, err)
	}

	completion, result, err := service.CompleteBrowserHandoff(context.Background(), handoff.ID, "https://soha.example", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if completion.DestinationURL != application.LaunchURL || result.Tokens.AccessToken == "" || result.Tokens.RefreshToken == "" {
		t.Fatalf("completion = %#v, result = %#v", completion, result)
	}
	if len(repo.sessionsByID) != 2 {
		t.Fatalf("sessions = %d, want source and independent browser sessions", len(repo.sessionsByID))
	}
	if _, err := service.GetBrowserHandoff(context.Background(), handoff.ID); !errors.Is(err, apperrors.ErrGone) {
		t.Fatalf("replayed handoff error = %v", err)
	}
}

func TestBrowserHandoffDoesNotReplaceDifferentBrowserAccount(t *testing.T) {
	service, repo, principal, access, application := newBrowserHandoffTestService(t)
	handoff, err := service.CreateBrowserHandoff(context.Background(), principal, access, application, "https://soha.example")
	if err != nil {
		t.Fatal(err)
	}
	repo.usersByID["user-2"] = userrepo.User{
		ID: "user-2", Username: "grace", DisplayName: "Grace", Email: "grace@example.com", Status: "active", AuthzVersion: 1,
	}
	other, err := service.issueAuthResult(context.Background(), domainidentity.Principal{
		UserID: "user-2", UserName: "Grace", Email: "grace@example.com",
	}, "password")
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := service.CompleteBrowserHandoff(context.Background(), handoff.ID, "https://soha.example", "", other.Tokens.RefreshToken); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("account conflict error = %v", err)
	}
	if _, err := service.GetBrowserHandoff(context.Background(), handoff.ID); err != nil {
		t.Fatalf("conflict consumed handoff: %v", err)
	}
}

func TestBrowserHandoffAllowsExplicitConfirmationForSameBrowserAccount(t *testing.T) {
	service, repo, principal, access, application := newBrowserHandoffTestService(t)
	handoff, err := service.CreateBrowserHandoff(context.Background(), principal, access, application, "https://soha.example")
	if err != nil {
		t.Fatal(err)
	}
	existing, err := service.issueAuthResult(context.Background(), principal, "password")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.CompleteBrowserHandoff(context.Background(), handoff.ID, "https://soha.example", "", existing.Tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if len(repo.sessionsByID) != 3 {
		t.Fatalf("sessions = %d, want source, existing browser, and independent handoff sessions", len(repo.sessionsByID))
	}
}

func TestBrowserHandoffRejectsWrongAudienceWithoutConsumingTicket(t *testing.T) {
	service, _, principal, access, application := newBrowserHandoffTestService(t)
	handoff, err := service.CreateBrowserHandoff(context.Background(), principal, access, application, "https://soha.example")
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := service.CompleteBrowserHandoff(context.Background(), handoff.ID, "https://evil.example", "", ""); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("wrong audience error = %v", err)
	}
	if _, err := service.GetBrowserHandoff(context.Background(), handoff.ID); err != nil {
		t.Fatalf("wrong audience consumed handoff: %v", err)
	}
}

func TestBrowserHandoffRejectsExpiredOrRevokedSourceSession(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*loginMappingUserRepo, string)
	}{
		{name: "expired", mutate: func(repo *loginMappingUserRepo, handoffID string) {
			repo.ephemeralMu.Lock()
			item := repo.ephemeral[browserHandoffKind+"|"+handoffID]
			item.ExpiresAt = time.Now().UTC().Add(-time.Second)
			repo.ephemeral[browserHandoffKind+"|"+handoffID] = item
			repo.ephemeralMu.Unlock()
		}},
		{name: "source revoked", mutate: func(repo *loginMappingUserRepo, _ string) {
			session := repo.sessionsByID["desktop-session"]
			session.Status = "revoked"
			repo.sessionsByID["desktop-session"] = session
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, principal, access, application := newBrowserHandoffTestService(t)
			handoff, err := service.CreateBrowserHandoff(context.Background(), principal, access, application, "https://soha.example")
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(repo, handoff.ID)
			if _, _, err := service.CompleteBrowserHandoff(context.Background(), handoff.ID, "https://soha.example", "", ""); !errors.Is(err, apperrors.ErrGone) {
				t.Fatalf("completion error = %v", err)
			}
			if len(repo.sessionsByID) != 1 {
				t.Fatalf("sessions = %d, want source session only", len(repo.sessionsByID))
			}
		})
	}
}

func TestBrowserHandoffConcurrentCompletionCreatesOneSession(t *testing.T) {
	service, repo, principal, access, application := newBrowserHandoffTestService(t)
	handoff, err := service.CreateBrowserHandoff(context.Background(), principal, access, application, "https://soha.example")
	if err != nil {
		t.Fatal(err)
	}

	var successes atomic.Int32
	start := make(chan struct{})
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			if _, _, err := service.CompleteBrowserHandoff(context.Background(), handoff.ID, "https://soha.example", "", ""); err == nil {
				successes.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	if successes.Load() != 1 || len(repo.sessionsByID) != 2 {
		t.Fatalf("successes=%d sessions=%d", successes.Load(), len(repo.sessionsByID))
	}
}

func TestBrowserHandoffRejectsInvalidBoundDestination(t *testing.T) {
	for _, destination := range []string{"", "relative", "//evil.example", "javascript:alert(1)", "https://user@evil.example"} {
		t.Run(destination, func(t *testing.T) {
			service, _, principal, access, application := newBrowserHandoffTestService(t)
			application.LaunchURL = destination
			if _, err := service.CreateBrowserHandoff(context.Background(), principal, access, application, "https://soha.example"); !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("destination %q error = %v", destination, err)
			}
		})
	}
}

type browserHandoffAuditRecorder struct {
	mu      sync.Mutex
	entries []domainaudit.Entry
}

func (r *browserHandoffAuditRecorder) Record(_ context.Context, entry domainaudit.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
	return nil
}

func TestBrowserHandoffAuditUsesStableReasonsWithoutSecrets(t *testing.T) {
	service, _, principal, access, application := newBrowserHandoffTestService(t)
	recorder := &browserHandoffAuditRecorder{}
	service.audit = recorder
	handoff, err := service.CreateBrowserHandoff(context.Background(), principal, access, application, "https://soha.example")
	if err != nil {
		t.Fatal(err)
	}
	handoffContext := requestctx.WithMetadata(context.Background(), requestctx.Metadata{Path: "/api/v1/auth/browser-handoffs/" + handoff.ID + "/complete"})
	_, _, _ = service.CompleteBrowserHandoff(handoffContext, handoff.ID, "https://evil.example", "access-secret", "refresh-secret")
	_, _, _ = service.CompleteBrowserHandoff(handoffContext, handoff.ID, "https://soha.example", "", "")
	service.AuditBrowserHandoffFailure(context.Background(), principal, application.ID, "rate_limited")

	raw, err := json.Marshal(recorder.entries)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	for _, secret := range []string{handoff.ID, application.LaunchURL, access.SessionID, principal.Email, "access-secret", "refresh-secret"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("audit leaked %q: %s", secret, serialized)
		}
	}
	for _, reason := range []string{"created", "origin_mismatch", "completed", "rate_limited"} {
		if !strings.Contains(serialized, reason) {
			t.Fatalf("audit missing reason %q: %s", reason, serialized)
		}
	}
}

func newBrowserHandoffTestService(t *testing.T) (*Service, *loginMappingUserRepo, domainidentity.Principal, domainidentity.AccessContext, domainportal.Application) {
	t.Helper()
	repo := newLoginMappingUserRepo()
	repo.usersByID["user-1"] = userrepo.User{
		ID: "user-1", Username: "ada", DisplayName: "Ada", Email: "ada@example.com", Status: "active", AuthzVersion: 1,
	}
	repo.sessionsByID["desktop-session"] = userrepo.Session{
		ID: "desktop-session", UserID: "user-1", RefreshTokenID: "desktop-refresh", ProviderType: "password",
		Status: "active", ExpiresAt: time.Now().UTC().Add(time.Hour), AuthzVersion: 1,
	}
	repo.refreshToID["desktop-refresh"] = "desktop-session"
	service := newTestServiceWithUserStore(repo)
	service.cfg = cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{
		Secret: "browser-handoff-test-secret", Issuer: "soha-test", AccessTTL: time.Minute, RefreshTTL: time.Hour,
	}}
	principal := domainidentity.Principal{UserID: "user-1", UserName: "Ada", Email: "ada@example.com"}
	access := domainidentity.AccessContext{TokenKind: "session_access", SessionID: "desktop-session", SubjectID: "user-1"}
	application := domainportal.Application{
		ID: "app-1", Slug: "console", Name: "Soha Console", ProviderType: domainportal.ProviderTypeOIDC,
		Status: domainportal.ApplicationStatusEnabled, LaunchURL: "/oauth2/authorize?client_id=console",
	}
	return service, repo, principal, access, application
}
