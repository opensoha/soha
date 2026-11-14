package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAIRunEventsFollowDrainsEventsCommittedBeforeTerminalStatus(t *testing.T) {
	for _, status := range []string{"completed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			pages, statusReads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("following a terminal run must only read")
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/events") {
					pages++
					if pages == 1 {
						_, _ = fmt.Fprint(w, `{"data":{"items":[],"nextSequence":0,"hasMore":false,"resyncRequired":false}}`)
					} else {
						_, _ = fmt.Fprintf(w, `{"data":{"items":[%s],"nextSequence":1,"hasMore":false,"resyncRequired":false}}`, aiTestStreamEvent(1, `"type":"agent.status","runId":"run:1","status":"`+status+`"`))
					}
					return
				}
				statusReads++
				// The terminal transaction commits its event after the first page
				// was read and before this authoritative status query.
				_, _ = fmt.Fprintf(w, `{"items":[{"id":"run:1","status":%q}]}`, status)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"ai", "runs", "events", "run:1", "--follow", "--wait-timeout", "5s"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			wantCode := 0
			if status == "canceled" {
				wantCode = 1
			}
			var sequences []int
			var receipt *aiRunStreamReceipt
			for _, raw := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'}) {
				var frame aiRunStreamOutput
				if err := json.Unmarshal(raw, &frame); err != nil {
					t.Fatal(err)
				}
				if frame.Data != nil && len(frame.Data.Items) > 0 {
					sequences = append(sequences, frame.Data.NextSequence)
				}
				if frame.Receipt != nil {
					receipt = frame.Receipt
				}
			}
			if code != wantCode || pages != 2 || statusReads != 1 || !reflect.DeepEqual(sequences, []int{1}) || receipt == nil || receipt.NextSequence != 1 || receipt.Status != status {
				t.Fatalf("terminal events lost: code=%d pages=%d statusReads=%d sequences=%v receipt=%+v stderr=%s", code, pages, statusReads, sequences, receipt, stderr.String())
			}
		})
	}
}

func TestAIRunEventsFollowUsesDurableCursorAndServerTerminalStatus(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer profile-token" {
			t.Error("follow must use authenticated reads only")
		}
		w.Header().Set("Content-Type", "application/json")
		switch len(paths) {
		case 1:
			_, _ = fmt.Fprintf(w, `{"data":{"items":[%s],"nextSequence":41,"hasMore":true,"resyncRequired":false}}`, aiTestStreamEvent(41, `"type":"agent.status","runId":"run:1","status":"running"`))
		case 2:
			_, _ = fmt.Fprint(w, `{"data":{"items":[],"nextSequence":41,"hasMore":false,"resyncRequired":false}}`)
		case 3:
			_, _ = fmt.Fprint(w, `{"items":[{"id":"other","status":"running"},{"id":"run:1","status":"completed"}]}`)
		case 4:
			_, _ = fmt.Fprint(w, `{"data":{"items":[],"nextSequence":41,"hasMore":false,"resyncRequired":false}}`)
		default:
			t.Error("unexpected extra request")
		}
	}))
	defer server.Close()
	var out, stderr bytes.Buffer
	code := Run(t.Context(), []string{"ai", "runs", "events", "run:1", "--after-sequence", "40", "--follow", "--session-id", "s:1"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
	want := []string{copilotPath + "/agent-runs/run:1/events?afterSequence=40", copilotPath + "/agent-runs/run:1/events?afterSequence=41", copilotPath + "/agent-runs?sessionId=s%3A1", copilotPath + "/agent-runs/run:1/events?afterSequence=41"}
	if code != 0 || !reflect.DeepEqual(paths, want) {
		t.Fatalf("code=%d paths=%v stderr=%s", code, paths, stderr.String())
	}
	var lines []map[string]any
	for _, raw := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'}) {
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil || line["source"] != "run-events" {
			t.Fatalf("invalid durable output: %s", raw)
		}
		lines = append(lines, line)
	}
	receipt, ok := lines[3]["receipt"].(map[string]any)
	if !ok {
		t.Fatal("durable output has no typed receipt")
	}
	if lines[0]["afterSequence"] != float64(40) || lines[1]["afterSequence"] != float64(41) || lines[2]["afterSequence"] != float64(41) || receipt["status"] != "completed" || receipt["nextSequence"] != float64(41) {
		t.Fatalf("incorrect applied cursor or terminal receipt: %s", out.String())
	}
}

