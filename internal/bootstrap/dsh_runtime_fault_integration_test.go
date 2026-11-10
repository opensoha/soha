package bootstrap_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	dc "github.com/opensoha/soha/internal/domain/copilot"
	di "github.com/opensoha/soha/internal/domain/identity"
	runtime "github.com/opensoha/soha/internal/infrastructure/dshruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Core persistence is an isolated in-memory fixture; Node uses its real journal.
type b5Store struct{ *b3Store }

func (s *b5Store) GetAgentRun(_ context.Context, owner, id string) (dc.AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.lease.Run.ID || (owner != "" && owner != s.lease.Run.CreatedBy) {
		return dc.AgentRun{}, apperrors.ErrNotFound
	}
	return b3Clone(s.lease.Run), nil
}
func (s *b5Store) RequestNativeCancel(_ context.Context, owner, id string) (dc.AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner != s.lease.Run.CreatedBy || id != s.lease.Run.ID {
		return dc.AgentRun{}, apperrors.ErrAccessDenied
	}
	s.lease.CancelRequested = true
	s.lease.Phase = "cancelling"
	s.lease.Version++
	return b3Clone(s.lease.Run), nil
}

type b5WaitingGateway struct {
	*b3Gateway
	entered, release, returned chan struct{}
	once                       sync.Once
	calls                      int
}

func (g *b5WaitingGateway) unblock() { g.once.Do(func() { close(g.release) }) }
func (g *b5WaitingGateway) InvokeWorkbenchModel(_ context.Context, p di.Principal, in gateway.WorkbenchRelayRequest) (gateway.WorkbenchRelayResponse, error) {
	g.mu.Lock()
	g.calls++
	n := g.calls
	g.mu.Unlock()
	if n != 1 || p.UserID != "b3-reader" || len(p.PermissionKeys) == 0 || in.PublicModel != "fixture" {
		return gateway.WorkbenchRelayResponse{}, errors.New("unexpected model identity or replay")
	}
	close(g.entered)
	// Deliberately emulate an upstream that returns after cancellation. Core must fence it.
	<-g.release
	close(g.returned)
	return gateway.WorkbenchRelayResponse{Content: "LATE MODEL MUST NOT COMMIT", PublicModel: "fixture", Usage: map[string]float64{"inputTokens": 8, "outputTokens": 2, "totalTokens": 10}}, nil
}
func (g *b5WaitingGateway) InvokeWorkbenchModelStream(ctx context.Context, p di.Principal, in gateway.WorkbenchRelayRequest, _ func(gateway.WorkbenchRelayStreamDelta) bool) (gateway.WorkbenchRelayResponse, error) {
	return g.InvokeWorkbenchModel(ctx, p, in)
}

type b5Runtime struct {
	mu                       sync.Mutex
	current                  copilot.DSHRuntime
	start                    dc.DSHStart
	receipt                  dc.DSHReceipt
	starts, lookups, cancels int
	holdReceipt              bool
	releaseReceipt           chan struct{}
	lookupStatuses           []string
}

func (r *b5Runtime) target() copilot.DSHRuntime { r.mu.Lock(); defer r.mu.Unlock(); return r.current }
func (r *b5Runtime) Start(ctx context.Context, in dc.DSHStart) (dc.DSHReceipt, error) {
	r.mu.Lock()
	r.starts++
	r.start = in
	r.mu.Unlock()
	out, err := r.target().Start(ctx, in)
	r.mu.Lock()
	r.receipt = out
	r.mu.Unlock()
	if err == nil && r.holdReceipt {
		select {
		case <-r.releaseReceipt:
			return dc.DSHReceipt{}, context.DeadlineExceeded
		case <-ctx.Done():
			return dc.DSHReceipt{}, ctx.Err()
		}
	}
	return out, err
}
func (r *b5Runtime) Lookup(ctx context.Context, id dc.DSHIdentity, hash string) (dc.DSHStatus, error) {
	r.mu.Lock()
	r.lookups++
	r.mu.Unlock()
	s, e := r.target().Lookup(ctx, id, hash)
	r.mu.Lock()
	r.lookupStatuses = append(r.lookupStatuses, s.Status)
	r.mu.Unlock()
	return s, e
}
func (r *b5Runtime) Status(ctx context.Context, in dc.DSHSubmission) (dc.DSHStatus, error) {
	return r.target().Status(ctx, in)
}
func (r *b5Runtime) Events(ctx context.Context, id dc.DSHIdentity, after int64) (dc.DSHEventPage, error) {
	return r.target().Events(ctx, id, after)
}
func (r *b5Runtime) Cancel(ctx context.Context, in dc.DSHSubmission, reason string) (dc.DSHStatus, error) {
	r.mu.Lock()
	r.cancels++
	r.mu.Unlock()
	return r.target().Cancel(ctx, in, reason)
}

