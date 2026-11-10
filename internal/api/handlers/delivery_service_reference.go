package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DeliveryServiceReferencePreviewService interface {
	PreviewEnvironmentServiceReference(context.Context, domainidentity.Principal, string, domaindelivery.ServiceReferencePreviewInput) (domaindelivery.ServiceReferencePreview, error)
}

func (h *DeliveryHandler) PreviewEnvironmentServiceReference(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	reader, ok := h.applications.(DeliveryServiceReferencePreviewService)
	if !ok {
		writeError(c, fmt.Errorf("%w: service reference preview is unavailable", apperrors.ErrUnsupportedOperation))
		return
	}
	var request struct {
		domaindelivery.ServiceReferencePreviewInput
		TargetIDs json.RawMessage `json:"sourceTargetIds"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid service reference preview payload")
		return
	}
	input := request.ServiceReferencePreviewInput
	if request.TargetIDs != nil {
		if string(request.TargetIDs) == "null" || json.Unmarshal(request.TargetIDs, &input.SourceTargetIDs) != nil || len(input.SourceTargetIDs) == 0 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "sourceTargetIds must be a nonempty array when provided")
			return
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "expected one service reference preview payload")
		return
	}
	item, err := reader.PreviewEnvironmentServiceReference(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationEnvironmentID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
