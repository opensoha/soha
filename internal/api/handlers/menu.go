package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmenu "github.com/opensoha/soha/internal/domain/menu"
)

type MenuService interface {
	ListAll(context.Context, domainidentity.Principal) ([]domainmenu.Record, error)
	Get(context.Context, domainidentity.Principal, string) (domainmenu.Record, error)
	ListVisible(context.Context, domainidentity.Principal) ([]domainmenu.Record, error)
	Create(context.Context, domainidentity.Principal, domainmenu.Input) (domainmenu.Record, error)
	Update(context.Context, domainidentity.Principal, string, domainmenu.Input) (domainmenu.Record, error)
	Delete(context.Context, domainidentity.Principal, string) error
}

type MenuHandler struct {
	service MenuService
}

func NewMenuHandler(service MenuService) *MenuHandler {
	return &MenuHandler{service: service}
}

func (h *MenuHandler) ListAll(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAll(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MenuHandler) Get(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Get(c.Request.Context(), principal, c.Param("menuID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MenuHandler) ListVisible(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListVisible(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MenuHandler) Create(c *gin.Context) {
	var req dto.UpsertMenuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid menu payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Create(c.Request.Context(), principal, domainmenu.Input{
		ID:        req.ID,
		ParentID:  req.ParentID,
		Path:      req.Path,
		LabelZH:   req.LabelZH,
		LabelEN:   req.LabelEN,
		IconKey:   req.IconKey,
		Section:   req.Section,
		SortOrder: req.SortOrder,
		Enabled:   req.Enabled,
		RoleIDs:   req.RoleIDs,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *MenuHandler) Update(c *gin.Context) {
	var req dto.UpsertMenuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid menu payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Update(c.Request.Context(), principal, c.Param("menuID"), domainmenu.Input{
		ID:        req.ID,
		ParentID:  req.ParentID,
		Path:      req.Path,
		LabelZH:   req.LabelZH,
		LabelEN:   req.LabelEN,
		IconKey:   req.IconKey,
		Section:   req.Section,
		SortOrder: req.SortOrder,
		Enabled:   req.Enabled,
		RoleIDs:   req.RoleIDs,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MenuHandler) Delete(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.Delete(c.Request.Context(), principal, c.Param("menuID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