type b5Node struct {
	cmd      *exec.Cmd
	done     chan error
	base     string
	stopped  bool
	t        *testing.T
	children []int
}

func (n *b5Node) stop(crash bool) {
	if n.stopped {
		return
	}
	n.stopped = true
	if crash {
		pg, e := syscall.Getpgid(n.cmd.Process.Pid)
		if e != nil || pg != n.cmd.Process.Pid {
			n.t.Fatalf("refusing unowned process group pid=%d pgid=%d err=%v", n.cmd.Process.Pid, pg, e)
		}
		_ = syscall.Kill(-pg, syscall.SIGKILL)
	} else {
		_ = n.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case err := <-n.done:
		if !crash && err != nil {
			n.t.Errorf("Node graceful exit: %v", err)
		}
	case <-time.After(12 * time.Second):
		_ = syscall.Kill(-n.cmd.Process.Pid, syscall.SIGKILL)
		<-n.done
		n.t.Error("Node shutdown exceeded bound")
	}
	n.t.Logf("B5 node exited: pid=%d crash=%t", n.cmd.Process.Pid, crash)
}
func b5Descendants(t *testing.T, parent int) []int {
	t.Helper()
	out, e := exec.Command("ps", "-axo", "pid=,ppid=").Output()
	if e != nil {
		t.Fatal(e)
	}
	tree := map[int]int{}
	for _, line := range strings.Split(string(out), "\n") {
		var p, pp int
		if _, e := fmt.Sscan(line, &p, &pp); e == nil {
			tree[p] = pp
		}
	}
	seen := map[int]bool{parent: true}
	for changed := true; changed; {
		changed = false
		for p, pp := range tree {
			if seen[pp] && !seen[p] {
				seen[p] = true
				changed = true
			}
		}
	}
	var children []int
	for p := range seen {
		if p != parent {
			children = append(children, p)
		}
	}
	return children
}
func b5Wait(t *testing.T, what string, condition func() bool) {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timeout: " + what)
}
func b5Gone(pids []int) bool {
	for _, pid := range pids {
		if syscall.Kill(pid, 0) == nil {
			return false
		}
	}
	return true
}

type b5Fixture struct {
	t                                  *testing.T
	store                              *b5Store
	g                                  *b5WaitingGateway
	svc                                *copilot.Service
	rt                                 *b5Runtime
	core                               *httptest.Server
	temp, tokenFile, token, runtimeDir string
	nodes                              []*b5Node
	clients                            []*runtime.Client
	ctx                                context.Context
	cancel                             context.CancelFunc
	callbackDone                       chan struct{}
}

