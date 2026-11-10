package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	appgateway "github.com/opensoha/soha/internal/application/aigateway"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const dshCallbackBodyLimit = 1 << 20
const dshMaxSafeInteger int64 = 9007199254740991

type DSHRuntimeService interface {
	ConfirmDSHAssembly(context.Context, string, domain.DSHAssemblyReport) (domain.DSHAssemblyDecision, error)
	AuthorizeDSHPluginTool(context.Context, string, domain.DSHToolCall) (*domain.DSHToolResult, error)
	SettleDSHPluginTool(context.Context, string, domain.DSHToolResult) (domain.DSHToolResult, error)
	InvokeDSHModel(context.Context, string, appcopilot.DSHModelCall) (appgateway.WorkbenchRelayResponse, error)
	InvokeDSHTool(context.Context, string, domain.DSHToolCall) (domain.DSHToolResult, error)
}

func (h *DSHRuntimeHandler) Assembly(c *gin.Context) {
	var report domain.DSHAssemblyReport
	grant, ok := h.readPluginCallback(c, &report)
	if !ok {
		return
	}
	if !validDSHCallbackIdentity(report.RunID, report.Attempt, report.Fence) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	decision, err := h.service.ConfirmDSHAssembly(c.Request.Context(), grant, report)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, decision)
}

func (h *DSHRuntimeHandler) AuthorizePluginTool(c *gin.Context) {
	var call domain.DSHToolCall
	grant, ok := h.readPluginCallback(c, &call)
	if !ok {
		return
	}
	if !validDSHCallbackIdentity(call.RunID, call.Attempt, call.Fence) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	result, err := h.service.AuthorizeDSHPluginTool(c.Request.Context(), grant, call)
	if err != nil {
		writeError(c, err)
		return
	}
	if result == nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *DSHRuntimeHandler) SettlePluginTool(c *gin.Context) {
	var result domain.DSHToolResult
	grant, ok := h.readPluginCallback(c, &result)
	if !ok {
		return
	}
	if !validDSHCallbackIdentity(result.RunID, result.Attempt, result.Fence) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	result, err := h.service.SettleDSHPluginTool(c.Request.Context(), grant, result)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *DSHRuntimeHandler) readPluginCallback(c *gin.Context, payload any) (string, bool) {
	grant, ok := h.authorize(c)
	if !ok {
		return "", false
	}
	raw, err := dshReadBody(c)
	if err != nil {
		if c.Writer.Status() != http.StatusRequestEntityTooLarge {
			writeError(c, err)
		}
		return "", false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(payload) != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return "", false
	}
	return grant, true
}

func validDSHCallbackIdentity(id string, attempt, fence int64) bool {
	return len(id) != 0 && len(id) <= 128 && attempt >= 1 && attempt <= dshMaxSafeInteger && fence >= 1 && fence <= dshMaxSafeInteger
}

// Streaming remains optional at construction, but a stream request requires it.
type dshRuntimeStreamService interface {
	InvokeDSHModelStream(context.Context, string, appcopilot.DSHModelCall, func(appgateway.WorkbenchRelayStreamDelta) bool) (appgateway.WorkbenchRelayResponse, error)
}

type DSHRuntimeHandler struct {
	service   DSHRuntimeService
	tokenHash [32]byte
}

