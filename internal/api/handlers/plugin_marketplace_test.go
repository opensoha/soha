package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

type legacyMarketplaceStub struct{}

func (legacyMarketplaceStub) ListMarketplace(context.Context, domainidentity.Principal, domainplugin.MarketplaceFilter) ([]domainplugin.MarketplacePlugin, error) {
	return []domainplugin.MarketplacePlugin{{ID: "legacy"}}, nil
}

func (legacyMarketplaceStub) GetMarketplace(context.Context, domainidentity.Principal, domainplugin.PluginVersionRef) (domainplugin.MarketplacePlugin, error) {
	return domainplugin.MarketplacePlugin{}, nil
}

type marketplaceSourceListerStub struct {
	legacyMarketplaceStub
	filter domainplugin.MarketplaceFilter
}

func (s *marketplaceSourceListerStub) ListMarketplaceWithSources(_ context.Context, _ domainidentity.Principal, filter domainplugin.MarketplaceFilter) (sohaapi.MarketplacePluginListEnvelope, error) {
	s.filter = filter
	return sohaapi.MarketplacePluginListEnvelope{
		Items: []sohaapi.MarketplacePlugin{{ID: "plugin"}},
		Meta: &sohaapi.MarketplacePluginListMeta{Sources: []sohaapi.MarketplaceSourceStatus{
			{SourceID: "official", Status: sohaapi.MarketplaceSourceStatusAvailable},
			{SourceID: "community", Status: sohaapi.MarketplaceSourceStatusUnavailable, Code: "source_unavailable"},
		}},
	}, nil
}

func TestMarketplaceListEnvelopePreservesItemsAndAddsSourceStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lister := &marketplaceSourceListerStub{}
	read := func(service PluginMarketplaceService) sohaapi.MarketplacePluginListEnvelope {
		t.Helper()
		handler := NewPluginHandlerWithServices(service, nil, nil, nil)
		router := gin.New()
		router.GET("/plugins/marketplace", handler.ListMarketplace)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/plugins/marketplace?q=skill&sourceId=official", nil))
		var result sohaapi.MarketplacePluginListEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &result); response.Code != http.StatusOK || err != nil {
			t.Fatalf("marketplace list returned %d %s (%v)", response.Code, response.Body, err)
		}
		return result
	}
	result := read(lister)
	if len(result.Items) != 1 || result.Items[0].ID != "plugin" || result.Meta == nil || len(result.Meta.Sources) != 2 ||
		result.Meta.Sources[1] != (sohaapi.MarketplaceSourceStatus{SourceID: "community", Status: sohaapi.MarketplaceSourceStatusUnavailable, Code: "source_unavailable"}) {
		t.Fatalf("marketplace list envelope = %#v", result)
	}
	if lister.filter.SourceID != "official" || lister.filter.Query != "skill" {
		t.Fatalf("marketplace filter = %#v", lister.filter)
	}
	result = read(legacyMarketplaceStub{})
	if len(result.Items) != 1 || result.Items[0].ID != "legacy" || result.Meta != nil {
		t.Fatalf("legacy marketplace list envelope = %#v", result)
	}
}
