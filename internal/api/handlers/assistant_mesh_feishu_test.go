package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"

	appevent "github.com/opensoha/soha/internal/application/event"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type feishuPublicEventSpy struct {
	appevent.Repository
	created int
}

func (r *feishuPublicEventSpy) Create(context.Context, domainevent.Envelope) error {
	r.created++
	return nil
}

func TestConnectorSinkFeishuMeshUsesPrivateDurableReceiver(t *testing.T) {
	public := &feishuPublicEventSpy{}
	service := appevent.New(public)
	service.SetConnectorEventSinkToken("generic-fixture")
	attempts := 0
	service.SetFeishuMeshReceiver("feishu-fixture", "cli_managed", "tenant_a", func(_ context.Context, ev domainevent.ConnectorEvent) error {
		if ev.ID != "provider-event" {
			t.Fatalf("wrong event delivered: %+v", ev)
		}
		attempts++
		if attempts == 1 {
			return apperrors.ErrServiceUnavailable
		}
		return nil
	})
	handler := newTestPlatformHandler(nil, nil, nil, service, nil, nil)
	const body = `{"connectorId":"feishu","events":[{"id":"provider-event","type":"im.message.receive_v1","source":"feishu","occurredAt":"2026-06-11T08:00:00Z","payload":{"appId":"cli_managed","tenantKey":"tenant_a","event":{"message":{"content":"private question"}}}}]}`
	for _, tc := range []struct {
		token    string
		status   int
		attempts int
	}{
		{"", http.StatusUnauthorized, 0},
		{"generic-fixture", http.StatusForbidden, 0},
		{"feishu-fixture", http.StatusServiceUnavailable, 1},
		{"feishu-fixture", http.StatusAccepted, 2},
	} {
		response := postConnectorEventSinkRequest(handler, body, map[string]string{"Authorization": "Bearer " + tc.token}, nil)
		if response.Code != tc.status || attempts != tc.attempts || public.created != 0 {
			t.Fatalf("status=%d want=%d attempts=%d want=%d public=%d", response.Code, tc.status, attempts, tc.attempts, public.created)
		}
	}
	other := strings.Replace(body, "cli_managed", "cli_legacy", 1)
	response := postConnectorEventSinkRequest(handler, other, map[string]string{"Authorization": "Bearer generic-fixture"}, nil)
	if response.Code != http.StatusAccepted || public.created != 1 || attempts != 2 {
		t.Fatalf("unrelated Feishu app stopped using its existing sink: status=%d public=%d attempts=%d", response.Code, public.created, attempts)
	}
}
