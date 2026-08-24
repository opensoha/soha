package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type alertEventStreamServiceStub struct {
	signals   chan domainalert.AlertEventStreamSignal
	clusterID string
}

func (s *alertEventStreamServiceStub) ListEvents(context.Context, domainidentity.Principal, domainalert.AlertEventFilter) ([]domainalert.AlertEvent, error) {
	return nil, nil
}

func (s *alertEventStreamServiceStub) GetEvent(context.Context, domainidentity.Principal, string) (domainalert.AlertEvent, error) {
	return domainalert.AlertEvent{}, nil
}

func (s *alertEventStreamServiceStub) AcknowledgeEvent(context.Context, domainidentity.Principal, string) (domainalert.AlertEvent, error) {
	return domainalert.AlertEvent{}, nil
}

func (s *alertEventStreamServiceStub) ResolveEvent(context.Context, domainidentity.Principal, string) (domainalert.AlertEvent, error) {
	return domainalert.AlertEvent{}, nil
}

func (s *alertEventStreamServiceStub) HealEvent(context.Context, domainidentity.Principal, string, string) (domainalert.HealingRun, error) {
	return domainalert.HealingRun{}, nil
}

func (s *alertEventStreamServiceStub) SubscribeEventSignals(_ context.Context, _ domainidentity.Principal, clusterID string) (<-chan domainalert.AlertEventStreamSignal, error) {
	s.clusterID = clusterID
	return s.signals, nil
}

func TestStreamAlertEventsWritesSignals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	signals := make(chan domainalert.AlertEventStreamSignal, 1)
	signals <- domainalert.AlertEventStreamSignal{
		Type: "changed", ObservedAt: time.Date(2026, 8, 23, 8, 0, 0, 0, time.UTC),
		ClusterID: "cluster-a", EventID: "evt-1", EventStatus: "firing",
	}
	service := &alertEventStreamServiceStub{signals: signals}
	handler := &eventHandler{service: service}
	router := gin.New()
	router.GET("/alert-events/stream", handler.StreamEvents)
	server := httptest.NewServer(router)
	defer server.Close()

	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/alert-events/stream?clusterId=cluster-a"
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial alert event websocket: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var signal domainalert.AlertEventStreamSignal
	if err := conn.ReadJSON(&signal); err != nil {
		t.Fatalf("read alert event signal: %v", err)
	}
	if signal.EventID != "evt-1" || service.clusterID != "cluster-a" {
		t.Fatalf("signal = %#v, clusterID = %q", signal, service.clusterID)
	}
}
