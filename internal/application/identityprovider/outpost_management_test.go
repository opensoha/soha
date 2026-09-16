package identityprovider

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestOutpostManagementHealth(t *testing.T) {
	now := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	expires := now.Add(time.Minute)
	healthy := domainprovider.Outpost{Mode: "external", ClaimedAgentID: "edge", ProtocolVersion: "v1", LastHeartbeatAt: &now, ConfigurationVersion: 2, AppliedConfigurationVersion: 2, ConfigurationExpiresAt: &expires, RuntimeStatus: "available"}
	for _, tt := range []struct {
		name, status, reason string
		age                  time.Duration
		change               func(*domainprovider.Outpost)
	}{
		{name: "fresh", status: "available"},
		{name: "45 seconds", age: 45 * time.Second, status: "available"},
		{name: "stale", age: 45*time.Second + time.Nanosecond, status: "degraded", reason: "heartbeat_stale"},
		{name: "90 seconds", age: 90 * time.Second, status: "degraded", reason: "heartbeat_stale"},
		{name: "timeout", age: 90*time.Second + time.Nanosecond, status: "unavailable", reason: "heartbeat_timeout"},
		{name: "unregistered", status: "unavailable", reason: "awaiting_registration", change: func(o *domainprovider.Outpost) { o.ClaimedAgentID = "" }},
		{name: "claimed only", status: "unavailable", reason: "awaiting_heartbeat", change: func(o *domainprovider.Outpost) { o.LastHeartbeatAt = nil }},
		{name: "incompatible", status: "unavailable", reason: "protocol_incompatible", change: func(o *domainprovider.Outpost) { o.ProtocolVersion = "v2" }},
		{name: "legacy", status: "degraded", reason: "legacy_runtime", change: func(o *domainprovider.Outpost) { o.ProtocolVersion = "" }},
		{name: "legacy timeout", age: 91 * time.Second, status: "unavailable", reason: "heartbeat_timeout", change: func(o *domainprovider.Outpost) { o.ProtocolVersion = "" }},
		{name: "unissued", status: "unavailable", reason: "configuration_not_applied", change: func(o *domainprovider.Outpost) { o.ConfigurationVersion = 0 }},
		{name: "not applied", status: "unavailable", reason: "configuration_not_applied", change: func(o *domainprovider.Outpost) { o.AppliedConfigurationVersion = 0 }},
		{name: "expired at boundary", status: "unavailable", reason: "configuration_expired", change: func(o *domainprovider.Outpost) { o.ConfigurationExpiresAt = &now }},
		{name: "pending", status: "degraded", reason: "configuration_pending", change: func(o *domainprovider.Outpost) { o.ConfigurationVersion++ }},
		{name: "unknown lease", status: "degraded", reason: "configuration_expiry_unknown", change: func(o *domainprovider.Outpost) { o.ConfigurationExpiresAt = nil }},
		{name: "failed apply", status: "unavailable", reason: "signature_invalid", change: func(o *domainprovider.Outpost) { o.RuntimeStatus, o.RuntimeReason = "unavailable", "signature_invalid" }},
		{name: "rejected first configuration", status: "unavailable", reason: "configuration_rejected", change: func(o *domainprovider.Outpost) {
			o.RuntimeStatus, o.RuntimeReason = "unavailable", "configuration_rejected"
			o.AppliedConfigurationVersion = 0
			o.ConfigurationExpiresAt = nil
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := healthy
			lastHeartbeat := now.Add(-tt.age)
			item.LastHeartbeatAt = &lastHeartbeat
			if tt.change != nil {
				tt.change(&item)
			}
			status, reason := outpostRuntimeHealth(item, now, true)
			ssoCheck(t, status == tt.status && reason == tt.reason, "health = %s/%s, want %s/%s", status, reason, tt.status, tt.reason)
		})
	}
	if status, reason := outpostRuntimeHealth(healthy, now, false); status != "unavailable" || reason != "signing_key_unconfigured" {
		t.Fatalf("missing signer: %s/%s", status, reason)
	}
	if status, reason := outpostRuntimeHealth(domainprovider.Outpost{Mode: "embedded"}, now, false); status != "available" || reason != "embedded_runtime" {
		t.Fatalf("embedded: %s/%s", status, reason)
	}
}

