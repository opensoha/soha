package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type tuiFake struct {
	loads, actions, chats int
	scope                 tuiScope
	row                   tuiRow
	action                string
	err                   error
}

func (f *tuiFake) load(ctx context.Context, scope tuiScope, row *tuiRow) (tuiSnapshot, error) {
	f.loads++
	f.scope = scope
	return tuiSnapshot{rows: []tuiRow{{id: "task-1", name: "Original task", status: "running", value: map[string]any{"domain": "docker"}}}}, f.err
}
func (f *tuiFake) act(ctx context.Context, scope tuiScope, row tuiRow, action string) (any, error) {
	f.actions++
	f.scope, f.row, f.action = scope, row, action
	return map[string]any{"status": "canceling"}, nil
}
func (f *tuiFake) chat(ctx context.Context, session, key, content string, emit func(aiStreamOutput) error) error {
	f.chats++
	return f.err
}
func (f *tuiFake) follow(ctx context.Context, runID, sessionID string, sequence int, emit func(aiRunStreamOutput) error) error {
	return f.err
}
func tuiKey(s string) tea.KeyPressMsg { r := []rune(s); return tea.KeyPressMsg{Code: r[0], Text: s} }
func testTUI(t *testing.T) (*tuiModel, *tuiFake) {
	t.Helper()
	f := &tuiFake{}
	m := newTUIModel(t.Context(), f, "local", "http://localhost", "subject", "cluster-1", "default")
	t.Cleanup(m.detach)
	return m, f
}
func applyTUICmd(m *tuiModel, cmd tea.Cmd) {
	if cmd != nil {
		m.Update(cmd())
	}
}

func TestTUIRequiresInteractiveTerminalBeforeLoadingCredentials(t *testing.T) {
	var out, errout bytes.Buffer
	code := Run(t.Context(), []string{"tui", "--profile", "local"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &errout, ConfigPath: t.TempDir() + "/missing"})
	if code != 2 || !strings.Contains(errout.String(), "interactive") {
		t.Fatalf("code=%d error=%s", code, errout.String())
	}
}

func TestTUIIgnoresStaleResponsesAfterWorkbenchSwitch(t *testing.T) {
	m, f := testTUI(t)
	old := m.Init()
	_, current := m.Update(tuiKey("2"))
	applyTUICmd(m, current)
	applyTUICmd(m, old)
	if m.scope.tab != 1 || m.snapshot.rows[0].id != "task-1" || m.busy || f.loads != 2 {
		t.Fatal("stale snapshot changed current epoch")
	}
	m.Update(tuiResult{epoch: m.epoch - 1, err: fmt.Errorf("stale error")})
	if strings.Contains(m.message, "stale") {
		t.Fatal("stale error replaced current status")
	}
}

func TestTUIActionIsFrozenAndRequiresVisibleConfirmation(t *testing.T) {
	m, f := testTUI(t)
	m.scope.tab = 4
	m.scope.section = "compute"
	applyTUICmd(m, m.Init())
	m.detail = true
	m.Update(tuiKey("x"))
	if m.confirm == nil || f.actions != 0 {
		t.Fatal("review was skipped")
	}
	m.width = 20
	_, cmd := m.Update(tuiKey("y"))
	applyTUICmd(m, cmd)
	if f.actions != 0 || m.confirm == nil {
		t.Fatal("hidden confirmation executed")
	}
	m.width = 80
	m.snapshot.rows[0].id = "replacement"
	_, cmd = m.Update(tuiKey("y"))
	applyTUICmd(m, cmd)
	if f.actions != 1 || f.row.id != "task-1" || f.action != "cancel" {
		t.Fatal("reviewed target changed")
	}
}

func TestTUIQuitOnlyDetaches(t *testing.T) {
	m, f := testTUI(t)
	ctx, cancel := context.WithCancel(t.Context())
	m.cancel = cancel
	_, cmd := m.Update(tuiKey("q"))
	if cmd == nil || ctx.Err() != context.Canceled || f.actions != 0 {
		t.Fatal("quit canceled a durable task or failed to detach")
	}
}

