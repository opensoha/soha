package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	gateway "github.com/opensoha/soha/internal/application/aigateway"
	copilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const callbackTestToken = "0123456789abcdef0123456789abcdef"

type dshCallbackFixture struct {
	calls       int
	input       copilot.DSHModelCall
	tool        domain.DSHToolCall
	err         error
	streamCalls int
	deltas      []string
	afterDelta  func(int)
	afterErr    error
}

func (s *dshCallbackFixture) InvokeDSHModel(_ context.Context, grant string, in copilot.DSHModelCall) (gateway.WorkbenchRelayResponse, error) {
	s.calls++
	s.input = in
	if grant != "run-grant" {
		return gateway.WorkbenchRelayResponse{}, apperrors.ErrAccessDenied
	}
	return gateway.WorkbenchRelayResponse{Content: "real answer", PublicModel: "resolved", ToolCalls: []gateway.WorkbenchToolCall{{ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`)}}, Usage: map[string]float64{"inputTokens": 11, "outputTokens": 7, "totalTokens": 18}}, s.err
}
func (s *dshCallbackFixture) InvokeDSHModelStream(ctx context.Context, grant string, in copilot.DSHModelCall, emit func(gateway.WorkbenchRelayStreamDelta) bool) (gateway.WorkbenchRelayResponse, error) {
	s.streamCalls++
	response, err := s.InvokeDSHModel(ctx, grant, in)
	if err != nil {
		return response, err
	}
	deltas := s.deltas
	if deltas == nil {
		deltas = []string{"real ", "answer"}
	}
	for index, text := range deltas {
		if !emit(gateway.WorkbenchRelayStreamDelta{ContentDelta: text}) {
			return response, nil
		}
		if s.afterDelta != nil {
			s.afterDelta(index)
		}
	}
	return response, s.afterErr
}
func (s *dshCallbackFixture) InvokeDSHTool(_ context.Context, _ string, in domain.DSHToolCall) (domain.DSHToolResult, error) {
	s.calls++
	s.tool = in
	return domain.DSHToolResult{CallID: in.CallID, Status: "completed"}, s.err
}
func (s *dshCallbackFixture) ConfirmDSHAssembly(_ context.Context, grant string, report domain.DSHAssemblyReport) (domain.DSHAssemblyDecision, error) {
	s.calls++
	if grant != "run-grant" {
		return domain.DSHAssemblyDecision{}, apperrors.ErrAccessDenied
	}
	report.Kind = "assembly_decision"
	return domain.DSHAssemblyDecision{DSHAssemblyReport: report, Status: "confirmed"}, s.err
}
func (s *dshCallbackFixture) AuthorizeDSHPluginTool(context.Context, string, domain.DSHToolCall) (*domain.DSHToolResult, error) {
	s.calls++
	return nil, s.err
}
func (s *dshCallbackFixture) SettleDSHPluginTool(_ context.Context, _ string, result domain.DSHToolResult) (domain.DSHToolResult, error) {
	s.calls++
	return result, s.err
}
func dshRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/model", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+callbackTestToken)
	r.Header.Set("X-Soha-Tool-Grant", "run-grant")
	for k, v := range map[string]string{"X-Soha-Run-ID": "run-1", "X-Soha-Run-Attempt": "1", "X-Soha-Run-Fence": "2", "X-Soha-Context-Revision": "ctx-1", "X-Soha-Tool-Catalog-Revision": "catalog-1", "X-Soha-Model-Call-ID": "model-1"} {
		r.Header.Set(k, v)
	}
	sum := sha256.Sum256([]byte(body))
	r.Header.Set("X-Soha-Input-Hash", hex.EncodeToString(sum[:]))
	return r
}
func dshTestHandler(t *testing.T, s *dshCallbackFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h, err := NewDSHRuntimeHandler(s, callbackTestToken)
	if err != nil {
		t.Fatal(err)
	}
	e := gin.New()
	e.POST("/model", h.Model)
	e.POST("/tool", h.Tool)
	return e
}
func TestDSHCallbackBoundary(t *testing.T) {
	body := `{"model":"soha-workbench","messages":[{"role":"user","content":"hi"}]}`
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"missing token", func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"wrong token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }, 401},
		{"duplicate token", func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+callbackTestToken) }, 401},
		{"duplicate grant", func(r *http.Request) { r.Header.Add("X-Soha-Tool-Grant", "other") }, 403},
		{"duplicate binding", func(r *http.Request) { r.Header.Add("X-Soha-Run-ID", "other") }, 400},
		{"unsafe fence", func(r *http.Request) { r.Header.Set("X-Soha-Run-Fence", "9007199254740992") }, 400},
		{"zero attempt", func(r *http.Request) { r.Header.Set("X-Soha-Run-Attempt", "0") }, 400},
		{"wrong hash", func(r *http.Request) { r.Header.Set("X-Soha-Input-Hash", strings.Repeat("a", 64)) }, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &dshCallbackFixture{}
			e := dshTestHandler(t, s)
			r := dshRequest(body)
			tc.mutate(r)
			w := httptest.NewRecorder()
			e.ServeHTTP(w, r)
			if w.Code != tc.status || s.calls != 0 {
				t.Fatalf("code %d calls %d body %s", w.Code, s.calls, w.Body.String())
			}
		})
	}
	s := &dshCallbackFixture{}
	e := dshTestHandler(t, s)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, dshRequest(strings.Repeat(" ", dshCallbackBodyLimit+1)))
	if w.Code != 413 || s.calls != 0 {
		t.Fatalf("oversize %d", w.Code)
	}
}
func TestDSHModelJSONAndSSE(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			body := `{"model":"soha-workbench","stream":STREAM,"max_tokens":20,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]},{"role":"assistant","content":null,"tool_calls":[{"id":"prior","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"prior","content":"ok"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`
			body = strings.ReplaceAll(body, "STREAM", map[bool]string{false: "false", true: "true"}[stream])
			s := &dshCallbackFixture{}
			e := dshTestHandler(t, s)
			w := httptest.NewRecorder()
			e.ServeHTTP(w, dshRequest(body))
			if w.Code != 200 || s.calls != 1 || string(s.input.RawBody) != body || s.input.Request.Messages[0].Content != "hi" || s.input.Request.Messages[2].ToolCallID != "prior" || s.input.Request.Tools[0].Name != "lookup" {
				t.Fatalf("mapping %d %s %#v", w.Code, w.Body.String(), s.input)
			}
			if stream {
				frames := strings.Split(w.Body.String(), "\n\n")
				if !strings.Contains(w.Body.String(), "data: [DONE]") || !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
					t.Fatal("SSE framing")
				}
				for _, frame := range frames {
					if frame == "" || frame == "data: [DONE]" {
						continue
					}
					var chunk map[string]any
					if json.Unmarshal([]byte(strings.TrimPrefix(frame, "data: ")), &chunk) != nil || chunk["object"] != "chat.completion.chunk" {
						t.Fatal("invalid chunk")
					}
				}
			} else {
				var out map[string]any
				if json.Unmarshal(w.Body.Bytes(), &out) != nil || out["object"] != "chat.completion" {
					t.Fatal("invalid completion")
				}
			}
			if !strings.Contains(w.Body.String(), `"total_tokens":18`) || !strings.Contains(w.Body.String(), `"name":"lookup"`) {
				t.Fatal("missing real usage/tools")
			}
		})
	}
}
func TestDSHModelApplicationFailure(t *testing.T) {
	s := &dshCallbackFixture{err: apperrors.ErrAccessDenied}
	e := dshTestHandler(t, s)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, dshRequest(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`))
	if w.Code != 403 || strings.Contains(w.Body.String(), "[DONE]") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestDSHCallbackConstruction(t *testing.T) {
	var typedNil *dshCallbackFixture
	if _, err := NewDSHRuntimeHandler(typedNil, callbackTestToken); err == nil {
		t.Fatal("typed nil")
	}
	if _, err := NewDSHRuntimeHandler(&dshCallbackFixture{}, "short"); err == nil {
		t.Fatal("short token")
	}
}
func TestDSHToolRawInput(t *testing.T) {
	s := &dshCallbackFixture{}
	e := dshTestHandler(t, s)
	r := dshRequest(`{"schemaVersion":"opensoha.dev/dsh-runtime/v1","kind":"tool_call","attempt":1,"fence":2,"callId":"call-1","input":{ "a" : 1 }}`)
	r.URL.Path = "/tool"
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code != 200 || string(s.tool.Input) != `{ "a" : 1 }` {
		t.Fatal(w.Code, string(s.tool.Input))
	}
}
func TestDSHOpenAIRejectMalformed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"messages":[{"role":"unknown","content":"x"}]}`, `{"messages":[{"role":"assistant","tool_calls":[{"id":"x","type":"function","function":{"name":"a","arguments":"bad"}}]}]}`} {
		if _, _, err := parseDSHOpenAI([]byte(raw)); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatal(raw, err)
		}
	}
}