func TestOutpostManagementUsesRuntimeObservations(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")
	seed := sha256.Sum256([]byte("management-outpost-test"))
	service.SetOutpostSigningKey("test-key", ed25519.NewKeyFromSeed(seed[:]))
	principal := (&memoryUsers{}).principal()
	input := domainprovider.OutpostInput{Name: "Edge", Mode: "external", Status: "online", Version: "forged", ForwardAuthURL: "https://edge.example.com/api/v1/outpost/forward-auth"}
	created, err := service.CreateOutpost(ctx, principal, input)
	ssoNoError(t, err)
	ssoCheck(t, created.Status == "offline" && created.Version == "" && created.ConfigurationVersion == 0 && created.RuntimeReason == "awaiting_registration", "management input forged observations: %#v", created)
	claim := sohaapi.IdentityOutpostClaimRequest{AgentID: created.ID, SupportedProtocolVersion: "v1", RuntimeVersion: "0.2.0"}
	config, err := service.ClaimIdentityOutpostRuntime(ctx, created.Token, claim)
	ssoNoError(t, err)
	view, err := service.GetOutpost(ctx, principal, created.ID)
	ssoCheck(t, err == nil && view.RuntimeReason == "awaiting_heartbeat" && view.LastHeartbeatAt == nil && view.RuntimeVersion == "0.2.0", "claim view = %#v, %v", view, err)
	request := sohaapi.IdentityOutpostHeartbeatRequest{AgentID: created.ID, ConfigurationVersion: config.ConfigurationVersion, ConfigurationExpiresAt: &config.ExpiresAt, Status: sohaapi.IdentityOutpostHeartbeatRequestStatusHealthy, RuntimeVersion: "0.2.0", CheckedAt: time.Now().Add(24 * time.Hour)}
	if _, err := service.HeartbeatIdentityOutpostRuntime(ctx, created.ID, created.Token, request); err != nil {
		t.Fatal(err)
	}
	view, err = service.UpdateOutpost(ctx, principal, created.ID, input)
	ssoCheck(t, err == nil && view.RuntimeStatus == "available" && view.Version == "0.2.0" && !view.LastHeartbeatAt.After(time.Now()), "update erased observations or trusted client clock: %#v, %v", view, err)
	lastHeartbeat := *view.LastHeartbeatAt
	if _, err := service.ClaimIdentityOutpostRuntime(ctx, created.Token, claim); err != nil {
		t.Fatal(err)
	}
	view, err = service.GetOutpost(ctx, principal, created.ID)
	ssoCheck(t, err == nil && view.LastHeartbeatAt.Equal(lastHeartbeat) && view.ConfigurationVersion == config.ConfigurationVersion, "poll changed heartbeat or desired version: %#v, %v", view, err)
	late, err := repo.GetOutpost(ctx, created.ID)
	ssoNoError(t, err)
	rotated, err := service.RotateOutpostToken(ctx, principal, created.ID)
	ssoCheck(t, err == nil && rotated.RuntimeReason == "awaiting_registration" && rotated.LastHeartbeatAt == nil && rotated.AppliedConfigurationVersion == 0, "rotate retained observations: %#v, %v", rotated, err)
	if _, err := service.HeartbeatIdentityOutpostRuntime(ctx, created.ID, created.Token, request); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("old token heartbeat = %v", err)
	}
	if _, err := repo.RecordOutpostHeartbeat(ctx, late); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("in-flight old heartbeat = %v", err)
	}
	if _, err := service.ClaimIdentityOutpostRuntime(ctx, rotated.Token, claim); err != nil {
		t.Fatal(err)
	}
}

func TestOutpostForwardAuthURLValidation(t *testing.T) {
	for _, value := range []string{"javascript:alert(1)", "/api/outpost", "https://user:secret@edge.example.com/auth", "https://edge.example.com/auth?token=x", "https://edge.example.com/auth#fragment"} {
		if _, err := normalizeOutpostForwardAuthURL(value); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Errorf("accepted invalid URL %q: %v", value, err)
		}
	}
	if got, err := normalizeOutpostForwardAuthURL(" https://edge.example.com/api/v1/outpost/forward-auth "); err != nil || got != "https://edge.example.com/api/v1/outpost/forward-auth" {
		t.Fatalf("valid URL = %q, %v", got, err)
	}
}

// Keep protocol assertions at the caller without adding a test framework.
func ssoCheck(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}

func ssoNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