func TestTUILongChatCannotBypassTruncatedReview(t *testing.T) {
	m, f := testTUI(t)
	m.confirm = &tuiConfirmation{action: "send", row: tuiRow{id: "s:1"}, content: strings.Repeat("验", 1000), requestID: "turn-1"}
	_, cmd := m.Update(tuiKey("y"))
	applyTUICmd(m, cmd)
	if f.chats != 0 || m.confirm == nil {
		t.Fatal("truncated input submitted")
	}
	m.Update(tuiKey("n"))
	if m.confirm != nil {
		t.Fatal("decline did not return")
	}
}

func TestTUIUnknownOutcomePreservesOriginalReferences(t *testing.T) {
	m, _ := testTUI(t)
	m.epoch = 3
	m.sessionID, m.requestID = "session-1", "turn-1"
	m.busy = true
	m.Update(tuiStreamResult{epoch: 3, done: true, err: aiStreamUnknownError()})
	if m.busy || !strings.Contains(m.message, "unknown") || m.sessionID != "session-1" || m.requestID != "turn-1" {
		t.Fatal("unknown result or original identity lost")
	}
}

func TestTUIEscapesTerminalAndBidiControls(t *testing.T) {
	m, _ := testTUI(t)
	m.detail = true
	m.snapshot.detail = "\x1b]52;c;payload\x07\x1b[2J\u202Ehidden\nnormal"
	view := m.View().Content
	if strings.ContainsAny(view, "\x1b\x07\u202e") || !strings.Contains(view, "\\u001b") || !strings.Contains(view, "normal") {
		t.Fatalf("unsafe view %q", view)
	}
}

func TestTUIAPIReadUsesExistingAuthenticationAndNoFallback(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer token" || r.URL.Path != "/api/v1/copilot/sessions" {
			t.Error("wrong auth/path")
		}
		w.WriteHeader(403)
		_, _ = fmt.Fprint(w, `{"error":"forbidden"}`)
	}))
	defer srv.Close()
	a := tuiAPI{client: APIClient{ServerURL: srv.URL, Token: "token"}}
	_, err := a.load(t.Context(), tuiScope{tab: 3}, nil)
	if err == nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestTypedAIStreamConsumerReceivesOnlyValidatedRedactedEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, aiTestSSE(aiTestStreamEvent(1, `"type":"message.delta","contentDelta":"profile-token"`), aiTestStreamEvent(2, `"type":"agent.status","status":"succeeded"`)))
	}))
	defer srv.Close()
	client := APIClient{ServerURL: srv.URL, Token: "profile-token"}
	var updates []aiStreamOutput
	err := client.receiveAIChat(t.Context(), "s:1", sohaapi.WorkbenchSendMessageStreamRequest{Content: "hello", RequestID: "original"}, func(update aiStreamOutput) error { updates = append(updates, update); return nil })
	raw, _ := json.Marshal(updates)
	if err != nil || len(updates) != 3 || updates[2].Receipt == nil || strings.Contains(string(raw), "profile-token") {
		t.Fatalf("updates=%s err=%v", raw, err)
	}
}

func TestTUIDockerInventoryOmitsConfiguration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":{"items":[{"id":"project-1","name":"demo","status":"running","composeYaml":"PRIVATE","env":{"PASSWORD":"PRIVATE"}}],"total":1,"page":1,"pageSize":50}}`)
	}))
	defer srv.Close()
	snapshot, err := (tuiAPI{client: APIClient{ServerURL: srv.URL}}).load(t.Context(), tuiScope{tab: 1, section: "projects", page: 1}, nil)
	raw, _ := json.Marshal(snapshot.rows[0].value)
	if err != nil || strings.Contains(string(raw), "PRIVATE") {
		t.Fatalf("raw=%s error=%v", raw, err)
	}
}

func TestTUICursorNavigationNeverInventsAnOffset(t *testing.T) {
	m, _ := testTUI(t)
	m.scope.tab = 4
	m.snapshot.rows = make([]tuiRow, 50)
	if m.pageKey("n") != nil || m.scope.page != 1 {
		t.Fatal("invented pagination for a bounded snapshot")
	}
	m.snapshot.nextCursor = "server-cursor-1"
	command := m.pageKey("n")
	if command == nil || m.scope.cursor != "server-cursor-1" || m.scope.page != 2 {
		t.Fatal("server cursor was not retained")
	}
	applyTUICmd(m, command)
	if m.pageKey("p") == nil || m.scope.cursor != "" || m.scope.page != 1 {
		t.Fatal("previous cursor was not restored")
	}
}

