package networkcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	middleware "github.com/opensoha/soha/internal/api/middleware"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"io"
	"net/http"
)

type ProxyPublication interface {
	DirectProjection(context.Context, networkidentity.Identity, string, string) (api.NetworkProxyDirectProjection, error)
	ServerReady(context.Context, networkidentity.Identity, string, api.NetworkProxyBundleReadback) error
	IngressCheck(context.Context, networkidentity.Identity, string, api.NetworkProxyIngressCheckInput) (api.NetworkProxyIngressCheckResult, error)
	Projection(context.Context, networkidentity.Identity, string, string, string) (api.NetworkProxyEmployeeProjection, error)
	ProjectionApplied(context.Context, networkidentity.Identity, string, api.NetworkProxyProjectionApplied) error
}
type PublicationHandler struct {
	base    *handler
	service ProxyPublication
}

func NewPublicationHandler(service ProxyPublication, options Options) (*PublicationHandler, error) {
	if service == nil || options.MaxBodyBytes <= 0 || options.RequestsPerMinute <= 0 {
		return nil, apperrors.ErrInvalidArgument
	}
	return &PublicationHandler{service: service, base: &handler{options: options, limiter: middleware.NewBoundedRateLimiter(10000)}}, nil
}
func (h *PublicationHandler) IngressCheck(c *gin.Context) {
	id, ok := h.base.proxyIdentity(c)
	if !ok {
		return
	}
	raw, ok := h.base.body(c)
	if !ok {
		return
	}
	input, ok := decodePublicationPayload[api.NetworkProxyIngressCheckInput](c, raw)
	if !ok {
		return
	}
	out, err := h.service.IngressCheck(c.Request.Context(), id, "", input)
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, out)
}
func (h *PublicationHandler) ServerReady(c *gin.Context) {
	id, ok := h.base.proxyIdentity(c)
	if !ok {
		return
	}
	raw, ok := h.base.body(c)
	if !ok {
		return
	}
	input, ok := decodePublicationPayload[api.NetworkProxyBundleReadback](c, raw)
	if !ok {
		return
	}
	if err := h.service.ServerReady(c.Request.Context(), id, "", input); err != nil {
		respondError(c, err)
		return
	}
	input.ReasonCode = ""
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, input)
}
func (h *PublicationHandler) Projection(c *gin.Context) {
	id, ok := h.base.identity(c)
	if !ok {
		return
	}
	if id.Kind != "endpoint" {
		respondError(c, apperrors.ErrAccessDenied)
		return
	}
	out, err := h.service.Projection(c.Request.Context(), id, "", c.Param("profileID"), c.Query("processEpoch"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, out)
}
func (h *PublicationHandler) ProjectionReceipt(c *gin.Context) {
	id, ok := h.base.identity(c)
	if !ok {
		return
	}
	if id.Kind != "endpoint" {
		respondError(c, apperrors.ErrAccessDenied)
		return
	}
	raw, ok := h.base.body(c)
	if !ok {
		return
	}
	input, ok := decodePublicationPayload[api.NetworkProxyProjectionApplied](c, raw)
	if !ok {
		return
	}
	if err := h.service.ProjectionApplied(c.Request.Context(), id, c.Param("profileID"), input); err != nil {
		respondError(c, err)
		return
	}
	input.ReasonCode = ""
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, input)
}

func decodePublicationPayload[T any](c *gin.Context, raw []byte) (T, bool) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil {
		respondError(c, apperrors.ErrInvalidArgument)
		return value, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		respondError(c, apperrors.ErrInvalidArgument)
		return value, false
	}
	return value, true
}

func (h *PublicationHandler) DirectProjection(c *gin.Context) {
	id, ok := h.base.identity(c)
	if !ok {
		return
	}
	if id.Kind != "endpoint" {
		respondError(c, apperrors.ErrAccessDenied)
		return
	}
	out, err := h.service.DirectProjection(c.Request.Context(), id, c.Param("profileID"), c.Query("processEpoch"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, out)
}
