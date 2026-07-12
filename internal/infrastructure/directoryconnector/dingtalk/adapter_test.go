package dingtalk

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"net/http"
	"net/http/httptest"
	"testing"
)

type resolverStub struct{}

func (resolverStub) ResolveLoginProvider(context.Context, string) (domainsettings.LoginProviderSettings, error) {
	return domainsettings.LoginProviderSettings{ClientID: "app", ClientSecret: "secret"}, nil
}
func TestAdapterNormalizesDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			_, _ = w.Write([]byte(`{"accessToken":"token","expireIn":7200}`))
		case "/topapi/v2/department/listsub":
			_, _ = w.Write([]byte(`{"errcode":0,"result":[{"dept_id":2,"parent_id":1,"name":"Engineering"}]}`))
		case "/topapi/v2/user/list":
			_, _ = w.Write([]byte(`{"errcode":0,"result":{"has_more":false,"list":[{"userid":"u1","name":"Ada","active":true,"dept_id_list":[2]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter, err := NewAdapter(resolverStub{}, server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := domain.Connection{ID: "c1", LoginProviderID: "p1"}
	orgs, _, err := adapter.ListOrganizations(context.Background(), c)
	if err != nil || len(orgs) != 1 {
		t.Fatalf("orgs=%#v err=%v", orgs, err)
	}
	people, err := adapter.ListPeople(context.Background(), c)
	if err != nil || len(people) != 1 || people[0].ProviderSubject != "u1" {
		t.Fatalf("people=%#v err=%v", people, err)
	}
	memberships, err := adapter.ListMemberships(context.Background(), c)
	if err != nil || len(memberships) != 1 {
		t.Fatalf("memberships=%#v err=%v", memberships, err)
	}
}
