package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestAuthorizeExternalRunnerAcceptsStaticBearerToken(t *testing.T) {
	t.Parallel()

	ctx := newRunnerAuthTestContext(http.Header{"Authorization": []string{"Bearer runner-token"}})

	if !authorizeDeliveryRunner(ctx, "runner-token") {
		t.Fatal("authorizeDeliveryRunner = false, want static runner token accepted")
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
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/execution-tasks/claim", nil)
	req.Header = headers
	ctx.Request = req
	return ctx
}
