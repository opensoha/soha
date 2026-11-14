package api

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	"k8s.io/client-go/tools/remotecommand"
)

type podTerminalMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

func registerPodTerminalRoutes(
	platform *gin.RouterGroup,
	client *k8sagent.Client,
	actions actionPolicy,
	origins websocketOriginPolicy,
) {
	upgrader := websocket.Upgrader{CheckOrigin: origins.Check}
	terminal := func(c *gin.Context, expectedUID string) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		configureWebSocketReadLimit(conn)

		ctx, cancel := context.WithCancel(c.Request.Context())
		defer cancel()
		startPodTerminalHeartbeat(ctx, cancel, conn)

		stdinReader, stdinWriter := io.Pipe()
		defer func() { _ = stdinWriter.Close() }()
		sizeQueue := newPodTerminalSizeQueue()
		defer sizeQueue.Close()

		var writeMu sync.Mutex
		stdout := podTerminalStreamWriter{conn: conn, writeMu: &writeMu, channel: "stdout"}
		stderr := podTerminalStreamWriter{conn: conn, writeMu: &writeMu, channel: "stderr"}
		streamErrCh := make(chan error, 1)
		go func() {
			streamErrCh <- client.StreamPodTerminal(
				ctx,
				c.DefaultQuery("namespace", "default"),
				c.Param("name"),
				c.Query("container"),
				normalizePodTerminalShell(c.DefaultQuery("shell", "/bin/sh")),
				stdinReader,
				stdout,
				stderr,
				sizeQueue,
				expectedUID,
			)
		}()

		input := make(chan string, 16)
		inputDone := make(chan struct{})
		go func() {
			defer close(inputDone)
			for {
				select {
				case <-ctx.Done():
					return
				case data := <-input:
					if _, err := io.WriteString(stdinWriter, data); err != nil {
						cancel()
						return
					}
				}
			}
		}()
		go func() { <-ctx.Done(); _ = stdinWriter.Close() }()
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			defer func() { _ = stdinWriter.Close() }()
			for {
				var message podTerminalMessage
				if err := conn.ReadJSON(&message); err != nil {
					cancel()
					return
				}
				switch message.Type {
				case "input":
					select {
					case input <- message.Data:
					default:
						cancel()
						return
					}
				case "resize":
					sizeQueue.Push(message.Cols, message.Rows)
				case "close":
					cancel()
					return
				}
			}
		}()

		streamErr := <-streamErrCh
		cancel()
		_ = stdinWriter.Close()
		if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
			_ = writePodTerminalMessage(conn, &writeMu, podTerminalMessage{Type: "error", Message: "terminal session ended with an error"})
		} else {
			_ = writePodTerminalMessage(conn, &writeMu, podTerminalMessage{Type: "exit", Message: "terminal session closed"})
		}
		_ = conn.Close()
		<-readDone
		<-inputDone

	}
	platform.GET("/workloads/pods/:name/terminal", actions.Require(actionPlatformPodsExec), podSessionHandler(false, terminal))
	platform.GET("/workloads/pods/:name/uid-bound/terminal", actions.Require(actionPlatformPodsExec), podSessionHandler(true, terminal))
}

func startPodTerminalHeartbeat(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(45 * time.Second)) })
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					cancel()
					_ = conn.Close()
					return
				}
			}
		}
	}()
}

type podTerminalStreamWriter struct {
	conn    *websocket.Conn
	writeMu *sync.Mutex
	channel string
}

func (w podTerminalStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := writePodTerminalMessage(w.conn, w.writeMu, podTerminalMessage{Type: w.channel, Data: string(p)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

type podTerminalSizeQueue struct {
	ch   chan remotecommand.TerminalSize
	once sync.Once
}

func newPodTerminalSizeQueue() *podTerminalSizeQueue {
	return &podTerminalSizeQueue{ch: make(chan remotecommand.TerminalSize, 1)}
}

func (q *podTerminalSizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.ch
	if !ok {
		return nil
	}
	return &size
}

func (q *podTerminalSizeQueue) Push(cols, rows int) {
	if cols <= 0 || rows <= 0 || cols > 65535 || rows > 65535 {
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

func (q *podTerminalSizeQueue) Close() {
	q.once.Do(func() {
		close(q.ch)
	})
}

func writePodTerminalMessage(conn *websocket.Conn, writeMu *sync.Mutex, message podTerminalMessage) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(message)
}

func normalizePodTerminalShell(shell string) string {
	switch strings.TrimSpace(shell) {
	case "/bin/bash":
		return "/bin/bash"
	case "/bin/ash":
		return "/bin/ash"
	case "/busybox/sh":
		return "/busybox/sh"
	default:
		return "/bin/sh"
	}
}
