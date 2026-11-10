package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type CopilotRuntimeProfileService interface {
	ListRuntimeProfileRevisions(context.Context, identity.Principal, string) ([]domain.RuntimeProfile, error)
	PublishRuntimeProfile(context.Context, identity.Principal, int64, domain.RuntimeProfile) (domain.RuntimeProfile, error)
	ListRuntimeProfiles(context.Context, identity.Principal) ([]domain.RuntimeProfile, error)
	SelectSessionRuntimeProfile(context.Context, identity.Principal, string, string) (domain.Session, error)
}

type copilotRuntimeProfileHandler struct{ service CopilotRuntimeProfileService }

type publishRuntimeProfileRequest struct {
	ExpectedRevision *int64 `json:"expectedRevision"`
	Name             string `json:"name"`
	Scope            string `json:"scope"`
	Enabled          *bool  `json:"enabled"`
	Assets           []struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Version  string `json:"version"`
		Required *bool  `json:"required"`
	} `json:"assets"`
	ModelPreferences *struct {
		PublicModel     string  `json:"publicModel"`
		ReasoningEffort *string `json:"reasoningEffort"`
	} `json:"modelPreferences"`
	MaxOutputTokens *int `json:"maxOutputTokens"`
}

func (r publishRuntimeProfileRequest) profile(id string) (domain.RuntimeProfile, error) {
	if r.ExpectedRevision == nil || r.Enabled == nil || r.Assets == nil {
		return domain.RuntimeProfile{}, apperrors.ErrInvalidArgument
	}
	profile := domain.RuntimeProfile{ID: id, Name: r.Name, Scope: r.Scope, Enabled: *r.Enabled, Assets: make([]domain.RuntimeAssetRef, 0, len(r.Assets))}
	for _, asset := range r.Assets {
		if asset.Required == nil {
			return domain.RuntimeProfile{}, apperrors.ErrInvalidArgument
		}
		profile.Assets = append(profile.Assets, domain.RuntimeAssetRef{Kind: asset.Kind, ID: asset.ID, Version: asset.Version, Required: *asset.Required})
	}
	if r.ModelPreferences != nil {
		profile.ModelPreferences.PublicModel = r.ModelPreferences.PublicModel
		if r.ModelPreferences.ReasoningEffort != nil {
			if *r.ModelPreferences.ReasoningEffort == "" {
				return domain.RuntimeProfile{}, apperrors.ErrInvalidArgument
			}
			profile.ModelPreferences.ReasoningEffort = *r.ModelPreferences.ReasoningEffort
		}
	}
	if r.MaxOutputTokens != nil {
		if *r.MaxOutputTokens < 1 {
			return domain.RuntimeProfile{}, apperrors.ErrInvalidArgument
		}
		profile.MaxOutputTokens = *r.MaxOutputTokens
	}
	return profile, nil
}

func (h *copilotRuntimeProfileHandler) ListRuntimeProfileRevisions(c *gin.Context) {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	items, err := h.service.ListRuntimeProfileRevisions(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("profileId"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *copilotRuntimeProfileHandler) PublishRuntimeProfile(c *gin.Context) {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	var input publishRuntimeProfileRequest
	if !bindRuntimeProfileJSON(c, &input) {
		return
	}
	profile, err := input.profile(c.Param("profileId"))
	if err != nil {
		writeError(c, err)
		return
	}
	item, err := h.service.PublishRuntimeProfile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), *input.ExpectedRevision, profile)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *copilotRuntimeProfileHandler) ListRuntimeProfiles(c *gin.Context) {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	items, err := h.service.ListRuntimeProfiles(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *copilotRuntimeProfileHandler) SelectSessionRuntimeProfile(c *gin.Context) {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	var input struct {
		ProfileID *string `json:"profileId"`
	}
	if !bindRuntimeProfileJSON(c, &input) {
		return
	}
	if input.ProfileID == nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	item, err := h.service.SelectSessionRuntimeProfile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sessionID"), *input.ProfileID)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func bindRuntimeProfileJSON(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(c, apperrors.ErrInvalidArgument)
		return false
	}
	return true
}
