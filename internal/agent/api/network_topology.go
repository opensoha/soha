package api

import (
	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	resource "github.com/opensoha/soha-contracts/resource"
	"net/http"
)

func registerTopologySnapshotRoute(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	registerRelationSnapshotRoute(platform, client, actions)
	platform.GET("/infrastructure/resource-monitoring", func(c *gin.Context) {
		output, err := client.ResourceMonitoringInventory(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, output)
	})
	platform.POST("/network/topology/snapshot", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var input resource.NetworkTopologySnapshotRequest
		if c.ShouldBindJSON(&input) != nil || resource.ValidateNetworkTopologySnapshotRequest(input) != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "invalid topology source query")
			return
		}
		allowed := resource.NetworkTopologySnapshotRequest{}
		denied := []resource.NetworkTopologySourceResult{}
		for _, query := range input.Queries {
			definition, _ := resource.LookupNetworkTopologySource(query.SourceID)
			if definition.RequiresCustomResourceAccess() && !actions.allows(actionPlatformCustomResourcesList) {
				actions.audit(c, actionPlatformCustomResourcesList, false, "topology custom source is not allowlisted")
				denied = append(denied, resource.NetworkTopologySourceResult{Coverage: resource.NetworkTopologyCoverage{Provider: definition.Provider, SourceID: query.SourceID, Namespace: query.Namespace, State: "denied", ReasonCode: "action_not_allowed"}, Objects: []resource.NetworkTopologySourceObject{}})
			} else {
				allowed.Queries = append(allowed.Queries, query)
			}
		}
		output := resource.NetworkTopologySnapshot{Version: resource.NetworkTopologySnapshotFeature, Sources: denied}
		if len(allowed.Queries) > 0 {
			read, err := client.NetworkTopologySnapshot(c.Request.Context(), allowed)
			if err != nil {
				writeError(c, err)
				return
			}
			output.Sources = append(output.Sources, read.Sources...)
		}
		apiresponse.Item(c, http.StatusOK, output)
	})
}
