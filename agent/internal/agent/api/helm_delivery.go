package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
)

type helmDeliveryReader interface {
	PrepareHelmDelivery(context.Context, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error)
	ExecuteHelmDelivery(context.Context, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error)
}

func registerHelmDeliveryRoutes(platform *gin.RouterGroup, client helmDeliveryReader, clusterID string) {
	read := func(operation string) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Header("Cache-Control", "no-store")
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
			var input sohaapi.HelmExecutionTaskPayload
			decoder := json.NewDecoder(c.Request.Body)
			decoder.DisallowUnknownFields()
			expected := sohaapi.Preflight
			if operation == "observe" {
				expected = sohaapi.Observe
			}
			if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Action != expected || input.Snapshot.ClusterID != clusterID || helmrelease.ValidateTaskIdentity(input) != nil {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid read-only Helm delivery input")
				return
			}
			if operation != "prepare" {
				item, err := client.ExecuteHelmDelivery(c.Request.Context(), input)
				if err != nil {
					apiresponse.Error(c, http.StatusConflict, "conflict", "Helm release does not match the expected deployment")
					return
				}
				apiresponse.Item(c, http.StatusOK, item)
				return
			}
			item, err := client.PrepareHelmDelivery(c.Request.Context(), input)
			if err != nil {
				apiresponse.Error(c, http.StatusConflict, "conflict", "Helm preparation failed")
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		}
	}
	platform.POST("/helm/delivery/prepare", read("prepare"))
	platform.POST("/helm/delivery/observe", read("observe"))
	platform.POST("/helm/delivery/preflight", read("preflight"))
}
