package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/keyring"
)

func TestAuthorizeExternalRunnerAcceptsStaticBearerToken(t *testing.T) {
	t.Parallel()

	ctx := newRunnerAuthTestContext(http.Header{"Authorization": []string{"Bearer runner-token"}})

	if !authorizeDeliveryRunner(ctx, "runner-token") {
		t.Fatal("authorizeDeliveryRunner = false, want static runner token accepted")
	}
}

func TestAuthorizeExternalRunnerAcceptsUnexpiredPreviousKey(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)
	active, _ := keyring.NewKey("active", "active-runner-token", now, nil)
	previous, _ := keyring.NewKey("previous", "previous-runner-token", now.Add(-time.Hour), &expiresAt)
	keys, _ := keyring.New(active, []keyring.Key{previous})
	ctx := newRunnerAuthTestContext(http.Header{"Authorization": []string{"Bearer previous-runner-token"}})
	if !authorizeDeliveryRunnerKeys(ctx, keys) {
		t.Fatal("unexpired previous runner key was rejected")
	}
}

func TestAuthorizeExternalRunnerRejectsExpiredPreviousKey(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	expiresAt := now.Add(-time.Minute)
	active, _ := keyring.NewKey("active", "active-runner-token", now, nil)
	previous, _ := keyring.NewKey("previous", "previous-runner-token", now.Add(-time.Hour), &expiresAt)
	keys, _ := keyring.New(active, []keyring.Key{previous})
	ctx := newRunnerAuthTestContext(http.Header{"Authorization": []string{"Bearer previous-runner-token"}})
	if authorizeDeliveryRunnerKeys(ctx, keys) {
		t.Fatal("expired previous runner key was accepted")
	}
}

func TestAuthorizeExternalRunnerRejectsMultipleAuthorizationHeaders(t *testing.T) {
	t.Parallel()

	ctx := newRunnerAuthTestContext(http.Header{
		"Authorization": []string{"Bearer runner-token", "Bearer attacker-token"},
	})
	if authorizeDeliveryRunner(ctx, "runner-token") {
		t.Fatal("multiple authorization headers were accepted")
	}
}

func TestAuthorizeExternalRunnerAcceptsServiceAccountTokenPermission(t *testing.T) {
	t.Parallel()

	ctx := newRunnerAuthTestContext(nil)
	ctx.Set("principal", domainidentity.Principal{
		UserID:         "service_account:runner-1",
		UserName:       "runner-1",
		PermissionKeys: []string{appaccess.PermDeliveryExecutionTasksManage},
	})
	ctx.Set("access_context", domainidentity.AccessContext{
		TokenKind:   "service_account_token",
		SubjectType: "service_account",
		SubjectID:   "runner-1",
	})

	if !authorizeDeliveryRunner(ctx, "") {
		t.Fatal("authorizeDeliveryRunner = false, want service account token permission accepted")
	}
}

func TestAuthorizeExternalRunnerRejectsServiceAccountWithoutPermission(t *testing.T) {
	t.Parallel()

	ctx := newRunnerAuthTestContext(nil)
	ctx.Set("principal", domainidentity.Principal{
		UserID:         "service_account:runner-1",
		UserName:       "runner-1",
		PermissionKeys: []string{appaccess.PermDeliveryExecutionTasksView},
	})
	ctx.Set("access_context", domainidentity.AccessContext{
		TokenKind:   "service_account_token",
		SubjectType: "service_account",
		SubjectID:   "runner-1",
	})

	if authorizeDeliveryRunner(ctx, "") {
		t.Fatal("authorizeDeliveryRunner = true, want missing runner permission rejected")
	}
}

func newRunnerAuthTestContext(headers http.Header) *gin.Context {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/execution-tasks/claim", nil)
	req.Header = headers
	ctx.Request = req
	return ctx
}
