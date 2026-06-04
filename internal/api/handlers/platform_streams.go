package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	"k8s.io/client-go/tools/remotecommand"
)

func (h *PlatformHandler) StreamPodLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	container := c.Query("container")
	tailLines := int64(parseLimit(c.Query("tailLines"), 200))
	sinceSeconds := int64(parseLimit(c.Query("sinceSeconds"), 0))

	session, err := newWebSocketStreamSession(c)
	if err != nil {
		return
	}
	defer session.Close()
	session.SetPongWait(podLogPongWait)
	session.StartPing(podLogPingInterval)

	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- h.streamPodLogsWithReconnect(
			session.Context(),
			principal,
			c.Param("clusterID"),
			namespace,
			c.Param("podName"),
			container,
			tailLines,
			sinceSeconds,
			&logStreamWriter{conn: session.conn, writeMu: &session.writeMu},
		)
	}()

	readDone := session.ReadMessages(func(message terminalMessage) bool {
		return message.Type != "close"
	}, nil)

	writeExit := func(message string) {
		_ = session.WriteMessage(terminalMessage{Type: "exit", Message: message})
	}

	select {
	case err := <-streamErrCh:
		message := "log stream closed"
		if err != nil && !errors.Is(err, context.Canceled) {
			message = err.Error()
		}
		writeExit(message)
	case <-readDone:
		writeExit("log stream closed")
	}
}

func (h *PlatformHandler) streamPodLogsWithReconnect(
	ctx context.Context,
	principal domainidentity.Principal,
	clusterID, namespace, podName, container string,
	tailLines, sinceSeconds int64,
	writer *logStreamWriter,
) error {
	currentTailLines := tailLines
	connectedOnce := false
	for {
		err := h.resources.StreamPodLogs(
			ctx,
			principal,
			clusterID,
			namespace,
			podName,
			container,
			currentTailLines,
			sinceSeconds,
			writer,
		)
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if !connectedOnce && err != nil {
			return err
		}
		connectedOnce = true
		currentTailLines = 0
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(podLogReconnectDelay):
		}
	}
}

func (h *PlatformHandler) StreamPodTerminal(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	container := c.Query("container")
	shell := c.DefaultQuery("shell", "/bin/sh")

	session, err := newWebSocketStreamSession(c)
	if err != nil {
		return
	}
	defer session.Close()

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	sizeQueue := newTerminalSizeQueue()

	_ = session.WriteMessage(terminalMessage{
		Type:    "status",
		Message: "terminal session connected",
	})

	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- h.resources.StreamPodTerminal(
			session.Context(),
			principal,
			c.Param("clusterID"),
			namespace,
			c.Param("podName"),
			container,
			shell,
			stdinReader,
			terminalStreamWriter{conn: session.conn, writeMu: &session.writeMu, channel: "stdout"},
			terminalStreamWriter{conn: session.conn, writeMu: &session.writeMu, channel: "stderr"},
			sizeQueue,
		)
	}()

	readDone := session.ReadMessages(func(message terminalMessage) bool {
		switch message.Type {
		case "input":
			if _, err := io.WriteString(stdinWriter, message.Data); err != nil {
				return false
			}
		case "resize":
			sizeQueue.Push(message.Cols, message.Rows)
		case "close":
			return false
		}
		return true
	}, func(error) {
		_ = session.WriteMessage(terminalMessage{Type: "status", Message: "ignored invalid terminal message"})
	})
	go func() {
		<-readDone
		_ = stdinWriter.Close()
	}()

	streamErr := <-streamErrCh
	session.Cancel()
	<-readDone

	exitMessage := terminalMessage{Type: "exit", Message: "terminal session closed"}
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
		exitMessage.Message = streamErr.Error()
	}
	_ = session.WriteMessage(exitMessage)
}

type terminalMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

type websocketStreamSession struct {
	conn    *websocket.Conn
	ctx     context.Context
	cancel  context.CancelFunc
	writeMu sync.Mutex
}

func newWebSocketStreamSession(c *gin.Context) (*websocketStreamSession, error) {
	conn, err := podTerminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(c.Request.Context())
	return &websocketStreamSession{conn: conn, ctx: ctx, cancel: cancel}, nil
}

func (s *websocketStreamSession) Context() context.Context {
	return s.ctx
}

func (s *websocketStreamSession) Cancel() {
	s.cancel()
}

func (s *websocketStreamSession) Close() {
	s.cancel()
	_ = s.conn.Close()
}

func (s *websocketStreamSession) SetPongWait(wait time.Duration) {
	_ = s.conn.SetReadDeadline(time.Now().Add(wait))
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(wait))
	})
}

func (s *websocketStreamSession) StartPing(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				if err := s.WriteControl(websocket.PingMessage, nil); err != nil {
					s.cancel()
					return
				}
			}
		}
	}()
}

func (s *websocketStreamSession) ReadMessages(onMessage func(terminalMessage) bool, onInvalid func(error)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, payload, err := s.conn.ReadMessage()
			if err != nil {
				s.cancel()
				return
			}
			var message terminalMessage
			if err := json.Unmarshal(payload, &message); err != nil {
				if onInvalid != nil {
					onInvalid(err)
				}
				continue
			}
			if onMessage != nil && !onMessage(message) {
				s.cancel()
				return
			}
		}
	}()
	return done
}

func (s *websocketStreamSession) WriteMessage(message terminalMessage) error {
	return writeTerminalMessage(s.conn, &s.writeMu, message)
}

func (s *websocketStreamSession) WriteControl(messageType int, data []byte) error {
	return writeControlMessage(s.conn, &s.writeMu, messageType, data)
}

type terminalStreamWriter struct {
	conn    *websocket.Conn
	writeMu *sync.Mutex
	channel string
}

type logStreamWriter struct {
	conn          *websocket.Conn
	writeMu       *sync.Mutex
	pendingBuffer string
}

func (w terminalStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := writeTerminalMessage(w.conn, w.writeMu, terminalMessage{
		Type: w.channel,
		Data: string(p),
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *logStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	data := w.pendingBuffer + string(p)
	lines := strings.Split(data, "\n")
	for _, line := range lines[:len(lines)-1] {
		if err := writeTerminalMessage(w.conn, w.writeMu, terminalMessage{
			Type: "log",
			Data: line,
		}); err != nil {
			return 0, err
		}
	}
	last := lines[len(lines)-1]
	if strings.HasSuffix(data, "\n") {
		if last != "" {
			if err := writeTerminalMessage(w.conn, w.writeMu, terminalMessage{Type: "log", Data: last}); err != nil {
				return 0, err
			}
		}
		w.pendingBuffer = ""
	} else {
		w.pendingBuffer = last
	}
	return len(p), nil
}

type terminalSizeQueue struct {
	ch chan remotecommand.TerminalSize
}

func newTerminalSizeQueue() *terminalSizeQueue {
	return &terminalSizeQueue{ch: make(chan remotecommand.TerminalSize, 1)}
}

func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.ch
	if !ok {
		return nil
	}
	return &size
}

func (q *terminalSizeQueue) Push(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	size := remotecommand.TerminalSize{Width: uint16(cols), Height: uint16(rows)}
	select {
	case q.ch <- size:
	default:
		select {
		case <-q.ch:
		default:
		}
		q.ch <- size
	}
}

func writeTerminalMessage(conn *websocket.Conn, writeMu *sync.Mutex, message terminalMessage) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteJSON(message)
}

func writeControlMessage(conn *websocket.Conn, writeMu *sync.Mutex, messageType int, data []byte) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteControl(messageType, data, time.Now().Add(5*time.Second))
}
