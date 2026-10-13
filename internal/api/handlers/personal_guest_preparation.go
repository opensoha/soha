package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	identity "github.com/opensoha/soha/internal/domain/identity"
	virtualization "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type PersonalGuestPreparationService interface {
	Prepare(context.Context, identity.Principal, sohaapi.PersonalGuestPreparationInput) (sohaapi.PersonalGuestPreparation, error)
}

type PersonalGuestPreparationHandler struct {
	service PersonalGuestPreparationService
}

func (h *PersonalGuestPreparationHandler) Tools(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s, ok := h.service.(interface {
		Tools(context.Context, identity.Principal, sohaapi.PersonalGuestToolsInput) (virtualization.Task, error)
	})
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	var input sohaapi.PersonalGuestToolsInput
	body := http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	result, err := s.Tools(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(result))
}

func (h *PersonalGuestPreparationHandler) Provision(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s, ok := h.service.(interface {
		Provision(context.Context, identity.Principal, sohaapi.PersonalGuestPreparationInput) (virtualization.Task, error)
	})
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	if err != nil || !utf8.Valid(body) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	input, err := decodeGuestPreparation(body)
	if err != nil {
		writeError(c, err)
		return
	}
	result, err := s.Provision(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(result))
}

func (h *PersonalGuestPreparationHandler) Reconcile(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s, ok := h.service.(interface {
		Reconcile(context.Context, identity.Principal, string, sohaapi.PersonalGuestPreparationInput) (virtualization.Task, error)
	})
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	if err != nil || !utf8.Valid(body) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	input, err := decodeGuestPreparation(body)
	if err != nil {
		writeError(c, err)
		return
	}
	result, err := s.Reconcile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("operationID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(result))
}

// A nil service denotes the explicitly disabled preparation capability.
func NewPersonalGuestPreparationHandler(service PersonalGuestPreparationService) *PersonalGuestPreparationHandler {
	if service != nil && reflect.ValueOf(service).Kind() == reflect.Pointer && reflect.ValueOf(service).IsNil() {
		service = nil
	}
	return &PersonalGuestPreparationHandler{service: service}
}

func (h *PersonalGuestPreparationHandler) Prepare(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	if err != nil || !utf8.Valid(body) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	input, err := decodeGuestPreparation(body)
	if err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	result, err := h.service.Prepare(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, result)
}

func decodeGuestPreparation(body []byte) (sohaapi.PersonalGuestPreparationInput, error) {
	var input sohaapi.PersonalGuestPreparationInput
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return input, apperrors.ErrInvalidArgument
	}
	seen := make(map[string]bool, 2)
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return input, apperrors.ErrInvalidArgument
		}
		seen[name] = true
		switch name {
		case "environmentVersion":
			err = decoder.Decode(&input.EnvironmentVersion)
		case "csrPEM":
			err = decoder.Decode(&input.CsrPEM)
		default:
			return input, apperrors.ErrInvalidArgument
		}
		if err != nil {
			return input, apperrors.ErrInvalidArgument
		}
	}
	if _, err := decoder.Token(); err != nil || decoder.Decode(new(any)) != io.EOF || input.EnvironmentVersion < 1 || input.EnvironmentVersion > guest.MaxCounter || len(input.CsrPEM) < 1 || len(input.CsrPEM) > 4096 {
		return input, apperrors.ErrInvalidArgument
	}
	return input, nil
}
