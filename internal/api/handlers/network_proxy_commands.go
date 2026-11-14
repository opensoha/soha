package handlers

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"io"
	"net/http"
)

type NetworkProxyTypedCommands interface {
	QueueCommand(context.Context, identity.Principal, string, domain.RuntimeCommandInput) (domain.RuntimeCommandReceipt, error)
	GetCommand(context.Context, identity.Principal, string, string) (domain.RuntimeCommandReceipt, error)
	CancelCommand(context.Context, identity.Principal, string, string) error
}
type NetworkProxyCommandsHandler struct{ service NetworkProxyTypedCommands }

func NewNetworkProxyCommandsHandler(s NetworkProxyTypedCommands) *NetworkProxyCommandsHandler {
	return &NetworkProxyCommandsHandler{service: s}
}
func (h *NetworkProxyCommandsHandler) Queue(c *gin.Context) {
	var input domain.RuntimeCommandInput
	if !bindProxyCommand(c, &input) {
		return
	}
	item, err := h.service.QueueCommand(c.Request.Context(), principal(c), c.Param("instanceID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respondItem(c, http.StatusAccepted, item, nil)
}
func (h *NetworkProxyCommandsHandler) Get(c *gin.Context) {
	item, err := h.service.GetCommand(c.Request.Context(), principal(c), c.Param("instanceID"), c.Param("commandID"))
	c.Header("Cache-Control", "no-store")
	if err != nil {
		writeError(c, err)
		return
	}
	respondItem(c, http.StatusOK, item, nil)
}

func (h *NetworkProxyCommandsHandler) Cancel(c *gin.Context) {
	err := h.service.CancelCommand(c.Request.Context(), principal(c), c.Param("instanceID"), c.Param("commandID"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func bindProxyCommand(c *gin.Context, input *domain.RuntimeCommandInput) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeError(c, apperrors.ErrInvalidArgument)
		return false
	}
	return true
}
