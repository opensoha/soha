package apphosting_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apphosting "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	hostrepo "github.com/opensoha/soha/internal/repository/apphosting"
)

func TestHostedRuntimeEnvironmentIsMaskedAndBoundToApp(t *testing.T) {
	ctx := t.Context()
	key := []byte(strings.Repeat("a", 32))
	h := newB2TestHarness(t, apphosting.WithRuntimeConfigKey(key), apphosting.WithDynamicBackend(&dynamicDeploymentProbe{}))
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Kind: sohaapi.HostedAppKindDynamic, Name: "Dynamic config"})
	if err != nil {
		t.Fatal(err)
	}
	set := sohaapi.UpdateHostedAppRuntimeConfigRequest{SetEnvironment: []sohaapi.HostedAppEnvironmentVariableInput{
		{Name: "APP_TOKEN", Value: " synthetic-test-secret "},
	}}
	if _, err := h.svc.UpdateRuntimeConfig(ctx, h.admin, app.ID, set); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("admin may not edit author secrets: %v", err)
	}
	result, err := h.svc.UpdateRuntimeConfig(ctx, h.alice, app.ID, set)
	if err != nil || len(result.EnvironmentKeys) != 1 || result.EnvironmentKeys[0] != "APP_TOKEN" {
		t.Fatalf("masked config result: %+v, %v", result, err)
	}
	if body, _ := json.Marshal(result); strings.Contains(string(body), "synthetic-test-secret") {
		t.Fatal("plaintext appeared in the configuration response")
	}
	cfg, err := h.repo.GetRuntimeConfig(ctx, app.ID)
	if err != nil || !secretcrypto.Encrypted(cfg.Environment["APP_TOKEN"]) {
		t.Fatal("configuration did not store ciphertext")
	}
	rev := domain.Revision{AppID: app.ID, RuntimeEnvironment: cfg.Environment}
	data, err := apphosting.OpenRuntimeEnvironment(key, rev)
	if err != nil || string(data["APP_TOKEN"]) != set.SetEnvironment[0].Value {
		t.Fatal("runtime credential has changed or could not be resolved")
	}
	rev.AppID = "some-other-app"
	if _, err := apphosting.OpenRuntimeEnvironment(key, rev); err == nil {
		t.Fatal("another app was able to decrypt the snapshot")
	}
	set.SetEnvironment[0].Name = "DATABASE_URL"
	if _, err := h.svc.UpdateRuntimeConfig(ctx, h.alice, app.ID, set); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("platform DATABASE_URL accepted: %v", err)
	}
}

func TestHostedVisitorAttributesGrantRevocationAndReplay(t *testing.T) {
	h := newB2TestHarness(t)
	ctx := t.Context()
	h.audience.UpsertUser(hostrepo.MemoryUser{
		ID: h.alice.UserID, Username: "alice", DisplayName: "Alice", Email: "alice@example.test", Department: "Engineering", Status: "active",
	})
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Visitor Info", Kind: sohaapi.HostedAppKindStatic})
	if err != nil {
		t.Fatal(err)
	}
	uploadTestDynamic(t, h.testHarness, app.ID, "static", makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<h1>Visitor</h1>"}}))
	check := func() sohaapi.HostedAppVisitorIdentity {
		t.Helper()
		out, err := h.svc.CheckGatewayAccess(ctx, "bootstrap-gw-token", sohaapi.HostedAppGatewayAccessCheckRequest{
			Host: app.Host, UserID: h.alice.UserID, SessionID: "sess_alice",
		})
		if err != nil || out.User == nil || out.Decision != sohaapi.HostedAppAccessDecisionKindAllow {
			t.Fatalf("expected author to be allowed: %+v %v", out, err)
		}
		return *out.User
	}
	original := check()
	if original.Email != "" || original.Department != "" || original.ID == h.alice.UserID {
		t.Fatal("default visitor projection includes restricted fields")
	}
	req, err := h.svc.RequestVisitorAttributes(ctx, h.alice, app.ID, sohaapi.RequestHostedAppVisitorAttributesRequest{
		Fields: []sohaapi.HostedAppVisitorAttribute{"email", "department"}, Reason: "display requested identity",
	})
	if err != nil || req.Pending == nil || check().Email != "" {
		t.Fatalf("pending grant leaked fields: %+v %v", req, err)
	}
	approve := sohaapi.DecideHostedAppVisitorAttributesRequest{RequestID: req.Pending.ID, Fields: []sohaapi.HostedAppVisitorAttribute{"department"}}
	if _, err := h.svc.DecideVisitorAttributes(ctx, h.alice, app.ID, approve); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("non-admin accepted a visitor grant: %v", err)
	}
	if _, err := h.svc.DecideVisitorAttributes(ctx, h.admin, app.ID, approve); err != nil {
		t.Fatal(err)
	}
	approved := check()
	if approved.Department != "Engineering" || approved.Email != "" || approved.ID != original.ID {
		t.Fatalf("unexpected approved visitor projection: %+v", approved)
	}
	if _, err := h.svc.DecideVisitorAttributes(ctx, h.admin, app.ID, sohaapi.DecideHostedAppVisitorAttributesRequest{Fields: []sohaapi.HostedAppVisitorAttribute{}}); err != nil {
		t.Fatal(err)
	}
	if check().Department != "" {
		t.Fatal("old SSO session retained the revoked attribute")
	}
	if _, err := h.svc.DecideVisitorAttributes(ctx, h.admin, app.ID, approve); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale approval was accepted: %v", err)
	}
}

