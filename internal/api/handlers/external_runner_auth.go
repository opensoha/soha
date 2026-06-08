package handlers

import (
	"crypto/subtle"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	appaccess "github.com/opensoha/soha/internal/application/access"
)

func authorizeDeliveryRunner(c *gin.Context, staticToken string) bool {
	return authorizeExternalRunner(c, staticToken, appaccess.PermDeliveryExecutionTasksManage)
}

func authorizeDockerRunner(c *gin.Context, staticToken string) bool {
	return authorizeExternalRunner(c, staticToken, appaccess.PermDockerOperationsManage)
}

func authorizeAIAgentRunner(c *gin.Context, staticToken string) bool {
	return authorizeExternalRunner(c, staticToken, appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse)
}

func authorizeExternalRunner(c *gin.Context, staticToken string, acceptedPermissionKeys ...string) bool {
	if authorizeStaticBearerToken(c.GetHeader("Authorization"), staticToken) {
		return true
	}
	accessCtx := apiMiddleware.AccessContextFromContext(c)
	if accessCtx.TokenKind != "service_account_token" || accessCtx.SubjectType != "service_account" {
		return false
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	return hasAnyRunnerPermission(principal.PermissionKeys, acceptedPermissionKeys...)
}

func authorizeStaticBearerToken(header, staticToken string) bool {
	expected := strings.TrimSpace(staticToken)
	if expected == "" {
		return false
	}
	actual := strings.TrimSpace(header)
	if len(actual) >= len("Bearer ") && strings.EqualFold(actual[:len("Bearer ")], "Bearer ") {
		actual = strings.TrimSpace(actual[len("Bearer "):])
	}
	if len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func hasAnyRunnerPermission(permissionKeys []string, acceptedPermissionKeys ...string) bool {
	if len(permissionKeys) == 0 || len(acceptedPermissionKeys) == 0 {
		return false
	}
	allowed := make(map[string]struct{}, len(permissionKeys))
	for _, key := range permissionKeys {
		key = strings.TrimSpace(key)
		if key != "" {
			allowed[key] = struct{}{}
		}
	}
	for _, key := range acceptedPermissionKeys {
		if _, ok := allowed[strings.TrimSpace(key)]; ok {
			return true
		}
	}
	return false
}
