package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *copilotPersonalEnvironmentHandler) ListPersonalEnvironmentTemplates(c *gin.Context) {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	items, err := h.service.ListPersonalEnvironmentTemplates(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, items)
}

func (h *copilotPersonalEnvironmentHandler) ExportPersonalEnvironmentData(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || !utf8.Valid(body) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var req sohaapi.PersonalEnvironmentDataExportRequest
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || req.AssistantID == "" || len(req.AssistantID) > 128 || req.EnvironmentVersion < 1 {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	file, err := os.CreateTemp("", "soha-personal-export-*")
	if err != nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	// Remove the directory entry immediately: a crash cannot retain private
	// export files. The open descriptor lasts only for this request.
	if err := os.Remove(file.Name()); err != nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	result, err := h.service.ExportPersonalEnvironmentData(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req, localeFromRequest(c.GetHeader("Accept-Language")), file)
	if err != nil {
		writeError(c, err)
		return
	}
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil || size != result.SizeBytes {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	c.Header("Content-Type", "application/x-tar")
	c.Header("Content-Disposition", `attachment; filename="personal-data.tar"`)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Assistant-ID", req.AssistantID)
	c.Header("X-Content-SHA256", strings.TrimPrefix(result.SHA256, "sha256:"))
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, file)
}

func (h *copilotPersonalEnvironmentHandler) GetPersonalEnvironment(c *gin.Context) {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	item, err := h.service.GetPersonalEnvironment(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *copilotPersonalEnvironmentHandler) ChangePersonalEnvironment(c *gin.Context) {
	var req sohaapi.PersonalEnvironmentActionRequest
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || !utf8.Valid(body) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal environment action")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF || json.Unmarshal(body, &fields) != nil || !req.Action.Valid() || (fields["templateId"] != nil && (req.Action != sohaapi.Wake || req.TemplateID == "")) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal environment action")
		return
	}
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	item, err := h.service.ChangePersonalEnvironment(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), string(req.Action), req.TemplateID, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *copilotPersonalEnvironmentHandler) DeletePersonalEnvironmentData(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || !utf8.Valid(body) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal data deletion confirmation")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var req sohaapi.PersonalEnvironmentDataDeletionRequest
	if decoder.Decode(&req) != nil || decoder.Decode(new(any)) != io.EOF || req.AssistantID == "" || req.EnvironmentVersion < 1 || req.ConfirmationName == "" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid personal data deletion confirmation")
		return
	}
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	item, err := h.service.DeletePersonalEnvironmentData(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
