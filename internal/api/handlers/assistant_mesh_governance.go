package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	mesh "github.com/opensoha/soha/internal/application/assistantmesh"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
)

func (h *AssistantMeshHandler) ReadInquiryAudit(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input mesh.InquiryAuditInput
	if !decodeMeshRequest(c, &input) {
		return
	}
	item, err := h.inquiryAudit.Read(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AssistantMeshHandler) ListInheritance(c *gin.Context) {
	item, err := h.inheritance.List(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Query("ownerId"), c.Query("after"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AssistantMeshHandler) TransferInheritance(c *gin.Context) {
	var input struct {
		OwnerID  string          `json:"ownerId"`
		HeirID   string          `json:"heirId"`
		AssetIDs json.RawMessage `json:"assetIds"`
	}
	if !decodeMeshRequest(c, &input) {
		return
	}
	request := domain.InheritanceInput{OwnerID: input.OwnerID, HeirID: input.HeirID}
	ids, ok := inheritanceAssetIDs(c, input.AssetIDs)
	if !ok {
		return
	}
	request.AssetIDs = ids
	result, err := h.inheritance.Transfer(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}

func (h *AssistantMeshHandler) RetireInheritance(c *gin.Context) {
	var input struct {
		OwnerID  string          `json:"ownerId"`
		AssetIDs json.RawMessage `json:"assetIds"`
	}
	if !decodeMeshRequest(c, &input) {
		return
	}
	ids, ok := inheritanceAssetIDs(c, input.AssetIDs)
	if !ok {
		return
	}
	count, err := h.inheritance.Retire(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input.OwnerID, ids)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, map[string]int{"retired": count})
}

func inheritanceAssetIDs(c *gin.Context, raw json.RawMessage) ([]string, bool) {
	var ids []string
	if len(raw) > 0 && (json.Unmarshal(raw, &ids) != nil || len(ids) == 0) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "assetIds must be a nonempty list when supplied")
		return nil, false
	}
	return ids, true
}
