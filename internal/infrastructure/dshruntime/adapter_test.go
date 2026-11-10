package dshruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
)

func TestApplicationAdapter(t *testing.T) {
	in := startInput()
	in.ToolGrant = "grant"
	in.Tools = []Tool{{Name: "lookup", Description: "find", InputSchema: json.RawMessage(`{"type":"object"}`), ReadOnly: true}}
	in.Context = []ContextMessage{{Role: "user", Content: "old"}}
	calls := 0
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var out any
		switch r.URL.Path {
		case "/v1/runs":
			var wire startWire
			if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
				t.Fatal(err)
			}
			if wire.ToolGrant != in.ToolGrant || wire.Context[0].Content != "old" || wire.Tools[0].Name != "lookup" || wire.MaxOutputTokens != in.MaxOutputTokens {
				t.Fatal("start mapping", wire)
			}
			out = Receipt{SchemaVersion: SchemaVersion, Kind: "receipt", Submission: submission(), Status: "accepted", Sequence: 1}
		case "/v1/runs/run-1/attempts/1/events":
			out = EventPage{Items: []Event{eventResult(2)}, NextSequence: 2}
		default:
			s := statusResult()
			s.UsageKnown = true
			s.Usage = &Usage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}
			out = s
		}
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Fatal(err)
		}
	}, time.Second)
	a, err := NewAdapter(c)
	if err != nil {
		t.Fatal(err)
	}
	start := domain.DSHStart{DSHIdentity: domainIdentity(in.Identity), OwnerKey: in.OwnerKey, SessionID: in.SessionID, RequestID: in.RequestID, InputHash: in.InputHash, ContextRevision: in.ContextRevision, Profile: in.Profile, Model: in.Model, Prompt: in.Prompt, ToolCatalogRevision: in.ToolCatalogRevision, ToolGrant: in.ToolGrant, Deadline: in.Deadline, MaxOutputTokens: in.MaxOutputTokens, Context: []domain.DSHContextMessage{{Role: "user", Content: "old"}}, Tools: []domain.DSHTool{{Name: "lookup", Description: "find", InputSchema: in.Tools[0].InputSchema, ReadOnly: true}}}
	receipt, err := a.Start(context.Background(), start)
	if err != nil || receipt.SubmissionID != "sub-1" {
		t.Fatal(receipt, err)
	}
	for _, invoke := range []func() (domain.DSHStatus, error){func() (domain.DSHStatus, error) {
		return a.Lookup(context.Background(), start.DSHIdentity, start.InputHash)
	}, func() (domain.DSHStatus, error) { return a.Status(context.Background(), receipt.DSHSubmission) }, func() (domain.DSHStatus, error) { return a.Cancel(context.Background(), receipt.DSHSubmission, "user") }} {
		s, err := invoke()
		if err != nil || s.Usage == nil || s.Usage.TotalTokens != 5 || s.Fence != 2 {
			t.Fatal(s, err)
		}
	}
	page, err := a.Events(context.Background(), start.DSHIdentity, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Delta != "ok" || page.NextSequence != 2 {
		t.Fatal(page, err)
	}
	if calls != 5 {
		t.Fatal(calls)
	}
	if _, err := NewAdapter(nil); err == nil {
		t.Fatal("nil")
	}
}
func TestLookupDoesNotSubmitAndRejectsWrongBinding(t *testing.T) {
	for _, wrong := range []string{"identity", "hash"} {
		t.Run(wrong, func(t *testing.T) {
			calls := 0
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet {
					t.Fatal("lookup replayed submission")
				}
				s := statusResult()
				if wrong == "identity" {
					s.Fence++
				} else {
					s.InputHash = strings.Repeat("b", 64)
				}
				_ = json.NewEncoder(w).Encode(s)
			}, time.Second)
			_, err := c.Lookup(context.Background(), submission().Identity, submission().InputHash)
			if !errors.Is(err, ErrProtocol) || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestAdapterDistinguishesRejectedStartFromUncertainSubmission(t *testing.T) {
	wire := startInput()
	in := domain.DSHStart{DSHIdentity: domainIdentity(wire.Identity), OwnerKey: wire.OwnerKey, SessionID: wire.SessionID, RequestID: wire.RequestID, InputHash: wire.InputHash, ContextRevision: wire.ContextRevision, Profile: wire.Profile, Model: wire.Model, Prompt: wire.Prompt, ToolCatalogRevision: wire.ToolCatalogRevision, ToolGrant: wire.ToolGrant, Deadline: wire.Deadline, MaxOutputTokens: wire.MaxOutputTokens}
	for _, tc := range []struct {
		code     int
		rejected bool
	}{
		{http.StatusTooManyRequests, true},
		{http.StatusConflict, false},
		{http.StatusServiceUnavailable, false},
	} {
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
			}, time.Second)
			adapter, err := NewAdapter(client)
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.Start(t.Context(), in)
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.code || errors.Is(err, appcopilot.ErrDSHStartRejected) != tc.rejected {
				t.Fatalf("unexpected disposition: %v", err)
			}
			_, err = adapter.Start(t.Context(), domain.DSHStart{})
			if !errors.Is(err, ErrInput) || !errors.Is(err, appcopilot.ErrDSHStartRejected) {
				t.Fatalf("client input was not rejected before sending: %v", err)
			}
		})
	}
}
