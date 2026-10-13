package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func (h *SecretHandler) GenerateSSHKey(c *gin.Context) {
	if h.sshGenerator == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "SSH credential generation is unavailable")
		return
	}
	var input sohaapi.SecretGenerateSSHKeyRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32768))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid SSH credential request")
		return
	}
	item, err := h.sshGenerator.GenerateSSHKey(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
