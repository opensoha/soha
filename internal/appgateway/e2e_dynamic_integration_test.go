package appgateway_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/domain/apphosting"
)

func TestB3DynamicOldBrowserLinkUsesOnlineAudience(t *testing.T) {
	backend := e2eDynamicRuntime{}
	var requests atomic.Int32
	relay := e2eDynamicRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if !strings.HasPrefix(r.Host, "soha-app-") || !strings.HasSuffix(r.Host, ".task135-runtime.svc.cluster.local") {
			t.Errorf("wrong revision routing authority: %q", r.Host)
		}
		if strings.TrimSpace(r.Header.Get("X-Soha-User-ID")) == "" {
			t.Error("gateway did not send an app-scoped visitor identity")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("hosted-form-response")),
			Request:    r,
		}, nil
	})
	env := setupE2ERuntimeEnv(t, backend, relay)
	ctx := t.Context()
	app, err := env.aliceClient.CreateHostedApp(ctx, sohaapi.CreateHostedAppRequest{
		Name: "Dynamic form", Kind: sohaapi.HostedAppKindDynamic, IdempotencyKey: "b3-dynamic-create",
		Visibility: &sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeSelected, TeamIDs: []string{"team-data"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	source := buildTarGzBundle(t, map[string]string{
		"soha-host.json": `{"template":"python","database":false}`,
		"app.py":         "print('ready')", "requirements.txt": "",
	})
	receipt, err := env.aliceClient.UploadHostedAppRevision(ctx, app.ID, bytes.NewReader(source), sohaapi.UploadHostedAppRevisionParams{IdempotencyKey: "b3-dynamic-source"})
	if err != nil || receipt.Revision.BuildPhase != "queued" {
		t.Fatalf("queued source: %v %v", receipt.Revision.BuildPhase, err)
	}
	for range 3 {
		if err := env.svc.ReconcileDynamicRevision(ctx); err != nil {
			t.Fatal(err)
		}
	}
	link := "http://" + app.DNSLabel + ".apps.local:" + env.gwPort + "/form"
	bob := newLoggedInGatewayBrowser(t, env, "tok-bob", env.gwURL)
	page := followAllRedirects(t, bob, link)
	if page.status != http.StatusOK || page.body != "hosted-form-response" {
		t.Fatalf("authorized colleague could not access the dynamic app: status=%d", page.status)
	}
	if _, err := env.aliceClient.UpdateHostedAppVisibility(ctx, app.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate},
	}); err != nil {
		t.Fatal(err)
	}
	assertDynamicHTTP(t, bob, link, http.StatusForbidden, false)
	bobReplica2 := env.newBrowserClientForGateway(t, bob.Jar, env.gwURL2)
	assertDynamicHTTP(t, bobReplica2, link, http.StatusForbidden, false)
	if requests.Load() != 1 {
		t.Fatalf("old session reached dynamic origin after revocation: requests=%d", requests.Load())
	}
	alice := newLoggedInGatewayBrowser(t, env, "tok-alice", env.gwURL)
	page = followAllRedirects(t, alice, link)
	if page.status != http.StatusOK || page.body != "hosted-form-response" {
		t.Fatalf("author lost access to the private dynamic app: status=%d", page.status)
	}
}

func assertDynamicHTTP(t *testing.T, browser *http.Client, link string, status int, wantBody bool) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, link, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != status || strings.Contains(string(body), "hosted-form-response") != wantBody {
		t.Fatalf("dynamic GET: status=%d err=%v body=%q", resp.StatusCode, err, body)
	}
}

type e2eDynamicRoundTrip func(*http.Request) (*http.Response, error)

func (roundTrip e2eDynamicRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	return roundTrip(r)
}

type e2eDynamicRuntime struct{}

func (e2eDynamicRuntime) StartBuild(context.Context, apphosting.Revision) error { return nil }
func (e2eDynamicRuntime) BuildResult(context.Context, apphosting.Revision) (string, bool, error) {
	return "registry.example/hosted@sha256:" + strings.Repeat("a", 64), true, nil
}
func (e2eDynamicRuntime) PrepareRuntime(context.Context, apphosting.Revision) error { return nil }
func (e2eDynamicRuntime) RuntimeReady(context.Context, apphosting.Revision) (bool, error) {
	return true, nil
}
func (e2eDynamicRuntime) ActivateRuntime(context.Context, apphosting.Revision) error { return nil }
func (e2eDynamicRuntime) RestrictRuntime(context.Context, apphosting.Revision) error { return nil }
func (e2eDynamicRuntime) CheckRollback(context.Context, apphosting.Revision) error   { return nil }
func (e2eDynamicRuntime) DeleteRuntime(context.Context, apphosting.Revision) error   { return nil }
func (e2eDynamicRuntime) DeleteBuild(context.Context, apphosting.Revision) error     { return nil }
