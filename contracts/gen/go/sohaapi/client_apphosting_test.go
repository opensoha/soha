package sohaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHostedAppRuntimeConfigPreservesEmptyConsent(t *testing.T) {
	for _, test := range []struct {
		name string
		ids  []string
		want string
	}{
		{name: "omit", want: `{}`},
		{name: "clear", ids: []string{}, want: `{"sensitivePublisherIds":[]}`},
		{name: "grant", ids: []string{"maintainer"}, want: `{"sensitivePublisherIds":["maintainer"]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(UpdateHostedAppRuntimeConfigRequest{SensitivePublisherIDs: test.ids})
			if err != nil || string(body) != test.want {
				t.Fatalf("marshal = %s, %v, want %s", body, err, test.want)
			}
		})
	}
}

func TestAppHostingEnumsValid(t *testing.T) {
	for _, kind := range []HostedAppKind{HostedAppKindStatic, HostedAppKindDynamic} {
		if !kind.Valid() {
			t.Fatalf("HostedAppKind %q should be valid", kind)
		}
	}
	for _, status := range []HostedAppStatus{
		HostedAppStatusDraft,
		HostedAppStatusOnline,
		HostedAppStatusOffline,
		HostedAppStatusSuspendedByAdmin,
	} {
		if !status.Valid() {
			t.Fatalf("HostedAppStatus %q should be valid", status)
		}
	}
	for _, mode := range []HostedAppVisibilityMode{
		HostedAppVisibilityModePrivate,
		HostedAppVisibilityModeSelected,
		HostedAppVisibilityModeOrganization,
	} {
		if !mode.Valid() {
			t.Fatalf("HostedAppVisibilityMode %q should be valid", mode)
		}
	}
	for _, revStatus := range []HostedAppRevisionStatus{
		HostedAppRevisionStatusUploading,
		HostedAppRevisionStatusValidating,
		HostedAppRevisionStatusReady,
		HostedAppRevisionStatusFailed,
		HostedAppRevisionStatusEvicted,
	} {
		if !revStatus.Valid() {
			t.Fatalf("HostedAppRevisionStatus %q should be valid", revStatus)
		}
	}
	for _, decision := range []HostedAppAccessDecisionKind{
		HostedAppAccessDecisionKindAllow,
		HostedAppAccessDecisionKindUnauthenticated,
		HostedAppAccessDecisionKindForbidden,
		HostedAppAccessDecisionKindOffline,
		HostedAppAccessDecisionKindNotFound,
	} {
		if !decision.Valid() {
			t.Fatalf("HostedAppAccessDecisionKind %q should be valid", decision)
		}
	}
	for _, policy := range []AppHostingOrganizationVisibilityPolicy{
		AppHostingOrganizationVisibilityPolicyAllowDirect,
		AppHostingOrganizationVisibilityPolicyRequireApproval,
		AppHostingOrganizationVisibilityPolicyDisabled,
	} {
		if !policy.Valid() {
			t.Fatalf("AppHostingOrganizationVisibilityPolicy %q should be valid", policy)
		}
	}
	for _, approvalStatus := range []HostedAppVisibilityApprovalStatus{
		HostedAppVisibilityApprovalStatusPending,
		HostedAppVisibilityApprovalStatusApproved,
		HostedAppVisibilityApprovalStatusRejected,
		HostedAppVisibilityApprovalStatusWithdrawn,
		HostedAppVisibilityApprovalStatusInvalidated,
	} {
		if !approvalStatus.Valid() {
			t.Fatalf("HostedAppVisibilityApprovalStatus %q should be valid", approvalStatus)
		}
	}
	for _, approvalScope := range []HostedAppApprovalListScope{
		HostedAppApprovalListScopeMine,
		HostedAppApprovalListScopeAll,
	} {
		if !approvalScope.Valid() {
			t.Fatalf("HostedAppApprovalListScope %q should be valid", approvalScope)
		}
	}
	for _, sourceKind := range []WorkspaceHostedAppSourceKind{
		WorkspaceHostedAppSourceKindPersonalEnvironment,
		WorkspaceHostedAppSourceKindConversationArtifact,
	} {
		if !sourceKind.Valid() {
			t.Fatalf("WorkspaceHostedAppSourceKind %q should be valid", sourceKind)
		}
	}
}

func TestAppHostingSettingsAndAudienceClientMethods(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	t.Run("get settings", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/app-hosting/settings")
			writeJSON(t, w, AppHostingSettingsResponse{
				Data: AppHostingSettings{
					Enabled:                     true,
					AppsDomain:                  "apps.soha.internal",
					PublicScheme:                "https",
					StorageIntegrationID:        "int_minio_1",
					StoragePrefix:               "hosted-apps",
					GatewayTokenConfigured:      true,
					AllowOrganizationVisibility: true,
					MaxBundleCompressedBytes:    52428800,
					MaxBundleUncompressedBytes:  209715200,
					MaxBundleFileCount:          2000,
					MaxAppsPerOwner:             20,
					RetainedRevisionsPerApp:     10,
					UpdatedBy:                   "usr_admin",
					UpdatedAt:                   &now,
				},
			})
		})
		got, err := client.GetAppHostingSettings(context.Background())
		if err != nil {
			t.Fatalf("GetAppHostingSettings: %v", err)
		}
		if !got.Enabled || got.AppsDomain != "apps.soha.internal" || !got.GatewayTokenConfigured {
			t.Fatalf("settings = %#v", got)
		}
	})

	t.Run("update settings", func(t *testing.T) {
		allowOrg := false
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assertCommonRequest(t, r, http.MethodPut, "/api/v1/app-hosting/settings")
			req := decodeJSONRequestBody[AppHostingSettingsUpdateRequest](t, r)
			if !req.Enabled || req.AllowOrganizationVisibility == nil || *req.AllowOrganizationVisibility {
				t.Fatalf("request = %#v", req)
			}
			writeJSON(t, w, AppHostingSettingsResponse{
				Data: AppHostingSettings{
					Enabled:                     req.Enabled,
					AppsDomain:                  req.AppsDomain,
					PublicScheme:                "https",
					StorageIntegrationID:        req.StorageIntegrationID,
					StoragePrefix:               "hosted-apps",
					GatewayTokenConfigured:      false,
					AllowOrganizationVisibility: false,
					MaxBundleCompressedBytes:    52428800,
					MaxBundleUncompressedBytes:  209715200,
					MaxBundleFileCount:          2000,
					MaxAppsPerOwner:             20,
					RetainedRevisionsPerApp:     10,
				},
			})
		})
		got, err := client.UpdateAppHostingSettings(context.Background(), AppHostingSettingsUpdateRequest{
			Enabled:                     true,
			AppsDomain:                  "apps.soha.internal",
			StorageIntegrationID:        "int_minio_1",
			AllowOrganizationVisibility: &allowOrg,
		})
		if err != nil {
			t.Fatalf("UpdateAppHostingSettings: %v", err)
		}
		if got.AllowOrganizationVisibility {
			t.Fatalf("AllowOrganizationVisibility = true, want false")
		}
	})

	t.Run("rotate gateway token", func(t *testing.T) {
		wantToken := "fixture-for-" + t.Name()
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/app-hosting/settings/gateway-token/rotate")
			req := decodeJSONRequestBody[AppHostingGatewayTokenRotateRequest](t, r)
			if req.Reason != "initial bootstrap" {
				t.Fatalf("reason = %q", req.Reason)
			}
			writeJSON(t, w, AppHostingGatewayTokenRotateResponse{
				Data: AppHostingGatewayTokenSecret{
					Token:     wantToken,
					RotatedAt: now,
				},
			})
		})
		secret, err := client.RotateAppHostingGatewayToken(context.Background(), &AppHostingGatewayTokenRotateRequest{
			Reason: "initial bootstrap",
		})
		if err != nil {
			t.Fatalf("RotateAppHostingGatewayToken: %v", err)
		}
		if secret.Token != wantToken || !secret.RotatedAt.Equal(now) {
			t.Fatalf("secret = %#v", secret)
		}
	})

	t.Run("list audience candidates", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/app-hosting/audience-candidates")
			if r.URL.Query().Get("query") != "fin" || r.URL.Query().Get("limit") != "15" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			writeJSON(t, w, AppHostingAudienceCandidatesResponse{
				Data: AppHostingAudienceCandidates{
					Users: []AppHostingAudienceUser{{ID: "usr_1", Username: "carol", DisplayName: "Carol", Department: "Finance"}},
					Teams: []AppHostingAudienceTeam{{ID: "team_1", Name: "Finance", Slug: "finance"}},
				},
			})
		})
		got, err := client.ListAppHostingAudienceCandidates(context.Background(), ListAppHostingAudienceCandidatesParams{
			Query: "fin",
			Limit: 15,
		})
		if err != nil {
			t.Fatalf("ListAppHostingAudienceCandidates: %v", err)
		}
		if len(got.Users) != 1 || len(got.Teams) != 1 || got.Teams[0].Slug != "finance" {
			t.Fatalf("candidates = %#v", got)
		}
	})
}

func TestHostedAppCRUDVisibilityAndLifecycleClientMethods(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 30, 0, 0, time.UTC)
	sampleApp := HostedApp{
		ID:                "app_123",
		Name:              "Ops Report",
		Kind:              HostedAppKindStatic,
		DNSLabel:          "ops-report-a1b2c3",
		Host:              "ops-report-a1b2c3.apps.soha.internal",
		URL:               "https://ops-report-a1b2c3.apps.soha.internal",
		SpaFallback:       true,
		OwnerUserID:       "usr_alice",
		MaintainerUserIDs: []string{"usr_alice"},
		Visibility:        HostedAppVisibilityScope{Mode: HostedAppVisibilityModePrivate},
		Status:            HostedAppStatusOnline,
		ActiveRevisionID:  "rev_1",
		RouteGeneration:   2,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	t.Run("list hosted apps", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/app-hosting/apps")
			q := r.URL.Query()
			if q.Get("scope") != "mine" || q.Get("status") != "online" || q.Get("query") != "ops" || q.Get("limit") != "10" || q.Get("offset") != "20" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			writeJSON(t, w, HostedAppListResponse{Data: HostedAppList{Items: []HostedApp{sampleApp}, Total: 1}})
		})
		got, err := client.ListHostedApps(context.Background(), ListHostedAppsParams{
			Scope:  HostedAppListScopeMine,
			Query:  "ops",
			Status: HostedAppStatusOnline,
			Limit:  10,
			Offset: 20,
		})
		if err != nil || got.Total != 1 || got.Items[0].ID != "app_123" {
			t.Fatalf("ListHostedApps = %#v, %v", got, err)
		}
	})

	t.Run("create get patch visibility and lifecycle", func(t *testing.T) {
		spaFalse := false
		newName := "Ops Report v2"
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method + " " + r.URL.Path {
			case "POST /api/v1/app-hosting/apps":
				req := decodeJSONRequestBody[CreateHostedAppRequest](t, r)
				if req.Name != "Ops Report" || req.IdempotencyKey != "idem-1" {
					t.Fatalf("create req = %#v", req)
				}
				w.WriteHeader(http.StatusCreated)
				writeJSON(t, w, HostedAppResponse{Data: sampleApp})
			case "GET /api/v1/app-hosting/apps/app_123":
				writeJSON(t, w, HostedAppResponse{Data: sampleApp})
			case "PATCH /api/v1/app-hosting/apps/app_123":
				var raw map[string]any
				if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
					t.Fatal(err)
				}
				if raw["spaFallback"] != false || raw["name"] != newName {
					t.Fatalf("patch raw = %#v", raw)
				}
				updated := sampleApp
				updated.Name = newName
				updated.SpaFallback = false
				writeJSON(t, w, HostedAppResponse{Data: updated})
			case "PUT /api/v1/app-hosting/apps/app_123/visibility":
				req := decodeJSONRequestBody[UpdateHostedAppVisibilityRequest](t, r)
				if req.Visibility.Mode != HostedAppVisibilityModeSelected || len(req.Visibility.TeamIDs) != 1 {
					t.Fatalf("visibility req = %#v", req)
				}
				updated := sampleApp
				updated.Visibility = HostedAppVisibilityScope{Mode: req.Visibility.Mode, TeamIDs: req.Visibility.TeamIDs}
				updated.RouteGeneration = 3
				writeJSON(t, w, HostedAppResponse{Data: updated})
			case "POST /api/v1/app-hosting/apps/app_123/offline":
				updated := sampleApp
				updated.Status = HostedAppStatusOffline
				writeJSON(t, w, HostedAppResponse{Data: updated})
			case "POST /api/v1/app-hosting/apps/app_123/online":
				writeJSON(t, w, HostedAppResponse{Data: sampleApp})
			case "POST /api/v1/app-hosting/apps/app_123/suspend":
				req := decodeJSONRequestBody[HostedAppSuspendRequest](t, r)
				if req.Reason != "policy violation" {
					t.Fatalf("suspend req = %#v", req)
				}
				updated := sampleApp
				updated.Status = HostedAppStatusSuspendedByAdmin
				updated.SuspendedReason = req.Reason
				writeJSON(t, w, HostedAppResponse{Data: updated})
			case "POST /api/v1/app-hosting/apps/app_123/unsuspend":
				updated := sampleApp
				updated.Status = HostedAppStatusOffline
				writeJSON(t, w, HostedAppResponse{Data: updated})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		})

		if _, err := client.CreateHostedApp(context.Background(), CreateHostedAppRequest{Name: "Ops Report", IdempotencyKey: "idem-1"}); err != nil {
			t.Fatalf("CreateHostedApp: %v", err)
		}
		if _, err := client.GetHostedApp(context.Background(), "app_123"); err != nil {
			t.Fatalf("GetHostedApp: %v", err)
		}
		patched, err := client.UpdateHostedApp(context.Background(), "app_123", UpdateHostedAppRequest{Name: &newName, SpaFallback: &spaFalse})
		if err != nil || patched.SpaFallback || patched.Name != newName {
			t.Fatalf("UpdateHostedApp = %#v, %v", patched, err)
		}
		vis, err := client.UpdateHostedAppVisibility(context.Background(), "app_123", UpdateHostedAppVisibilityRequest{
			Visibility: HostedAppVisibilityInput{Mode: HostedAppVisibilityModeSelected, TeamIDs: []string{"team_ops"}},
		})
		if err != nil || vis.RouteGeneration != 3 {
			t.Fatalf("UpdateHostedAppVisibility = %#v, %v", vis, err)
		}
		if off, err := client.TakeHostedAppOffline(context.Background(), "app_123", nil); err != nil || off.Status != HostedAppStatusOffline {
			t.Fatalf("TakeHostedAppOffline = %#v, %v", off, err)
		}
		if on, err := client.BringHostedAppOnline(context.Background(), "app_123", &HostedAppLifecycleActionRequest{Reason: "verified"}); err != nil || on.Status != HostedAppStatusOnline {
			t.Fatalf("BringHostedAppOnline = %#v, %v", on, err)
		}
		if susp, err := client.SuspendHostedApp(context.Background(), "app_123", HostedAppSuspendRequest{Reason: "policy violation"}); err != nil || susp.Status != HostedAppStatusSuspendedByAdmin {
			t.Fatalf("SuspendHostedApp = %#v, %v", susp, err)
		}
		if unsusp, err := client.UnsuspendHostedApp(context.Background(), "app_123", nil); err != nil || unsusp.Status != HostedAppStatusOffline {
			t.Fatalf("UnsuspendHostedApp = %#v, %v", unsusp, err)
		}
		if _, err := client.GetHostedApp(context.Background(), "   "); err == nil {
			t.Fatal("expected empty appID error")
		}
	})
}

func TestHostedAppPreflightRevisionUploadAndRollbackClientMethods(t *testing.T) {
	now := time.Date(2026, 11, 20, 13, 0, 0, 0, time.UTC)
	bundleBytes := []byte{0x1f, 0x8b, 0x08, 0x00, 0x01, 0x02, 0x03, 0x04}
	spaFalse := false

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/app-hosting/apps/app_123/deployments/preflight":
			req := decodeJSONRequestBody[HostedAppPreflightRequest](t, r)
			if !req.HasIndexHTML || req.FileCount != 4 {
				t.Fatalf("preflight req = %#v", req)
			}
			writeJSON(t, w, HostedAppPreflightResponse{
				Data: HostedAppPreflightResult{
					Allowed:              true,
					AppID:                "app_123",
					URL:                  "https://ops-report-a1b2c3.apps.soha.internal",
					Blockers:             []string{},
					Warnings:             []string{},
					MaxCompressedBytes:   52428800,
					MaxUncompressedBytes: 209715200,
					MaxFileCount:         2000,
				},
			})
		case "POST /api/v1/app-hosting/apps/app_123/revisions":
			if got := r.Header.Get("Content-Type"); got != "application/gzip" {
				t.Fatalf("Content-Type = %q, want application/gzip", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("Authorization = %q", got)
			}
			q := r.URL.Query()
			if q.Get("idempotencyKey") != "deploy-1" || q.Get("spaFallback") != "false" || q.Get("description") != "v1 static bundle" {
				t.Fatalf("upload query = %s", r.URL.RawQuery)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(body, bundleBytes) {
				t.Fatalf("uploaded bytes = %x, err = %v", body, err)
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, HostedAppDeploymentResponse{
				Data: HostedAppDeploymentReceipt{
					App: HostedApp{
						ID:                "app_123",
						Name:              "Ops Report",
						Kind:              HostedAppKindStatic,
						DNSLabel:          "ops-report-a1b2c3",
						Host:              "ops-report-a1b2c3.apps.soha.internal",
						URL:               "https://ops-report-a1b2c3.apps.soha.internal",
						OwnerUserID:       "usr_alice",
						MaintainerUserIDs: []string{"usr_alice"},
						Visibility:        HostedAppVisibilityScope{Mode: HostedAppVisibilityModePrivate},
						Status:            HostedAppStatusOnline,
						ActiveRevisionID:  "rev_2",
						RouteGeneration:   4,
						CreatedAt:         now,
						UpdatedAt:         now,
					},
					Revision: HostedAppRevision{
						ID:             "rev_2",
						AppID:          "app_123",
						RevisionNumber: 2,
						Kind:           HostedAppKindStatic,
						Status:         HostedAppRevisionStatusReady,
						BundleSha256:   strings.Repeat("a", 64),
						FileCount:      4,
						TotalBytes:     4096,
						SpaFallback:    false,
						CreatedBy:      "usr_alice",
						CreatedAt:      now,
						ActivatedAt:    &now,
					},
					ReusedIdempotencyKey: false,
				},
			})
		case "GET /api/v1/app-hosting/apps/app_123/revisions":
			if r.URL.Query().Get("limit") != "5" {
				t.Fatalf("revisions query = %s", r.URL.RawQuery)
			}
			writeJSON(t, w, HostedAppRevisionListResponse{
				Data: HostedAppRevisionList{
					Items: []HostedAppRevision{{ID: "rev_2", AppID: "app_123", RevisionNumber: 2, Kind: HostedAppKindStatic, Status: HostedAppRevisionStatusReady, CreatedBy: "usr_alice", CreatedAt: now}},
					Total: 1,
				},
			})
		case "GET /api/v1/app-hosting/apps/app_123/revisions/rev_2":
			writeJSON(t, w, HostedAppRevisionResponse{
				Data: HostedAppRevision{ID: "rev_2", AppID: "app_123", RevisionNumber: 2, Kind: HostedAppKindStatic, Status: HostedAppRevisionStatusReady, CreatedBy: "usr_alice", CreatedAt: now},
			})
		case "POST /api/v1/app-hosting/apps/app_123/rollback":
			req := decodeJSONRequestBody[HostedAppRollbackRequest](t, r)
			if req.TargetRevisionID != "rev_1" {
				t.Fatalf("rollback req = %#v", req)
			}
			writeJSON(t, w, HostedAppDeploymentResponse{
				Data: HostedAppDeploymentReceipt{
					Revision: HostedAppRevision{ID: "rev_1", AppID: "app_123", RevisionNumber: 1, Kind: HostedAppKindStatic, Status: HostedAppRevisionStatusReady, CreatedBy: "usr_alice", CreatedAt: now},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	preflight, err := client.PreflightHostedAppDeployment(context.Background(), "app_123", HostedAppPreflightRequest{
		Kind:              HostedAppKindStatic,
		UncompressedBytes: 4096,
		FileCount:         4,
		HasIndexHTML:      true,
	})
	if err != nil || !preflight.Allowed {
		t.Fatalf("PreflightHostedAppDeployment = %#v, %v", preflight, err)
	}

	if _, err := client.UploadHostedAppRevision(context.Background(), "app_123", nil, UploadHostedAppRevisionParams{}); err == nil {
		t.Fatal("expected nil bundle reader error")
	}

	receipt, err := client.UploadHostedAppRevision(context.Background(), "app_123", bytes.NewReader(bundleBytes), UploadHostedAppRevisionParams{
		IdempotencyKey: "deploy-1",
		SpaFallback:    &spaFalse,
		Description:    "v1 static bundle",
	})
	if err != nil || receipt.Revision.ID != "rev_2" || receipt.App.ActiveRevisionID != "rev_2" {
		t.Fatalf("UploadHostedAppRevision = %#v, %v", receipt, err)
	}

	revList, err := client.ListHostedAppRevisions(context.Background(), "app_123", ListHostedAppRevisionsParams{Limit: 5})
	if err != nil || revList.Total != 1 {
		t.Fatalf("ListHostedAppRevisions = %#v, %v", revList, err)
	}

	rev, err := client.GetHostedAppRevision(context.Background(), "app_123", "rev_2")
	if err != nil || rev.ID != "rev_2" {
		t.Fatalf("GetHostedAppRevision = %#v, %v", rev, err)
	}
	if _, err := client.GetHostedAppRevision(context.Background(), "app_123", " "); err == nil {
		t.Fatal("expected empty revisionID error")
	}

	rolledBack, err := client.RollbackHostedApp(context.Background(), "app_123", HostedAppRollbackRequest{
		TargetRevisionID: "rev_1",
		Reason:           "revert regression",
	})
	if err != nil || rolledBack.Revision.ID != "rev_1" {
		t.Fatalf("RollbackHostedApp = %#v, %v", rolledBack, err)
	}
}

func TestUploadHostedAppRevisionResponseErrorAndOversize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-upload-oversize")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, strings.Repeat("x", maxJSONResponseBytes+1))
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("tok"))
	_, err := client.UploadHostedAppRevision(context.Background(), "app_123", strings.NewReader("gz"), UploadHostedAppRevisionParams{})
	var oversized *ResponseTooLargeError
	if !errors.As(err, &oversized) || oversized.RequestID != "req-upload-oversize" {
		t.Fatalf("expected ResponseTooLargeError, got %v", err)
	}
}

func TestAppHostingGatewayClientMethodsSupportClientAndOverrideBearerTokens(t *testing.T) {
	now := time.Date(2026, 11, 20, 14, 0, 0, 0, time.UTC)
	var seenAuthHeaders []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authValues := r.Header.Values("Authorization")
		if len(authValues) != 1 {
			t.Fatalf("expected exactly 1 Authorization header, got %v", authValues)
		}
		seenAuthHeaders = append(seenAuthHeaders, authValues[0])

		switch r.URL.Path {
		case "/api/v1/app-hosting/gateway/tickets/exchange":
			req := decodeJSONRequestBody[HostedAppTicketExchangeRequest](t, r)
			writeJSON(t, w, HostedAppTicketExchangeResponse{
				Data: HostedAppTicketExchangeResult{
					SessionID:       "sess_1",
					PrincipalUserID: "usr_carol",
					AppID:           "app_123",
					DNSLabel:        "ops-report-a1b2c3",
					Host:            req.Host,
					RouteGeneration: 2,
					ReturnPath:      "/reports/q4",
					ExpiresAt:       now.Add(8 * time.Hour),
					User:            HostedAppVisitorIdentity{ID: "ahv_carol_app123", DisplayName: "Carol"},
				},
			})
		case "/api/v1/app-hosting/gateway/access-check":
			req := decodeJSONRequestBody[HostedAppGatewayAccessCheckRequest](t, r)
			writeJSON(t, w, HostedAppGatewayAccessCheckResponse{
				Data: HostedAppGatewayAccessDecision{
					Decision:        HostedAppAccessDecisionKindAllow,
					AppID:           "app_123",
					Host:            req.Host,
					RouteGeneration: 2,
					SpaFallback:     true,
				},
			})
		default:
			t.Fatalf("unexpected gateway path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, WithBearerToken("default-gateway-token"))

	exchanged, err := client.ExchangeHostedAppGatewayTicket(context.Background(), HostedAppTicketExchangeRequest{
		Ticket:       "tkt_once",
		Host:         "ops-report-a1b2c3.apps.soha.internal",
		GatewayState: "state_nonce_1",
	})
	if err != nil || exchanged.SessionID != "sess_1" || exchanged.PrincipalUserID != "usr_carol" || exchanged.User.DisplayName != "Carol" {
		t.Fatalf("ExchangeHostedAppGatewayTicket = %#v, %v", exchanged, err)
	}

	decision, err := client.CheckHostedAppGatewayAccess(context.Background(), HostedAppGatewayAccessCheckRequest{
		Host:      "ops-report-a1b2c3.apps.soha.internal",
		UserID:    "usr_carol",
		SessionID: "sess_1",
	}, "override-gateway-token")
	if err != nil || decision.Decision != HostedAppAccessDecisionKindAllow {
		t.Fatalf("CheckHostedAppGatewayAccess = %#v, %v", decision, err)
	}

	assertGatewayDecisionHost(t, decision)

	if len(seenAuthHeaders) != 2 || seenAuthHeaders[0] != "Bearer default-gateway-token" || seenAuthHeaders[1] != "Bearer override-gateway-token" {
		t.Fatalf("seenAuthHeaders = %v", seenAuthHeaders)
	}
}

func assertGatewayDecisionHost(t *testing.T, aliasCheck AppHostingGatewayAccessDecision) {
	t.Helper()
	if aliasCheck.Host != "ops-report-a1b2c3.apps.soha.internal" {
		t.Fatalf("alias check = %#v", aliasCheck)
	}
}

func TestAppHostingB2PortalCollaborationApprovalAndWorkspaceClientMethods(t *testing.T) {
	now := time.Date(2026, 11, 21, 3, 0, 0, 0, time.UTC)
	aliceSummary := HostedAppUserSummary{
		UserID:      "usr_alice",
		DisplayName: "Alice Chen",
		AvatarURL:   "https://cdn.soha.internal/avatars/alice.png",
		TeamNames:   []string{"Growth Analytics"},
	}
	bobSummary := HostedAppUserSummary{
		UserID:      "usr_bob",
		DisplayName: "Bob Li",
		TeamNames:   []string{"Platform"},
	}
	pendingApproval := HostedAppVisibilityApproval{
		ID:                  "appr_1",
		AppID:               "app_123",
		AppName:             "Ops Report",
		AppDNSLabel:         "ops-report-a1b2c3",
		RequesterUserID:     "usr_alice",
		RequesterProfile:    &aliceSummary,
		CurrentVisibility:   HostedAppVisibilityScope{Mode: HostedAppVisibilityModeSelected, TeamIDs: []string{"team_ops"}},
		RequestedVisibility: HostedAppVisibilityScope{Mode: HostedAppVisibilityModeOrganization},
		BaseRouteGeneration: 3,
		PolicyVersion:       2,
		Status:              HostedAppVisibilityApprovalStatusPending,
		Reason:              "Open to all employees",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	richApp := HostedApp{
		ID:                        "app_123",
		Name:                      "Ops Report",
		Kind:                      HostedAppKindStatic,
		DNSLabel:                  "ops-report-a1b2c3",
		Host:                      "ops-report-a1b2c3.apps.soha.internal",
		URL:                       "https://ops-report-a1b2c3.apps.soha.internal",
		SpaFallback:               true,
		OwnerUserID:               "usr_alice",
		OwnerProfile:              &aliceSummary,
		MaintainerUserIDs:         []string{"usr_bob"},
		Maintainers:               []HostedAppUserSummary{bobSummary},
		Visibility:                HostedAppVisibilityScope{Mode: HostedAppVisibilityModeSelected, TeamIDs: []string{"team_ops"}},
		PendingVisibilityApproval: &pendingApproval,
		Status:                    HostedAppStatusOnline,
		ActiveRevisionID:          "rev_2",
		ActiveRevisionNumber:      2,
		TotalBytes:                16384,
		RouteGeneration:           3,
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}

	t.Run("settings policy and list apps filters", func(t *testing.T) {
		aliasReq := UpdateAppHostingSettingsRequest{
			Enabled:                      true,
			AppsDomain:                   "apps.soha.internal",
			StorageIntegrationID:         "int_minio_1",
			OrganizationVisibilityPolicy: AppHostingOrganizationVisibilityPolicyRequireApproval,
		}
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method + " " + r.URL.Path {
			case "PUT /api/v1/app-hosting/settings":
				req := decodeJSONRequestBody[AppHostingSettingsUpdateRequest](t, r)
				if req.OrganizationVisibilityPolicy != AppHostingOrganizationVisibilityPolicyRequireApproval {
					t.Fatalf("unexpected policy: %#v", req)
				}
				writeJSON(t, w, AppHostingSettingsResponse{
					Data: AppHostingSettings{
						Enabled:                      true,
						AppsDomain:                   req.AppsDomain,
						PublicScheme:                 "https",
						StorageIntegrationID:         req.StorageIntegrationID,
						StoragePrefix:                "hosted-apps",
						GatewayTokenConfigured:       true,
						AllowOrganizationVisibility:  true,
						OrganizationVisibilityPolicy: AppHostingOrganizationVisibilityPolicyRequireApproval,
						PolicyVersion:                2,
					},
				})
			case "GET /api/v1/app-hosting/apps":
				q := r.URL.Query()
				if q.Get("scope") != "all" || q.Get("ownerUserId") != "usr_alice" || q.Get("teamId") != "team_ops" {
					t.Fatalf("unexpected query: %s", r.URL.RawQuery)
				}
				writeJSON(t, w, HostedAppListResponse{Data: HostedAppList{Items: []HostedApp{richApp}, Total: 1}})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		})

		settings, err := client.UpdateAppHostingSettings(context.Background(), aliasReq)
		if err != nil || settings.OrganizationVisibilityPolicy != AppHostingOrganizationVisibilityPolicyRequireApproval || settings.PolicyVersion != 2 {
			t.Fatalf("UpdateAppHostingSettings = %#v, %v", settings, err)
		}
		list, err := client.ListHostedApps(context.Background(), ListHostedAppsParams{
			Scope:       HostedAppListScopeAll,
			OwnerUserID: "usr_alice",
			TeamID:      "team_ops",
		})
		if err != nil || list.Total != 1 || list.Items[0].PendingVisibilityApproval == nil || list.Items[0].ActiveRevisionNumber != 2 {
			t.Fatalf("ListHostedApps = %#v, %v", list, err)
		}
	})

	t.Run("portal catalog", func(t *testing.T) {
		authorSummary := PortalHostedAppAuthorSummary{
			UserID:          "usr_alice",
			DisplayName:     "Alice Chen",
			TeamNames:       []string{"Growth Analytics"},
			VisibleAppCount: 1,
		}
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assertCommonRequest(t, r, http.MethodGet, "/api/v1/app-hosting/portal/apps")
			q := r.URL.Query()
			if q.Get("query") != "ops" || q.Get("kind") != "static" || q.Get("authorUserId") != "usr_alice" || q.Get("limit") != "24" || q.Get("offset") != "24" {
				t.Fatalf("unexpected portal query: %s", r.URL.RawQuery)
			}
			writeJSON(t, w, PortalHostedAppCatalogResponse{
				Data: PortalHostedAppCatalog{
					Items: []PortalHostedAppItem{{
						ID:          "app_123",
						Name:        "Ops Report",
						Description: "Weekly metrics",
						Kind:        HostedAppKindStatic,
						DNSLabel:    "ops-report-a1b2c3",
						Host:        "ops-report-a1b2c3.apps.soha.internal",
						URL:         "https://ops-report-a1b2c3.apps.soha.internal",
						Author:      aliceSummary,
						UpdatedAt:   now,
					}},
					Authors:        []PortalHostedAppAuthorSummary{authorSummary},
					SelectedAuthor: &authorSummary,
					MyAppsCount:    2,
					Total:          1,
				},
			})
		})
		catalog, err := client.ListPortalHostedApps(context.Background(), ListPortalHostedAppsParams{
			Query:        "ops",
			Kind:         HostedAppKindStatic,
			AuthorUserID: "usr_alice",
			Limit:        24,
			Offset:       24,
		})
		if err != nil || catalog.Total != 1 || catalog.MyAppsCount != 2 || catalog.SelectedAuthor == nil || catalog.Items[0].Author.UserID != "usr_alice" {
			t.Fatalf("ListPortalHostedApps = %#v, %v", catalog, err)
		}
	})

	t.Run("maintainers transfer and delete", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method + " " + r.URL.Path {
			case "PUT /api/v1/app-hosting/apps/app_123/maintainers":
				req := decodeJSONRequestBody[UpdateHostedAppMaintainersRequest](t, r)
				if len(req.MaintainerUserIDs) != 2 || req.MaintainerUserIDs[0] != "usr_bob" {
					t.Fatalf("maintainers req = %#v", req)
				}
				writeJSON(t, w, HostedAppResponse{Data: richApp})
			case "POST /api/v1/app-hosting/apps/app_123/transfer":
				req := decodeJSONRequestBody[TransferHostedAppOwnershipRequest](t, r)
				if req.NewOwnerUserID != "usr_bob" || req.Reason != "handover" {
					t.Fatalf("transfer req = %#v", req)
				}
				transferred := richApp
				transferred.OwnerUserID = "usr_bob"
				transferred.RouteGeneration = 4
				writeJSON(t, w, HostedAppResponse{Data: transferred})
			case "POST /api/v1/app-hosting/apps/app_123/delete":
				req := decodeJSONRequestBody[DeleteHostedAppRequest](t, r)
				if req.ConfirmationName != "Ops Report" {
					t.Fatalf("delete req = %#v", req)
				}
				writeJSON(t, w, DeleteHostedAppResponse{Data: DeleteHostedAppResult{AppID: "app_123", Deleted: true}})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		})

		if _, err := client.UpdateHostedAppMaintainers(context.Background(), " ", UpdateHostedAppMaintainersRequest{}); err == nil {
			t.Fatal("expected empty appID error for UpdateHostedAppMaintainers")
		}
		if _, err := client.TransferHostedAppOwnership(context.Background(), "", TransferHostedAppOwnershipRequest{}); err == nil {
			t.Fatal("expected empty appID error for TransferHostedAppOwnership")
		}
		if _, err := client.DeleteHostedApp(context.Background(), "", DeleteHostedAppRequest{}); err == nil {
			t.Fatal("expected empty appID error for DeleteHostedApp")
		}

		updated, err := client.UpdateHostedAppMaintainers(context.Background(), "app_123", UpdateHostedAppMaintainersRequest{
			MaintainerUserIDs: []string{"usr_bob", "usr_carol"},
		})
		if err != nil || updated.ID != "app_123" {
			t.Fatalf("UpdateHostedAppMaintainers = %#v, %v", updated, err)
		}

		transferred, err := client.TransferHostedAppOwnership(context.Background(), "app_123", TransferHostedAppOwnershipRequest{
			NewOwnerUserID: "usr_bob",
			Reason:         "handover",
		})
		if err != nil || transferred.OwnerUserID != "usr_bob" || transferred.RouteGeneration != 4 {
			t.Fatalf("TransferHostedAppOwnership = %#v, %v", transferred, err)
		}

		deleted, err := client.DeleteHostedApp(context.Background(), "app_123", DeleteHostedAppRequest{
			ConfirmationName: "Ops Report",
			Reason:           "cleanup",
		})
		if err != nil || !deleted.Deleted || deleted.AppID != "app_123" {
			t.Fatalf("DeleteHostedApp = %#v, %v", deleted, err)
		}
	})

	t.Run("approvals list withdraw approve and reject", func(t *testing.T) {
		decidedAt := now.Add(5 * time.Minute)
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method + " " + r.URL.Path {
			case "GET /api/v1/app-hosting/approvals":
				q := r.URL.Query()
				if q.Get("scope") != "all" || q.Get("status") != "pending" || q.Get("appId") != "app_123" || q.Get("limit") != "50" {
					t.Fatalf("unexpected approvals query: %s", r.URL.RawQuery)
				}
				writeJSON(t, w, HostedAppVisibilityApprovalListResponse{
					Data: HostedAppVisibilityApprovalList{Items: []HostedAppVisibilityApproval{pendingApproval}, Total: 1},
				})
			case "POST /api/v1/app-hosting/apps/app_123/visibility-approval/withdraw":
				withdrawnApp := richApp
				withdrawnApp.PendingVisibilityApproval = nil
				writeJSON(t, w, HostedAppResponse{Data: withdrawnApp})
			case "POST /api/v1/app-hosting/approvals/appr_1/approve":
				req := decodeJSONRequestBody[ApproveHostedAppVisibilityRequest](t, r)
				approved := pendingApproval
				approved.Status = HostedAppVisibilityApprovalStatusApproved
				approved.ReviewerUserID = "usr_admin"
				approved.ReviewComment = req.Comment
				approved.DecidedAt = &decidedAt
				approvedApp := richApp
				approvedApp.Visibility = HostedAppVisibilityScope{Mode: HostedAppVisibilityModeOrganization}
				approvedApp.PendingVisibilityApproval = nil
				writeJSON(t, w, HostedAppVisibilityApprovalDecisionResponse{
					Data: HostedAppVisibilityApprovalDecisionResult{Approval: approved, App: approvedApp},
				})
			case "POST /api/v1/app-hosting/approvals/appr_1/reject":
				req := decodeJSONRequestBody[RejectHostedAppVisibilityRequest](t, r)
				if req.Comment != "restricted data" {
					t.Fatalf("reject req = %#v", req)
				}
				rejected := pendingApproval
				rejected.Status = HostedAppVisibilityApprovalStatusRejected
				rejected.ReviewerUserID = "usr_admin"
				rejected.ReviewComment = req.Comment
				rejected.DecidedAt = &decidedAt
				writeJSON(t, w, HostedAppVisibilityApprovalDecisionResponse{
					Data: HostedAppVisibilityApprovalDecisionResult{Approval: rejected, App: richApp},
				})
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		})

		if _, err := client.ApproveHostedAppVisibility(context.Background(), " ", nil); err == nil {
			t.Fatal("expected empty approvalID error for ApproveHostedAppVisibility")
		}
		if _, err := client.RejectHostedAppVisibility(context.Background(), "", RejectHostedAppVisibilityRequest{Comment: "no"}); err == nil {
			t.Fatal("expected empty approvalID error for RejectHostedAppVisibility")
		}

		approvals, err := client.ListHostedAppVisibilityApprovals(context.Background(), ListHostedAppVisibilityApprovalsParams{
			Scope:  HostedAppApprovalListScopeAll,
			Status: HostedAppVisibilityApprovalStatusPending,
			AppID:  "app_123",
			Limit:  50,
		})
		if err != nil || approvals.Total != 1 || approvals.Items[0].ID != "appr_1" {
			t.Fatalf("ListHostedAppVisibilityApprovals = %#v, %v", approvals, err)
		}

		withdrawn, err := client.WithdrawHostedAppVisibilityApproval(context.Background(), "app_123", &HostedAppLifecycleActionRequest{Reason: "update audience"})
		if err != nil || withdrawn.PendingVisibilityApproval != nil {
			t.Fatalf("WithdrawHostedAppVisibilityApproval = %#v, %v", withdrawn, err)
		}

		approvedRes, err := client.ApproveHostedAppVisibility(context.Background(), "appr_1", nil)
		if err != nil || approvedRes.Approval.Status != HostedAppVisibilityApprovalStatusApproved || approvedRes.App.Visibility.Mode != HostedAppVisibilityModeOrganization {
			t.Fatalf("ApproveHostedAppVisibility = %#v, %v", approvedRes, err)
		}

		rejectedRes, err := client.RejectHostedAppVisibility(context.Background(), "appr_1", RejectHostedAppVisibilityRequest{Comment: "restricted data"})
		if err != nil || rejectedRes.Approval.Status != HostedAppVisibilityApprovalStatusRejected {
			t.Fatalf("RejectHostedAppVisibility = %#v, %v", rejectedRes, err)
		}
	})

	t.Run("deploy from workspace", func(t *testing.T) {
		spaTrue := true
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assertCommonRequest(t, r, http.MethodPost, "/api/v1/app-hosting/workspace-deployments")
			req := decodeJSONRequestBody[WorkspaceHostedAppDeploymentRequest](t, r)
			if req.Source.Kind != WorkspaceHostedAppSourceKindPersonalEnvironment || req.Source.SourcePath != "dist" || req.IdempotencyKey != "ws-idem-1" {
				t.Fatalf("workspace deploy req = %#v", req)
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, WorkspaceHostedAppDeploymentResponse{
				Data: WorkspaceHostedAppDeploymentResult{
					App: richApp,
					Revision: HostedAppRevision{
						ID:             "rev_3",
						AppID:          "app_123",
						RevisionNumber: 3,
						Kind:           HostedAppKindStatic,
						Status:         HostedAppRevisionStatusReady,
						BundleSha256:   strings.Repeat("b", 64),
						FileCount:      3,
						TotalBytes:     8192,
						SpaFallback:    true,
						CreatedBy:      "usr_alice",
						CreatedAt:      now,
						ActivatedAt:    &now,
					},
					Created: true,
				},
			})
		})

		deployed, err := client.DeployHostedAppFromWorkspace(context.Background(), WorkspaceHostedAppDeploymentRequest{
			Name:        "Ops Report",
			SpaFallback: &spaTrue,
			Visibility:  &HostedAppVisibilityInput{Mode: HostedAppVisibilityModePrivate},
			Source: WorkspaceHostedAppSource{
				Kind:               WorkspaceHostedAppSourceKindPersonalEnvironment,
				SourcePath:         "dist",
				EnvironmentVersion: 2,
			},
			IdempotencyKey: "ws-idem-1",
		})
		if err != nil || !deployed.Created || deployed.Revision.ID != "rev_3" {
			t.Fatalf("DeployHostedAppFromWorkspace = %#v, %v", deployed, err)
		}
	})
}
