package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	mesh "github.com/opensoha/soha/internal/application/assistantmesh"
	domesh "github.com/opensoha/soha/internal/domain/assistantmesh"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
)

type AssistantMeshDraftSource interface {
	AssistantMeshDraftFromSession(context.Context, identity.Principal, string, string, string, string, []string) (domesh.AssetInput, error)
	LaunchAssistantMeshMCP(context.Context, identity.Principal, string, string, string, domaincopilot.AssistantMeshMCP) (string, error)
}

type AssistantMeshHandler struct {
	service      *mesh.Service
	source       AssistantMeshDraftSource
	mcp          *mesh.MCPService
	reception    *mesh.Reception
	delegations  *mesh.Delegations
	inquiryAudit *mesh.InquiryAudit
	inheritance  *mesh.Inheritance
}

func NewAssistantMeshHandler(service *mesh.Service, source AssistantMeshDraftSource, mcp *mesh.MCPService, reception *mesh.Reception, delegations *mesh.Delegations, inquiryAudit *mesh.InquiryAudit, inheritance *mesh.Inheritance) *AssistantMeshHandler {
	return &AssistantMeshHandler{service: service, source: source, mcp: mcp, reception: reception, delegations: delegations, inquiryAudit: inquiryAudit, inheritance: inheritance}
}

func (h *AssistantMeshHandler) List(c *gin.Context) {
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid offset")
		return
	}
	if c.Query("kind") == "mcp" {
		result, err := h.mcp.List(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.DefaultQuery("view", "mine"), offset)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.JSON(c, http.StatusOK, result)
		return
	}
	result, err := h.service.List(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.DefaultQuery("view", "mine"), c.Query("kind"), offset)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *AssistantMeshHandler) Get(c *gin.Context) {
	if c.Query("kind") == "mcp" {
		item, err := h.mcp.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("assetId"))
		writeMeshAsset(c, http.StatusOK, item, err)
		return
	}
	item, err := h.service.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("assetId"))
	writeMeshAsset(c, http.StatusOK, item, err)
}

func (h *AssistantMeshHandler) Create(c *gin.Context) {
	var input domesh.AssetInput
	if !decodeMeshRequest(c, &input) {
		return
	}
	if input.Kind == "mcp" {
		item, err := h.mcp.Create(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
		writeMeshAsset(c, http.StatusCreated, item, err)
		return
	}
	item, err := h.service.Create(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	writeMeshAsset(c, http.StatusCreated, item, err)
}

func (h *AssistantMeshHandler) Extract(c *gin.Context) {
	var input struct {
		Kind       string   `json:"kind"`
		Title      string   `json:"title"`
		SessionID  string   `json:"sessionId"`
		RunID      string   `json:"runId"`
		MessageIDs []string `json:"messageIds"`
	}
	if !decodeMeshRequest(c, &input) {
		return
	}
	ctx, principal := c.Request.Context(), apiMiddleware.PrincipalFromContext(c)
	draft, err := h.source.AssistantMeshDraftFromSession(ctx, principal, input.SessionID, input.RunID, input.Kind, input.Title, input.MessageIDs)
	if err != nil {
		writeError(c, err)
		return
	}
	item, err := h.service.Create(ctx, principal, draft)
	writeMeshAsset(c, http.StatusCreated, item, err)
}

func (h *AssistantMeshHandler) Update(c *gin.Context) {
	var input struct {
		Input          domesh.AssetInput `json:"input"`
		ExpectedDigest string            `json:"expectedDigest"`
	}
	if !decodeMeshRequest(c, &input) {
		return
	}
	item, err := h.service.Update(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("assetId"), input.ExpectedDigest, input.Input)
	writeMeshAsset(c, http.StatusOK, item, err)
}

func (h *AssistantMeshHandler) Act(c *gin.Context) {
	var input mesh.ActionRequest
	if !decodeMeshRequest(c, &input) {
		return
	}
	if c.Query("kind") == "mcp" {
		ctx, principal, id, action := c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("assetId"), c.Param("action")
		if action == "run" {
			asset, err := h.mcp.Get(ctx, principal, id)
			if err != nil {
				writeError(c, err)
				return
			}
			runID, err := h.source.LaunchAssistantMeshMCP(ctx, principal, input.SessionID, input.RequestID, input.Prompt,
				domaincopilot.AssistantMeshMCP{AssetID: id, Digest: input.ExpectedDigest})
			writeMeshAsset(c, http.StatusOK, map[string]any{"asset": asset, "runId": runID}, err)
			return
		}
		result, err := h.mcp.Act(ctx, principal, id, action, input)
		writeMeshAsset(c, http.StatusOK, result, err)
		return
	}
	result, err := h.service.Act(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("assetId"), c.Param("action"), input)
	writeMeshAsset(c, http.StatusOK, result, err)
}

func writeMeshAsset(c *gin.Context, status int, value any, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, status, value)
}

func decodeMeshRequest(c *gin.Context, input any) bool {
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(input) != nil || d.Decode(&extra) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid Assistant Mesh request")
		return false
	}
	return true
}
