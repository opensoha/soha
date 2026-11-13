package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type originalHealingService interface {
	CreateOriginalHealingRequest(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string, string) (domainalert.HealingOriginalReceipt, error)
	GetOriginalHealingRequest(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string) (domainalert.HealingOriginalReceipt, error)
}

func (h *healingRunHandler) CreateOriginalHealingRequest(c *gin.Context) {
	service, ok := h.service.(originalHealingService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	input, err := decodeOriginalHealingInput(c.Request.Body)
	if err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	item, err := service.CreateOriginalHealingRequest(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("eventID"), input.PolicyID, input.IdempotencyKey.String())
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusCreated, item)
}

func decodeOriginalHealingInput(reader io.Reader) (sohaapi.HealingOriginalRequestInput, error) {
	var input sohaapi.HealingOriginalRequestInput
	body, err := io.ReadAll(io.LimitReader(reader, 4097))
	if err != nil || len(body) > 4096 || !utf8.Valid(body) {
		return input, apperrors.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return input, apperrors.ErrInvalidArgument
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return input, apperrors.ErrInvalidArgument
		}
		key, ok := token.(string)
		if !ok || (key != "idempotencyKey" && key != "policyId") || seen[key] {
			return input, apperrors.ErrInvalidArgument
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return input, apperrors.ErrInvalidArgument
		}
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return input, apperrors.ErrInvalidArgument
	}
	if decoder.Decode(new(any)) != io.EOF || len(seen) != 2 || json.Unmarshal(body, &input) != nil {
		return input, apperrors.ErrInvalidArgument
	}
	return input, nil
}

func (h *healingRunHandler) GetOriginalHealingRequest(c *gin.Context) {
	service, ok := h.service.(originalHealingService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	item, err := service.GetOriginalHealingRequest(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("eventID"), c.Param("requestID"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, item)
}
