package networkcontrol

import (
	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/networkprotocol"
	"net/http"
)

func (h *handler) eapCertificate(c *gin.Context, activate bool) {
	c.Header("Cache-Control", "no-store")
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	var result networkprotocol.RuntimeMessage
	var err error
	if activate {
		result, err = h.service.ActivateEAPCertificate(c.Request.Context(), identity, raw)
	} else {
		result, err = h.service.IssueEAPCertificate(c.Request.Context(), identity, raw)
	}
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}
