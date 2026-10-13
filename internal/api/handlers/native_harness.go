package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type CopilotNativeRunService interface {
	ListAgentRunEvents(context.Context, domainidentity.Principal, string, int64) (domaincopilot.NativeEventPage, error)
	ResumeAgentRun(context.Context, domainidentity.Principal, string, string) (domaincopilot.AgentRun, error)
	FindNativeRequest(context.Context, domainidentity.Principal, string, string) (domaincopilot.AgentRun, error)
}

func (h *copilotAgentRunHandler) FindNativeRequest(c *gin.Context) {
	service, ok := h.service.(CopilotNativeRunService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	run, err := service.FindNativeRequest(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sessionID"), c.Query("requestId"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, run)
}

func (h *copilotAgentRunHandler) ListAgentRunEvents(c *gin.Context) {
	service, ok := h.service.(CopilotNativeRunService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	sequence, err := strconv.ParseInt(c.DefaultQuery("afterSequence", "0"), 10, 64)
	if err != nil || sequence < 0 {
		writeError(c, fmt.Errorf("%w: invalid event cursor", apperrors.ErrInvalidArgument))
		return
	}
	page, err := service.ListAgentRunEvents(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("runID"), sequence)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}

func (h *copilotAgentRunHandler) ResumeAgentRun(c *gin.Context) {
	service, ok := h.service.(CopilotNativeRunService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	var input *sohaapi.AgentRunResumeRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input == nil || len([]rune(input.Input)) > 16384 || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(c, fmt.Errorf("%w: invalid bounded resume input", apperrors.ErrInvalidArgument))
		return
	}
	run, err := service.ResumeAgentRun(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("runID"), input.Input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, run)
}
