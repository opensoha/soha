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
	expectedUID string,
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
		"",
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
	expectedUID string,
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
			conn, response, err := websocket.DefaultDialer.Dial(endpoint, nil)
			if response != nil && response.Body != nil {
				defer func() { _ = response.Body.Close() }()
			}
			if err != nil {
				t.Fatalf("dial terminal websocket: %v", err)
			}
			defer func() { _ = conn.Close() }()
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

type resourceEventStreamServiceStub struct {
	events    chan domainresource.ResourceStreamEvent
	namespace string
	kinds     []string
}

func (s *resourceEventStreamServiceStub) SubscribeResourceEvents(_ context.Context, _ domainidentity.Principal, _ string, namespace string, kinds []string) (<-chan domainresource.ResourceStreamEvent, func(), error) {
	s.namespace = namespace
	s.kinds = append([]string(nil), kinds...)
	return s.events, func() {}, nil
}

func TestStreamResourceEventsWritesStatusAndMetadataEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	events := make(chan domainresource.ResourceStreamEvent, 2)
	events <- domainresource.ResourceStreamEvent{
		Type: "status", ClusterID: "cluster-a", ObservedAt: "2026-08-23T07:59:59Z", CacheStatus: "live",
	}
	events <- domainresource.ResourceStreamEvent{
		Type: "added", ClusterID: "cluster-a", ObservedAt: "2026-08-23T08:00:00Z",
		Resource: &domainresource.ResourceRef{
			ClusterID: "cluster-a", APIVersion: "v1", Kind: "Pod", Name: "api", Namespace: "team-a",
			ScopeMode: domainresource.ResourceScopeModeNamespace,
		},
	}
	service := &resourceEventStreamServiceStub{events: events}
	handler := &resourceEventStreamHandler{service: service}
	router := gin.New()
	router.GET("/clusters/:clusterID/resources/stream", handler.StreamResourceEvents)
	server := httptest.NewServer(router)
	defer server.Close()

	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/clusters/cluster-a/resources/stream?namespace=team-a&kinds=Pod,Deployment"
	conn, response, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		t.Fatalf("dial resource websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var status domainresource.ResourceStreamEvent
	if err := conn.ReadJSON(&status); err != nil {
		t.Fatalf("read resource stream status: %v", err)
	}
	if status.Type != "status" || status.CacheStatus != "live" {
		t.Fatalf("status = %#v", status)
	}
	var event domainresource.ResourceStreamEvent
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read resource event: %v", err)
	}
	if event.Resource == nil || event.Resource.Name != "api" {
		t.Fatalf("event = %#v", event)
	}
	if service.namespace != "team-a" || !reflect.DeepEqual(service.kinds, []string{"Pod", "Deployment"}) {
		t.Fatalf("subscription = namespace %q kinds %#v", service.namespace, service.kinds)
	}
}

type cancelAwarePodStreamService struct {
	PodStreamService
	started  chan string
	released chan struct{}
}

func (s *cancelAwarePodStreamService) StreamPodTerminal(ctx context.Context, _ domainidentity.Principal, _, _, _, _, _ string, _ io.Reader, _, _ io.Writer, _ domainresource.TerminalSizeQueue, expectedUID string) error {
	s.started <- expectedUID
	defer close(s.released)
	<-ctx.Done()
	return ctx.Err()
}

func TestPodTerminalDisconnectReleasesUpstream(t *testing.T) {
	for _, mode := range []string{"close-message", "transport-close", "silent-peer-timeout"} {
		t.Run(mode, func(t *testing.T) {
			service := &cancelAwarePodStreamService{started: make(chan string, 1), released: make(chan struct{})}
			handler := &podStreamResourceHandler{service: service}
			router := gin.New()
			router.GET("/clusters/:clusterID/workloads/pods/:podName/terminal", handler.StreamPodTerminal)
			server := httptest.NewServer(router)
			defer server.Close()
			conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/clusters/c/workloads/pods/api/terminal?expectedUid=uid-a&namespace=test", nil)
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			select {
			case uid := <-service.started:
				if uid != "uid-a" {
					t.Fatalf("UID = %q", uid)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("upstream did not start")
			}
			switch mode {
			case "close-message":
				if err := conn.WriteJSON(terminalMessage{Type: "input", Data: "blocked-input"}); err != nil {
					t.Fatal(err)
				}
				if err := conn.WriteJSON(terminalMessage{Type: "close"}); err != nil {
					t.Fatal(err)
				}
			case "transport-close":
				_ = conn.Close()
			}
			limit := 2 * time.Second
			if mode == "silent-peer-timeout" {
				limit = podLogPongWait + 2*time.Second
			}
			select {
			case <-service.released:
			case <-time.After(limit):
				t.Fatal("upstream was not released")
			}
		})
	}
}
