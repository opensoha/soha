package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	ec "github.com/opensoha/soha-contracts/endpointcontrol"
	middleware "github.com/opensoha/soha/internal/api/middleware"
	response "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/domain/identity"
)

type EndpointControlsService interface {
	Get(context.Context, identity.Principal, string) (ec.View, error)
	Save(context.Context, identity.Principal, string, ec.SaveInput) (ec.Policy, error)
	Publish(context.Context, identity.Principal, string, ec.PublishInput) (ec.Policy, error)
}
type EndpointControlsHandler struct{ service EndpointControlsService }

func NewEndpointControlsHandler(s EndpointControlsService) *EndpointControlsHandler {
	return &EndpointControlsHandler{service: s}
}
func (h *EndpointControlsHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	v, err := h.service.Get(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("deviceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, v)
}
func (h *EndpointControlsHandler) Save(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var in struct {
		ExpectedRevision *int64 `json:"expectedRevision"`
		Draft            *struct {
			USB       *bool `json:"usb"`
			Print     *bool `json:"print"`
			Clipboard *bool `json:"clipboard"`
		} `json:"draft"`
	}
	if !decodeContentInput(c, &in) {
		return
	}
	if in.ExpectedRevision == nil || in.Draft == nil || in.Draft.USB == nil || in.Draft.Print == nil || in.Draft.Clipboard == nil {
		response.Error(c, 400, "invalid_argument", "All channel decisions and expectedRevision are required.")
		return
	}
	v, err := h.service.Save(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("deviceID"), ec.SaveInput{ExpectedRevision: *in.ExpectedRevision, Draft: ec.Controls{USB: *in.Draft.USB, Print: *in.Draft.Print, Clipboard: *in.Draft.Clipboard}})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, v)
}
func (h *EndpointControlsHandler) Publish(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var in ec.PublishInput
	if !decodeContentInput(c, &in) {
		return
	}
	v, err := h.service.Publish(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("deviceID"), in)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, v)
}