func b5New(t *testing.T, hold bool) *b5Fixture {
	t.Helper()
	if os.Getenv("SOHA_TEST_DSH_INTEGRATION") != "1" {
		t.Skip("opt-in real Node/dsh fault integration")
	}
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	f := &b5Fixture{t: t, store: &b5Store{b3Store: &b3Store{}}, g: &b5WaitingGateway{b3Gateway: &b3Gateway{}, entered: make(chan struct{}), release: make(chan struct{}), returned: make(chan struct{})}, temp: t.TempDir(), token: uuid.NewString() + uuid.NewString(), runtimeDir: filepath.Join(root, "soha-agent/runtimes/dsh"), rt: &b5Runtime{holdReceipt: hold, releaseReceipt: make(chan struct{})}, callbackDone: make(chan struct{}, 1)}
	f.ctx, f.cancel = context.WithTimeout(context.Background(), 45*time.Second)
	f.tokenFile = filepath.Join(f.temp, "service-token")
	if e = os.WriteFile(f.tokenFile, []byte(f.token), 0600); e != nil {
		t.Fatal(e)
	}
	g, s := f.g, f.store
	f.svc, e = copilot.New(copilot.Dependencies{Sessions: s, Messages: s, DataSources: s, AnalysisProfiles: s, AutomationPolicies: s, RootCauseRuns: s, AgentRuns: s, InspectionTasks: s, InspectionRuns: s, NativeRuns: s, NativeTaskParents: s, NativeTasks: s, Settings: g, Permissions: access.NewPermissionResolver(g), NativeAuthorization: g, AgentPrincipals: g})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.svc.ConfigureRuntime(copilot.RuntimeDependencies{Model: g, Stream: g, Tools: g, ApprovalRequests: g, Approvals: g, ModelCatalog: g, CapabilityCatalog: g, GatewayAuthorization: g, PlanValidator: g}); e != nil {
		t.Fatal(e)
	}
	handler, e := handlers.NewDSHRuntimeHandler(f.svc, f.token)
	if e != nil {
		t.Fatal(e)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.POST("/model/chat/completions", func(c *gin.Context) { defer func() { f.callbackDone <- struct{}{} }(); handler.Model(c) })
	router.POST("/tool", handler.Tool)
	f.core = httptest.NewServer(router)
	t.Cleanup(func() {
		f.g.unblock()
		f.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.svc.Stop(ctx); err != nil {
			t.Error(err)
		}
		for _, n := range f.nodes {
			n.stop(false)
		}
		f.core.Close()
		for _, c := range f.clients {
			c.Close()
		}
		t.Logf("B5 cleanup: tempRoot=%s nodeProcessesExited=%d", f.temp, len(f.nodes))
	})
	f.launch()
	if e = f.svc.ConfigureDSHRuntime(f.rt, s); e != nil {
		t.Fatal(e)
	}
	f.svc.Start(f.ctx)
	if !f.svc.Running() {
		t.Fatal("Core not running")
	}
	principal, _ := g.CurrentPrincipal(f.ctx, "b3-reader")
	if _, e = f.svc.StreamMessage(f.ctx, principal, "b3-session", dc.WorkbenchSendMessageInput{RequestID: "b5-fault", Content: "Wait for the authoritative model receipt."}, "en-US"); e != nil {
		t.Fatal(e)
	}
	select {
	case <-g.entered:
	case <-f.ctx.Done():
		t.Fatal("real SDK did not enter model wait")
	}
	b5Wait(t, "actual accepted receipt", func() bool { f.rt.mu.Lock(); defer f.rt.mu.Unlock(); return f.rt.receipt.SubmissionID != "" })
	n := f.nodes[0]
	n.children = b5Descendants(t, n.cmd.Process.Pid)
	if len(n.children) == 0 {
		t.Fatal("no real SDK runtime child observed")
	}
	t.Logf("B5 model waiting: nodePid=%d sdkChildren=%v tempRoot=%s nodeURL=%s coreURL=%s", n.cmd.Process.Pid, n.children, f.temp, n.base, f.core.URL)
	return f
}
func (f *b5Fixture) launch() {
	t := f.t
	launch := `import {createRuntime,configFromEnv} from './service.mjs';const r=await createRuntime(await configFromEnv());await new Promise(ok=>r.server.listen(0,'127.0.0.1',ok));console.log('http://127.0.0.1:'+r.server.address().port);process.on('SIGTERM',async()=>{await r.close();process.exit(0)});`
	cmd := exec.Command("node", "--input-type=module", "-e", launch)
	cmd.Dir = f.runtimeDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + f.temp, "TMPDIR=" + f.temp, "SOHA_DSH_SERVICE_TOKEN_FILE=" + f.tokenFile, "SOHA_DSH_GATEWAY_URL=" + f.core.URL + "/model", "SOHA_DSH_CORE_CALLBACK_URL=" + f.core.URL + "/tool", "SOHA_DSH_MODELS=soha-workbench", "SOHA_DSH_DATA_DIR=" + filepath.Join(f.temp, "journal"), "SOHA_DSH_MAX_RUN_MS=30000", "SOHA_DSH_MODEL_AUTH_MODE=core"}
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	stderr, e := os.CreateTemp(f.temp, "node-stderr-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { stderr.Close() })
	cmd.Stderr = stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	n := &b5Node{cmd: cmd, done: make(chan error, 1), t: t}
	f.nodes = append(f.nodes, n)
	go func() { n.done <- cmd.Wait() }()
	ready := make(chan bool, 1)
	go func() {
		scan := bufio.NewScanner(out)
		if scan.Scan() {
			n.base = scan.Text()
			ready <- true
		} else {
			ready <- false
		}
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("Node startup failed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Node startup bound")
	}
	client, e := runtime.New(runtime.Config{BaseURL: n.base, ServiceToken: f.token, Timeout: 5 * time.Second})
	if e != nil {
		t.Fatal(e)
	}
	f.clients = append(f.clients, client)
	h, e := client.Health(f.ctx)
	if e != nil || !h.Ready || h.RuntimeVersion != "0.2.2-rc.1" {
		t.Fatalf("runtime health: %+v %v", h, e)
	}
	adapter, e := runtime.NewAdapter(client)
	if e != nil {
		t.Fatal(e)
	}
	f.rt.mu.Lock()
	f.rt.current = adapter
	f.rt.mu.Unlock()
	t.Logf("B5 node started: pid=%d pgid=%d url=%s", cmd.Process.Pid, cmd.Process.Pid, n.base)
}
func (f *b5Fixture) lease() dc.NativeLease { l, _ := f.store.ReadNativeLease(f.ctx, "", ""); return l }
func b5Budget(t *testing.T, l dc.NativeLease) (int64, int64) {
	t.Helper()
	var cp struct {
		Reservation int64 `json:"modelReservation"`
		Budget      int64 `json:"budgetTokens"`
	}
	if e := json.Unmarshal(l.Checkpoint, &cp); e != nil {
		t.Fatal(e)
	}
	return cp.Reservation, cp.Budget
}
func (f *b5Fixture) assertNoLateSuccess() {
	f.t.Helper()
	f.g.unblock()
	select {
	case <-f.callbackDone:
	case <-time.After(5 * time.Second):
		f.t.Fatal("late callback did not finish")
	}
	l := f.lease()
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	for _, m := range f.store.final {
		if strings.Contains(m.Content, "LATE MODEL") {
			f.t.Fatal("late result committed")
		}
	}
	if l.Run.Status == "completed" || l.Run.Output["usageKnown"] == true {
		f.t.Fatalf("late success: status=%s usage=%v", l.Run.Status, l.Run.Output["usageKnown"])
	}
	f.g.mu.Lock()
	defer f.g.mu.Unlock()
	if f.g.calls != 1 {
		f.t.Fatalf("model replayed: %d", f.g.calls)
	}
}

func TestDSHRuntimeCoreFaultIntegration(t *testing.T) {
	t.Run("durable_cancel_rejects_late_model", func(t *testing.T) {
		f := b5New(t, false)
		l := f.lease()
		principal, _ := f.g.CurrentPrincipal(f.ctx, "b3-reader")
		if _, e := f.svc.CancelAgentRun(f.ctx, principal, l.Run.ID); e != nil {
			t.Fatal(e)
		}
		if !f.lease().CancelRequested {
			t.Fatal("cancel intent not stored")
		}
		b5Wait(t, "Core cancellation terminal", func() bool { return f.lease().Run.Status == "canceled" })
		b5Wait(t, "actual SDK subprocess exit", func() bool { return b5Gone(f.nodes[0].children) })
		f.rt.mu.Lock()
		receipt := f.rt.receipt
		count := f.rt.cancels
		f.rt.mu.Unlock()
		status, e := f.rt.target().Status(f.ctx, receipt.DSHSubmission)
		if e != nil || status.Status != "canceled" || count == 0 {
			t.Fatalf("no actual runtime cancel receipt: status=%s calls=%d error=%v", status.Status, count, e)
		}
		f.assertNoLateSuccess()
		t.Log("B5 V07 PASS: durable cancel, actual SDK process exit, canceled Node receipt, late model rejected; modelCalls=1")
	})
	t.Run("restart_unknown_lookup_preserves_budget", func(t *testing.T) {
		f := b5New(t, true)
		reservation, budget := b5Budget(t, f.lease())
		if reservation <= 0 {
			t.Fatal("no pre-dispatch budget reservation")
		}
		f.nodes[0].stop(true)
		b5Wait(t, "crashed SDK process group exit", func() bool { return b5Gone(f.nodes[0].children) })
		f.launch()
		close(f.rt.releaseReceipt)
		b5Wait(t, "Core pause unknown", func() bool { return f.lease().Phase == "paused" })
		f.rt.mu.Lock()
		starts, lookups := f.rt.starts, f.rt.lookups
		statuses := append([]string(nil), f.rt.lookupStatuses...)
		receipt := f.rt.receipt
		f.rt.mu.Unlock()
		if starts != 1 || lookups < 1 || len(statuses) == 0 || statuses[0] != "unknown" {
			t.Fatalf("recovery replay or no real unknown lookup: starts=%d lookups=%d states=%v", starts, lookups, statuses)
		}
		status, e := f.rt.target().Status(f.ctx, receipt.DSHSubmission)
		if e != nil || status.Status != "unknown" {
			t.Fatalf("restored submission not unknown: %s %v", status.Status, e)
		}
		f.assertNoLateSuccess()
		afterR, afterB := b5Budget(t, f.lease())
		if afterR != reservation || afterB != budget {
			t.Fatalf("unknown result refunded reservation/budget: before=%d/%d after=%d/%d", reservation, budget, afterR, afterB)
		}
		t.Logf("B5 V14 PASS: real journal restart unknown; Core Lookup=%d Start=%d modelCalls=1 reservation=%s budget=%s unchanged", lookups, starts, strconv.FormatInt(reservation, 10), strconv.FormatInt(budget, 10))
	})
}