// The service token is read by bootstrap from its configured secret file.
func NewDSHRuntimeHandler(service DSHRuntimeService, serviceToken string) (*DSHRuntimeHandler, error) {
	if service == nil || (reflect.ValueOf(service).Kind() == reflect.Ptr && reflect.ValueOf(service).IsNil()) || !dshValidServiceToken(serviceToken) {
		return nil, errors.New("invalid dsh callback dependencies")
	}
	return &DSHRuntimeHandler{service: service, tokenHash: sha256.Sum256([]byte(serviceToken))}, nil
}
func dshValidServiceToken(token string) bool {
	if len(token) < 32 || len(token) > 8192 {
		return false
	}
	for _, c := range token {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	return true
}

func dshHeader(r *http.Request, name string, max int) (string, error) {
	values := r.Header.Values(name)
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > max || strings.TrimSpace(values[0]) != values[0] || strings.ContainsAny(values[0], "\r\n,") {
		return "", apperrors.ErrInvalidArgument
	}
	return values[0], nil
}
func (h *DSHRuntimeHandler) authorize(c *gin.Context) (string, bool) {
	auth, err := dshHeader(c.Request, "Authorization", 8200)
	hash := sha256.Sum256([]byte(strings.TrimPrefix(auth, "Bearer ")))
	if err != nil || !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare(hash[:], h.tokenHash[:]) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid service authorization", "type": "authentication_error"}})
		return "", false
	}
	grant, err := dshHeader(c.Request, "X-Soha-Tool-Grant", 8192)
	if err != nil {
		writeError(c, apperrors.ErrAccessDenied)
		return "", false
	}
	return grant, true
}
func dshReadBody(c *gin.Context) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, dshCallbackBodyLimit)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.Status(http.StatusRequestEntityTooLarge)
			return nil, err
		}
		return nil, apperrors.ErrInvalidArgument
	}
	if !json.Valid(raw) {
		return nil, apperrors.ErrInvalidArgument
	}
	return raw, nil
}
func dshSafeHeader(r *http.Request, name string) (int64, error) {
	raw, err := dshHeader(r, name, 16)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > dshMaxSafeInteger || strconv.FormatInt(n, 10) != raw {
		return 0, apperrors.ErrInvalidArgument
	}
	return n, nil
}
func dshModelBinding(r *http.Request, raw []byte) (appcopilot.DSHModelCall, error) {
	var in appcopilot.DSHModelCall
	var err error
	if in.RunID, err = dshHeader(r, "X-Soha-Run-ID", 128); err != nil {
		return in, err
	}
	if in.Attempt, err = dshSafeHeader(r, "X-Soha-Run-Attempt"); err != nil {
		return in, err
	}
	if in.Fence, err = dshSafeHeader(r, "X-Soha-Run-Fence"); err != nil {
		return in, err
	}
	for name, target := range map[string]*string{"X-Soha-Context-Revision": &in.ContextRevision, "X-Soha-Tool-Catalog-Revision": &in.ToolCatalogRevision, "X-Soha-Model-Call-ID": &in.CallID, "X-Soha-Input-Hash": &in.InputHash} {
		if *target, err = dshHeader(r, name, 128); err != nil {
			return in, err
		}
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != in.InputHash {
		return in, apperrors.ErrAccessDenied
	}
	in.RawBody = raw
	return in, nil
}

// Model performs exactly one governed application invocation.
func (h *DSHRuntimeHandler) Model(c *gin.Context) {
	grant, ok := h.authorize(c)
	if !ok {
		return
	}
	raw, err := dshReadBody(c)
	if err != nil {
		if c.Writer.Status() != http.StatusRequestEntityTooLarge {
			writeError(c, err)
		}
		return
	}
	in, err := dshModelBinding(c.Request, raw)
	if err != nil {
		writeError(c, err)
		return
	}
	request, stream, err := parseDSHOpenAI(raw)
	if err != nil {
		writeError(c, err)
		return
	}
	in.Request = request
	if stream {
		h.streamModel(c, grant, in)
		return
	}
	result, err := h.service.InvokeDSHModel(c.Request.Context(), grant, in)
	if err != nil {
		writeError(c, err)
		return
	}
	writeDSHOpenAI(c, in.CallID, result, stream)
}
func (h *DSHRuntimeHandler) streamModel(c *gin.Context, grant string, in appcopilot.DSHModelCall) {
	service, ok := h.service.(dshRuntimeStreamService)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "service_unavailable", "message": "model streaming unavailable"}})
		return
	}
	emitted, stopped := false, false
	result, err := service.InvokeDSHModelStream(c.Request.Context(), grant, in, func(delta appgateway.WorkbenchRelayStreamDelta) bool {
		if stopped || c.Request.Context().Err() != nil {
			stopped = true
			return false
		}
		if delta.ContentDelta == "" {
			return true
		}
		dshStreamHeaders(c)
		message := gin.H{"content": delta.ContentDelta}
		if !emitted {
			message["role"] = "assistant"
		}
		if !dshSSE(c, gin.H{"id": in.CallID, "model": in.Request.PublicModel, "created": 0, "object": "chat.completion.chunk", "choices": []any{gin.H{"index": 0, "delta": message, "finish_reason": nil}}}) {
			stopped = true
			return false
		}
		emitted = true
		return true
	})
	if stopped || c.Request.Context().Err() != nil {
		return
	}
	if err != nil {
		if !c.Writer.Written() {
			writeError(c, err)
			return
		}
		_ = c.Error(apperrors.ErrClusterUnready)
		dshSSE(c, gin.H{"error": gin.H{"type": "runtime_error", "message": "model stream interrupted"}})
		return
	}
	// Cached responses emit no new provider deltas; replay their committed content.
	result.PublicModel = in.Request.PublicModel
	writeDSHOpenAIResult(c, in.CallID, result, true, emitted)
}

func dshStreamHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
}

func (h *DSHRuntimeHandler) Tool(c *gin.Context) {
	grant, ok := h.authorize(c)
	if !ok {
		return
	}
	raw, err := dshReadBody(c)
	if err != nil {
		if c.Writer.Status() != http.StatusRequestEntityTooLarge {
			writeError(c, err)
		}
		return
	}
	var in domain.DSHToolCall
	if json.Unmarshal(raw, &in) != nil || in.Attempt < 1 || in.Attempt > dshMaxSafeInteger || in.Fence < 1 || in.Fence > dshMaxSafeInteger {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	result, err := h.service.InvokeDSHTool(c.Request.Context(), grant, in)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

type dshFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Arguments   string         `json:"arguments"`
}
type dshFunctionCall struct {
	ID       string      `json:"id,omitempty"`
	Type     string      `json:"type"`
	Function dshFunction `json:"function"`
}
type dshOpenAIMessage struct {
	Role       string            `json:"role"`
	Content    json.RawMessage   `json:"content"`
	ToolCallID string            `json:"tool_call_id"`
	ToolCalls  []dshFunctionCall `json:"tool_calls"`
}
type dshOpenAIRequest struct {
	Model               string             `json:"model"`
	Stream              bool               `json:"stream"`
	Messages            []dshOpenAIMessage `json:"messages"`
	Tools               []dshFunctionCall  `json:"tools"`
	MaxTokens           int                `json:"max_tokens"`
	MaxCompletionTokens int                `json:"max_completion_tokens"`
	ReasoningEffort     string             `json:"reasoning_effort"`
}

func parseDSHOpenAI(raw []byte) (appgateway.WorkbenchRelayRequest, bool, error) {
	var body dshOpenAIRequest
	if json.Unmarshal(raw, &body) != nil || len(body.Messages) == 0 || len(body.Messages) > 128 || len(body.Tools) > 64 || body.MaxTokens < 0 || body.MaxCompletionTokens < 0 {
		return appgateway.WorkbenchRelayRequest{}, false, apperrors.ErrInvalidArgument
	}
	out := appgateway.WorkbenchRelayRequest{PublicModel: body.Model, Endpoint: "chat/completions", MaxOutputTokens: body.MaxTokens, ReasoningEffort: body.ReasoningEffort}
	if body.MaxCompletionTokens > 0 {
		out.MaxOutputTokens = body.MaxCompletionTokens
	}
	for _, m := range body.Messages {
		message, err := dshRelayMessage(m)
		if err != nil {
			return out, false, err
		}
		out.Messages = append(out.Messages, message)
	}
	for _, t := range body.Tools {
		if t.Type != "function" || t.Function.Name == "" || t.Function.Parameters == nil {
			return out, false, apperrors.ErrInvalidArgument
		}
		out.Tools = append(out.Tools, appgateway.WorkbenchTool{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters})
	}
	return out, body.Stream, nil
}
func dshRelayMessage(m dshOpenAIMessage) (appgateway.WorkbenchRelayMessage, error) {
	out := appgateway.WorkbenchRelayMessage{Role: m.Role, ToolCallID: m.ToolCallID}
	switch m.Role {
	case "system", "developer", "user", "assistant", "tool":
	default:
		return out, apperrors.ErrInvalidArgument
	}
	if len(m.Content) > 0 && string(m.Content) != "null" {
		if err := json.Unmarshal(m.Content, &out.Content); err != nil {
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(m.Content, &parts) != nil {
				return out, apperrors.ErrInvalidArgument
			}
			for _, p := range parts {
				switch p.Type {
				case "text":
					out.Content += p.Text
				default:
					return out, apperrors.ErrInvalidArgument
				}
			}
		}
	}
	for _, call := range m.ToolCalls {
		if call.Type != "function" || call.ID == "" || call.Function.Name == "" || !json.Valid([]byte(call.Function.Arguments)) {
			return out, apperrors.ErrInvalidArgument
		}
		out.ToolCalls = append(out.ToolCalls, appgateway.WorkbenchToolCall{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
	}
	return out, nil
}
func writeDSHOpenAI(c *gin.Context, id string, result appgateway.WorkbenchRelayResponse, stream bool) {
	writeDSHOpenAIResult(c, id, result, stream, false)
}
func writeDSHOpenAIResult(c *gin.Context, id string, result appgateway.WorkbenchRelayResponse, stream, contentEmitted bool) {
	message := gin.H{"role": "assistant"}
	if !contentEmitted {
		message["content"] = result.Content
	}
	finish := "stop"
	if len(result.ToolCalls) > 0 {
		calls := []gin.H{}
		for index, call := range result.ToolCalls {
			item := gin.H{"id": call.ID, "type": "function", "function": gin.H{"name": call.Name, "arguments": string(call.Arguments)}}
			if stream {
				item["index"] = index
			}
			calls = append(calls, item)
		}
		message["tool_calls"] = calls
		finish = "tool_calls"
	}
	var usage any
	if result.Usage != nil && !result.UsageEstimated {
		usage = gin.H{"prompt_tokens": result.Usage["inputTokens"], "completion_tokens": result.Usage["outputTokens"], "total_tokens": result.Usage["totalTokens"]}
	}
	base := gin.H{"id": id, "model": result.PublicModel, "created": 0}
	if !stream {
		base["object"] = "chat.completion"
		base["choices"] = []any{gin.H{"index": 0, "message": message, "finish_reason": finish}}
		base["usage"] = usage
		c.JSON(http.StatusOK, base)
		return
	}
	dshStreamHeaders(c)
	base["object"] = "chat.completion.chunk"
	base["choices"] = []any{gin.H{"index": 0, "delta": message, "finish_reason": nil}}
	if !contentEmitted || len(result.ToolCalls) > 0 {
		if !dshSSE(c, base) {
			return
		}
	}
	base["choices"] = []any{gin.H{"index": 0, "delta": gin.H{}, "finish_reason": finish}}
	if !dshSSE(c, base) {
		return
	}
	if usage != nil {
		base["choices"] = []any{}
		base["usage"] = usage
		if !dshSSE(c, base) {
			return
		}
	}
	_, _ = io.WriteString(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
func dshSSE(c *gin.Context, value any) bool {
	if c.Request.Context().Err() != nil {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if _, err = fmt.Fprintf(c.Writer, "data: %s\n\n", raw); err != nil {
		return false
	}
	c.Writer.Flush()
	return c.Request.Context().Err() == nil
}
