package kubernetes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/rest"
)

func TestResourcePresenceUsesExactMetadataCollection(t *testing.T) {
	for _, state := range []string{"absent", "present", "unsupported"} {
		t.Run(state, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/namespaces/team-a/configmaps" || r.URL.Query().Get("fieldSelector") != "metadata.name=app" || r.Header.Get("Accept") != "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1" {
					t.Error("presence expanded target or accepted full-object fallback")
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if state == "unsupported" {
					w.WriteHeader(404)
					return
				}
				items := ""
				if state == "present" {
					items = `{"metadata":{"name":"app","namespace":"team-a","uid":"uid-original","resourceVersion":"opaque-object"}}`
				}
				_, _ = fmt.Fprintf(w, `{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"resourceVersion":"opaque-collection"},"items":[%s]}`, items)
			}))
			defer server.Close()
			client := &Client{restConfig: &rest.Config{Host: server.URL}}
			result, err := client.ObserveResourcePresence(t.Context(), "team-a", "ConfigMap", "app")
			if (err == nil) != (state != "unsupported") || err == nil && (result.State != state || result.ValidateFor("ConfigMap", "team-a", "app") != nil) {
				t.Fatal("Agent presence result lost protocol or confused missing API with absence", err)
			}
		})
	}
}
