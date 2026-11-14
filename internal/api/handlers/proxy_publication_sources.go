package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"io"
	"net/http"
	"strconv"
)

type ProxySources interface {
	List(context.Context, identity.Principal, string) (api.NetworkProxySourceList, error)
	Get(context.Context, identity.Principal, string, string) (api.NetworkProxySource, error)
	Put(context.Context, identity.Principal, string, string, api.NetworkProxySourceInput) (api.NetworkProxySource, error)
	Delete(context.Context, identity.Principal, string, string, int64) error
	Preview(context.Context, identity.Principal, string, api.NetworkProxySourceInput) (api.NetworkProxyConversionPreview, error)
}
type ProxyPublicationSourcesHandler struct{ service ProxySources }

func NewProxyPublicationSourcesHandler(service ProxySources) *ProxyPublicationSourcesHandler {
	return &ProxyPublicationSourcesHandler{service: service}
}
func publicationError(err error) error {
	var conversion *domain.ConversionError
	if errors.As(err, &conversion) {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, conversion.Code, "Proxy source rejected at "+conversion.Position, "代理源转换被拒绝："+conversion.Position)
	}
	return err
}
func (h *ProxyPublicationSourcesHandler) List(c *gin.Context) {
	out, err := h.service.List(c.Request.Context(), principal(c), c.Param("instanceID"))
	respondItem(c, http.StatusOK, out, publicationError(err))
}
func (h *ProxyPublicationSourcesHandler) Get(c *gin.Context) {
	out, err := h.service.Get(c.Request.Context(), principal(c), c.Param("instanceID"), c.Param("sourceID"))
	respondItem(c, http.StatusOK, out, publicationError(err))
}
func (h *ProxyPublicationSourcesHandler) Put(c *gin.Context) {
	var input api.NetworkProxySourceInput
	if !bindProxyPublicationJSON(c, &input) {
		return
	}
	out, err := h.service.Put(c.Request.Context(), principal(c), c.Param("instanceID"), c.Param("sourceID"), input)
	respondItem(c, http.StatusOK, out, publicationError(err))
}
func (h *ProxyPublicationSourcesHandler) Preview(c *gin.Context) {
	var input api.NetworkProxySourceInput
	if !bindProxyPublicationJSON(c, &input) {
		return
	}
	out, err := h.service.Preview(c.Request.Context(), principal(c), c.Param("instanceID"), input)
	respondItem(c, http.StatusOK, out, publicationError(err))
}
func (h *ProxyPublicationSourcesHandler) Delete(c *gin.Context) {
	revision, err := strconv.ParseInt(c.Query("expectedRevision"), 10, 64)
	if err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	err = h.service.Delete(c.Request.Context(), principal(c), c.Param("instanceID"), c.Param("sourceID"), revision)
	if err != nil {
		writeError(c, publicationError(err))
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ProxyPublicationSourcesHandler) Create(c *gin.Context) {
	var input api.NetworkProxySourceInput
	if !bindProxyPublicationJSON(c, &input) {
		return
	}
	if input.ExpectedRevision != 0 {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	out, err := h.service.Put(c.Request.Context(), principal(c), c.Param("instanceID"), uuid.NewString(), input)
	respondItem(c, http.StatusAccepted, out, publicationError(err))
}

func bindProxyPublicationJSON(c *gin.Context, destination any) bool {
	if c.ContentType() != "application/json" {
		c.Status(http.StatusUnsupportedMediaType)
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(c, apperrors.ErrInvalidArgument)
		return false
	}
	return true
}
