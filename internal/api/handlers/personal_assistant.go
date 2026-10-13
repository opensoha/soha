package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/copilot"
)

func (h *copilotPersonalAssistantHandler) EnsurePersonalAssistant(c *gin.Context) {
	item, err := h.service.EnsurePersonalAssistant(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *copilotPersonalAssistantHandler) UpdatePersonalAssistantAvatar(c *gin.Context) {
	var req struct {
		AvatarURL       *string `json:"avatarUrl"`
		AvatarPosterURL string  `json:"avatarPosterUrl,omitempty"`
		AvatarMotion    string  `json:"avatarMotion,omitempty"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 900000))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF || req.AvatarURL == nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid assistant image")
		return
	}
	input := domain.PersonalAssistantAvatar{AvatarURL: *req.AvatarURL, AvatarPosterURL: req.AvatarPosterURL, AvatarMotion: req.AvatarMotion}
	item, err := h.service.UpdatePersonalAssistantAvatar(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *copilotPersonalAssistantHandler) RenamePersonalAssistant(c *gin.Context) {
	// Keep the CRUD adapter compatible with the published SDK; contracts owns this shape.
	var req struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid assistant name")
		return
	}
	item, err := h.service.RenamePersonalAssistant(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req.Name, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *copilotPersonalAssistantHandler) ListPersonalAssistantTasks(c *gin.Context) {
	items, err := h.service.ListPersonalAssistantTasks(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *copilotPersonalAssistantHandler) ListPersonalAssistantArtifacts(c *gin.Context) {
	items, err := h.service.ListPersonalAssistantArtifacts(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
