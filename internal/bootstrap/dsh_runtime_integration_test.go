package bootstrap_test

// This opt-in integration uses real Core orchestration, HTTP adapters and dsh SDK,
// with in-memory persistence and a deterministic WorkbenchRelay model/tool fixture.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlers "github.com/opensoha/soha/internal/api/handlers"
	access "github.com/opensoha/soha/internal/application/access"
	gateway "github.com/opensoha/soha/internal/application/aigateway"
	copilot "github.com/opensoha/soha/internal/application/copilot"
	dg "github.com/opensoha/soha/internal/domain/aigateway"
	dc "github.com/opensoha/soha/internal/domain/copilot"
	di "github.com/opensoha/soha/internal/domain/identity"
	ds "github.com/opensoha/soha/internal/domain/settings"
	runtime "github.com/opensoha/soha/internal/infrastructure/dshruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Unused ports deliberately panic if this bounded scenario starts using them.
type b3Store struct {
	copilot.SessionStore
	copilot.MessageStore
	copilot.DataSourceStore
	copilot.AnalysisProfileStore
	copilot.AutomationPolicyStore
	copilot.RootCauseRunStore
	copilot.AgentRunStore
	copilot.InspectionTaskStore
	copilot.InspectionRunStore
	copilot.NativeRunStore
	copilot.NativeTaskParentStore
	copilot.NativeCapabilityTasks
	mu      sync.Mutex
	lease   dc.NativeLease
	created int
	final   []dc.Message
	events  []dc.WorkbenchStreamEvent
}

type b3ProcessOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *b3ProcessOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *b3ProcessOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func b3Clone[T any](v T) T {
	raw, _ := json.Marshal(v)
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}
func (s *b3Store) GetSession(_ context.Context, owner, id string) (dc.Session, error) {
	if owner != "b3-reader" || id != "b3-session" {
		return dc.Session{}, apperrors.ErrAccessDenied
	}
	return dc.Session{ID: id, CreatedBy: owner}, nil
}
func (s *b3Store) ListMessages(context.Context, string, int) ([]dc.Message, error) { return nil, nil }
func (s *b3Store) CreateNativeRun(_ context.Context, r dc.AgentRun, _ dc.Message, _, hash string) (dc.AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.created > 0 {
		return b3Clone(s.lease.Run), nil
	}
	s.created++
	s.lease = dc.NativeLease{Run: b3Clone(r), RequestHash: hash, Phase: "queued"}
	return r, nil
}
func (s *b3Store) ClaimNativeRun(context.Context, string) (dc.NativeLease, error) {
	return dc.NativeLease{}, apperrors.ErrNotFound
}
func (s *b3Store) ClaimDSHRun(_ context.Context, worker string) (dc.NativeLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lease.Run.Status != "queued" {
		return dc.NativeLease{}, apperrors.ErrNotFound
	}
	s.lease.Run.Status = "running"
	s.lease.Run.ClaimedByAgentID = worker
	s.lease.Version = 1
	s.lease.Fence = 1
	s.lease.Phase = "preparing"
	until := time.Now().Add(time.Minute)
	s.lease.LeaseUntil = &until
	return b3Clone(s.lease), nil
}
func (s *b3Store) ReadNativeLease(context.Context, string, string) (dc.NativeLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return b3Clone(s.lease), nil
}
func (s *b3Store) RenewNativeLease(context.Context, dc.NativeLease) (bool, error) { return true, nil }
func (s *b3Store) AdvanceNativeRun(_ context.Context, l dc.NativeLease, n dc.NativeAdvance) (dc.NativeLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.Version != s.lease.Version || l.Fence != s.lease.Fence {
		return l, apperrors.ErrConflict
	}
	s.lease.Run = b3Clone(n.Run)
	s.lease.Checkpoint = append(json.RawMessage(nil), n.Checkpoint...)
	s.lease.Phase = n.Phase
	s.lease.Version++
	s.events = append(s.events, n.Events...)
	if n.FinalMessage != nil {
		s.final = append(s.final, b3Clone(*n.FinalMessage))
	}
	return b3Clone(s.lease), nil
}
func (s *b3Store) ReserveNativeToolCall(_ context.Context, l dc.NativeLease, limit int) (dc.NativeToolUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.Fence != s.lease.Fence || s.lease.ToolUsage.WorkCalls >= limit {
		return s.lease.ToolUsage, apperrors.ErrConflict
	}
	s.lease.ToolUsage.WorkLimit = limit
	s.lease.ToolUsage.WorkCalls++
	return s.lease.ToolUsage, nil
}
func (s *b3Store) ListAgentRuns(context.Context, dc.AgentRunFilter) ([]dc.AgentRun, error) {
	return nil, nil
}
func (s *b3Store) ListDueInspectionTasks(context.Context, time.Time, int) ([]dc.InspectionTask, error) {
	return nil, nil
}

