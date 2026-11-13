package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type chatToolCaller interface {
	CallChatTool(context.Context, string, sohaapi.AgentRunnerToolCallRequest) (sohaapi.AgentToolCallResult, error)
}

func registerAgentToolRoutes(router *gin.Engine, cfg cfgpkg.Config, runtime RuntimeTaskController) {
	caller, ok := runtime.(chatToolCaller)
	if !ok {
		return
	}
	// This endpoint uses a short-lived per-run grant, not the runner admin token.
	router.POST(fmt.Sprintf("%s/runtime/agent-tools", cfg.HTTP.BasePath), func(c *gin.Context) {
		authorization := c.GetHeader("Authorization")
		if !strings.HasPrefix(authorization, "Bearer soha-tools:") || len(authorization) > 128 {
			apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "active agent tool grant required")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var body struct {
			ToolName string                        `json:"toolName"`
			Input    *sohaapi.AgentRunnerToolInput `json:"input"`
		}
		if err := decoder.Decode(&body); err != nil || body.ToolName == "" || len(body.ToolName) > 128 || body.Input == nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid agent tool request")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid agent tool request")
			return
		}
		input := sohaapi.AgentRunnerToolCallRequest{ToolName: body.ToolName, Input: *body.Input}
		result, err := caller.CallChatTool(c.Request.Context(), strings.TrimPrefix(authorization, "Bearer "), input)
		if err != nil {
			apiresponse.Error(c, http.StatusForbidden, "access_denied", "agent tool call denied or unavailable")
			return
		}
		apiresponse.Item(c, http.StatusOK, result)
	})
}
