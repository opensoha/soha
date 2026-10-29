package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/domain/identity"
)

type ContentDLPWorkflowService interface {
	Activity(context.Context, identity.Principal, string) ([]domain.Case, error)
	Case(context.Context, identity.Principal, string, string) (domain.Case, error)
	Authorize(context.Context, identity.Principal, string, string, domain.AuthorizationInput) (domain.Authorization, error)
	Appeal(context.Context, identity.Principal, string, string, domain.AppealInput) (domain.Appeal, error)
	Review(context.Context, identity.Principal, string, string, domain.ReviewInput) (domain.Appeal, error)
}
type ContentDLPWorkflowHandler struct{ service ContentDLPWorkflowService }

func NewContentDLPWorkflowHandler(s ContentDLPWorkflowService) *ContentDLPWorkflowHandler {
	return &ContentDLPWorkflowHandler{service: s}
}
func (h *ContentDLPWorkflowHandler) Activity(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	items, err := h.service.Activity(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"items": items})
}
func (h *ContentDLPWorkflowHandler) Case(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	item, err := h.service.Case(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), c.Param("eventID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *ContentDLPWorkflowHandler) Authorize(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input domain.AuthorizationInput
	if !decodeContentInput(c, &input) {
		return
	}
	input.SessionID = apiMiddleware.AccessContextFromContext(c).SessionID
	item, err := h.service.Authorize(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), c.Param("eventID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *ContentDLPWorkflowHandler) Appeal(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input domain.AppealInput
	if !decodeContentInput(c, &input) {
		return
	}
	item, err := h.service.Appeal(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), c.Param("eventID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *ContentDLPWorkflowHandler) Review(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input domain.ReviewInput
	if !decodeContentInput(c, &input) {
		return
	}
	item, err := h.service.Review(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), c.Param("eventID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) ReadProjectVolumeTransfer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		ServiceName   string `json:"serviceName"`
		Target        string `json:"target"`
		Path          string `json:"path"`
		LimitBytes    int64  `json:"limitBytes"`
		Authorization string `json:"authorization"`
	}
	if !decodeContentInput(c, &input) {
		return
	}
	if input.Authorization != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(input.Authorization)
		if err != nil || len(decoded) != 32 || len(input.Authorization) != 43 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Invalid transfer authorization.")
			return
		}
	}
	file, err := h.projectStorage.ReadProjectVolumeFile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), domaindocker.ProjectVolumeFileReadInput{SessionID: apiMiddleware.AccessContextFromContext(c).SessionID, ServiceName: input.ServiceName, Target: input.Target, Path: input.Path, LimitBytes: input.LimitBytes, Authorization: input.Authorization})
	var denied *appdlp.Denial
	if errors.As(err, &denied) {
		apiresponse.Item(c, http.StatusOK, struct {
			Decision domain.Decision `json:"decision"`
		}{denied.Decision})
		return
	}
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, struct {
		File     domaindocker.ProjectVolumeFileContent `json:"file"`
		Decision *domain.Decision                      `json:"decision,omitempty"`
	}{file, file.DLP})
}
