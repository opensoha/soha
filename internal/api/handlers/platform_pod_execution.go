package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	contractresource "github.com/opensoha/soha/contracts/resource"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type podExecutionService interface {
	ExecutePodArgv(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string, string, string, contractresource.PodArgvInput) (contractresource.PodExecutionReceipt, error)
	GetPodExecution(context.Context, domainidentity.Principal, string, string, string, string, string) (contractresource.PodExecutionReceipt, error)
}

func (h *podResourceHandler) ExecutePodArgv(c *gin.Context) {
	service, ok := h.diagnostics.(podExecutionService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	var input contractresource.PodArgvInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid Pod argv payload")
		return
	}
	input, err := input.Normalized()
	if err != nil || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "bounded argv and one original Idempotency-Key are required")
		return
	}
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(time.Duration(input.TimeoutSeconds+20) * time.Second))
	runCtx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(input.TimeoutSeconds+20)*time.Second)
	defer cancel()
	item, err := service.ExecutePodArgv(runCtx, apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("podName"), c.GetHeader("Idempotency-Key"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	writePodExecution(c, item)
}

func (h *podResourceHandler) GetPodExecution(c *gin.Context) {
	service, ok := h.diagnostics.(podExecutionService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	id := c.Param("executionID")
	if id == "" && len(c.Request.Header.Values("Idempotency-Key")) != 1 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "one original Idempotency-Key is required")
		return
	}
	item, err := service.GetPodExecution(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("podName"), id, c.GetHeader("Idempotency-Key"))
	if err != nil {
		writeError(c, err)
		return
	}
	writePodExecution(c, item)
}

func writePodExecution(c *gin.Context, item contractresource.PodExecutionReceipt) {
	status := http.StatusOK
	if item.Status == "running" {
		status = http.StatusAccepted
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, status, item)
}
