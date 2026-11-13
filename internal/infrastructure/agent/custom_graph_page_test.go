package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	cr "github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestCustomGraphPageDoesNotReplayRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != customGraphPagePath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	c := &Client{baseURL: server.URL, httpClient: server.Client(), token: "synthetic"}
	_, err := c.ReadCustomResourceGraphPage(context.Background(), cr.CRDResourceDefinition{}, "")
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("expected unsupported redirect, got %v", err)
	}
}

func TestCustomGraphObjectRejectsLegacyRedirectAndOversizedResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		want error
	}{
		{"legacy Agent", http.StatusNotFound, apperrors.ErrUnsupportedOperation},
		{"redirect", http.StatusTemporaryRedirect, apperrors.ErrUnsupportedOperation},
		{"oversized", http.StatusOK, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != customGraphObjectPath || r.Header.Get("Authorization") != "Bearer synthetic" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(tc.code)
				if tc.code == http.StatusOK {
					_, _ = io.WriteString(w, `{"data":{}}`+strings.Repeat(" ", customGraphObjectEnvelopeLimit))
				}
			}))
			defer server.Close()
			c := &Client{baseURL: server.URL, httpClient: server.Client(), token: "synthetic"}
			item, err := c.ReadCustomResourceGraphObject(context.Background(), cr.CustomResourceGraphObjectRequest{Name: "widget"})
			if tc.want != nil && !errors.Is(err, tc.want) || tc.want == nil && (err == nil || !strings.Contains(err.Error(), "byte limit")) || item != nil || calls.Load() != 1 {
				t.Fatalf("response=%s err=%v calls=%d", item, err, calls.Load())
			}
		})
	}
}