func TestDSHUnknownUsageIsNotSynthesized(t *testing.T) {
	for _, stream := range []bool{false, true} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/model", nil)
		writeDSHOpenAI(c, "call", gateway.WorkbenchRelayResponse{Content: "answer", Usage: map[string]float64{"totalTokens": 99}, UsageEstimated: true}, stream)
		if strings.Contains(w.Body.String(), "total_tokens") {
			t.Fatal("estimated usage reported as measured", w.Body.String())
		}
	}
}

func TestDSHModelIncrementalSSE(t *testing.T) {
	s := &dshCallbackFixture{}
	w := httptest.NewRecorder()
	s.afterDelta = func(index int) {
		if !w.Flushed || !strings.Contains(w.Body.String(), `"content":"real "`) {
			t.Fatal("delta was buffered")
		}
		if index == 0 && strings.Contains(w.Body.String(), `"content":"answer"`) {
			t.Fatal("second delta arrived before invocation")
		}
		if strings.Contains(w.Body.String(), "[DONE]") || strings.Contains(w.Body.String(), "total_tokens") {
			t.Fatal("success before invocation finished")
		}
	}
	e := dshTestHandler(t, s)
	e.ServeHTTP(w, dshRequest(`{"model":"soha-workbench","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	if s.streamCalls != 1 || strings.Count(w.Body.String(), `"content":"real "`) != 1 || strings.Count(w.Body.String(), `"content":"answer"`) != 1 || strings.Contains(w.Body.String(), `"content":"real answer"`) || !strings.Contains(w.Body.String(), "[DONE]") {
		t.Fatal(s.streamCalls, w.Body.String())
	}
}
func TestDSHModelStreamInterrupted(t *testing.T) {
	for _, reason := range []string{"upstream", "context", "writer"} {
		t.Run(reason, func(t *testing.T) {
			s := &dshCallbackFixture{}
			e := dshTestHandler(t, s)
			r := dshRequest(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`)
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			w := httptest.NewRecorder()
			var writer http.ResponseWriter = w
			switch reason {
			case "upstream":
				s.afterErr = errors.New("private upstream failure")
			case "context":
				s.afterDelta = func(int) { cancel() }
			case "writer":
				writer = &dshFailWriter{ResponseRecorder: w}
			}
			e.ServeHTTP(writer, r)
			if strings.Contains(w.Body.String(), "[DONE]") || strings.Contains(w.Body.String(), "total_tokens") || strings.Contains(w.Body.String(), `"finish_reason":"tool_calls"`) || strings.Contains(w.Body.String(), "private upstream failure") {
				t.Fatal("stream manufactured success", w.Body.String())
			}
			if reason == "upstream" && !strings.Contains(w.Body.String(), "model stream interrupted") {
				t.Fatal("missing bounded stream error")
			}
		})
	}
}

type dshFailWriter struct{ *httptest.ResponseRecorder }

func (w *dshFailWriter) Write([]byte) (int, error) { return 0, errors.New("closed peer") }
func TestDSHModelStreamCached(t *testing.T) {
	s := &dshCallbackFixture{deltas: []string{}}
	e := dshTestHandler(t, s)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, dshRequest(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`))
	if strings.Count(w.Body.String(), `"content":"real answer"`) != 1 || !strings.Contains(w.Body.String(), "[DONE]") {
		t.Fatal(w.Body.String())
	}
}

type dshNonStreamService struct{ DSHRuntimeService }

func TestDSHModelStreamRequiresCapability(t *testing.T) {
	s := &dshCallbackFixture{}
	h, err := NewDSHRuntimeHandler(dshNonStreamService{s}, callbackTestToken)
	if err != nil {
		t.Fatal(err)
	}
	e := gin.New()
	e.POST("/model", h.Model)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, dshRequest(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`))
	if w.Code != 503 || s.calls != 0 || strings.Contains(w.Body.String(), "[DONE]") {
		t.Fatal(w.Code, s.calls, w.Body.String())
	}
}
