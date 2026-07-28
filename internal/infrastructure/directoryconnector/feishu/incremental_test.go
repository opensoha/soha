package feishu_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/infrastructure/directoryconnector/feishu"
)

func TestAdapterResolveDeltaFetchesOnlyChangedPerson(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/open-apis/contact/v3/users/ou_1" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"user":{"open_id":"ou_1","name":"Shan","email":"shan@example.com","department_ids":["od-1","od-2"],"status":{"is_activated":true}}}}`))
	}))
	defer server.Close()
	adapter, err := feishu.NewAdapter(func(context.Context, domain.Connection) (string, error) { return "token", nil }, feishu.WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	event := domain.EventEnvelope{EventType: "contact.user.updated_v3", OccurredAt: time.Now().UTC(), Payload: []byte(`{"object":{"open_id":"ou_1"}}`)}
	delta, err := adapter.ResolveDelta(context.Background(), domain.Connection{ID: "c1", ProviderType: domain.ProviderFeishu}, event)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || delta.Person == nil || delta.Person.ExternalID != "ou_1" || len(delta.Memberships) != 2 {
		t.Fatalf("requests=%d delta=%+v", requests, delta)
	}
}

func TestAdapterResolveDeltaUsesCompleteEventPayloadWithoutProviderFetch(t *testing.T) {
	t.Parallel()
	adapter, err := feishu.NewAdapter(func(context.Context, domain.Connection) (string, error) {
		t.Fatal("complete event payload must not resolve provider credentials")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	payload := []byte(`{"object":{"open_id":"ou_1","name":"Shan","email":"shan@example.com","department_ids":["od-1"],"status":{"is_activated":true}}}`)
	delta, err := adapter.ResolveDelta(context.Background(), domain.Connection{ID: "c1", ProviderType: domain.ProviderFeishu}, domain.EventEnvelope{EventType: "contact.user.updated_v3", OccurredAt: at, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if delta.Person == nil || delta.Person.DisplayName != "Shan" || delta.Person.Status != domain.ProjectionActive || len(delta.Memberships) != 1 {
		t.Fatalf("delta=%+v", delta)
	}
	departmentPayload := []byte(`{"object":{"open_department_id":"od-2","name":"Platform","parent_department_id":"od-1"}}`)
	delta, err = adapter.ResolveDelta(context.Background(), domain.Connection{ID: "c1", ProviderType: domain.ProviderFeishu}, domain.EventEnvelope{EventType: "contact.department.updated_v3", OccurredAt: at, Payload: departmentPayload})
	if err != nil {
		t.Fatal(err)
	}
	if delta.Organization == nil || delta.Organization.Name != "Platform" || delta.Organization.ExternalParentID != "od-1" {
		t.Fatalf("delta=%+v", delta)
	}
}

func TestAdapterResolveDeltaMapsDeletedPersonWithoutProviderFetch(t *testing.T) {
	t.Parallel()
	adapter, err := feishu.NewAdapter(func(context.Context, domain.Connection) (string, error) { return "token", nil })
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	delta, err := adapter.ResolveDelta(context.Background(), domain.Connection{ID: "c1", ProviderType: domain.ProviderFeishu}, domain.EventEnvelope{EventType: "contact.user.deleted_v3", OccurredAt: at, Payload: []byte(`{"object":{"open_id":"ou_1"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if delta.Person == nil || delta.Person.Status != domain.ProjectionArchived || delta.Person.DepartedAt == nil || !delta.Person.DepartedAt.Equal(at) {
		t.Fatalf("delta=%+v", delta)
	}
}

func TestAdapterResolveDeltaFetchesOnlyChangedOrganization(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/contact/v3/departments/od-2" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"department":{"open_department_id":"od-2","name":"Runtime","parent_department_id":"od-1"}}}`))
	}))
	defer server.Close()
	adapter, err := feishu.NewAdapter(func(context.Context, domain.Connection) (string, error) { return "token", nil }, feishu.WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	delta, err := adapter.ResolveDelta(context.Background(), domain.Connection{ID: "c1", ProviderType: domain.ProviderFeishu}, domain.EventEnvelope{EventType: "contact.department.updated_v3", OccurredAt: time.Now().UTC(), Payload: []byte(`{"object":{"open_department_id":"od-2"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if delta.Organization == nil || delta.Organization.Name != "Runtime" || delta.Organization.ExternalParentID != "od-1" {
		t.Fatalf("delta=%+v", delta)
	}
}
