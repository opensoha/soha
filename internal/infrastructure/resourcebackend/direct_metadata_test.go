package resourcebackend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func TestListPartialMetadataRequestsOnlyMetadata(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/secrets" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		if request.Header.Get("Accept") != partialMetadataListAcceptType {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		if request.URL.Query().Get("labelSelector") != "owner=helm" {
			t.Errorf("labelSelector = %q", request.URL.Query().Get("labelSelector"))
		}
		if request.URL.Query().Get("limit") != "500" {
			t.Errorf("limit = %q", request.URL.Query().Get("limit"))
		}
		writer.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			_, _ = writer.Write([]byte(`{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"continue":"next"},"items":[{"metadata":{"name":"release-1","namespace":"team-a"}}]}`))
			return
		}
		if request.URL.Query().Get("continue") != "next" {
			t.Errorf("continue = %q", request.URL.Query().Get("continue"))
		}
		_, _ = writer.Write([]byte(`{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","items":[{"metadata":{"name":"release-2","namespace":"team-a"}}]}`))
	}))
	defer server.Close()

	items, err := listPartialMetadata(context.Background(), &k8sinfra.Bundle{RESTConfig: &rest.Config{Host: server.URL}}, schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, true, "", metav1.ListOptions{LabelSelector: "owner=helm"})
	if err != nil {
		t.Fatalf("listPartialMetadata() error = %v", err)
	}
	if len(items) != 2 || items[0].Name != "release-1" || items[1].Name != "release-2" || calls.Load() != 2 {
		t.Fatalf("listPartialMetadata() = %#v", items)
	}
}

func TestListSecretMetadataDoesNotRequestSecretData(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != partialMetadataListAcceptType {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","items":[{"metadata":{"name":"registry-token","namespace":"team-a"}}]}`))
	}))
	defer server.Close()

	items, err := listSecretMetadata(context.Background(), &k8sinfra.Bundle{RESTConfig: &rest.Config{Host: server.URL}}, "")
	if err != nil {
		t.Fatalf("listSecretMetadata() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "registry-token" || items[0].Type != "" || items[0].DataEntries != 0 {
		t.Fatalf("listSecretMetadata() = %#v", items)
	}
}

func TestListTableRequestsMetadataForAllNamespaces(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/configmaps" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		if request.Header.Get("Accept") != tableListAcceptType {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		if request.URL.Query().Get("includeObject") != string(metav1.IncludeMetadata) {
			t.Errorf("includeObject = %q", request.URL.Query().Get("includeObject"))
		}
		if request.URL.Query().Get("limit") != "500" {
			t.Errorf("limit = %q", request.URL.Query().Get("limit"))
		}
		writer.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			_, _ = writer.Write([]byte(`{"apiVersion":"meta.k8s.io/v1","kind":"Table","metadata":{"continue":"next"},"columnDefinitions":[{"name":"Name","type":"string"}],"rows":[{"cells":["first"]}]}`))
			return
		}
		if request.URL.Query().Get("continue") != "next" {
			t.Errorf("continue = %q", request.URL.Query().Get("continue"))
		}
		_, _ = writer.Write([]byte(`{"apiVersion":"meta.k8s.io/v1","kind":"Table","columnDefinitions":[{"name":"Name","type":"string"}],"rows":[{"cells":["second"]}]}`))
	}))
	defer server.Close()

	table, err := listTable(context.Background(), &k8sinfra.Bundle{RESTConfig: &rest.Config{Host: server.URL}}, schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, true, "")
	if err != nil {
		t.Fatalf("listTable() error = %v", err)
	}
	if len(table.Rows) != 2 || calls.Load() != 2 {
		t.Fatalf("listTable() rows = %d, calls = %d", len(table.Rows), calls.Load())
	}
}

func TestListTableRejectsRepeatedContinueToken(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"apiVersion":"meta.k8s.io/v1","kind":"Table","metadata":{"continue":"stuck"},"rows":[]}`))
	}))
	defer server.Close()

	_, err := listTable(context.Background(), &k8sinfra.Bundle{RESTConfig: &rest.Config{Host: server.URL}}, schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, true, "")
	if err == nil || !strings.Contains(err.Error(), "repeated continue token") {
		t.Fatalf("listTable() error = %v, want repeated token error", err)
	}
}

func tableTestMetadata(name, namespace string) runtime.RawExtension {
	return runtime.RawExtension{Object: &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, CreationTimestamp: metav1.Now()}}}
}

func TestListPartialMetadataRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat("x", maxMetadataResponseBytes+1)))
	}))
	defer server.Close()

	_, err := listPartialMetadata(context.Background(), &k8sinfra.Bundle{RESTConfig: &rest.Config{Host: server.URL}}, schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, true, "", metav1.ListOptions{})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("listPartialMetadata() error = %v, want response size limit", err)
	}
}
