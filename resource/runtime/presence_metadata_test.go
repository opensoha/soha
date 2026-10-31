package runtime

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func TestPresenceUsesOnlyBoundedPartialMetadataTransport(t *testing.T) {
	for _, scenario := range []string{"partial", "partial-null", "missing-items", "full-object", "unsupported", "oversized", "top-extra", "item-extra"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/namespaces/team-a/configmaps" || r.URL.Query().Get("fieldSelector") != "metadata.name=app" || r.URL.Query().Get("limit") != "2" || r.Header.Get("Accept") != "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1" {
					t.Error("metadata-only target or negotiation changed")
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch scenario {
				case "unsupported":
					w.WriteHeader(404)
				case "oversized":
					_, _ = io.WriteString(w, strings.Repeat(" ", (2<<20)+1))
				case "full-object":
					_, _ = io.WriteString(w, `{"apiVersion":"v1","kind":"ConfigMapList","metadata":{"resourceVersion":"opaque"},"items":[]}`)
				case "partial-null":
					_, _ = io.WriteString(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"resourceVersion":"opaque"},"items":null}`)
				case "missing-items":
					_, _ = io.WriteString(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"resourceVersion":"opaque"}}`)
				case "top-extra":
					_, _ = io.WriteString(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"resourceVersion":"opaque"},"items":[],"spec":{"private":"must-not-cross"}}`)
				case "item-extra":
					_, _ = io.WriteString(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"resourceVersion":"opaque"},"items":[{"metadata":{"name":"app","namespace":"team-a","uid":"uid","resourceVersion":"v"},"data":{"private":"must-not-cross"}}]}`)
				default:
					_, _ = io.WriteString(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"resourceVersion":"opaque"},"items":[]}`)
				}
			}))
			defer server.Close()
			client, err := NewPresenceMetadataClient(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			resource := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("team-a")
			presence, err := ObserveResourcePresence(t.Context(), resource, "ConfigMap", "team-a", "app")
			valid := scenario == "partial" || scenario == "partial-null"
			if (err == nil) != valid || err == nil && (presence.State != "absent" || presence.CollectionResourceVersion != "opaque") {
				t.Fatal("non-metadata/error/truncated response became absence", err)
			}
		})
	}
}
