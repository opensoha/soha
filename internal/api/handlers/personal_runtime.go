package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *PersonalGuestPreparationHandler) Runtime(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s, ok := h.service.(interface {
		Runtime(context.Context, identity.Principal, sohaapi.PersonalGuestRuntimeInput) (domain.PersonalRuntime, error)
	})
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
	var input sohaapi.PersonalGuestRuntimeInput
	if err != nil || !utf8.Valid(body) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !runtimeRequiredFields(body, input) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	result, err := s.Runtime(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, result)
}

func runtimeRequiredFields(body []byte, input sohaapi.PersonalGuestRuntimeInput) bool {
	fields, valid := runtimeObject(body, "environmentVersion", "idempotencyKey", "kind", "argv", "file", "browser")
	if !valid {
		return false
	}
	if !validRuntimeNestedInput(fields, input) {
		return false
	}
	for _, key := range []string{"environmentVersion", "idempotencyKey", "kind"} {
		if len(fields[key]) == 0 || string(fields[key]) == "null" {
			return false
		}
	}
	if input.Kind == "file_write" {
		var file map[string]json.RawMessage
		if json.Unmarshal(fields["file"], &file) != nil || len(file["contentBase64"]) == 0 || string(file["contentBase64"]) == "null" {
			return false
		}
	}
	if input.Kind == "browser" && input.Browser != nil && input.Browser.Action == "pointer" {
		var browser map[string]json.RawMessage
		if json.Unmarshal(fields["browser"], &browser) != nil {
			return false
		}
		for _, key := range []string{"x", "y"} {
			if len(browser[key]) == 0 || string(browser[key]) == "null" {
				return false
			}
		}
	}
	return true
}

func runtimeObject(raw []byte, keys ...string) (map[string]json.RawMessage, bool) {
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || fields[name] != nil || !slices.Contains(keys, name) {
			return nil, false
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return nil, false
		}
		fields[name] = value
	}
	if _, err = d.Token(); err != nil || d.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return fields, true
}

func (h *PersonalGuestPreparationHandler) GetRuntime(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s, ok := h.service.(interface {
		GetRuntime(context.Context, identity.Principal, string) (domain.PersonalRuntime, error)
	})
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	result, err := s.GetRuntime(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("operationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}

func (h *PersonalGuestPreparationHandler) WorkspaceRuntime(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	s, ok := h.service.(interface {
		WorkspaceRuntime(context.Context, identity.Principal) (domain.PersonalWorkspaceRuntime, error)
	})
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	view, err := s.WorkspaceRuntime(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, view)
}

func validRuntimeNestedInput(fields map[string]json.RawMessage, input sohaapi.PersonalGuestRuntimeInput) bool {
	var valid bool
	if raw, ok := fields["file"]; ok {
		if _, valid = runtimeObject(raw, "path", "contentBase64", "expectedSHA256"); !valid {
			return false
		}
	}
	if raw, ok := fields["browser"]; ok {
		var fields map[string]json.RawMessage
		fields, valid = runtimeObject(raw, "action", "controlId", "generation", "url", "selector", "text", "x", "y")
		if !valid {
			return false
		}
		if input.Browser != nil && input.Browser.Action != "pointer" && (fields["x"] != nil || fields["y"] != nil) {
			return false
		}
	}
	return true
}
