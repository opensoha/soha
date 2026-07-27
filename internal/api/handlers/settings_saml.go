package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func (h *SettingsHandler) ListSAMLLoginSources(c *gin.Context) {
	items, err := h.saml.ListSAMLLoginSources(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *SettingsHandler) GetSAMLLoginSource(c *gin.Context) {
	item, err := h.saml.GetSAMLLoginSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("samlLoginSourceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) CreateSAMLLoginSource(c *gin.Context) {
	var request sohaapi.SAMLLoginSourceInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid SAML login source")
		return
	}
	item, err := h.saml.CreateSAMLLoginSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *SettingsHandler) UpdateSAMLLoginSource(c *gin.Context) {
	var request sohaapi.SAMLLoginSourceInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid SAML login source")
		return
	}
	item, err := h.saml.UpdateSAMLLoginSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("samlLoginSourceID"), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) DeleteSAMLLoginSource(c *gin.Context) {
	if err := h.saml.DeleteSAMLLoginSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("samlLoginSourceID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *SettingsHandler) ValidateSAMLMetadata(c *gin.Context) {
	var request sohaapi.SAMLMetadataInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid SAML metadata")
		return
	}
	item, err := h.saml.ValidateSAMLMetadata(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SettingsHandler) ImportSAMLLoginSourceMetadata(c *gin.Context) {
	var request sohaapi.SAMLMetadataImportRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid SAML metadata import")
		return
	}
	item, err := h.saml.ImportSAMLLoginSourceMetadata(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
