package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type CompanionService interface {
	GetProfile(context.Context, domainidentity.Principal) (domaincompanion.Profile, error)
	RecordInteraction(context.Context, domainidentity.Principal, string, domaincompanion.InteractionRequest) (domaincompanion.InteractionReceipt, bool, error)
	ResetProfile(context.Context, domainidentity.Principal, string, domaincompanion.ProfileResetRequest) (domaincompanion.Profile, error)
}

type CompanionHandler struct {
	service CompanionService
}

func NewCompanionHandler(service CompanionService) *CompanionHandler {
	return &CompanionHandler{service: service}
}

func (h *CompanionHandler) GetProfile(c *gin.Context) {
	profile, err := h.service.GetProfile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, profile)
}

func (h *CompanionHandler) RecordInteraction(c *gin.Context) {
	var req domaincompanion.InteractionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid companion interaction payload")
		return
	}
	receipt, created, err := h.service.RecordInteraction(
		c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.GetHeader("Idempotency-Key"), req,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	apiresponse.Item(c, status, receipt)
}

func (h *CompanionHandler) ResetProfile(c *gin.Context) {
	var req domaincompanion.ProfileResetRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid companion reset payload")
		return
	}
	profile, err := h.service.ResetProfile(
		c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.GetHeader("Idempotency-Key"), req,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, profile)
}
