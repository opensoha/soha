package feishu_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	directoryconnector "github.com/opensoha/soha/internal/infrastructure/directoryconnector"
	"github.com/opensoha/soha/internal/infrastructure/directoryconnector/feishu"
)

func TestClient_ListOrganizations(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("page_token"); got != "page-1" {
			t.Errorf("page_token = %q", got)
		}
		if got := r.URL.Query().Get("fetch_child"); got != "true" {
			t.Errorf("fetch_child = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"items":[{"department_id":"legacy-1","open_department_id":"od-1","name":"Platform","parent_department_id":"0"},{"open_department_id":"od-2","name":"Runtime","parent_department_id":"od-1"}],"page_token":"page-2","has_more":true}}`))
	}))
	defer server.Close()

	client := newClient(t, server.URL)
	page, err := client.ListOrganizations(context.Background(), "page-1")
	if err != nil {
		t.Fatalf("ListOrganizations() error = %v", err)
	}
	if page.NextToken != "page-2" || len(page.Items) != 2 {
		t.Fatalf("page = %#v", page)
	}
	if page.Items[0].ExternalID != "od-1" || page.Items[0].ParentExternalID != "" {
		t.Fatalf("root organization = %#v", page.Items[0])
	}
	if page.Items[1].ParentExternalID != "od-1" {
		t.Fatalf("child organization = %#v", page.Items[1])
	}
}

func TestClient_ListPeopleAndMemberships(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("department_id"); got != "od-1" {
			t.Errorf("department_id = %q", got)
		}
		if got := r.URL.Query().Get("user_id_type"); got != "open_id" {
			t.Errorf("user_id_type = %q", got)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"user_id":"u1","open_id":"ou_1","union_id":"on_1","name":"Shan","email":"shan@example.com","mobile":"13800000000","department_ids":["od-1","od-2"],"avatar":{"avatar_origin":"https://avatar.example/original"},"status":{"is_activated":true,"is_frozen":false,"is_resigned":false}}],"page_token":"next"}}`))
	}))
	defer server.Close()

	client := newClient(t, server.URL)
	people, err := client.ListPeople(context.Background(), "od-1", "")
	if err != nil {
		t.Fatalf("ListPeople() error = %v", err)
	}
	if len(people.Items) != 1 || people.Items[0].ExternalID != "ou_1" || people.Items[0].ProviderSubject != "ou_1" || !people.Items[0].Active {
		t.Fatalf("people = %#v", people)
	}
	memberships, err := client.ListMemberships(context.Background(), "od-1", "")
	if err != nil {
		t.Fatalf("ListMemberships() error = %v", err)
	}
	if len(memberships.Items) != 2 || memberships.Items[1].OrganizationExternalID != "od-2" {
		t.Fatalf("memberships = %#v", memberships)
	}
}

func TestClient_NormalizesProviderErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.Header().Set("X-Tt-Logid", "log-123")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":99991400,"msg":"rate limited"}`))
	}))
	defer server.Close()

	client := newClient(t, server.URL)
	_, err := client.ListOrganizations(context.Background(), "")
	var providerErr *directoryconnector.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want ProviderError", err)
	}
	if providerErr.RetryAfter != 7*time.Second || providerErr.RequestID != "log-123" || !providerErr.Temporary() {
		t.Fatalf("provider error = %#v", providerErr)
	}
	if got := providerErr.Error(); got == "" || contains(got, "tenant-token") {
		t.Fatalf("unsafe error = %q", got)
	}
}

func TestClient_RejectsMissingOrganizationForPeople(t *testing.T) {
	t.Parallel()
	client, err := feishu.New("token")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListPeople(context.Background(), "", "")
	if err == nil {
		t.Fatal("ListPeople() error = nil")
	}
}

func newClient(t *testing.T, baseURL string) *feishu.Client {
	t.Helper()
	client, err := feishu.New("tenant-token", feishu.WithBaseURL(baseURL), feishu.WithPageSize(20))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func contains(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
