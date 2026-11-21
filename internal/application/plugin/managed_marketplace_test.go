package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

func TestManagedMarketplaceSourceCanonicalIdentity(t *testing.T) {
	sources, err := ManagedMarketplaceSources([]string{" https://CATALOG.example.com.:443/%69ndex.json ", "https://catalog.example.com/other.json"})
	if err != nil || len(sources) != 2 || sources[0].URL != "https://catalog.example.com/index.json" || sources[0].ID == sources[1].ID || sources[0].Official {
		t.Fatalf("sources = %#v, error = %v", sources, err)
	}
	canonical, err := managedMarketplaceSource(sources[0].URL)
	if err != nil || canonical != sources[0] {
		t.Fatalf("canonical source = %#v, error = %v", canonical, err)
	}
	for _, reserved := range []MarketplaceSource{{ID: sources[0].ID}, {URL: "https://CATALOG.example.com.:443/index.json"}} {
		remaining, err := ManagedMarketplaceSources([]string{sources[0].URL, sources[1].URL}, reserved)
		if err == nil || len(remaining) != 1 || remaining[0] != sources[1] {
			t.Fatalf("remaining = %#v, error = %v", remaining, err)
		}
	}
	if _, err := ManagedMarketplaceSources(make([]string, maxManagedMarketplaceSources+1)); err == nil {
		t.Fatal("oversized source list accepted")
	}
}

func TestManagedMarketplaceRejectsUnsafeURLsAndDialTargets(t *testing.T) {
	for _, target := range []string{
		"http://catalog.example.com/index.json", "https://localhost/index.json", "https://sub.localhost/index.json",
		"https://169.254.169.254/index.json", "https://[::ffff:10.0.0.1]/index.json", "https://[64:ff9b::a00:1]/index.json",
		"https://user:password@catalog.example.com/index.json", "https://catalog.example.com/index.json#fragment",
		"https://catalog.example.com/index.json?token=example", "https://catalog.example.com:65536/index.json",
	} {
		if _, err := managedMarketplaceSource(target); err == nil {
			t.Errorf("unsafe URL accepted: %s", target)
		}
	}
	for _, target := range []string{"127.0.0.1:443", "169.254.169.254:443", "168.63.129.16:443", "[::ffff:192.168.0.1]:443", "[2002:a00:1::]:443"} {
		if err := validateManagedMarketplaceDialTarget(context.Background(), "tcp", target, nil); err == nil {
			t.Errorf("unsafe resolved address accepted: %s", target)
		}
	}
	if err := validateManagedMarketplaceDialTarget(context.Background(), "tcp", "8.8.8.8:443", nil); err != nil {
		t.Fatalf("public resolved address rejected: %v", err)
	}
}

type marketplaceTestRoundTripper func(*http.Request) (*http.Response, error)

func (f marketplaceTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestManagedMarketplaceGuardsManifestAndRedirects(t *testing.T) {
	const url = "https://catalog.example.com/index.json"
	for _, test := range []struct {
		name, manifest, redirect string
		wantRequests             int
		wantSuccess              bool
	}{
		{name: "public manifest redirect", manifest: "https://catalog.example.com/manifest.json", redirect: "https://cdn.example.com/manifest.json", wantRequests: 3, wantSuccess: true},
		{name: "manifest downgrade", manifest: "http://catalog.example.com/manifest.json", wantRequests: 1},
		{name: "manifest private host", manifest: "https://127.0.0.1/manifest.json", wantRequests: 1},
		{name: "redirect downgrade", manifest: "https://catalog.example.com/manifest.json", redirect: "http://cdn.example.com/manifest.json", wantRequests: 2},
		{name: "redirect private host", manifest: "https://catalog.example.com/manifest.json", redirect: "https://192.168.0.1/manifest.json", wantRequests: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider, err := NewManagedMarketplaceProvider(MarketplaceSource{URL: url, ID: "opensoha-official", Official: true})
			if err != nil {
				t.Fatal(err)
			}
			if provider.client.Transport.(managedMarketplaceTransport).transport.(*http.Transport).Proxy != nil {
				t.Fatal("managed sources inherited a proxy")
			}
			requests := 0
			provider.client.Transport = managedMarketplaceTransport{transport: marketplaceTestRoundTripper(func(request *http.Request) (*http.Response, error) {
				requests++
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: request}
				body := `{"id":"sample","version":"1.0.0"}`
				switch request.URL.String() {
				case url:
					body = fmt.Sprintf(`{"schemaVersion":"marketplace.opensoha.io/v1","generatedAt":"2026-11-21T00:00:00Z","sourceId":"opensoha-official","plugins":[{"id":"sample","name":"Sample","publisher":"Example","type":"skill-pack","source":"marketplace:sample","manifest":{},"version":"1.0.0","verified":true,"publisherInfo":{"id":"example","name":"Example","verified":true,"verificationLevel":"official"},"versions":[{"version":"1.0.0","manifestUrl":%q}]}]}`, test.manifest)
				case test.manifest:
					if test.redirect != "" {
						response.StatusCode = http.StatusTemporaryRedirect
						response.Header.Set("Location", test.redirect)
					}
				case test.redirect:
					if request.Header.Get("Referer") != "" {
						t.Error("redirect leaked the referring URL")
					}
				default:
					t.Errorf("unexpected request to %s", request.URL)
				}
				response.Body = io.NopCloser(strings.NewReader(body))
				return response, nil
			})}
			resolved, err := provider.FetchManifest(context.Background(), domainplugin.PluginVersionRef{PluginID: "sample", Version: "1.0.0"})
			if (err == nil) != test.wantSuccess || requests != test.wantRequests {
				t.Fatalf("requests = %d, error = %v", requests, err)
			}
			if test.wantSuccess && (resolved.SourceInfo.Official || resolved.SourceID == "opensoha-official" || resolved.Plugin.Verified || resolved.Plugin.PublisherInfo.Verified || resolved.Plugin.PublisherInfo.VerificationLevel != "community") {
				t.Fatalf("remote official identity claims were not stripped: %#v", resolved)
			}
		})
	}
}

func TestManagedMarketplaceCatalogSchema(t *testing.T) {
	const emptyCatalog = `{"schemaVersion":"marketplace.opensoha.io/v1","generatedAt":"2026-11-21T00:00:00Z","sourceId":"example","plugins":[]}`
	if err := validateManagedMarketplaceCatalog([]byte(emptyCatalog)); err != nil {
		t.Fatalf("empty catalog rejected: %v", err)
	}
	for _, invalid := range []string{
		strings.Replace(emptyCatalog, "marketplace.opensoha.io/v1", "v2", 1),
		strings.Replace(emptyCatalog, `"plugins":[]`, `"plugins":[{}]`, 1),
		strings.Replace(emptyCatalog, `,"plugins":[]`, "", 1),
	} {
		if err := validateManagedMarketplaceCatalog([]byte(invalid)); err == nil {
			t.Fatalf("invalid catalog accepted: %s", invalid)
		}
	}
}
