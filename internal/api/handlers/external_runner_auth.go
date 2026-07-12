package handlers

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	appaccess "github.com/opensoha/soha/internal/application/access"
	"github.com/opensoha/soha/internal/platform/keyring"
)

func authorizeDeliveryRunner(c *gin.Context, staticToken string) bool {
	return authorizeDeliveryRunnerKeys(c, legacyRunnerKeyring(staticToken))
}

func authorizeDeliveryRunnerKeys(c *gin.Context, keys keyring.Ring) bool {
	return authorizeExternalRunnerKeys(c, keys, appaccess.PermDeliveryExecutionTasksManage)
}

func authorizeDockerRunnerKeys(c *gin.Context, keys keyring.Ring) bool {
	return authorizeExternalRunnerKeys(c, keys, appaccess.PermDockerOperationsManage)
}

func authorizeAIAgentRunnerKeys(c *gin.Context, keys keyring.Ring) bool {
	return authorizeExternalRunnerKeys(c, keys, appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse)
}

func authorizeExternalRunnerKeys(c *gin.Context, keys keyring.Ring, acceptedPermissionKeys ...string) bool {
	authorization, valid := apiMiddleware.SingleAuthorizationHeader(c.Request)
	if !valid {
		return false
	}
	if authorizeStaticBearerKeys(authorization, keys) {
		return true
	}
	accessCtx := apiMiddleware.AccessContextFromContext(c)
	if accessCtx.TokenKind != "service_account_token" || accessCtx.SubjectType != "service_account" {
		return false
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	return hasAnyRunnerPermission(principal.PermissionKeys, acceptedPermissionKeys...)
}

func authorizeStaticBearerKeys(header string, keys keyring.Ring) bool {
	if keys.Active().ID() == "" {
		return false
	}
	actual := strings.TrimSpace(header)
	if len(actual) >= len("Bearer ") && strings.EqualFold(actual[:len("Bearer ")], "Bearer ") {
		actual = strings.TrimSpace(actual[len("Bearer "):])
	}
	return keys.Match(actual, time.Now().UTC())
}

func legacyRunnerKeyring(token string) keyring.Ring {
	token = strings.TrimSpace(token)
	if token == "" {
		return keyring.Ring{}
	}
	key, err := keyring.NewKey("legacy-config-key", token, time.Unix(0, 0).UTC(), nil)
	if err != nil {
		return keyring.Ring{}
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		return keyring.Ring{}
	}
	return ring
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