type b3Gateway struct {
	copilot.WorkbenchApprovalRequester
	copilot.WorkbenchApprovalReader
	copilot.WorkbenchModelCatalog
	copilot.NativeTaskPlanValidator
	mu                        sync.Mutex
	models, tools, identities int
	assembled                 bool
}

func (*b3Gateway) ListRolePermissions(context.Context) (map[string][]string, error) {
	keys := []string{access.PermObserveAIChatUse, access.PermPlatformNamespacesView, access.PermAIGatewayInvoke}
	for _, tool := range (gateway.BuiltinCapabilityProvider{}).Tools() {
		if tool.Name == "k8s.namespaces.list" {
			keys = append(keys, tool.PermissionKeys...)
		}
	}
	return map[string][]string{"b3-reader": keys}, nil
}
func (*b3Gateway) CurrentPrincipal(context.Context, string) (di.Principal, error) {
	return di.Principal{UserID: "b3-reader", Roles: []string{"b3-reader"}}, nil
}
func (*b3Gateway) ExecutionAuthorizationRevision(context.Context, di.Principal) (string, error) {
	return strings.Repeat("a", 64), nil
}
func (*b3Gateway) WorkbenchAuthorizationRevision(context.Context, di.Principal) (string, error) {
	return strings.Repeat("b", 64), nil
}
func (*b3Gateway) ResolveAIWorkbenchSettings(context.Context) (ds.AIWorkbenchModelSettings, error) {
	return ds.AIWorkbenchModelSettings{Enabled: true, DefaultPublicModel: "fixture"}, nil
}
func (*b3Gateway) ResolveAISkillsRegistry(context.Context) ([]ds.AISkillSettings, error) {
	return nil, nil
}
func (*b3Gateway) WorkbenchCapabilities(context.Context, di.Principal, dg.ManifestRequest) (dg.Manifest, error) {
	return dg.Manifest{Tools: []dg.ToolCapability{{Name: "k8s.namespaces.list", Version: "1", RiskLevel: dg.RiskLevelRead, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"clusterId": map[string]any{"type": "string"}}, "required": []string{"clusterId"}, "additionalProperties": false}}}}, nil
}
func (g *b3Gateway) InvokeWorkbenchModelStream(ctx context.Context, p di.Principal, r gateway.WorkbenchRelayRequest, _ func(gateway.WorkbenchRelayStreamDelta) bool) (gateway.WorkbenchRelayResponse, error) {
	return g.InvokeWorkbenchModel(ctx, p, r)
}
func (g *b3Gateway) InvokeWorkbenchModel(_ context.Context, p di.Principal, r gateway.WorkbenchRelayRequest) (gateway.WorkbenchRelayResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if p.UserID != "b3-reader" || p.AccessTokenID != "" || len(p.PermissionKeys) == 0 || r.PublicModel != "fixture" || r.MaxOutputTokens <= 0 || r.MaxOutputTokens > 4096 {
		return gateway.WorkbenchRelayResponse{}, fmt.Errorf("wrong current model identity/policy")
	}
	for _, message := range r.Messages {
		if strings.HasPrefix(message.Content, "Current runtime context.") || message.Content == "You are a helpful software engineer assistant." {
			return gateway.WorkbenchRelayResponse{}, fmt.Errorf("SDK scaffolding reached Gateway")
		}
	}
	g.identities++
	g.models++
	reply := gateway.WorkbenchRelayResponse{PublicModel: "fixture", ContextWindowTokens: 128000, Usage: map[string]float64{"inputTokens": 8, "outputTokens": 2, "totalTokens": 10}}
	if g.models == 1 {
		found := false
		for _, tool := range r.Tools {
			if tool.Name == "k8s_namespaces_list" {
				found = true
			}
		}
		if !found {
			return reply, fmt.Errorf("authorized tool absent")
		}
		reply.ToolCalls = []gateway.WorkbenchToolCall{{ID: "b3-tool", Name: "k8s_namespaces_list", Arguments: json.RawMessage(`{"clusterId":"b3-cluster"}`)}}
		if g.assembled {
			hasSkill := false
			for _, tool := range r.Tools {
				hasSkill = hasSkill || tool.Name == "skill_evidence_load"
			}
			if !hasSkill || r.MaxOutputTokens > 512 {
				return reply, fmt.Errorf("confirmed skill or profile budget absent")
			}
			reply.ToolCalls = append(reply.ToolCalls, gateway.WorkbenchToolCall{ID: "b3-skill", Name: "skill_evidence_load", Arguments: json.RawMessage(`{}`)})
		}
	} else if g.models == 2 {
		limit := 4094
		if g.assembled {
			limit = 510
		}
		if r.MaxOutputTokens > limit {
			return reply, fmt.Errorf("second model call exceeded runtime's remaining output cap")
		}
		found := false
		skillFound := !g.assembled
		for _, m := range r.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, "b3 observed receipt") {
				found = true
			}
			if m.Role == "tool" && strings.Contains(m.Content, "Pinned evidence instructions.") {
				skillFound = true
			}
		}
		if !found || !skillFound {
			return reply, fmt.Errorf("Core tool observation missing from next loop")
		}
		reply.Content = "B3 completed with observed receipt."
	} else {
		return reply, fmt.Errorf("unexpected extra inference")
	}
	return reply, nil
}
func (g *b3Gateway) InvokeTool(_ context.Context, p di.Principal, r dg.ToolInvocationRequest) (dg.ToolInvocationResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if p.UserID != "b3-reader" || r.ToolName != "k8s.namespaces.list" {
		return dg.ToolInvocationResult{}, apperrors.ErrAccessDenied
	}
	g.tools++
	return dg.ToolInvocationResult{Result: "success", Output: map[string]any{"receipt": "b3 observed receipt"}}, nil
}

