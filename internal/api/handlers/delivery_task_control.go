package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

// ControlExecutionTask is the action-scoped protocol used by native clients.
// Legacy cancel/retry routes share the same application methods, but older
// servers that lack this route must not receive a client-side fallback.
func (h *DeliveryHandler) ControlExecutionTask(c *gin.Context) {
	req, ok := decodeExecutionTaskControl(c)
	if !ok {
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	input := domaindelivery.ExecutionTaskActionInput{Reason: req.Reason}
	var task domaindelivery.ExecutionTask
	var err error
	switch req.Action {
	case sohaapi.ExecutionTaskControlCancel:
		task, err = h.actions.CancelExecutionTask(c.Request.Context(), principal, c.Param("taskID"), input)
	case sohaapi.ExecutionTaskControlRetry:
		task, err = h.actions.RetryExecutionTask(c.Request.Context(), principal, c.Param("taskID"), input)
	}
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, domaindelivery.PublicExecutionTask(task))
}

func decodeExecutionTaskControl(c *gin.Context) (sohaapi.ExecutionTaskControlInput, bool) {
	var req sohaapi.ExecutionTaskControlInput
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		apiresponse.Error(c, http.StatusRequestEntityTooLarge, "payload_too_large", "execution task control exceeds 32 KiB")
		return req, false
	}
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&req)
		if err == nil && decoder.Decode(new(any)) != io.EOF {
			err = errors.New("multiple JSON values")
		}
	}
	if err != nil || !executionTaskControlValid(raw, req) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "control requires action cancel or retry and an optional UTF-8 reason of at most 2048 characters")
		return req, false
	}
	return req, true
}

func executionTaskControlValid(raw []byte, req sohaapi.ExecutionTaskControlInput) bool {
	if !utf8.Valid(raw) || utf8.RuneCountInString(req.Reason) > 2048 {
		return false
	}
	if req.Action != sohaapi.ExecutionTaskControlCancel && req.Action != sohaapi.ExecutionTaskControlRetry {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}
