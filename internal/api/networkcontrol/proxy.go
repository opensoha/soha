package networkcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkidentity"
)

type ProxyRuntime interface {
	Configuration(context.Context, networkidentity.Identity) (*domain.Configuration, error)
	Applied(context.Context, networkidentity.Identity, domain.Applied) error
	Observe(context.Context, networkidentity.Identity, time.Time, domain.Observation) error
	PutConnections(context.Context, networkidentity.Identity, time.Time, string, []domain.Connection) error
	NextClose(context.Context, networkidentity.Identity) (*domain.CloseCommand, error)
	CompleteClose(context.Context, networkidentity.Identity, string, string, string) error
}

type proxyMessage struct {
	SchemaVersion string          `json:"schemaVersion"`
	RuntimeID     string          `json:"runtimeId"`
	MessageType   string          `json:"messageType"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Payload       json.RawMessage `json:"payload"`
}

type proxyConnectionsPayload struct {
	ProcessEpoch   string              `json:"processEpoch,omitempty"`
	SampleSequence int64               `json:"sampleSequence,omitempty"`
	TotalActive    *int                `json:"totalActive,omitempty"`
	ReturnedCount  *int                `json:"returnedCount,omitempty"`
	Truncated      *bool               `json:"truncated,omitempty"`
	Freshness      string              `json:"freshness,omitempty"`
	Engine         string              `json:"engine"`
	Connections    []domain.Connection `json:"connections"`
}

type proxyCloseResultPayload struct {
	ProcessEpoch string `json:"processEpoch,omitempty"`
	CommandID    string `json:"commandId"`
	Status       string `json:"status"`
	ReasonCode   string `json:"reasonCode"`
}

func (h *handler) proxyIdentity(c *gin.Context) (networkidentity.Identity, bool) {
	identity, ok := h.identity(c)
	if !ok {
		return identity, false
	}
	if identity.Kind != "proxy" {
		apiresponse.Error(c, http.StatusForbidden, "proxy_identity_required", "a proxy runtime identity is required")
		return identity, false
	}
	return identity, true
}

func (h *handler) proxyBody(c *gin.Context, messageType string, identity networkidentity.Identity) (proxyMessage, bool) {
	raw, ok := h.body(c)
	if !ok {
		return proxyMessage{}, false
	}
	if err := h.options.ProxySchemas.ValidateProxyRuntime(raw); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_proxy_message", "the proxy runtime message is invalid")
		return proxyMessage{}, false
	}
	var message proxyMessage
	if err := json.Unmarshal(raw, &message); err != nil || message.RuntimeID != identity.ID || message.MessageType != messageType ||
		message.OccurredAt.Before(time.Now().Add(-2*time.Minute)) || message.OccurredAt.After(time.Now().Add(time.Minute)) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_proxy_message", "the proxy runtime message does not match this request")
		return proxyMessage{}, false
	}
	return message, true
}

func decodeProxyPayload[T any](c *gin.Context, raw json.RawMessage) (T, bool) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_proxy_payload", "the proxy runtime payload is invalid")
		return value, false
	}
	return value, true
}

func proxyResponse(c *gin.Context, runtimeID, kind string, payload any) {
	apiresponse.JSON(c, http.StatusOK, gin.H{
		"schemaVersion": "proxy-runtime/v1alpha1", "runtimeId": runtimeID,
		"messageType": kind, "occurredAt": time.Now().UTC(), "payload": payload,
	})
}

func (h *handler) proxyConfiguration(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	configuration, err := h.options.ProxyRuntime.Configuration(c.Request.Context(), identity)
	if err != nil {
		respondError(c, err)
		return
	}
	if configuration == nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.Header("Cache-Control", "no-store")
	proxyResponse(c, identity.ID, "configuration.desired", configuration)
}

func (h *handler) proxyApplied(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	message, ok := h.proxyBody(c, "configuration.applied", identity)
	if !ok {
		return
	}
	payload, ok := decodeProxyPayload[domain.Applied](c, message.Payload)
	if !ok {
		return
	}
	if err := h.options.ProxyRuntime.Applied(c.Request.Context(), identity, payload); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handler) proxyObservation(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	message, ok := h.proxyBody(c, "observation", identity)
	if !ok {
		return
	}
	payload, ok := decodeProxyPayload[domain.Observation](c, message.Payload)
	if !ok {
		return
	}
	if err := h.options.ProxyRuntime.Observe(c.Request.Context(), identity, message.OccurredAt, payload); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handler) proxyConnections(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	message, ok := h.proxyBody(c, "connections.snapshot", identity)
	if !ok {
		return
	}
	payload, valid := decodeProxyPayload[proxyConnectionsPayload](c, message.Payload)
	if !valid {
		return
	}
	var err error
	if payload.ProcessEpoch != "" {
		svc, ok := h.options.ProxyRuntime.(ProxyEpochRuntime)
		if !ok {
			respondError(c, fmt.Errorf("epoch snapshot unavailable"))
			return
		}
		err = svc.PutConnectionsSnapshot(c.Request.Context(), identity, message.OccurredAt, payload.Engine, domain.ConnectionsSnapshot{ProcessEpoch: payload.ProcessEpoch, SampleSequence: payload.SampleSequence, TotalActive: payload.TotalActive, ReturnedCount: payload.ReturnedCount, Truncated: payload.Truncated, Freshness: payload.Freshness, Connections: payload.Connections})
	} else {
		err = h.options.ProxyRuntime.PutConnections(c.Request.Context(), identity, message.OccurredAt, payload.Engine, payload.Connections)
	}
	if err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handler) proxyNextClose(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	command, err := h.options.ProxyRuntime.NextClose(c.Request.Context(), identity)
	if err != nil {
		respondError(c, err)
		return
	}
	if command == nil {
		c.Status(http.StatusNoContent)
		return
	}
	payload := gin.H{"commandId": command.ID, "connectionId": command.ConnectionID, "expiresAt": command.ExpiresAt}
	if command.ProcessEpoch != "" {
		payload["processEpoch"] = command.ProcessEpoch
	}
	proxyResponse(c, identity.ID, "connection.close.request", payload)
}

func (h *handler) proxyCloseResult(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	message, ok := h.proxyBody(c, "connection.close.result", identity)
	if !ok {
		return
	}
	payload, valid := decodeProxyPayload[proxyCloseResultPayload](c, message.Payload)
	if !valid {
		return
	}
	if payload.CommandID != c.Param("commandID") {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_proxy_payload", "the proxy close result is invalid")
		return
	}
	var err error
	if payload.ProcessEpoch != "" {
		svc, ok := h.options.ProxyRuntime.(ProxyEpochRuntime)
		if !ok {
			respondError(c, fmt.Errorf("epoch close unavailable"))
			return
		}
		err = svc.CompleteCloseWithEpoch(c.Request.Context(), identity, payload.CommandID, payload.ProcessEpoch, payload.Status, payload.ReasonCode)
	} else {
		err = h.options.ProxyRuntime.CompleteClose(c.Request.Context(), identity, payload.CommandID, payload.Status, payload.ReasonCode)
	}
	if err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type ProxyEpochRuntime interface {
	PutConnectionsSnapshot(context.Context, networkidentity.Identity, time.Time, string, domain.ConnectionsSnapshot) error
	CompleteCloseWithEpoch(context.Context, networkidentity.Identity, string, string, string, string) error
}
type ProxyCommandRuntime interface {
	NextCommand(context.Context, networkidentity.Identity, string) (*domain.RuntimeCommand, error)
	CompleteCommand(context.Context, networkidentity.Identity, domain.RuntimeCommandResult) error
}

func (h *handler) proxyNextCommand(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	command, err := h.options.ProxyCommands.NextCommand(c.Request.Context(), identity, c.Query("processEpoch"))
	if err != nil {
		respondError(c, err)
		return
	}
	if command == nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.Header("Cache-Control", "no-store")
	proxyResponse(c, identity.ID, "command.request", command.Input)
}
func (h *handler) proxyCommandResult(c *gin.Context) {
	identity, ok := h.proxyIdentity(c)
	if !ok {
		return
	}
	message, ok := h.proxyBody(c, "command.result", identity)
	if !ok {
		return
	}
	receipt, ok := decodeProxyPayload[domain.RuntimeCommandReceipt](c, message.Payload)
	if !ok {
		return
	}
	if receipt.CommandID != c.Param("commandID") {
		c.Status(http.StatusBadRequest)
		return
	}
	err := h.options.ProxyCommands.CompleteCommand(c.Request.Context(), identity, domain.RuntimeCommandResult{Kind: string(receipt.Kind), ExpiresAt: receipt.ExpiresAt, CommandID: receipt.CommandID, RequestID: receipt.RequestID, ProcessEpoch: receipt.ProcessEpoch, Status: string(receipt.Status), ReasonCode: receipt.ReasonCode, Result: receipt.Result})
	if err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
