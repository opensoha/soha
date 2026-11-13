package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	contractresource "github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/contracts/resource/terminalstream"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DockerNativeTerminalService interface {
	Create(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, protocol.TerminalInput) (protocol.TerminalCreated, error)
	Get(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string) (protocol.TerminalLease, error)
	Close(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string) (protocol.TerminalLease, error)
	Acquire(context.Context, domainidentity.Principal, domainidentity.AccessContext, string, string, string) (appdocker.NativeDockerTerminalSession, error)
}

func dockerNativeTerminalsMissing(service DockerNativeTerminalService) bool {
	return service == nil || reflect.ValueOf(service).Kind() == reflect.Pointer && reflect.ValueOf(service).IsNil()
}

func (h *DockerHandler) nativeDockerTerminalRequest(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	if len(c.Request.Header.Values("Origin")) != 0 {
		writeError(c, apperrors.ErrAccessDenied)
		return false
	}
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.Method != http.MethodPost && (c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0) {
		writeError(c, apperrors.ErrInvalidArgument)
		return false
	}
	if h.nativeTerminals == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return false
	}
	return true
}

func (h *DockerHandler) CreateNativeDockerTerminal(c *gin.Context) {
	if !h.nativeDockerTerminalRequest(c) {
		return
	}
	input, err := decodeNativeDockerTerminalInput(c.Request.Body)
	if err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	item, err := h.nativeTerminals.Create(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func decodeNativeDockerTerminalInput(reader io.Reader) (protocol.TerminalInput, error) {
	var input protocol.TerminalInput
	body, err := io.ReadAll(io.LimitReader(reader, 4097))
	if err != nil || len(body) > 4096 || !utf8.Valid(body) {
		return input, apperrors.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return input, apperrors.ErrInvalidArgument
	}
	allowed := map[string]bool{"expectedContainerId": true, "serviceName": true, "shell": true, "width": true, "height": true, "timeoutSeconds": true}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return input, apperrors.ErrInvalidArgument
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || seen[key] {
			return input, apperrors.ErrInvalidArgument
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return input, apperrors.ErrInvalidArgument
		}
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return input, apperrors.ErrInvalidArgument
	}
	if decoder.Decode(new(any)) != io.EOF || json.Unmarshal(body, &input) != nil {
		return input, apperrors.ErrInvalidArgument
	}
	if seen["shell"] && input.Shell == "" {
		return input, apperrors.ErrInvalidArgument
	}
	return input.Normalized()
}

func (h *DockerHandler) GetNativeDockerTerminal(c *gin.Context) {
	if !h.nativeDockerTerminalRequest(c) {
		return
	}
	item, err := h.nativeTerminals.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("id"), c.Param("sessionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) CloseNativeDockerTerminal(c *gin.Context) {
	if !h.nativeDockerTerminalRequest(c) {
		return
	}
	item, err := h.nativeTerminals.Close(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("id"), c.Param("sessionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) StreamNativeDockerTerminal(c *gin.Context) {
	if !h.nativeDockerTerminalRequest(c) {
		return
	}
	if !websocket.IsWebSocketUpgrade(c.Request) || len(c.Request.Header.Values(contractresource.NativePodTerminalTicketHeader)) != 1 {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	session, err := h.nativeTerminals.Acquire(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), apiMiddleware.AccessContextFromContext(c), c.Param("id"), c.Param("sessionID"), c.GetHeader(contractresource.NativePodTerminalTicketHeader))
	if err != nil {
		writeError(c, err)
		return
	}
	defer func() { _ = session.Close() }()
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(request *http.Request) bool { return len(request.Header.Values("Origin")) == 0 }}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, http.Header{"Cache-Control": []string{"no-store"}})
	if err != nil {
		return
	}
	stream := terminalstream.NewServer(session.Context(), conn, session.InitialSize())
	defer func() { _ = stream.Close() }()
	stopDisconnect := context.AfterFunc(stream.Context(), session.Cancel)
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