func TestAIRunEventsFollowRejectsInvalidPagesWithoutFallback(t *testing.T) {
	cases := []struct{ name, page, message string }{
		{"resync", `{"items":[],"nextSequence":0,"hasMore":false,"resyncRequired":true}`, "requires resynchronization"},
		{"missing-fields", `{}`, "invalid run-events page"},
		{"null-items", `{"items":null,"nextSequence":4,"hasMore":false,"resyncRequired":false}`, "invalid run-events page"},
		{"cursor-backwards", `{"items":[],"nextSequence":3,"hasMore":false,"resyncRequired":false}`, "invalid run-events cursor"},
		{"cursor-stuck", `{"items":[],"nextSequence":4,"hasMore":true,"resyncRequired":false}`, "invalid run-events cursor"},
		{"wrong-run", fmt.Sprintf(`{"items":[%s],"nextSequence":5,"hasMore":false,"resyncRequired":false}`, aiTestStreamEvent(5, `"type":"agent.status","runId":"other","status":"succeeded"`)), "invalid run-events identity"},
		{"out-of-order", fmt.Sprintf(`{"items":[%s,%s],"nextSequence":6,"hasMore":false,"resyncRequired":false}`, aiTestStreamEvent(6, `"type":"agent.status","runId":"run:1","status":"running"`), aiTestStreamEvent(5, `"type":"agent.status","runId":"run:1","status":"succeeded"`)), "invalid run-events identity"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = fmt.Fprint(w, `{"data":`+test.page+`}`)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"ai", "runs", "events", "run:1", "--after-sequence", "4", "--follow"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 1 || calls != 1 || !strings.Contains(stderr.String(), test.message) || strings.Contains(out.String(), `"receipt"`) {
				t.Fatalf("code=%d calls=%d out=%s stderr=%s", code, calls, out.String(), stderr.String())
			}
			if test.name == "resync" && !strings.Contains(out.String(), `"resyncRequired":true`) {
				t.Fatal("server recovery instruction was dropped")
			}
		})
	}
}

func TestAIRunEventsFollowNeverTreatsPausedAsTerminalOrCancelsOnExit(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != "GET" {
			t.Error("reader submitted a mutation")
		}
		if strings.HasSuffix(r.URL.Path, "/events") {
			_, _ = fmt.Fprint(w, `{"data":{"items":[],"nextSequence":0,"hasMore":false,"resyncRequired":false}}`)
		} else {
			_, _ = fmt.Fprint(w, `{"items":[{"id":"run:1","status":"waiting_for_input"}]}`)
			cancel()
		}
	}))
	defer server.Close()
	var out, stderr bytes.Buffer
	code := Run(t.Context(), []string{"ai", "runs", "events", "run:1", "--follow"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL), StreamingContext: func(context.Context) (context.Context, context.CancelFunc) { return ctx, cancel }})
	if code != 130 || len(paths) != 2 || strings.Contains(out.String(), `"receipt"`) {
		t.Fatalf("code=%d paths=%v out=%s stderr=%s", code, paths, out.String(), stderr.String())
	}
}

func TestAIRunEventsFollowTerminalFailuresAndUnsupported(t *testing.T) {
	for _, status := range []string{"failed", "canceled", "callback_timeout", "not-returned", "unsupported"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if status == "unsupported" {
					w.WriteHeader(http.StatusNotImplemented)
					_, _ = fmt.Fprint(w, `{"code":"unsupported","message":"profile-token"}`)
				} else if strings.HasSuffix(r.URL.Path, "/events") {
					_, _ = fmt.Fprint(w, `{"data":{"items":[],"nextSequence":0,"hasMore":false,"resyncRequired":false}}`)
				} else if status == "not-returned" {
					_, _ = fmt.Fprint(w, `{"items":[]}`)
				} else {
					_, _ = fmt.Fprintf(w, `{"items":[{"id":"run:1","status":%q}]}`, status)
				}
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"ai", "runs", "events", "run:1", "--follow"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			wantCalls := 2
			if aiRunTerminal(status) {
				wantCalls = 3
			}
			if status == "unsupported" {
				wantCalls = 1
			}
			if code != 1 || calls != wantCalls || strings.Contains(stderr.String(), "profile-token") {
				t.Fatalf("code=%d calls=%d stderr=%s", code, calls, stderr.String())
			}
			if aiRunTerminal(status) && !strings.Contains(out.String(), `"status":"`+status+`"`) {
				t.Fatal("terminal failure receipt was lost")
			}
		})
	}
}

func TestAIStreamSecretRedactionPreservesNDJSON(t *testing.T) {
	secret := "quoted-\"\\-credential"
	var out bytes.Buffer
	err := writeAINdjson(&out, map[string]any{secret: []any{map[string]any{"content": secret, "password": "hidden"}}}, []string{secret})
	if err != nil || !json.Valid(out.Bytes()) || strings.Contains(out.String(), "credential") || strings.Contains(out.String(), "hidden") {
		t.Fatalf("invalid or unredacted output: %s err=%v", out.String(), err)
	}
}
