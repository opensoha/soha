package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func TestResourceGraphMetadataTransportNeverFallsBackToSecretPayload(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusNotAcceptable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/namespaces/team-a/secrets/credentials" {
					t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Accept") != resourceGraphMetadataAccept {
					t.Errorf("unsafe Secret negotiation: %q", r.Header.Get("Accept"))
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == http.StatusOK {
					_ = json.NewEncoder(w).Encode(metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "meta.k8s.io/v1", Kind: "PartialObjectMetadata"}, ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "team-a", UID: "secret-uid"}})
					return
				}
				reason := metav1.StatusReasonForbidden
				if status == http.StatusNotAcceptable {
					reason = metav1.StatusReasonNotAcceptable
				}
				_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Reason: reason, Code: int32(status)})
			}))
			defer server.Close()
			config := &rest.Config{Host: server.URL}
			client, err := NewResourceGraphMetadataClient(config)
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace("team-a").Get(context.Background(), "credentials", metav1.GetOptions{})
			switch status {
			case http.StatusOK:
				if err != nil || result.UID != "secret-uid" {
					t.Fatalf("metadata=%+v err=%v", result, err)
				}
			case http.StatusForbidden:
				if !apierrors.IsForbidden(err) {
					t.Fatalf("forbidden mapping lost: %v", err)
				}
			case http.StatusNotAcceptable:
				if !apierrors.IsNotAcceptable(err) {
					t.Fatalf("metadata rejection lost: %v", err)
				}
			}
			if requests.Load() != 1 {
				t.Fatalf("metadata request fell back or retried: %d", requests.Load())
			}
			if config.WrapTransport != nil {
				t.Fatal("shared REST configuration was mutated")
			}
		})
	}
}

func TestResourceGraphMetadataTransportRejectsOrdinaryObjects(t *testing.T) {
	for _, tc := range []struct{ name, contentType, payload string }{
		{"ordinary Secret", "application/json", `{"kind":"Secret","apiVersion":"v1","metadata":{"name":"credentials","namespace":"team-a"},"data":{"password":"must-not-read"}}`},
		{"disguised Secret", resourceGraphMetadataAccept, `{"kind":"Secret","apiVersion":"v1","metadata":{"name":"credentials"},"data":{"password":"must-not-read"}}`},
		{"payload on metadata", "application/json", `{"kind":"PartialObjectMetadata","apiVersion":"meta.k8s.io/v1","metadata":{"name":"credentials"},"data":{"password":"must-not-read"}}`},
		{"wrong metadata version", "application/json", `{"kind":"PartialObjectMetadata","apiVersion":"v1","metadata":{"name":"credentials"}}`},
		{"missing identity", "application/json", `{"metadata":{"name":"credentials"}}`},
		{"duplicate identity", "application/json", `{"kind":"PartialObjectMetadata","kind":"Secret","apiVersion":"meta.k8s.io/v1","metadata":{"name":"credentials"}}`},
		{"trailing object", "application/json", `{"kind":"PartialObjectMetadata","apiVersion":"meta.k8s.io/v1","metadata":{}} {"kind":"Secret"}`},
		{"non-json metadata", "application/yaml", "kind: PartialObjectMetadata"},
		{"oversized metadata", "application/json", `{"kind":"PartialObjectMetadata","apiVersion":"meta.k8s.io/v1","metadata":{"name":"` + strings.Repeat("a", resourceGraphMetadataResponseLimit) + `"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = io.WriteString(w, tc.payload)
			}))
			defer server.Close()
			client, err := NewResourceGraphMetadataClient(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace("team-a").Get(context.Background(), "credentials", metav1.GetOptions{})
			if err == nil || result != nil || strings.Contains(err.Error(), "must-not-read") {
				t.Fatalf("ordinary payload accepted or exposed: result=%+v err=%v", result, err)
			}
			if requests.Load() != 1 {
				t.Fatalf("unsafe payload retried or fell back: %d", requests.Load())
			}
		})
	}
}

func TestResourceGraphMetadataTransportRejectsCredentialRedirect(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Store(true)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-graph-token" {
			t.Error("authenticated metadata request did not carry its credential")
		}
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewResourceGraphMetadataClient(&rest.Config{Host: server.URL, BearerToken: "synthetic-graph-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace("team-a").Get(context.Background(), "credentials", metav1.GetOptions{})
	if err == nil || !strings.Contains(err.Error(), "endpoint redirected") || redirected.Load() {
		t.Fatalf("redirect was not rejected: requested=%v err=%v", redirected.Load(), err)
	}
}

func TestResourceGraphMetadataTransportBoundsErrorPayload(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotAcceptable, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			transport := resourceGraphMetadataTransport{RoundTripper: resourceGraphCancelRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(strings.Repeat("a", resourceGraphMetadataResponseLimit+1)))}, nil
			})}
			request := httptest.NewRequest(http.MethodGet, "https://cluster.invalid/api/v1/namespaces/team-a/secrets/credentials", nil)
			response, err := transport.RoundTrip(request)
			if err == nil || !strings.Contains(err.Error(), "size limit") || response != nil {
				t.Fatalf("oversized error reached the metadata client: response=%v err=%v", response, err)
			}
		})
	}
}

type resourceGraphCancelRoundTripper func(*http.Request) (*http.Response, error)

func (f resourceGraphCancelRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type resourceGraphCancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	read   *atomic.Bool
}

func (b resourceGraphCancelBody) Read(payload []byte) (int, error) {
	b.read.Store(true)
	b.cancel()
	return b.ReadCloser.Read(payload)
}

func TestResourceGraphMetadataTransportRetainsBodyReadCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"kind":"PartialObjectMetadata","apiVersion":"meta.k8s.io/v1","metadata":`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var bodyRead atomic.Bool
	config := &rest.Config{Host: server.URL, WrapTransport: func(base http.RoundTripper) http.RoundTripper {
		return resourceGraphCancelRoundTripper(func(request *http.Request) (*http.Response, error) {
			response, err := base.RoundTrip(request)
			if err == nil {
				response.Body = resourceGraphCancelBody{ReadCloser: response.Body, cancel: cancel, read: &bodyRead}
			}
			return response, err
		})
	}}
	client, err := NewResourceGraphMetadataClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace("team-a").Get(ctx, "credentials", metav1.GetOptions{})
	if !bodyRead.Load() || !errors.Is(err, context.Canceled) {
		t.Fatalf("HTTP 200 body cancellation lost: bodyRead=%v err=%v", bodyRead.Load(), err)
	}
}
