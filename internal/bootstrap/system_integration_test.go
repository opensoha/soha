package bootstrap

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/netguard"
)

func TestSourceFactoryUsesPinnedClientForRepositoryIdentity(t *testing.T) {
	hits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/api/v4/projects/42" || r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Error("repository identity or credential was not preserved")
		}
		_, _ = w.Write([]byte(`{"http_url_to_repo":"https://git.example/team/repo.git"}`))
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	client, err := netguard.PinnedHTTPSClient(t.Context(), endpoint.Hostname(), endpoint.Port(), []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, ca)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	item := domain.Integration{Configuration: []sohaapi.SystemIntegrationConfigurationField{{Key: "base_url", Value: server.URL + "/api/v4"}}}
	adapter, err := (gitLabSourceAdapterFactory{}).Build(item, map[string]string{"token": "test-token"}, client)
	if err != nil {
		t.Fatal(err)
	}
	metadataReader, ok := adapter.(domainapp.SourceMetadataReader)
	if !ok {
		t.Fatal("source adapter does not expose metadata reader")
	}
	urls, err := metadataReader.RepositoryCloneURLs(t.Context(), "42")
	if err != nil || len(urls) == 0 || urls[0] != "https://git.example/team/repo.git" || hits != 1 {
		t.Fatalf("pinned identity read failed: %v", err)
	}
}
