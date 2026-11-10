package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDeleteObservedWorkloadAgentNeverFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name, contentType string
		status            int
		want              error
	}{
		{name: "old agent", contentType: "text/plain", status: 404, want: apperrors.ErrUnsupportedOperation},
		{name: "resource missing", contentType: "application/json", status: 404, want: apperrors.ErrNotFound},
		{name: "redirect to legacy route", status: 307, want: apperrors.ErrUnsupportedOperation},
		{name: "unexpected successful response", status: 200, want: apperrors.ErrUnsupportedOperation},
		{name: "replacement", contentType: "application/json", status: 409, want: apperrors.ErrConflict},
		{name: "success", status: 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/platform/ownership-v2/resources/observed" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				var body contractresource.ObservedWorkloadDeleteRequest
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ExpectedUID != "uid-app" || body.Namespace != "team" || body.Kind != "Pod" || body.Name != "app" {
					t.Errorf("body=%+v, error=%v", body, err)
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set("Location", "/api/v1/platform/ownership-v2/resources")
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			client := &Client{baseURL: server.URL, httpClient: server.Client()}
			err := client.DeleteObservedWorkload(t.Context(), "team", "Pod", "app", "uid-app")
			if !errors.Is(err, tc.want) || requests.Load() != 1 {
				t.Fatalf("delete=%v, requests=%d", err, requests.Load())
			}
		})
	}
}
