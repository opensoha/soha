package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func (h *NetworkAccessHandler) GetSessionCommand(c *gin.Context) {
	item, err := h.session.GetSessionCommandForSession(c.Request.Context(), principal(c), c.Param("sessionID"), c.Param("commandID"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, sessionCommandDTO(item))
}

func (h *NetworkAccessHandler) GetOriginalSessionCommand(c *gin.Context) {
	var params sohaapi.GetOriginalNetworkSessionCommandParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	item, err := h.session.GetOriginalSessionCommand(c.Request.Context(), principal(c), c.Param("sessionID"), params.PlanHash)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, sessionCommandDTO(item))
}
