package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DockerConfigurationService interface {
	RegisterHostConfiguration(context.Context, domainidentity.Principal, sohaapi.DockerHostRegistrationInput) (sohaapi.DockerConfigurationReceipt, error)
	PatchConfiguration(context.Context, domainidentity.Principal, string, string, sohaapi.DockerConfigurationPatchInput) (sohaapi.DockerConfigurationReceipt, error)
	DeleteConfiguration(context.Context, domainidentity.Principal, string, string, sohaapi.DockerConfigurationDeleteInput) (sohaapi.DockerConfigurationReceipt, error)
	GetConfigurationRequest(context.Context, domainidentity.Principal, string) (sohaapi.DockerConfigurationReceipt, error)
}

func dockerConfigurationServiceMissing(service DockerConfigurationService) bool {
	return service == nil || reflect.ValueOf(service).Kind() == reflect.Pointer && reflect.ValueOf(service).IsNil()
}

func (h *DockerHandler) RegisterHostConfiguration(c *gin.Context) {
	if h.configuration == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	var input sohaapi.DockerHostRegistrationInput
	if err := readDockerConfigurationInput(c, &input); err != nil {
		writeError(c, err)
		return
	}
	item, err := h.configuration.RegisterHostConfiguration(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	writeDockerConfigurationReceipt(c, item, err)
}
func (h *DockerHandler) PatchHostConfiguration(c *gin.Context) {
	h.patchConfiguration(c, "docker.host")
}
func (h *DockerHandler) PatchProjectConfiguration(c *gin.Context) {
	h.patchConfiguration(c, "docker.project")
}
func (h *DockerHandler) patchConfiguration(c *gin.Context, kind string) {
	if h.configuration == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	var input sohaapi.DockerConfigurationPatchInput
	if err := readDockerConfigurationInput(c, &input); err != nil {
		writeError(c, err)
		return
	}
	item, err := h.configuration.PatchConfiguration(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), kind, c.Param("id"), input)
	writeDockerConfigurationReceipt(c, item, err)
}
func (h *DockerHandler) DeleteHostConfiguration(c *gin.Context) {
	h.deleteConfiguration(c, "docker.host")
}
func (h *DockerHandler) DeleteProjectConfiguration(c *gin.Context) {
	h.deleteConfiguration(c, "docker.project")
}
func (h *DockerHandler) deleteConfiguration(c *gin.Context, kind string) {
	if h.configuration == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	var input sohaapi.DockerConfigurationDeleteInput
	if err := readDockerConfigurationInput(c, &input); err != nil {
		writeError(c, err)
		return
	}
	item, err := h.configuration.DeleteConfiguration(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), kind, c.Param("id"), input)
	writeDockerConfigurationReceipt(c, item, err)
}
func (h *DockerHandler) GetConfigurationRequest(c *gin.Context) {
	if h.configuration == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	item, err := h.configuration.GetConfigurationRequest(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("requestKey"))
	writeDockerConfigurationReceipt(c, item, err)
}
func writeDockerConfigurationReceipt(c *gin.Context, item sohaapi.DockerConfigurationReceipt, err error) {
	c.Header("Cache-Control", "no-store")
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func readDockerConfigurationInput(c *gin.Context, out any) error {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 || !utf8.Valid(raw) {
		return apperrors.ErrInvalidArgument
	}
	keys := json.NewDecoder(bytes.NewReader(raw))
	keys.UseNumber()
	if validateConfigurationJSON(keys, 0) != nil || keys.Decode(new(any)) != io.EOF {
		return apperrors.ErrInvalidArgument
	}
	if validateConfigurationTypedKeys(raw, reflect.TypeOf(out)) != nil {
		return apperrors.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

// encoding/json matches struct fields case-insensitively. Require the exact
// public spelling without restricting case-sensitive keys in config maps.
func validateConfigurationTypedKeys(raw json.RawMessage, target reflect.Type) error {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if reflect.PointerTo(target).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	switch target.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return apperrors.ErrInvalidArgument
		}
		fields := map[string]reflect.Type{}
		for index := range target.NumField() {
			field := target.Field(index)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if field.IsExported() && name != "-" && name != "" {
				fields[name] = field.Type
			}
		}
		for name, value := range object {
			field, known := fields[name]
			if !known || validateConfigurationTypedKeys(value, field) != nil {
				return apperrors.ErrInvalidArgument
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return apperrors.ErrInvalidArgument
		}
		for _, value := range values {
			if validateConfigurationTypedKeys(value, target.Elem()) != nil {
				return apperrors.ErrInvalidArgument
			}
		}
	}
	return nil
}

// Duplicate fields and null cannot be normalized into a different reviewed
// patch. Bounded nesting keeps private config parsing predictable.
func validateConfigurationJSON(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return apperrors.ErrInvalidArgument
	}
	token, err := decoder.Token()
	if err != nil || token == nil {
		return apperrors.ErrInvalidArgument
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return apperrors.ErrInvalidArgument
			}
			seen[name] = true
			if err := validateConfigurationJSON(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return apperrors.ErrInvalidArgument
		}
	case json.Delim('['):
		for decoder.More() {
			if err := validateConfigurationJSON(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return apperrors.ErrInvalidArgument
		}
	}
	return nil
}
