package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

type podExecutionClient interface {
	InspectPodExecutionTarget(context.Context, string, string, string, string) (contractresource.PodExecutionTarget, error)
	ExecutePodArgv(context.Context, string, string, contractresource.PodArgvInput) (contractresource.PodArgvResult, error)
}

func registerPodExecutionRoutes(platform *gin.RouterGroup, client podExecutionClient, actions actionPolicy) {
	group := platform.Group("/pod-execution-v1")
	group.GET("/capabilities", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		capabilities := contractresource.AgentPodExecutionCapabilities{
			ProtocolVersion: contractresource.PodExecutionProtocolVersion, ArgvVersion: "1", Ready: true,
			MaxTimeoutSeconds: contractresource.PodExecMaxTimeoutSeconds, MaxOutputBytes: contractresource.PodArgvMaxOutputBytes,
		}
		if _, ok := client.(nativePodTerminalClient); ok {
			capabilities.TerminalVersion = contractresource.PodTerminalProtocolVersion
			capabilities.MaxSessionSeconds = contractresource.NativePodTerminalMaxTimeout
			capabilities.MaxTerminalInputBytes = contractresource.NativePodTerminalMaxInputBytes
			capabilities.MaxTerminalOutputBytes = contractresource.NativePodTerminalMaxOutputBytes
		}
		apiresponse.Item(c, http.StatusOK, capabilities)
	})
	group.GET("/pods/:podName/target", actions.Require(actionPlatformPodsExec), func(c *gin.Context) {
		item, err := client.InspectPodExecutionTarget(c.Request.Context(), c.Query("namespace"), c.Param("podName"), c.Query("expectedUid"), c.Query("container"))
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, http.StatusOK, item)
	})
	group.POST("/pods/:podName/argv", actions.Require(actionPlatformPodsExec), func(c *gin.Context) {
		input, ok := decodePodArgv(c)
		if !ok {
			return
		}
		// The normal server write timeout is shorter than the supported exec
		// deadline. This request remains bounded by the normalized input.
		_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(time.Duration(input.TimeoutSeconds+15) * time.Second))
		item, err := client.ExecutePodArgv(c.Request.Context(), c.Query("namespace"), c.Param("podName"), input)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, http.StatusOK, item)
	})
}

func decodePodArgv(c *gin.Context) (contractresource.PodArgvInput, bool) {
	var input contractresource.PodArgvInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid Pod argv payload")
		return input, false
	}
	normalized, err := input.Normalized()
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid Pod argv bounds or target")
		return input, false
	}
	return normalized, true
}