func TestTUICancelWaitUsesAuthoritativeTerminalState(t *testing.T) {
	m, _ := testTUI(t)
	m.watch = &tuiConfirmation{scope: tuiScope{tab: 4}, row: tuiRow{id: "task-1"}, action: "cancel"}
	m.snapshot.detail = map[string]any{"status": "canceling"}
	if m.pollAfterCancel() == nil || m.watch == nil {
		t.Fatal("accepted cancellation was treated as terminal")
	}
	m.snapshot.detail = map[string]any{"data": map[string]any{"normalizedStatus": "canceled"}}
	if m.pollAfterCancel() != nil || m.watch != nil {
		t.Fatal("authoritative terminal state was ignored")
	}
}

func TestTUIDurableCursorIsIndependentOfSubmissionSequence(t *testing.T) {
	m, _ := testTUI(t)
	m.runID = "run:1"
	var event sohaapi.WorkbenchStreamEvent
	if err := json.Unmarshal([]byte(aiTestStreamEvent(100, `"type":"message.delta","contentDelta":"hello"`)), &event); err != nil {
		t.Fatal(err)
	}
	m.applyStream(tuiStreamResult{update: aiStreamOutput{Event: &event}})
	if m.cursors["run:1"] != 0 {
		t.Fatal("submission sequence became a durable cursor")
	}
	m.applyRunStream(aiRunStreamOutput{Data: &sohaapi.AgentRunEventPage{Items: []sohaapi.WorkbenchStreamEvent{event}, NextSequence: 100}})
	if m.cursors["run:1"] != 100 {
		t.Fatal("durable cursor was not applied")
	}
	m.applyRunStream(aiRunStreamOutput{Data: &sohaapi.AgentRunEventPage{NextSequence: 0, ResyncRequired: true}})
	if m.cursors["run:1"] != 100 {
		t.Fatal("resync reset the last applied cursor")
	}
}

func TestTUIWideCharactersStayWithinTerminalCells(t *testing.T) {
	if got := tuiClip("你好世界", 5); got != "你好…" {
		t.Fatalf("cell clipping: %q", got)
	}
	m, _ := testTUI(t)
	m.width = 40
	m.height = 12
	m.snapshot.loaded = time.Now()
	m.snapshot.rows = []tuiRow{{id: "id", name: strings.Repeat("你", 40)}}
	if strings.Count(m.View().Content, "\n") >= m.height {
		t.Fatal("view exceeded terminal height")
	}
}

func TestTUIWorkloadLogsAreScopedAndBounded(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/v1/clusters/cluster-1/logs/query" {
			t.Errorf("wrong log endpoint %s %s", r.Method, r.URL.Path)
		}
		var query LogQuery
		if json.NewDecoder(r.Body).Decode(&query) != nil || (query.Selector == nil || query.Selector.Namespace != "owned") || query.Limit != 100 {
			t.Error("unbounded or wrong namespace")
		}
		_, _ = fmt.Fprint(w, `{"data":{"entries":[],"hasMore":false}}`)
	}))
	defer srv.Close()
	_, err := (tuiAPI{client: APIClient{ServerURL: srv.URL}}).load(t.Context(), tuiScope{tab: 0, section: "pods", cluster: "cluster-1", namespace: "default", logs: true}, &tuiRow{id: "pod-1", name: "pod-1", namespace: "owned"})
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestTUIPasteIsDataAndCannotSubmitOrActivateKeys(t *testing.T) {
	m, f := testTUI(t)
	m.composing = true
	m.sessionID = "s:1"
	text := "q\nx\ny\x1b]52;clipboard\a"
	m.Update(tea.PasteMsg{Content: text})
	if m.draft != text || f.actions != 0 || f.chats != 0 || m.confirm != nil {
		t.Fatal("paste became actions or was rewritten")
	}
	if strings.Contains(m.View().Content, "\x1b") {
		t.Fatal("paste controls reached terminal")
	}
}
