package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

func registerActionDiagnostics(router *gin.Engine, cfg cfgpkg.Config, client *k8sagent.Client) {
	var mu sync.Mutex
	var cached []contractresource.AgentActionCheck
	var expires time.Time
	policy := newActionPolicy(cfg.Security, nil, nil)
	router.GET(cfg.HTTP.BasePath+"/diagnostics/actions", authAnyMiddleware(cfg.Auth.BearerToken, cfg.ControlPlane.BearerToken), func(c *gin.Context) {
		mu.Lock()
		defer mu.Unlock()
		if time.Now().After(expires) {
			cached = connectedActionChecks(c.Request.Context(), client, policy)
			expires = time.Now().Add(30 * time.Second)
		}
		apiresponse.Items(c, http.StatusOK, cached)
	})
}

func connectedActionChecks(ctx context.Context, client *k8sagent.Client, policy actionPolicy) []contractresource.AgentActionCheck {
	checks := client.ActionAccess(ctx)
	for index := range checks {
		checks[index].Configured = "denied"
		if policy.allows(checks[index].Action) {
			checks[index].Configured = "allowed"
		}
		if client == nil {
			checks[index].KubernetesRBAC = "unknown"
			checks[index].ReasonCode = "runtime_unavailable"
		}
		if checks[index].Action == actionPlatformPortForwardsDelete {
			checks[index].KubernetesRBAC = "not_applicable"
			checks[index].ReasonCode = "agent_session_only"
		}
	}
	return checks
}
