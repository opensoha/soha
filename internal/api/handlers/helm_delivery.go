package handlers

import (
	"context"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
)

func (h *DeliveryHandler) authorizedExecutionProviders(c *gin.Context, providers []string) []string {
	header, valid := apiMiddleware.SingleAuthorizationHeader(c.Request)
	if !valid || len(providers) > 32 {
		return nil
	}
	token, _ := bearerToken(header)
	general := authorizeDeliveryRunnerKeys(c, h.runnerKeys)
	allowed := []string{}
	clusters := map[string]bool{}
	for _, provider := range providers {
		if provider == "helm_direct" || provider == "manifest_direct" {
			continue
		}
		clusterID := strings.TrimPrefix(provider, "helm_agent.")
		helm := clusterID != provider
		if !helm {
			clusterID = strings.TrimPrefix(provider, resourceruntime.ManifestAgentProviderPrefix)
			if clusterID == provider {
				clusterID = strings.TrimPrefix(provider, "manifest_agent.")
			}
		}
		if clusterID != provider && (!general || helm) {
			ok, checked := clusters[clusterID]
			if !checked && clusterID != "" && h.agents != nil && token != "" {
				ok = h.agents.AuthenticateAgentExecution(c.Request.Context(), clusterID, token) == nil
				clusters[clusterID] = ok
			}
			if ok {
				allowed = append(allowed, provider)
			}
		} else if general && !strings.HasPrefix(provider, "helm_") {
			allowed = append(allowed, provider)
		}
	}
	return allowed
}

type DeliveryHelmService interface {
	InspectApplicationHelmChart(context.Context, domainidentity.Principal, string, sohaapi.HelmChartInspectionInput) (sohaapi.HelmChartInspection, error)
}

func (h *DeliveryHandler) InspectApplicationHelmChart(c *gin.Context) {
	var input sohaapi.HelmChartInspectionInput
	if !decodeDeploymentTemplateRequest(c, &input, "applicationEnvironmentId", "source") {
		return
	}
	if h.helm == nil {
		writeError(c, apperrors.ErrClusterUnready)
		return
	}
	item, err := h.helm.InspectApplicationHelmChart(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
