package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	"github.com/opensoha/soha/internal/domain/identity"
)

type ContentDLPService interface {
	Preview(context.Context, identity.Principal, string, domain.ShadowInput) (domain.ShadowResult, error)
	Get(context.Context, identity.Principal, string) (domain.Policy, error)
	Save(context.Context, identity.Principal, string, domain.SaveInput) (domain.Policy, error)
	Publish(context.Context, identity.Principal, string, domain.PublishInput) (domain.Policy, error)
	Events(context.Context, identity.Principal, string) ([]domain.Event, error)
}
type ContentDLPHandler struct{ service ContentDLPService }

func NewContentDLPHandler(service ContentDLPService) *ContentDLPHandler {
	return &ContentDLPHandler{service: service}
}
func (h *ContentDLPHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	item, err := h.service.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *ContentDLPHandler) Save(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input domain.SaveInput
	if !decodeDLPInput(c, &input) {
		return
	}
	if input.Draft.Detectors == nil || input.Draft.Rules == nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "DLP draft is required.")
		return
	}
	item, err := h.service.Save(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *ContentDLPHandler) Publish(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input domain.PublishInput
	if !decodeDLPInput(c, &input) {
		return
	}
	item, err := h.service.Publish(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *ContentDLPHandler) Events(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	items, err := h.service.Events(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"items": items})
}

func decodeDLPInput(c *gin.Context, out any) bool     { return decodeContentRequest(c, out, true) }
func decodeContentInput(c *gin.Context, out any) bool { return decodeContentRequest(c, out, false) }
func decodeContentRequest(c *gin.Context, out any, requiredPolicy bool) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil || requiredPolicy && !requiredDLPFields(data) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Invalid or missing DLP request fields.")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Invalid DLP request.")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Expected one DLP request object.")
		return false
	}
	return true
}

func requiredDLPFields(data []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields["expectedRevision"]) == 0 || string(fields["expectedRevision"]) == "null" {
		return false
	}
	if draft, ok := fields["draft"]; ok {
		var content map[string]json.RawMessage
		if json.Unmarshal(draft, &content) != nil {
			return false
		}
		for _, key := range []string{"enabled", "mode", "detectors", "rules"} {
			if len(content[key]) == 0 || string(content[key]) == "null" {
				return false
			}
		}
	}
	return true
}

func (h *ContentDLPHandler) Preview(c *gin.Context) {
	var in domain.ShadowInput
	defer func() {
		for _, sample := range in.Samples {
			clear(sample)
		}
	}()
	if !managedInput(c, &in) {
		return
	}
	result, err := h.service.Preview(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), in)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, 200, result)
}