type diagnosticsProbe struct {
	dynamicDeploymentProbe
}

func (*diagnosticsProbe) SampleDataUsage(context.Context, string) (int64, error) {
	return 201 << 20, nil
}

func (*diagnosticsProbe) ObserveRuntimeSignals(context.Context, domain.Revision) ([]domain.RuntimeSignal, error) {
	return []domain.RuntimeSignal{{Code: "synthetic-console-secret", At: time.Now()}, {Code: "runtime_oom", At: time.Now()}}, nil
}

func TestHostedRuntimeUsageWarningAndRedactedLogs(t *testing.T) {
	ctx := t.Context()
	h := newTestHarness(t, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(&diagnosticsProbe{}))
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Dynamic diagnostics", Kind: sohaapi.HostedAppKindDynamic})
	if err != nil {
		t.Fatal(err)
	}
	source := makeTarGz(t, []tarEntrySpec{
		{name: "soha-host.json", body: `{"template":"python","database":true}`},
		{name: "app.py", body: "print('hello')"}, {name: "requirements.txt", body: ""},
	})
	receipt := uploadTestDynamic(t, h, app.ID, "diagnostics", source)
	reconcileDynamicSteps(t, h, 3)
	if _, err := h.svc.GetRuntimeUsage(ctx, h.carol, app.ID); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("non-maintainer sampled usage: %v", err)
	}
	usage, err := h.svc.GetRuntimeUsage(ctx, h.alice, app.ID)
	if err != nil || !usage.Warning || usage.WarningBytes != 200<<20 || usage.SampledAt == nil {
		t.Fatalf("unexpected warning status: %+v %v", usage, err)
	}
	if _, err := h.svc.UpdateRuntimeConfig(ctx, h.admin, app.ID, sohaapi.UpdateHostedAppRuntimeConfigRequest{DataWarningBytes: new(int64(202 << 20))}); err != nil {
		t.Fatal(err)
	}
	usage, err = h.svc.GetRuntimeUsage(ctx, h.alice, app.ID)
	if err != nil || usage.Warning {
		t.Fatalf("raised threshold did not clear the warning: %+v %v", usage, err)
	}
	if _, err := h.svc.GetRevisionLogs(ctx, h.carol, app.ID, receipt.Revision.ID); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("non-maintainer read revision logs: %v", err)
	}
	logs, err := h.svc.GetRevisionLogs(ctx, h.alice, app.ID, receipt.Revision.ID)
	body, _ := json.Marshal(logs)
	if err != nil || !strings.Contains(string(body), "runtime_oom") || strings.Contains(string(body), "synthetic-console-secret") {
		t.Fatalf("incorrect redacted event response: %v", err)
	}
}
