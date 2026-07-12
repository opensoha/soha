package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

type resolverStub struct{}

func (resolverStub) ResolveLoginProvider(context.Context, string) (domainsettings.LoginProviderSettings, error) {
	return domainsettings.LoginProviderSettings{ClientID: "corp", ClientSecret: "secret"}, nil
}

func TestAdapterNormalizesDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_, _ = w.Write([]byte(`{"errcode":0,"access_token":"token","expires_in":7200}`))
		case "/cgi-bin/department/list":
			_, _ = w.Write([]byte(`{"errcode":0,"department":[{"id":2,"name":"Engineering","parentid":1}]}`))
		case "/cgi-bin/user/list":
			_, _ = w.Write([]byte(`{"errcode":0,"userlist":[{"userid":"zhangsan","name":"张三","department":[2],"status":1}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter, err := NewAdapter(resolverStub{}, server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	connection := domain.Connection{ID: "c1", LoginProviderID: "p1"}
	orgs, _, err := adapter.ListOrganizations(context.Background(), connection)
	if err != nil || len(orgs) != 1 || orgs[0].ExternalID != "2" {
		t.Fatalf("orgs=%#v err=%v", orgs, err)
	}
	people, err := adapter.ListPeople(context.Background(), connection)
	if err != nil || len(people) != 1 || people[0].ProviderSubject != "zhangsan" {
		t.Fatalf("people=%#v err=%v", people, err)
	}
	memberships, err := adapter.ListMemberships(context.Background(), connection)
	if err != nil || len(memberships) != 1 || memberships[0].ExternalOrganizationID != "2" {
		t.Fatalf("memberships=%#v err=%v", memberships, err)
	}
}
