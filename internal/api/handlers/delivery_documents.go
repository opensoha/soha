package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type DeliveryDocumentService interface {
	Preview(context.Context, domainidentity.Principal, domaindocument.PreviewInput) (domaindocument.Preview, error)
	Apply(context.Context, domainidentity.Principal, string, domaindocument.ApplyInput) (domaindocument.Import, error)
	Export(context.Context, domainidentity.Principal, string, string, int64, string) (domaindocument.Export, error)
}

type DeliveryDocumentHandler struct{ service DeliveryDocumentService }

func NewDeliveryDocumentHandler(service DeliveryDocumentService) *DeliveryDocumentHandler {
	return &DeliveryDocumentHandler{service: service}
}

func (h *DeliveryDocumentHandler) Preview(c *gin.Context) {
	var input domaindocument.PreviewInput
	if !decodeDeliveryDocumentRequest(c, &input, 16<<20) {
		return
	}
	item, err := h.service.Preview(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryDocumentHandler) Apply(c *gin.Context) {
	var input domaindocument.ApplyInput
	if !decodeDeliveryDocumentRequest(c, &input, 4096) {
		return
	}
	item, err := h.service.Apply(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("previewID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryDocumentHandler) Export(c *gin.Context) {
	version := int64(0)
	if value, supplied := c.GetQuery("version"); supplied {
		var err error
		version, err = strconv.ParseInt(value, 10, 64)
		if err != nil || version < 1 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "version must be a positive integer")
			return
		}
	}
	item, err := h.service.Export(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("kind"), c.Param("objectID"), version, c.DefaultQuery("format", "yaml"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func decodeDeliveryDocumentRequest(c *gin.Context, input any, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil || decoder.Decode(new(any)) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid or oversized document request")
		return false
	}
	return true
}
