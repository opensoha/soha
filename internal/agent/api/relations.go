package api

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	cr "github.com/opensoha/soha-contracts/resource"
	"io"
	"net/http"
)

func registerRelationSnapshotRoute(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	platform.POST("/resources/relations/snapshot", actions.Require(actionPlatformCustomResourcesList), func(c *gin.Context) {
		// Allow the 16 KiB rule configuration plus generated default-source queries.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
		var input cr.RelationSnapshotRequest
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || cr.ValidateRelationSnapshotRequest(input) != nil {
			apiresponse.Error(c, 400, "invalid_request", "invalid resource metadata query")
			return
		}
		out, err := client.RelationSnapshot(c.Request.Context(), input)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, 200, out)
	})
}
