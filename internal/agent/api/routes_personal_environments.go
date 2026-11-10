package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"github.com/opensoha/soha-agent/internal/agent/environment"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	"go.uber.org/zap"
)

func registerPersonalEnvironmentRoutes(router *gin.Engine, cfg cfgpkg.Config, logger *zap.Logger, actions actionPolicy, streams *nativePodTerminals) *environment.DockerExecutor {
	if !cfg.PersonalEnvironment.Enabled {
		return nil
	}
	executor, err := environment.NewDockerExecutor(cfg.PersonalEnvironment.Image, cfg.PersonalEnvironment.QuotaHelperImage)
	if err != nil {
		logger.Warn("personal environment disabled: invalid image configuration")
		return nil
	}
	group := router.Group(cfg.HTTP.BasePath + "/runtime/personal-environments")
	group.Use(authRequiredAnyMiddleware(cfg.Auth.BearerToken))
	registerPersonalEnvironmentPreflight(group, executor, actions)
	registerPersonalEnvironmentExport(group, executor, actions)
	registerPersonalMCPStdio(group, actions, streams, cfg.PersonalEnvironment.MCPStdioAssets, executor.ReservePersonalMCPStdio)
	group.POST("", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var request contractenv.Request
		if err := decoder.Decode(&request); err != nil || request.Validate() != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal environment request")
			c.Abort()
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal environment request")
			c.Abort()
			return
		}
		c.Set("personal_environment_request", request)
	}, func(c *gin.Context) {
		request, ok := c.MustGet("personal_environment_request").(contractenv.Request)
		if !ok {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		action := map[string]string{"acquire": actionPersonalEnvironmentAcquire, "inspect": actionPersonalEnvironmentInspect,
			"execute": actionPersonalEnvironmentExecute, "release": actionPersonalEnvironmentRelease, "delete_data": actionPersonalEnvironmentDeleteData}[request.Action]
		actions.Require(action)(c)
	}, func(c *gin.Context) {
		request, ok := c.MustGet("personal_environment_request").(contractenv.Request)
		if !ok {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		result, err := executor.PersonalOperation(c.Request.Context(), request)
		if err != nil {
			// A timeout or lost Docker receipt is not proof that a write failed.
			apiresponse.Error(c, http.StatusBadGateway, "environment_outcome_unknown", "personal environment operation unavailable; do not retry command execution automatically")
			return
		}
		apiresponse.Item(c, http.StatusOK, result)
	})
	return executor
}

func registerPersonalEnvironmentExport(group *gin.RouterGroup, executor *environment.DockerExecutor, actions actionPolicy) {
	group.POST("/export", actions.Require(actionPersonalEnvironmentExportData), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var request contractenv.Request
		if decoder.Decode(&request) != nil || request.Action != "export_data" || request.Validate() != nil || decoder.Decode(new(any)) != io.EOF {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal data export request")
			return
		}
		c.Header("Content-Type", "application/vnd.opensoha.personal-data-stream")
		c.Header("Cache-Control", "no-store")
		writer := &contractenv.ExportWriter{Writer: c.Writer}
		if err := executor.ExportPersonalData(c.Request.Context(), request, writer); err != nil {
			if !c.Writer.Written() {
				apiresponse.Error(c, http.StatusBadGateway, "export_unavailable", "personal data export unavailable")
			}
			c.Abort()
			return
		}
		if _, err := writer.Finish(request); err != nil {
			c.Abort()
		}
	})
}

func registerPersonalEnvironmentPreflight(group *gin.RouterGroup, executor *environment.DockerExecutor, actions actionPolicy) {
	group.POST("/preflight", actions.Require(actionPersonalEnvironmentPreflight), func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var request contractenv.PreflightRequest
		if err := decoder.Decode(&request); err != nil || request.Validate() != nil || decoder.Decode(new(any)) != io.EOF {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal environment preflight")
			return
		}
		result, err := executor.Preflight(c.Request.Context(), request)
		if err != nil {
			apiresponse.Error(c, http.StatusServiceUnavailable, "personal_environment_preflight_unavailable", "personal Docker quota preflight is unavailable")
			return
		}
		apiresponse.Item(c, http.StatusOK, result)
	})
}
