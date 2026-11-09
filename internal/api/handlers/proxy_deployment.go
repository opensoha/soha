package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	"github.com/opensoha/soha/internal/api/middleware"
	response "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"io"
	"net/http"
	"strings"
)

type ProxyDeploymentManagement interface {
	View(context.Context, identity.Principal, string) (api.NetworkProxyDeployment, error)
	BindRequest(context.Context, identity.Principal, string, api.NetworkProxyDeploymentInput) (api.NetworkProxyDeployment, error)
	ControlRequest(context.Context, identity.Principal, string, api.NetworkProxyDeploymentControlInput) (api.NetworkProxyDeploymentOperation, error)
}
type ProxyDeploymentExecutor interface {
	Claim(context.Context, string, string) (delivery.ExecutionTask, error)
	Status(context.Context, string, string) (delivery.ExecutionTask, error)
	Callback(context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error)
}
type ProxyDeploymentHandler struct {
	management ProxyDeploymentManagement
	executor   ProxyDeploymentExecutor
}

func NewProxyDeploymentHandler(management ProxyDeploymentManagement, executor ProxyDeploymentExecutor) *ProxyDeploymentHandler {
	return &ProxyDeploymentHandler{management: management, executor: executor}
}
func proxyDeploymentError(c *gin.Context, err error) {
	response.Error(c, apierrors.StatusCode(err), apierrors.Code(err), apierrors.Message(err, apierrors.RequestLanguage(c.Request)))
}
func proxyDeploymentDecode(c *gin.Context, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) == nil {
		var extra any
		if errors.Is(decoder.Decode(&extra), io.EOF) {
			return true
		}
	}
	proxyDeploymentError(c, apperrors.ErrInvalidArgument)
	return false
}
func proxyDeploymentRespond(c *gin.Context, status int, item any, err error) {
	if err != nil {
		proxyDeploymentError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Item(c, status, item)
}
func (h *ProxyDeploymentHandler) Get(c *gin.Context) {
	item, err := h.management.View(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("instanceID"))
	proxyDeploymentRespond(c, http.StatusOK, item, err)
}
func (h *ProxyDeploymentHandler) Bind(c *gin.Context) {
	var input api.NetworkProxyDeploymentInput
	if !proxyDeploymentDecode(c, &input) {
		return
	}
	item, err := h.management.BindRequest(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("instanceID"), input)
	proxyDeploymentRespond(c, http.StatusAccepted, item, err)
}
func (h *ProxyDeploymentHandler) Control(c *gin.Context) {
	var input api.NetworkProxyDeploymentControlInput
	if !proxyDeploymentDecode(c, &input) {
		return
	}
	item, err := h.management.ControlRequest(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("instanceID"), input)
	proxyDeploymentRespond(c, http.StatusAccepted, item, err)
}
func proxyDeploymentToken(c *gin.Context) (string, bool) {
	values := c.Request.Header.Values("Authorization")
	if len(values) == 1 {
		parts := strings.Fields(values[0])
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1], true
		}
	}
	proxyDeploymentError(c, apperrors.ErrUnauthorized)
	return "", false
}
func (h *ProxyDeploymentHandler) Claim(c *gin.Context) {
	token, ok := proxyDeploymentToken(c)
	if !ok {
		return
	}
	var input struct {
		ExecutorID string `json:"executorId"`
	}
	if !proxyDeploymentDecode(c, &input) {
		return
	}
	task, err := h.executor.Claim(c.Request.Context(), token, input.ExecutorID)
	if errors.Is(err, apperrors.ErrNotFound) {
		c.Status(http.StatusNoContent)
		return
	}
	proxyDeploymentRespond(c, http.StatusAccepted, task, err)
}
func (h *ProxyDeploymentHandler) Status(c *gin.Context) {
	token, ok := proxyDeploymentToken(c)
	if !ok {
		return
	}
	ids := c.Request.URL.Query()["taskId"]
	if len(ids) != 1 || strings.TrimSpace(ids[0]) == "" {
		proxyDeploymentError(c, apperrors.ErrInvalidArgument)
		return
	}
	task, err := h.executor.Status(c.Request.Context(), token, ids[0])
	proxyDeploymentRespond(c, http.StatusOK, task, err)
}
func (h *ProxyDeploymentHandler) Callback(c *gin.Context) {
	token, ok := proxyDeploymentToken(c)
	if !ok {
		return
	}
	var input api.ExecutionCallbackRequest
	if !proxyDeploymentDecode(c, &input) {
		return
	}
	task, err := h.executor.Callback(c.Request.Context(), token, delivery.ExecutionCallbackInput{CallbackToken: input.CallbackToken, Status: input.Status, Payload: input.Payload})
	proxyDeploymentRespond(c, http.StatusOK, task, err)
}
