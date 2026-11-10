package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestConditionalAgentClientNeverFallsBackToLegacyMutation(t *testing.T) {
	for _, status := range []int{404, 405} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != conditionalResourcePath {
					t.Errorf("unexpected fallback %s", r.URL.Path)
				}
				http.Error(w, "old Agent", status)
			}))
			defer server.Close()
			client := (&Client{baseURL: server.URL, token: "test", httpClient: server.Client()})
			err := client.DeleteResourceConditional(context.Background(), "demo", "ConfigMap", "app", contractresource.ResourceMutationPreconditions{ExpectedUID: "uid", ExpectedResourceVersion: "rv"})
			if !errors.Is(err, apperrors.ErrUnsupportedOperation) || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}

func TestConditionalAgentDeletionRequiresMatchingReceipt(t *testing.T) {
	guard := contractresource.ResourceMutationPreconditions{ExpectedUID: "uid", ExpectedResourceVersion: "rv"}
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "matching", false: "wrong identity"}[valid], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req contractresource.ConditionalResourceMutationRequest
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.Preconditions != guard {
					t.Errorf("guard missing: %#v", req)
				}
				receipt := contractresource.ResourceDeletionReceipt{Accepted: true, State: "deletion_requested", Kind: req.Kind, Name: req.Name, Namespace: req.Namespace, Preconditions: req.Preconditions}
				if !valid {
					receipt.Preconditions.ExpectedUID = "replaced"
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(202)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": receipt})
			}))
			defer server.Close()
			err := (&Client{baseURL: server.URL, token: "test", httpClient: server.Client()}).DeleteResourceConditional(t.Context(), "demo", "ConfigMap", "app", guard)
			if (err == nil) != valid {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
