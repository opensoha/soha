package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
	domainresource "github.com/opensoha/soha/agent/internal/domain/resource"
	"github.com/opensoha/soha/contracts/helmrelease"
	helmruntime "github.com/opensoha/soha/contracts/helmrelease/runtime"
)

func writeHelmError(c *gin.Context, err error) {
	if errors.Is(err, helmruntime.ErrConflict) {
		apiresponse.Error(c, http.StatusConflict, "conflict", "Helm release ownership or revision conflicts with this operation")
		return
	}
	writeError(c, err)
}

func registerHelmRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	platform.POST("/helm/charts/install", actions.Require(actionPlatformHelmReleaseInstall), func(c *gin.Context) {
		var req domainresource.HelmChartInstallInput
		if err := c.ShouldBindJSON(&req); err != nil || invalidHelmInstallInput(req) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "repositoryUrl, chartName, version, releaseName, and namespace are required")
			return
		}
		item, err := client.InstallHelmChart(c.Request.Context(), req)
		if err != nil {
			writeHelmError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusCreated, item)
	})
	platform.PUT("/helm/releases/:name/values", actions.Require(actionPlatformHelmReleaseValuesUpdate), updateHelmValues(client, false))
	platform.PUT("/helm/releases/:name/values/conditional", actions.Require(actionPlatformHelmReleaseValuesUpdate), updateHelmValues(client, true))
	rollback := func(dryRun bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			var req domainresource.HelmReleaseRollbackInput
			if err := c.ShouldBindJSON(&req); err != nil {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid helm release rollback payload")
				return
			}
			namespace, name, req, err := normalizeHelmRollbackRequest(c.Query("namespace"), c.Param("name"), req)
			if err != nil {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
				return
			}
			if dryRun {
				if err := client.DryRunHelmReleaseRollback(c.Request.Context(), namespace, name, req); err != nil {
					writeHelmError(c, err)
					return
				}
				apiresponse.Item(c, http.StatusOK, gin.H{"valid": true})
				return
			}
			item, err := client.RollbackHelmRelease(c.Request.Context(), namespace, name, req)
			if err != nil {
				writeHelmError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		}
	}
	platform.POST("/helm/releases/:name/rollback/preflight", actions.Require(actionPlatformHelmReleaseRollback), rollback(true))
	platform.POST("/helm/releases/:name/rollback", actions.Require(actionPlatformHelmReleaseRollback), rollback(false))
	platform.DELETE("/helm/releases/:name", actions.Require(actionPlatformHelmReleaseDelete), func(c *gin.Context) {
		namespace := strings.TrimSpace(c.Query("namespace"))
		name := strings.TrimSpace(c.Param("name"))
		if namespace == "" || name == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace and releaseName are required")
			return
		}
		if err := client.DeleteHelmRelease(c.Request.Context(), namespace, name); err != nil {
			writeHelmError(c, err)
			return
		}
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})
}

func updateHelmValues(client *k8sagent.Client, conditional bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req helmrelease.ValuesUpdateRequest
		if err := c.ShouldBindJSON(&req); err != nil || (conditional && req.ExpectedRevision == 0) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid helm release values payload")
			return
		}
		namespace := strings.TrimSpace(c.Query("namespace"))
		name := strings.TrimSpace(c.Param("name"))
		if namespace == "" || name == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace and releaseName are required")
			return
		}
		item, err := client.UpdateHelmReleaseValues(c.Request.Context(), namespace, name, req.Content, int(req.ExpectedRevision))
		if err != nil {
			writeHelmError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	}
}

func invalidHelmInstallInput(input domainresource.HelmChartInstallInput) bool {
	return strings.TrimSpace(input.RepositoryURL) == "" ||
		strings.TrimSpace(input.ChartName) == "" ||
		strings.TrimSpace(input.Version) == "" ||
		strings.TrimSpace(input.ReleaseName) == "" ||
		strings.TrimSpace(input.Namespace) == ""
}

func normalizeHelmRollbackRequest(namespace, name string, input domainresource.HelmReleaseRollbackInput) (string, string, domainresource.HelmReleaseRollbackInput, error) {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" || name == "" {
		return namespace, name, input, fmt.Errorf("namespace and releaseName are required")
	}
	if input.Revision < 1 {
		return namespace, name, input, fmt.Errorf("revision must be a positive integer")
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 300
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > 3600 {
		return namespace, name, input, fmt.Errorf("timeoutSeconds must be between 1 and 3600")
	}
	return namespace, name, input, nil
}
