package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appresource "github.com/opensoha/soha/internal/application/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type PodTerminalService interface {
	Create(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string, string, contractresource.PodTerminalInput) (contractresource.PodTerminalCreated, error)
	Get(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.PodTerminalLease, error)
	Close(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.PodTerminalLease, error)
	Acquire(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string, string, string, string) (appresource.NativePodTerminalSession, error)
}

func (h *podResourceHandler) CreatePodTerminal(c *gin.Context) {
	if !h.nativeTerminalRequest(c) {
		return
	}
	var input contractresource.PodTerminalInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	input, err := input.Normalized()
	if err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	item, err := h.terminals.Create(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("podName"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *podResourceHandler) GetPodTerminal(c *gin.Context) {
	if !h.nativeTerminalRequest(c) {
		return
	}
	item, err := h.terminals.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("podName"), c.Param("sessionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *podResourceHandler) ClosePodTerminal(c *gin.Context) {
	if !h.nativeTerminalRequest(c) {
		return
	}
	item, err := h.terminals.Close(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("podName"), c.Param("sessionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *podResourceHandler) nativeTerminalRequest(c *gin.Context) bool {
	if len(c.Request.Header.Values("Origin")) != 0 {
		writeError(c, apperrors.ErrAccessDenied)
		return false
	}
	query := c.Request.URL.Query()
	if len(query) != 1 || len(query["namespace"]) != 1 || query.Get("namespace") == "" {
		writeError(c, apperrors.ErrInvalidArgument)
		return false
	}
	if h.terminals == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return false
	}
	return true
}

func (h *podResourceHandler) StreamNativePodTerminal(c *gin.Context) {
	if !h.nativeTerminalRequest(c) {
		return
	}
	if !websocket.IsWebSocketUpgrade(c.Request) || len(c.Request.Header.Values(contractresource.NativePodTerminalTicketHeader)) != 1 {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	session, err := h.terminals.Acquire(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("podName"), c.Param("sessionID"), c.GetHeader(contractresource.NativePodTerminalTicketHeader))
	if err != nil {
		writeError(c, err)
		return
	}
	defer func() { _ = session.Close() }()
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(request *http.Request) bool { return len(request.Header.Values("Origin")) == 0 }}
	header := http.Header{"Cache-Control": []string{"no-store"}}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, header)
	if err != nil {
		return
	}
	stream := terminalstream.NewServer(session.Context(), conn, session.InitialSize())
	defer func() { _ = stream.Close() }()
	// The WebSocket owns a child context. Disconnect must also cancel the
	// admitted application/provider session, including clients that send no
	// separate REST close request. Close waits for the provider's final fence.
	stopDisconnect := context.AfterFunc(stream.Context(), func() { _ = session.Close() })
	defer stopDisconnect()
	if stream.SendReady() != nil {
		return
	}
	result, err := session.Stream(stream.Input(), stream, stream)
	if err != nil {
		_ = stream.SendError("transport_lost")
		return
	}
	_ = stream.SendExit(result)
}
