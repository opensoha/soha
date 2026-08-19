package handlers

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type reconnectPodStreamService struct {
	PodStreamService
	tailLines []int64
	cancel    context.CancelFunc
}

func (s *reconnectPodStreamService) StreamPodLogs(
	_ context.Context,
	_ domainidentity.Principal,
	_, _, _, _ string,
	tailLines, _ int64,
	_ io.Writer,
) error {
	s.tailLines = append(s.tailLines, tailLines)
	if len(s.tailLines) == 2 {
		s.cancel()
		return context.Canceled
	}
	return nil
}

func TestStreamPodLogsWithReconnectKeepsReplayBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := &reconnectPodStreamService{cancel: cancel}
	handler := &podStreamResourceHandler{service: service}

	err := handler.streamPodLogsWithReconnect(
		ctx,
		domainidentity.Principal{},
		"cluster-a",
		"default",
		"pod-a",
		"container-a",
		37,
		0,
		&logStreamWriter{},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("streamPodLogsWithReconnect() error = %v, want context.Canceled", err)
	}
	if want := []int64{37, 1}; !reflect.DeepEqual(service.tailLines, want) {
		t.Fatalf("tail lines = %v, want %v", service.tailLines, want)
	}
}

type terminalPodStreamService struct {
	PodStreamService
	err error
}

func (s *terminalPodStreamService) StreamPodTerminal(
	_ context.Context,
	_ domainidentity.Principal,
	_, _, _, _, _ string,
	_ io.Reader,
	_, _ io.Writer,
	_ domainresource.TerminalSizeQueue,
) error {
	return s.err
}

func TestStreamPodTerminalReportsSafeResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		err  error
		want terminalMessage
	}{
		{name: "closed", want: terminalMessage{Type: "exit", Message: "terminal session closed"}},
		{name: "canceled", err: context.Canceled, want: terminalMessage{Type: "exit", Message: "terminal session closed"}},
		{name: "failed", err: errors.New("backend refused: secret-token"), want: terminalMessage{Type: "error", Message: "terminal session ended with an error"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &podStreamResourceHandler{service: &terminalPodStreamService{err: tt.err}}
			router := gin.New()
			router.GET("/clusters/:clusterID/workloads/pods/:podName/terminal", handler.StreamPodTerminal)
			server := httptest.NewServer(router)
			defer server.Close()

			endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/clusters/cluster-a/workloads/pods/pod-a/terminal"
			conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
			if err != nil {
				t.Fatalf("dial terminal websocket: %v", err)
			}
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

			var status terminalMessage
			if err := conn.ReadJSON(&status); err != nil {
				t.Fatalf("read terminal status: %v", err)
			}
			if status.Type != "status" || status.Message != "terminal session connected" {
				t.Fatalf("terminal status = %#v", status)
			}

			var result terminalMessage
			if err := conn.ReadJSON(&result); err != nil {
				t.Fatalf("read terminal result: %v", err)
			}
			if result != tt.want {
				t.Fatalf("terminal result = %#v, want %#v", result, tt.want)
			}
			if strings.Contains(result.Message, "secret-token") {
				t.Fatalf("terminal result leaked internal error: %q", result.Message)
			}
		})
	}
}
