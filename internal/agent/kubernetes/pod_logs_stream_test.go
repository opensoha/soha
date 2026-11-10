package kubernetes

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func TestStreamPodLogsCopiesKubernetesFollowStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/platform/pods/api-0/log" {
			t.Fatalf("path = %s, want pod log path", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("follow") != "true" || query.Get("container") != "app" || query.Get("tailLines") != "10" || query.Get("sinceSeconds") != "5" {
			t.Fatalf("query = %s, want follow/container/tail/since", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("line 1\nline 2\n"))
	}))
	defer server.Close()

	typed, err := kubernetes.NewForConfig(&rest.Config{
		Host:    server.URL,
		APIPath: "/api",
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{Version: "v1"},
			NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		},
	})
	if err != nil {
		t.Fatalf("NewForConfig() error = %v", err)
	}

	client := &Client{typed: typed}
	var out bytes.Buffer
	if err := client.StreamPodLogs(t.Context(), "platform", "api-0", "app", 10, 5, &out, ""); err != nil {
		t.Fatalf("StreamPodLogs() error = %v", err)
	}
	if out.String() != "line 1\nline 2\n" {
		t.Fatalf("output = %q, want streamed logs", out.String())
	}
}

func TestStreamPodLogsOmitsOptionalParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("follow") != "true" {
			t.Fatalf("query = %s, want follow=true", r.URL.RawQuery)
		}
		if query.Has("tailLines") || query.Has("sinceSeconds") || query.Has("container") {
			t.Fatalf("query = %s, want optional params omitted", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("ready\n"))
	}))
	defer server.Close()

	typed, err := kubernetes.NewForConfig(&rest.Config{
		Host:    server.URL,
		APIPath: "/api",
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &corev1.SchemeGroupVersion,
			NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		},
	})
	if err != nil {
		t.Fatalf("NewForConfig() error = %v", err)
	}

	client := &Client{typed: typed}
	var out bytes.Buffer
	if err := client.StreamPodLogs(t.Context(), "default", "api-0", "", 0, 0, &out, ""); err != nil {
		t.Fatalf("StreamPodLogs() error = %v", err)
	}
	if out.String() != "ready\n" {
		t.Fatalf("output = %q, want ready", out.String())
	}
}