type b3ObservedRuntime struct {
	copilot.DSHRuntime
	t *testing.T
}

func (r b3ObservedRuntime) Start(ctx context.Context, start dc.DSHStart) (dc.DSHReceipt, error) {
	roles := make([]string, 0, len(start.Context))
	for _, m := range start.Context {
		roles = append(roles, m.Role)
	}
	receipt, err := r.DSHRuntime.Start(ctx, start)
	if err != nil {
		wire := map[string]any{"schemaVersion": runtime.SchemaVersion, "kind": "start", "runId": start.RunID, "attempt": start.Attempt, "fence": start.Fence, "ownerKey": start.OwnerKey, "sessionId": start.SessionID, "requestId": start.RequestID, "inputHash": start.InputHash, "contextRevision": start.ContextRevision, "profile": start.Profile, "model": start.Model, "prompt": start.Prompt, "toolCatalogRevision": start.ToolCatalogRevision, "toolGrant": start.ToolGrant, "deadline": start.Deadline, "maxOutputTokens": start.MaxOutputTokens, "context": start.Context}
		tools := []map[string]any{}
		for _, tool := range start.Tools {
			tools = append(tools, map[string]any{"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema, "readOnly": tool.ReadOnly})
		}
		wire["tools"] = tools
		raw, _ := json.Marshal(wire)
		script := `import Ajv from 'ajv/dist/2020.js';import formats from 'ajv-formats';import {readFile} from 'node:fs/promises';import {compileTool} from './protocol.mjs';let text='';for await(const c of process.stdin)text+=c;const input=JSON.parse(text);const a=new Ajv({strict:true});formats(a);const schema=JSON.parse(await readFile('../../../soha-contracts/agent/dsh-runtime.schema.json','utf8'));a.addSchema(schema);const check=a.compile({$ref:schema.$id+'#/$defs/Start'});console.log(JSON.stringify({valid:check(input),errors:check.errors?.map(e=>({path:e.instancePath,keyword:e.keyword,params:e.params}))}));for(const t of input.tools){try{compileTool(t);console.log(t.name+':compile ok')}catch(e){console.log(t.name+':'+e.message)}}`
		cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", script)
		cmd.Dir = "../../../soha-agent/runtimes/dsh"
		cmd.Stdin = bytes.NewReader(raw)
		out, e := cmd.CombinedOutput()
		r.t.Logf("B3 local wire validation: %s error=%v", out, e)
	}
	r.t.Logf("B3 runtime start: contextRoles=%v tools=%d status=%s error=%v", roles, len(start.Tools), receipt.Status, err)
	return receipt, err
}

func (r b3ObservedRuntime) Status(ctx context.Context, in dc.DSHSubmission) (dc.DSHStatus, error) {
	status, err := r.DSHRuntime.Status(ctx, in)
	if err != nil || status.Terminal() {
		r.t.Logf("B3 runtime status: status=%s sequence=%d reason=%s error=%v", status.Status, status.Sequence, status.StopReason, err)
	}
	return status, err
}

func (r b3ObservedRuntime) Events(ctx context.Context, id dc.DSHIdentity, after int64) (dc.DSHEventPage, error) {
	page, err := r.DSHRuntime.Events(ctx, id, after)
	if err != nil || len(page.Items) > 0 {
		r.t.Logf("runtime events after=%d next=%d count=%d error=%v", after, page.NextSequence, len(page.Items), err)
	}
	return page, err
}

func TestDSHRuntimeCoreIntegration(t *testing.T) {
	runDSHRuntimeCoreIntegration(t, false)
}

func TestDSHRuntimeCoreAssemblyIntegration(t *testing.T) {
	runDSHRuntimeCoreIntegration(t, true)
}

type b3ProfileStore struct {
	copilot.RuntimeProfileStore
	profile dc.RuntimeProfile
}

func (r b3ProfileStore) GetCurrentRuntimeProfile(_ context.Context, id, scope string) (dc.RuntimeProfile, error) {
	if id != r.profile.ID || scope != r.profile.Scope {
		return dc.RuntimeProfile{}, apperrors.ErrNotFound
	}
	return r.profile, nil
}

func (r b3ProfileStore) RuntimeProfileRevisionAuthorized(_ context.Context, id string, revision int64, scope string) (bool, error) {
	return id == r.profile.ID && revision == r.profile.Revision && scope == r.profile.Scope, nil
}

func b3SkillRegistry(t *testing.T, temp string) string {
	t.Helper()
	temp, err := filepath.EvalSymlinks(temp)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(temp, "skill")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte("Pinned evidence instructions.\n")
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), body, 0600); err != nil {
		t.Fatal(err)
	}
	skill := map[string]any{"id": "evidence", "version": "1", "description": "Test evidence guidance", "files": map[string]string{"SKILL.md": fmt.Sprintf("%x", sha256.Sum256(body))}, "requires": []any{}}
	raw, err := json.Marshal(skill)
	if err != nil {
		t.Fatal(err)
	}
	skill["digest"], skill["root"] = fmt.Sprintf("%x", sha256.Sum256(raw)), root
	raw, err = json.Marshal(map[string]any{"skill": []map[string]any{skill}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(temp, "assets.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runDSHRuntimeCoreIntegration(t *testing.T, assembled bool) {
	t.Helper()
	if os.Getenv("SOHA_TEST_DSH_INTEGRATION") != "1" {
		t.Skip("opt-in real Node/dsh integration")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(root, "soha-agent/runtimes/dsh")
	temp := t.TempDir()
	token := uuid.NewString() + uuid.NewString()
	tokenFile := filepath.Join(temp, "service-token")
	if err = os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	store := &b3Store{}
	g := &b3Gateway{assembled: assembled}
	var options []copilot.Option
	if assembled {
		options = append(options, copilot.WithRuntimeProfiles(b3ProfileStore{profile: dc.RuntimeProfile{
			ID: "chat-default", Scope: "chat", Revision: 1, Enabled: true, MaxOutputTokens: 512,
			Assets: []dc.RuntimeAssetRef{{Kind: "skill", ID: "evidence", Version: "1", Required: true}, {Kind: "hook", ID: "tool-audit", Version: "1", Required: true}},
		}}))
	}
	svc, err := copilot.New(copilot.Dependencies{Sessions: store, Messages: store, DataSources: store, AnalysisProfiles: store, AutomationPolicies: store, RootCauseRuns: store, AgentRuns: store, InspectionTasks: store, InspectionRuns: store, NativeRuns: store, NativeTaskParents: store, NativeTasks: store, Settings: g, Permissions: access.NewPermissionResolver(g), NativeAuthorization: g, AgentPrincipals: g}, options...)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureRuntime(copilot.RuntimeDependencies{Model: g, Stream: g, Tools: g, ApprovalRequests: g, Approvals: g, ModelCatalog: g, CapabilityCatalog: g, GatewayAuthorization: g, PlanValidator: g}); err != nil {
		t.Fatal(err)
	}
	callback, err := handlers.NewDSHRuntimeHandler(svc, token)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.POST("/model/chat/completions", func(c *gin.Context) {
		callback.Model(c)
		if c.Writer.Status() >= 400 {
			t.Logf("synthetic model callback rejected: status=%d errors=%v", c.Writer.Status(), c.Errors)
		}
	})
	router.POST("/tool", callback.Tool)
	router.POST("/assembly", callback.Assembly)
	router.POST("/plugin-tools/authorize", callback.AuthorizePluginTool)
	router.POST("/plugin-tools/settle", callback.SettlePluginTool)
	var callbackMu sync.Mutex
	modelHeaders := 0
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/chat/completions" {
			if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-Soha-Run-ID") == "" || r.Header.Get("X-Soha-Run-Attempt") != "1" || r.Header.Get("X-Soha-Run-Fence") != "1" || r.Header.Get("X-Soha-Tool-Grant") == "" || r.Header.Get("X-Soha-Context-Revision") == "" || r.Header.Get("X-Soha-Tool-Catalog-Revision") == "" {
				t.Error("model loop missing service/run governance binding")
			}
			callbackMu.Lock()
			modelHeaders++
			callbackMu.Unlock()
		}
		router.ServeHTTP(w, r)
	}))
	defer core.Close()
	// Port zero is bound by Node itself; startup emits only its selected URL.
	launch := `import {createRuntime,configFromEnv} from './service.mjs';const r=await createRuntime(await configFromEnv());await new Promise(ok=>r.server.listen(0,'127.0.0.1',ok));console.log('http://127.0.0.1:'+r.server.address().port);process.on('SIGTERM',async()=>{await r.close();process.exit(0)});`
	cmd := exec.Command("node", "--input-type=module", "-e", launch)
	cmd.Dir = runtimeDir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + temp, "TMPDIR=" + temp, "SOHA_DSH_SERVICE_TOKEN_FILE=" + tokenFile, "SOHA_DSH_GATEWAY_URL=" + core.URL + "/model", "SOHA_DSH_CORE_CALLBACK_URL=" + core.URL + "/tool", "SOHA_DSH_MODELS=soha-workbench", "SOHA_DSH_DATA_DIR=" + filepath.Join(temp, "journal"), "SOHA_DSH_MAX_RUN_MS=45000", "SOHA_DSH_MODEL_AUTH_MODE=core"}
	if assembled {
		cmd.Env = append(cmd.Env, "SOHA_DSH_PLUGIN_ASSETS_FILE="+b3SkillRegistry(t, temp))
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr b3ProcessOutput
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case exitErr := <-exited:
			if exitErr != nil {
				t.Errorf("Node exit: %v", exitErr)
			} else {
				t.Logf("B3 cleanup: nodePid=%d gracefulExit=true", cmd.Process.Pid)
			}
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
			t.Error("Node required forced termination")
		}
	}()
	var base string
	scanDone := make(chan error, 1)
	go func() { _, e := fmt.Fscanln(stdout, &base); scanDone <- e }()
	select {
	case err = <-scanDone:
		if err != nil {
			t.Fatal("Node startup:", err, stderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Node startup timed out")
	}
	t.Logf("B3 resources: nodePid=%d tempRoot=%s coreURL=%s nodeURL=%s", cmd.Process.Pid, temp, core.URL, base)
	client, err := runtime.New(runtime.Config{BaseURL: base, ServiceToken: token, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	health, err := client.Health(context.Background())
	if err != nil || !health.Ready || health.RuntimeVersion != "0.2.2-rc.1" {
		t.Fatalf("runtime qualification: %+v %v", health, err)
	}
	adapter, err := runtime.NewAdapter(client)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureDSHRuntime(b3ObservedRuntime{DSHRuntime: adapter, t: t}, store); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	svc.Start(ctx)
	if !svc.Running() {
		t.Fatal("Core service did not start")
	}
	defer func() {
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if e := svc.Stop(stop); e != nil {
			t.Error(e)
		}
	}()
	input := dc.WorkbenchSendMessageInput{RequestID: "b3-integration-request", Content: "Read the authorized lease receipt."}
	principal, _ := g.CurrentPrincipal(ctx, "b3-reader")
	if _, err = svc.StreamMessage(ctx, principal, "b3-session", input, "en-US"); err != nil {
		t.Fatal(err)
	}
	for {
		lease, _ := store.ReadNativeLease(ctx, "", "")
		if lease.Run.Status == "completed" || lease.Run.Status == "failed" || lease.Run.Status == "paused" || lease.Phase == "paused" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("integration timed out", lease.Phase, lease.Run.Status, stderr.String())
		case <-time.After(100 * time.Millisecond):
		}
	}
	store.mu.Lock()
	lease := b3Clone(store.lease)
	final := b3Clone(store.final)
	created := store.created
	events := b3Clone(store.events)
	store.mu.Unlock()
	if lease.Run.Status != "completed" || lease.Run.ProviderKind != "dsh" || created != 1 || len(final) != 1 || !strings.Contains(final[0].Content, "B3 completed") {
		g.mu.Lock()
		models, tools := g.models, g.tools
		g.mu.Unlock()
		callbackMu.Lock()
		headers := modelHeaders
		callbackMu.Unlock()
		t.Fatalf("run failed: status=%s phase=%s output=%v final=%v callbacks=%d models=%d tools=%d stderr=%s", lease.Run.Status, lease.Phase, lease.Run.Output, final, headers, models, tools, stderr.String())
	}
	g.mu.Lock()
	models, tools, identities := g.models, g.tools, g.identities
	g.mu.Unlock()
	callbackMu.Lock()
	headers := modelHeaders
	callbackMu.Unlock()
	if models != 2 || tools != 1 || identities != 2 || headers != 2 {
		t.Fatalf("model=%d tool=%d identities=%d headers=%d", models, tools, identities, headers)
	}
	if lease.Run.Output["usageKnown"] != true {
		t.Fatalf("usage unknown: %v", lease.Run.Output)
	}
	if assembled {
		projected := dc.WithOperationState(lease.Run, time.Now())
		hooks, _ := final[0].Metadata["runtimeHooks"].([]any)
		if projected.RuntimeAssembly == nil || projected.RuntimeAssembly.Status != "confirmed" || len(hooks) != 2 || final[0].Metadata["runtimeAssembly"] == nil {
			t.Fatalf("missing durable assembly or hook outcomes: %+v %+v", projected.RuntimeAssembly, final[0].Metadata)
		}
	}
	done := false
	for _, e := range events {
		if e.Type == "message.done" {
			done = true
		}
	}
	if !done {
		t.Fatal("missing durable terminal event")
	}
	t.Logf("B3 PASS: real Node/dsh 0.2.2-rc.1; Core queued/claimed/completed one dsh run; models=%d tools=%d per-loop identities=%d service-header bindings=%d; usageKnown=true; one final message; no PAT; in-memory storage; deterministic Gateway fixture", models, tools, identities, headers)
}
