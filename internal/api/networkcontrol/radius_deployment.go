package networkcontrol

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/http"
)

type RadiusDeploymentRuntime interface {
	RuntimeControl(context.Context, networkidentity.Identity, string, string) (api.NetworkRADIUSRuntimeControl, error)
	RuntimeACK(context.Context, networkidentity.Identity, api.NetworkRADIUSRuntimeACK) error
}

func radiusIdentity(c *gin.Context) (networkidentity.Identity, bool) {
	id, e := authenticatedIdentity(c.Request)
	if e != nil || id.Kind != "nas" || id.ID != c.Param("runtimeID") {
		respondError(c, apperrors.ErrUnauthorized)
		return id, false
	}
	return id, true
}
func (h *handler) radiusControl(c *gin.Context) {
	id, ok := radiusIdentity(c)
	if !ok {
		return
	}
	q := c.Request.URL.Query()
	m, p := q["memberId"], q["processEpoch"]
	if len(q) != 2 || len(m) != 1 || len(p) != 1 || m[0] == "" || p[0] == "" {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	v, e := h.options.RadiusDeployment.RuntimeControl(c.Request.Context(), id, m[0], p[0])
	if e != nil {
		respondError(c, e)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, v)
}
func (h *handler) radiusACK(c *gin.Context) {
	id, ok := radiusIdentity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	if h.options.ProxySchemas.ValidateRadiusRuntime(raw) != nil {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	var v api.NetworkRADIUSRuntimeACK
	if json.Unmarshal(raw, &v) != nil || v.RuntimeID != id.ID {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	if e := h.options.RadiusDeployment.RuntimeACK(c.Request.Context(), id, v); e != nil {
		respondError(c, e)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, v)
}
