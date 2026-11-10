package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type dshPersonalStdioService interface {
	PrepareDSHPersonalStdio(context.Context, string, domain.DSHIdentity, string, string) (appcopilot.DSHPersonalStdioBinding, error)
}

var dshStdioUpgrader = websocket.Upgrader{HandshakeTimeout: 5 * time.Second, CheckOrigin: func(r *http.Request) bool {
	return len(r.Header.Values("Origin")) == 0
}}

const dshStdioFrameLimit = 1 << 20

func (h *DSHRuntimeHandler) PersonalStdio(c *gin.Context) {
	grant, ok := h.authorize(c)
	if !ok {
		return
	}
	if !websocket.IsWebSocketUpgrade(c.Request) || len(c.Request.Header.Values("Origin")) != 0 {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	query := c.Request.URL.Query()
	for _, key := range []string{"runId", "attempt", "fence", "assetId", "version"} {
		if len(query[key]) != 1 {
			writeError(c, apperrors.ErrInvalidArgument)
			return
		}
	}
	attempt, errA := strconv.ParseInt(query.Get("attempt"), 10, 64)
	fence, errF := strconv.ParseInt(query.Get("fence"), 10, 64)
	id := domain.DSHIdentity{RunID: query.Get("runId"), Attempt: attempt, Fence: fence}
	assetID, version := query.Get("assetId"), query.Get("version")
	if errA != nil || errF != nil || !validDSHCallbackIdentity(id.RunID, attempt, fence) ||
		!contractenv.ValidPersonalMCPStdioAssetRef(assetID, version) {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	service, ok := h.service.(dshPersonalStdioService)
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	binding, err := service.PrepareDSHPersonalStdio(c.Request.Context(), grant, id, assetID, version)
	if err != nil {
		writeError(c, err)
		return
	}
	client, err := dshStdioUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer client.Close()
	ctx, cancel := context.WithDeadline(c.Request.Context(), binding.Deadline)
	defer cancel()
	agent, err := binding.Open(ctx)
	if err != nil {
		if errors.Is(err, apperrors.ErrConflict) || errors.Is(err, apperrors.ErrAccessDenied) {
			_ = client.SetWriteDeadline(time.Now().Add(time.Second))
			_ = client.WriteMessage(websocket.TextMessage, []byte(`{"status":"unavailable"}`))
		}
		return
	}
	defer agent.Close()
	relayDSHStdio(ctx, cancel, binding, client, agent)
}

func relayDSHStdio(ctx context.Context, cancel context.CancelFunc, binding appcopilot.DSHPersonalStdioBinding, client, agent appcopilot.DSHPersonalStdioPeer) {
	client.SetReadLimit(dshStdioFrameLimit)
	agent.SetReadLimit(dshStdioFrameLimit)
	stop := context.AfterFunc(ctx, func() {
		_ = client.Close()
		_ = agent.Close()
	})
	defer stop()
	closing := make(chan struct{})
	var agentWrites sync.Mutex
	sendAgent := func(kind int, frame []byte) error {
		agentWrites.Lock()
		defer agentWrites.Unlock()
		select {
		case <-closing:
			return nil
		default:
		}
		if kind == websocket.TextMessage {
			close(closing)
		}
		return writeDSHStdio(agent, kind, frame)
	}
	go readDSHStdioInput(ctx, cancel, binding, client, sendAgent)
	go watchDSHStdioBinding(ctx, cancel, binding, closing)
	relayDSHStdioOutput(ctx, binding, client, agent, sendAgent)
}

func relayDSHStdioOutput(ctx context.Context, binding appcopilot.DSHPersonalStdioBinding, client, agent appcopilot.DSHPersonalStdioPeer, sendAgent func(int, []byte) error) {
	var pending []byte
	for {
		kind, frame, err := agent.ReadMessage()
		if err != nil {
			return
		}
		if kind == websocket.TextMessage {
			forwardDSHStdioStatus(ctx, binding, client, frame)
			return
		}
		if kind != websocket.BinaryMessage {
			return
		}
		// Agent streams stdout chunks, which may split or combine JSON-RPC lines.
		for len(frame) > 0 {
			lineEnd := bytes.IndexByte(frame, '\n')
			if lineEnd < 0 {
				if len(pending)+len(frame) >= dshStdioFrameLimit {
					return
				}
				pending = append(pending, frame...)
				break
			}
			if len(pending)+lineEnd+1 > dshStdioFrameLimit {
				return
			}
			line := append(pending, frame[:lineEnd+1]...)
			if response := appcopilot.DSHPersonalStdioPingResponse(line); response != nil {
				if binding.Check(ctx, nil) != nil || sendAgent(websocket.BinaryMessage, response) != nil {
					return
				}
			} else if writeDSHStdio(client, kind, line) != nil {
				return
			}
			pending = pending[:0]
			frame = frame[lineEnd+1:]
		}
	}
}

func forwardDSHStdioStatus(ctx context.Context, binding appcopilot.DSHPersonalStdioBinding, client appcopilot.DSHPersonalStdioPeer, frame []byte) {
	switch string(frame) {
	case `{"status":"stopped"}`:
		frame = []byte(`{"status":"unknown"}`)
		if binding.ContainerStopped != nil && binding.ContainerStopped(ctx) == nil {
			frame = []byte(`{"status":"closed"}`)
		}
	case `{"status":"closed"}`, `{"status":"unknown"}`, `{"status":"unavailable"}`:
	default:
		return
	}
	_ = writeDSHStdio(client, websocket.TextMessage, frame)
}

func writeDSHStdio(peer appcopilot.DSHPersonalStdioPeer, kind int, body []byte) error {
	if err := peer.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	return peer.WriteMessage(kind, body)
}

func readDSHStdioInput(ctx context.Context, cancel context.CancelFunc, binding appcopilot.DSHPersonalStdioBinding, client appcopilot.DSHPersonalStdioPeer, sendAgent func(int, []byte) error) {
	for {
		kind, frame, err := client.ReadMessage()
		if err != nil {
			cancel()
			return
		}
		if kind == websocket.TextMessage && string(frame) == `{"close":true}` {
			if sendAgent(kind, frame) != nil {
				cancel()
			}
			return
		}
		if kind != websocket.BinaryMessage || binding.Check(ctx, frame) != nil || sendAgent(kind, frame) != nil {
			cancel()
			return
		}
	}
}

func watchDSHStdioBinding(ctx context.Context, cancel context.CancelFunc, binding appcopilot.DSHPersonalStdioBinding, closing <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-closing:
			return
		case <-ticker.C:
			if binding.Check(ctx, nil) != nil {
				cancel()
				return
			}
		}
	}
}
