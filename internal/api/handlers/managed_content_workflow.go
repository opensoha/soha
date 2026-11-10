package handlers

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/middleware"
	"github.com/opensoha/soha/internal/api/response"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
)

type ManagedContentWorkflowService interface {
	NetworkState(context.Context, identity.Principal, string) (d.NetworkHold, error)
	Authorize(context.Context, identity.Principal, string, string, string) (dlp.Authorization, error)
	Appeal(context.Context, identity.Principal, string, string) (d.Record, error)
	Review(context.Context, identity.Principal, string, d.ReviewInput) (d.Record, error)
}
type ManagedContentWorkflowHandler struct{ service ManagedContentWorkflowService }

func NewManagedContentWorkflowHandler(s ManagedContentWorkflowService) *ManagedContentWorkflowHandler {
	return &ManagedContentWorkflowHandler{s}
}
func (h *ManagedContentWorkflowHandler) Authorize(c *gin.Context) {
	var in struct {
		Action string `json:"action"`
	}
	if !managedInput(c, &in) {
		return
	}
	result, err := h.service.Authorize(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("recordID"), in.Action, middleware.AccessContextFromContext(c).SessionID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentWorkflowHandler) Appeal(c *gin.Context) {
	var in struct {
		Reason string `json:"reasonCode"`
	}
	if !managedInput(c, &in) {
		return
	}
	result, err := h.service.Appeal(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("recordID"), in.Reason)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentWorkflowHandler) Review(c *gin.Context) {
	var in d.ReviewInput
	if !managedInput(c, &in) {
		return
	}
	result, err := h.service.Review(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("recordID"), in)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}

func (h *ManagedContentWorkflowHandler) NetworkState(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	result, err := h.service.NetworkState(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("recordID"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
