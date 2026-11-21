package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	app "github.com/opensoha/soha/internal/application/networkaccess"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NetworkAccessNetworkService interface {
	ListAccessNetworks(context.Context, identity.Principal, string) ([]domain.AccessNetwork, error)
	SaveAccessNetwork(context.Context, identity.Principal, string, domain.AccessNetworkInput) (domain.AccessNetwork, error)
	DeleteAccessNetwork(context.Context, identity.Principal, string) error
	PreviewAccessNetworks(context.Context, identity.Principal, string, string, string, string, *domain.AccessNetworkInput) ([]app.AccessNetworkDecision, error)
	ListAccessNetworkChoices(context.Context, identity.Principal, string, bool) ([]domain.ConnectionOption, []app.BootstrapNetworkChoice, error)
}

func (h *NetworkAccessHandler) ListAccessNetworks(c *gin.Context) {
	if h.accessNetwork == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	items, err := h.accessNetwork.ListAccessNetworks(c.Request.Context(), principal(c), c.Query("siteId"))
	respondItems(c, items, err)
}

func (h *NetworkAccessHandler) CreateAccessNetwork(c *gin.Context) {
	h.saveAccessNetwork(c, "", http.StatusCreated)
}

func (h *NetworkAccessHandler) UpdateAccessNetwork(c *gin.Context) {
	h.saveAccessNetwork(c, c.Param("accessNetworkId"), http.StatusOK)
}

func (h *NetworkAccessHandler) saveAccessNetwork(c *gin.Context, id string, status int) {
	if h.accessNetwork == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	var input domain.AccessNetworkInput
	if !bindStrictNetworkJSON(c, &input, "invalid access network payload") {
		return
	}
	item, err := h.accessNetwork.SaveAccessNetwork(c.Request.Context(), principal(c), id, input)
	respondItem(c, status, item, err)
}

func (h *NetworkAccessHandler) DeleteAccessNetwork(c *gin.Context) {
	if h.accessNetwork == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	respondDelete(c, h.accessNetwork.DeleteAccessNetwork(c.Request.Context(), principal(c), c.Param("accessNetworkId")))
}

func (h *NetworkAccessHandler) PreviewAccessNetworks(c *gin.Context) {
	if h.accessNetwork == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	var input struct {
		SiteID               string                     `json:"siteId"`
		UserID               string                     `json:"userId"`
		DeviceID             string                     `json:"deviceId"`
		AuthenticationMethod string                     `json:"authenticationMethod"`
		Draft                *domain.AccessNetworkInput `json:"draft"`
	}
	if !bindStrictNetworkJSON(c, &input, "invalid access network payload") {
		return
	}
	c.Header("Cache-Control", "no-store")
	items, err := h.accessNetwork.PreviewAccessNetworks(c.Request.Context(), principal(c), input.SiteID, input.UserID, input.DeviceID, input.AuthenticationMethod, input.Draft)
	respondItems(c, items, err)
}
