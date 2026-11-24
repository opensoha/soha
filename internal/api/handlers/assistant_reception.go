package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	mesh "github.com/opensoha/soha/internal/application/assistantmesh"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
)

func (h *AssistantMeshHandler) ReceptionProfile(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	result, err := h.reception.Profile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	writeMeshAsset(c, http.StatusOK, result, err)
}

func (h *AssistantMeshHandler) SaveReceptionProfile(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		Open  *bool   `json:"open"`
		Away  *bool   `json:"away"`
		Intro *string `json:"intro"`
	}
	if !decodeMeshRequest(c, &input) {
		return
	}
	if input.Open == nil || input.Away == nil || input.Intro == nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "open, away and intro are required")
		return
	}
	result, err := h.reception.SaveProfile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c),
		domain.Profile{Open: *input.Open, Away: *input.Away, Intro: *input.Intro})
	writeMeshAsset(c, http.StatusOK, result, err)
}

func (h *AssistantMeshHandler) DiscoverAssistants(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		Query string `json:"query"`
	}
	if !decodeMeshRequest(c, &input) {
		return
	}
	items, err := h.reception.Discover(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input.Query)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AssistantMeshHandler) PreviewInquiry(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		Question string `json:"question"`
	}
	if !decodeMeshRequest(c, &input) {
		return
	}
	result, err := h.reception.Preview(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input.Question)
	writeMeshAsset(c, http.StatusOK, result, err)
}

func (h *AssistantMeshHandler) AskAssistant(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input mesh.InquiryInput
	if !decodeMeshRequest(c, &input) {
		return
	}
	result, err := h.reception.Ask(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if errors.Is(err, domain.ErrInquiryRateLimited) {
		apiresponse.Error(c, http.StatusTooManyRequests, "rate_limited", "Inquiry limit reached")
		return
	}
	if errors.Is(err, domain.ErrInquiryInboxFull) {
		apiresponse.Error(c, http.StatusConflict, "inquiry_inbox_full", "The recipient inbox is full")
		return
	}
	writeMeshAsset(c, http.StatusOK, result, err)
}

func (h *AssistantMeshHandler) ListInquiries(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	items, err := h.reception.ListInquiries(c.Request.Context(), apiMiddleware.PrincipalFromContext(c),
		c.DefaultQuery("direction", "sent"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AssistantMeshHandler) ReplyInquiry(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		Answer string `json:"answer"`
	}
	if !decodeMeshRequest(c, &input) {
		return
	}
	result, err := h.reception.Reply(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("inquiryId"), input.Answer)
	writeMeshAsset(c, http.StatusOK, result, err)
}

func (h *AssistantMeshHandler) ShareInquiryReply(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input mesh.SharedReplyInput
	if !decodeMeshRequest(c, &input) {
		return
	}
	result, err := h.reception.ReplyAndPublish(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("inquiryId"), input)
	writeMeshAsset(c, http.StatusOK, result, err)
}
