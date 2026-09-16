package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

func (h *copilotStreamHandler) StreamMessage(c *gin.Context) {
	var req dto.WorkbenchSendMessageStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workbench stream payload")
		return
	}
	if err := clearResponseWriteDeadline(c); err != nil {
		_ = c.Error(err)
		apiresponse.Error(c, http.StatusInternalServerError, "stream_unavailable", "streaming response is unavailable")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sessionID := c.Param("sessionID")
	seq := 0
	emit := func(event domaincopilot.WorkbenchStreamEvent) bool {
		seq++
		if event.ID == "" {
			event.ID = fmt.Sprintf("evt:%s:%06d", sessionID, seq)
		}
		event.SessionID = sessionID
		event.Sequence = seq
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}
		data, _ := json.Marshal(event)
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
			return false
		}
		c.Writer.Flush()
		return true
	}

	principal := apiMiddleware.PrincipalFromContext(c)
	result, err := h.service.StreamMessage(c.Request.Context(), principal, sessionID, domaincopilot.WorkbenchSendMessageInput{
		ModelPreferences: req.ModelPreferences,
		KnowledgeContext: req.KnowledgeContext,
		ContextSelection: req.ContextSelection,
		Content:          req.Content,
		Mode:             req.Mode,
		AgentProviderID:  req.AgentProviderID,
		Toolset:          workbenchStreamToolsetFromMap(req.Toolset),
		ScopeOverrides:   req.ScopeOverrides,
		Source:           req.Source,
		LaunchContext:    req.LaunchContext,
		SelectionContext: req.SelectionContext,
		PinnedContext:    req.PinnedContext,
		EventSink:        emit,
	}, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		_ = c.Error(err)
		emit(publicWorkbenchStreamError(err))
		emit(domaincopilot.WorkbenchStreamEvent{Type: "agent.status", ProviderID: "internal", ProviderKind: "internal", Status: "failed"})
		return
	}

	for _, event := range result.Events {
		if !emit(event) {
			return
		}
	}
}

func workbenchStreamToolsetFromMap(input map[string]any) domaincopilot.SessionToolset {
	if input == nil {
		return domaincopilot.SessionToolset{}
	}
	var toolset domaincopilot.SessionToolset
	raw, err := json.Marshal(input)
	if err != nil {
		return domaincopilot.SessionToolset{}
	}
	if err := json.Unmarshal(raw, &toolset); err != nil {
		return domaincopilot.SessionToolset{}
	}
	return toolset
}

func publicWorkbenchStreamError(err error) domaincopilot.WorkbenchStreamEvent {
	code, message, retryable := "agent_failed", "助手请求失败，请稍后重试。", false
	switch {
	case errors.Is(err, apperrors.ErrAccessDenied):
		code, message, retryable = "access_denied", "当前账号无权执行此操作。", false
	case errors.Is(err, apperrors.ErrNotFound):
		code, message, retryable = "context_unavailable", "引用的背景或资源已不可用，请移除后重新发送。", false
	case errors.Is(err, apperrors.ErrInvalidArgument):
		code, message, retryable = "invalid_argument", "请求参数无效，请检查助手和输入内容。", false
	case errors.Is(err, apperrors.ErrUnsupportedOperation):
		code, message, retryable = "agent_unsupported", "所选助手不支持当前任务。", false
	case errors.Is(err, apperrors.ErrClusterUnready):
		code, message, retryable = "agent_unavailable", "助手暂不可用，请检查插件配置和运行器状态。", true
	}
	return domaincopilot.WorkbenchStreamEvent{Type: "error", Code: code, Message: message, Retryable: &retryable}
}
