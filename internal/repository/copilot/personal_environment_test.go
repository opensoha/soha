package copilot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type personalEnvironmentRoleReader struct{}

func (personalEnvironmentRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"chat": {appaccess.PermObserveAIChatUse}}, nil
}

// Real PostgreSQL state-machine proof; provider receipts are bounded stubs.
type personalEnvironmentFixture struct {
	t              *testing.T
	repo           *Repository
	db             *gorm.DB
	ctx            context.Context
	service        *appcopilot.Service
	policy         domain.PersonalEnvironmentPolicy
	calls          []contractenv.Request
	nodes          []string
	providerError  error
	preflightError error
	preflightCalls int
	scopeError     error
	receiptState   string
	cancelInvoke   context.CancelFunc
	fileOutput     string
	exportCalls    int
	exporter       func(contractenv.Request, io.Writer) (contractenv.ExportResult, error)
}

func TestPersonalEnvironmentWithPostgres(t *testing.T) {
	f := newPersonalEnvironmentFixture(t)
	for _, test := range []struct {
		name string
		run  func(*testing.T, *personalEnvironmentFixture)
	}{
		{"owner_isolation", testPersonalEnvironmentOwnerIsolation},
		{"quota_preflight_before_data", testPersonalEnvironmentPreflightBeforeData},
		{"not_dispatched_restore", testPersonalEnvironmentNotDispatchedRestore},
		{"file_read_owner_fence", testPersonalEnvironmentFileReadOwnerFence},
		{"data_deletion_confirmation", testPersonalDataDeletionConfirmation},
		{"data_deletion_unknown_recovery", testPersonalDataDeletionUnknownRecovery},
		{"data_deletion_local_abort", testPersonalDataDeletionLocalAbort},
		{"data_export_owner_fence", testPersonalDataExportOwnerFence},
		{"data_export_unknown_receipt", testPersonalDataExportUnknownReceipt},
		{"data_export_local_abort", testPersonalDataExportLocalAbort},
		{"frozen_binding_sleep_wake", testPersonalEnvironmentFrozenBindingSleepWake},
		{"binding_identity_fence", testPersonalBindingIdentityFence},
		{"unsupported_backend", testPersonalBindingUnsupportedBackend},
		{"unknown_no_replay", testPersonalEnvironmentUnknownNoReplay},
		{"missing_data_not_recreated", testPersonalEnvironmentMissingDataNotRecreated},
		{"dispatch_cas", testPersonalEnvironmentDispatchCas},
		{"late_receipt_fence", testPersonalEnvironmentLateReceiptFence},
		{"quota_scope_ceiling", testPersonalEnvironmentQuotaScopeCeiling},
		{"canceled_receipt_persisted", testPersonalEnvironmentCanceledReceiptPersisted},
		{"current_permission_denied", testPersonalEnvironmentCurrentPermissionDenied},
		{"legacy_binding_migration", testPersonalBindingLegacyMigration},
		{"legacy_writer_compatibility", testPersonalBindingLegacyWriter},
	} {
		t.Run(test.name, func(t *testing.T) { f.t = t; test.run(t, f) })
	}
}

func newPersonalEnvironmentFixture(t *testing.T) *personalEnvironmentFixture {
	t.Helper()
	repo, store, ctx := newCopilotPostgresTestRepository(t)
	return personalEnvironmentFixtureWithRepository(t, repo, store.DB(), ctx)
}

func personalEnvironmentFixtureWithRepository(t *testing.T, repo *Repository, db *gorm.DB, ctx context.Context) *personalEnvironmentFixture {
	t.Helper()
	t.Cleanup(func() { appaccess.SetRolePermissionMatrix(nil) })
	f := &personalEnvironmentFixture{repo: repo, db: db, ctx: ctx, receiptState: "active", policy: domain.PersonalEnvironmentPolicy{TemplateID: "personal", AgentClusterID: "00000000-0000-4000-8000-000000000048", LeaseSeconds: 600, Mode: "restricted-write", Quota: contractenv.Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}}
	var err error
	f.service, err = appcopilot.New(appcopilot.Dependencies{
		Sessions: repo, Messages: repo, DataSources: repo, AnalysisProfiles: repo, AutomationPolicies: repo, RootCauseRuns: repo, AgentRuns: repo, InspectionTasks: repo, InspectionRuns: repo,
		Permissions: appaccess.NewPermissionResolver(personalEnvironmentRoleReader{}),
		PersonalEnvironment: &appcopilot.PersonalEnvironmentDependencies{Store: repo, Policy: func(context.Context, string) (domain.PersonalEnvironmentPolicy, error) { return f.policy, nil }, Authorize: func(context.Context, domainidentity.Principal, string) error { return f.scopeError }, Preflight: func(_ context.Context, p domainidentity.Principal, node string, quota contractenv.Quota) error {
			f.preflightCalls++
			if p.UserID == "" || node != f.policy.AgentClusterID || quota != f.policy.Quota {
				f.t.Fatal("preflight changed owner binding or quota")
			}
			return f.preflightError
		}, Invoke: func(ctx context.Context, p domainidentity.Principal, node string, r contractenv.Request) (contractenv.Result, error) {
			return f.invoke(f.t, ctx, p, node, r)
		}, Export: func(ctx context.Context, p domainidentity.Principal, node string, request contractenv.Request, target io.Writer) (contractenv.ExportResult, error) {
			f.exportCalls++
			record, err := f.repo.GetPersonalEnvironment(ctx, p.UserID, request.Lease.OwnerID)
			if err != nil || record.State != "pending" || record.OperationID == "" || !reflect.DeepEqual(record.Lease, request.Lease) || node != record.AgentClusterID {
				f.t.Fatal("export reached provider before durable owner reservation")
			}
			return f.exporter(request, target)
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func testPersonalEnvironmentNotDispatchedRestore(t *testing.T, f *personalEnvironmentFixture) {
	for _, denied := range []bool{false, true} {
		owner := fmt.Sprintf("local-before-dispatch-%t", denied)
		cause := apperrors.ErrServiceUnavailable
		if denied {
			cause = apperrors.ErrAccessDenied
		}
		f.providerError = fmt.Errorf("%w: %w: private connection detail", domain.ErrPersonalEnvironmentNotDispatched, cause)
		view, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "wake", "personal", "en-US")
		f.providerError = nil
		if !errors.Is(err, cause) || strings.Contains(err.Error(), "private connection") || view.State != "unprovisioned" {
			t.Fatalf("local failure was not sanitized/restored: %+v %v", view, err)
		}
		record, err := f.repo.GetPersonalEnvironment(f.ctx, owner, view.AssistantID)
		if err != nil || record.DataCreationAttempted || record.DataInitialized || record.OperationID != "" || record.OperationUntil != nil || record.Version != view.Version {
			t.Fatalf("unissued first acquisition consumed reservation: %+v %v", record, err)
		}
		f.wake(t, owner)
		if !f.calls[len(f.calls)-1].CreateData {
			t.Fatal("explicit retry lost first acquisition")
		}
		assertPersonalEnvironmentLocalAbortKeepsData(t, f, owner, view.AssistantID)
	}
}

func assertPersonalEnvironmentLocalAbortKeepsData(t *testing.T, f *personalEnvironmentFixture, owner, assistantID string) {
	t.Helper()
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "sleep", "", "en-US"); err != nil {
		t.Fatal(err)
	}
	previous, err := f.repo.GetPersonalEnvironment(f.ctx, owner, assistantID)
	if err != nil {
		t.Fatal(err)
	}
	f.providerError = domain.ErrPersonalEnvironmentNotDispatched
	view, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "wake", "", "en-US")
	f.providerError = nil
	record, readErr := f.repo.GetPersonalEnvironment(f.ctx, owner, view.AssistantID)
	if !errors.Is(err, apperrors.ErrServiceUnavailable) || readErr != nil || record.State != "sleeping" || !record.DataCreationAttempted || !record.DataInitialized || !reflect.DeepEqual(record.Lease, previous.Lease) || !reflect.DeepEqual(record.ObservedAt, previous.ObservedAt) {
		t.Fatalf("local abort lost persistent data history/original lease: %+v %v %v", record, err, readErr)
	}
	f.wake(t, owner)
	if f.calls[len(f.calls)-1].CreateData {
		t.Fatal("local abort reset historical data creation")
	}
}

func testPersonalEnvironmentPreflightBeforeData(t *testing.T, f *personalEnvironmentFixture) {
	before, probes := len(f.calls), f.preflightCalls
	f.preflightError = apperrors.ErrServiceUnavailable
	_, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("unsupported-node"), "wake", "personal", "en-US")
	f.preflightError = nil
	if !errors.Is(err, apperrors.ErrServiceUnavailable) || len(f.calls) != before || f.preflightCalls != probes+1 {
		t.Fatalf("failed quota probe created data: %v", err)
	}
	view, err := f.service.GetPersonalEnvironment(f.ctx, f.actor("unsupported-node"), "en-US")
	if err != nil {
		t.Fatal(err)
	}
	record, err := f.repo.GetPersonalEnvironment(f.ctx, "unsupported-node", view.AssistantID)
	if err != nil || record.DataCreationAttempted || record.DataInitialized || record.OperationID != "" || record.State != "unprovisioned" {
		t.Fatalf("probe consumed personal creation reservation: %+v %v", record, err)
	}
	f.wake(t, "unsupported-node")
	if !f.calls[len(f.calls)-1].CreateData || f.preflightCalls != probes+2 {
		t.Fatal("verified retry lost initial data creation")
	}
}

func (f *personalEnvironmentFixture) actor(id string) domainidentity.Principal {
	f.t.Helper()
	access, err := f.repo.GetPersonalAgentAccess(f.ctx, id)
	if err != nil {
		f.t.Fatal(err)
	}
	if access.Status == "not_requested" {
		pending, err := f.repo.RequestPersonalAgentAccess(f.ctx, id)
		if err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.repo.DecidePersonalAgentAccess(f.ctx, id, "test-admin", "approved", pending.Version); err != nil {
			f.t.Fatal(err)
		}
	}
	return domainidentity.Principal{UserID: id, Roles: []string{"chat"}}
}

func (f *personalEnvironmentFixture) invoke(t *testing.T, callCtx context.Context, principal domainidentity.Principal, node string, request contractenv.Request) (contractenv.Result, error) {
	t.Helper()
	reserved, readErr := f.repo.GetPersonalEnvironment(f.ctx, principal.UserID, request.Lease.OwnerID)
	if readErr != nil || (reserved.State != "pending" && reserved.State != "deleting") || reserved.OperationID == "" || !reflect.DeepEqual(reserved.Lease, request.Lease) || !reserved.DataCreationAttempted {
		t.Fatalf("provider called before durable original lease: %v %+v", readErr, reserved)
	}
	f.calls, f.nodes = append(f.calls, request), append(f.nodes, node)
	if f.cancelInvoke != nil {
		f.cancelInvoke()
		return contractenv.Result{}, callCtx.Err()
	}
	if f.providerError != nil {
		return contractenv.Result{}, f.providerError
	}
	state := f.receiptState
	if request.Action == "release" {
		state = "released"
	}
	if request.Action == "delete_data" {
		state = "deleted"
	}
	result := contractenv.Result{SchemaVersion: contractenv.ResultVersion, LeaseID: request.Lease.ID, RunID: request.Lease.RunID, State: state, ObservedAt: time.Now().UTC()}
	if request.Action == "execute" {
		result.Output = &f.fileOutput
		result.TaskID = request.TaskID
	}
	if state == "active" || state == "quota_exceeded" {
		result.Snapshot = &contractenv.Snapshot{SchemaVersion: contractenv.SnapshotVersion, LeaseID: result.LeaseID, RunID: result.RunID, State: state, ObservedAt: result.ObservedAt, ResourceUsage: contractenv.Usage{Processes: 1}}
	}
	return result, nil
}

func testPersonalEnvironmentFileReadOwnerFence(t *testing.T, f *personalEnvironmentFixture) {
	view := f.wake(t, "file-owner")
	f.fileOutput = "ok\nZml4dHVyZQ=="
	before := len(f.calls)
	file, err := f.service.ReadPersonalEnvironmentFile(f.ctx, f.actor("file-owner"), "notes.txt")
	if err != nil || file.AssistantID != view.AssistantID || file.Content != "fixture" || file.Path != "notes.txt" || file.ObservedAt.IsZero() || len(f.calls) != before+1 {
		t.Fatalf("owned file receipt: %+v %v", file, err)
	}
	last := f.calls[len(f.calls)-1]
	if last.Action != "execute" || last.CreateData || last.Argv[len(last.Argv)-1] != "/data/notes.txt" || last.Lease.OwnerID != view.AssistantID {
		t.Fatal("file read changed the owner or selected an uncontrolled command")
	}
	f.scopeError = apperrors.ErrAccessDenied
	_, err = f.service.ReadPersonalEnvironmentFile(f.ctx, f.actor("file-owner"), "notes.txt")
	f.scopeError = nil
	if !errors.Is(err, apperrors.ErrAccessDenied) || len(f.calls) != before+1 {
		t.Fatal("revoked file read dispatched")
	}
	if _, err := f.service.ReadPersonalEnvironmentFile(f.ctx, f.actor("file-foreign"), "notes.txt"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("foreign owner borrowed environment: %v", err)
	}
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("file-owner"), "sleep", "", "en-US"); err != nil {
		t.Fatal(err)
	}
	before = len(f.calls)
	if _, err := f.service.ReadPersonalEnvironmentFile(f.ctx, f.actor("file-owner"), "notes.txt"); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
		t.Fatal("file read silently woke a sleeping environment")
	}
}

func (f *personalEnvironmentFixture) wake(t *testing.T, owner string) domain.PersonalEnvironment {
	t.Helper()
	item, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "wake", "personal", "en-US")
	if err != nil || item.State != "ready" || !item.DataInitialized {
		t.Fatalf("wake: %+v %v", item, err)
	}
	return item
}

func (f *personalEnvironmentFixture) allowReconcile(t *testing.T, owner, assistant string) {
	t.Helper()
	if err := f.db.Exec(`UPDATE ai_personal_environments SET operation_until=now()-interval '1 second' WHERE created_by=? AND assistant_id=?`, owner, assistant).Error; err != nil {
		t.Fatal(err)
	}
}

func testPersonalEnvironmentOwnerIsolation(t *testing.T, f *personalEnvironmentFixture) {
	before := len(f.calls)
	unbound, err := f.service.InspectPersonalEnvironment(f.ctx, f.actor("owner-a"))
	if err != nil || unbound.State != "unprovisioned" || len(f.calls) != before {
		t.Fatalf("read dispatched: %+v %v", unbound, err)
	}
	a, b := f.wake(t, "owner-a"), f.wake(t, "owner-b")
	if a.AssistantID == b.AssistantID || f.calls[before].Lease.ScopeHash == f.calls[before+1].Lease.ScopeHash {
		t.Fatal("owners share a binding")
	}
	if _, err := f.repo.GetPersonalEnvironment(f.ctx, "owner-b", a.AssistantID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("foreign read: %v", err)
	}
	forged, err := f.repo.GetPersonalEnvironment(f.ctx, "owner-a", a.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	forged.CreatedBy = "owner-b"
	if _, err := f.repo.BindPersonalEnvironment(f.ctx, forged); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("foreign bind: %v", err)
	}
}

func testPersonalEnvironmentFrozenBindingSleepWake(t *testing.T, f *personalEnvironmentFixture) {
	item := f.wake(t, "frozen")
	if item.Backend != "container" || item.Provider != "docker" || item.DataRef == "" || item.PolicyRevision == "" || item.ComputeGeneration != 1 || item.ComputeRef == "" {
		t.Fatalf("missing frozen binding identity: %+v", item)
	}
	first := f.calls[len(f.calls)-1]
	originalPolicy := f.policy
	f.policy.AgentClusterID, f.policy.Quota.DiskBytes = uuid.NewString(), 64<<20
	f.policy.Backend, f.policy.Provider, f.policy.PolicyRevision = "vm", "pve", "changed-template"
	defer func() { f.policy = originalPolicy }()
	count := len(f.calls)
	f.wake(t, "frozen")
	if len(f.calls) != count {
		t.Fatal("ready f.wake was replayed")
	}
	sleeping, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("frozen"), "sleep", "", "en-US")
	if err != nil || sleeping.State != "sleeping" || !reflect.DeepEqual(f.calls[len(f.calls)-1].Lease, first.Lease) {
		t.Fatalf("sleep lost original lease: %+v %v", sleeping, err)
	}
	reawakened := f.wake(t, "frozen")
	next := f.calls[len(f.calls)-1]
	if next.CreateData || next.Lease.ID == first.Lease.ID || next.Lease.OwnerID != item.AssistantID || next.Lease.Quota != first.Lease.Quota || f.nodes[len(f.nodes)-1] != originalPolicy.AgentClusterID {
		t.Fatal("wake rebound quota/node or recreated data")
	}
	assertPersonalEnvironmentWakeGeneration(t, item, reawakened)
}

func testPersonalEnvironmentUnknownNoReplay(t *testing.T, f *personalEnvironmentFixture) {
	f.providerError = context.DeadlineExceeded
	_, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("unknown"), "wake", "personal", "en-US")
	f.providerError = nil
	if !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("unknown result: %v", err)
	}
	original := f.calls[len(f.calls)-1]
	view, err := f.service.GetPersonalEnvironment(f.ctx, f.actor("unknown"), "en-US")
	if err != nil || view.State != "unknown" || view.DataInitialized {
		t.Fatalf("invented readiness: %+v %v", view, err)
	}
	before := len(f.calls)
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("unknown"), "reconcile", "", "en-US"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("active cleanup window ignored: %v", err)
	}
	f.allowReconcile(t, "unknown", view.AssistantID)
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("unknown"), "wake", "", "en-US"); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
		t.Fatalf("unknown replay: %v", err)
	}
	view, err = f.service.InspectPersonalEnvironment(f.ctx, f.actor("unknown"))
	last := f.calls[len(f.calls)-1]
	if err != nil || view.State != "ready" || last.Action != "inspect" || last.CreateData || !reflect.DeepEqual(last.Lease, original.Lease) {
		t.Fatalf("reconciliation changed original lease: %+v %v", view, err)
	}
}

func testPersonalEnvironmentMissingDataNotRecreated(t *testing.T, f *personalEnvironmentFixture) {
	item := f.wake(t, "missing")
	f.receiptState = "missing"
	missing, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("missing"), "reconcile", "", "en-US")
	f.receiptState = "active"
	if err != nil || missing.State != "missing" {
		t.Fatalf("missing receipt: %+v %v", missing, err)
	}
	f.providerError = errors.New("fixture data is absent")
	_, err = f.service.ChangePersonalEnvironment(f.ctx, f.actor("missing"), "wake", "", "en-US")
	f.providerError = nil
	if !errors.Is(err, apperrors.ErrServiceUnavailable) || f.calls[len(f.calls)-1].CreateData {
		t.Fatalf("missing data recreation: %v", err)
	}
	view, err := f.service.GetPersonalEnvironment(f.ctx, f.actor("missing"), "en-US")
	if err != nil || view.State != "unknown" || view.AssistantID != item.AssistantID {
		t.Fatalf("loss was hidden: %+v %v", view, err)
	}
}

func testPersonalEnvironmentDispatchCas(t *testing.T, f *personalEnvironmentFixture) {
	view := f.wake(t, "cas")
	current, err := f.repo.GetPersonalEnvironment(f.ctx, "cas", view.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	var success atomic.Int32
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			_, err := f.repo.ReservePersonalEnvironment(f.ctx, current, current.Lease, uuid.NewString(), false)
			if err == nil {
				success.Add(1)
			} else if !errors.Is(err, apperrors.ErrConflict) {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if success.Load() != 1 {
		t.Fatalf("dispatch winners=%d", success.Load())
	}
}

func testPersonalEnvironmentLateReceiptFence(t *testing.T, f *personalEnvironmentFixture) {
	view := f.wake(t, "late")
	current, err := f.repo.GetPersonalEnvironment(f.ctx, "late", view.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	previous := current
	old, err := f.repo.ReservePersonalEnvironment(f.ctx, current, current.Lease, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	f.allowReconcile(t, "late", view.AssistantID)
	current, err = f.repo.GetPersonalEnvironment(f.ctx, "late", view.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := f.repo.ReservePersonalEnvironment(f.ctx, current, current.Lease, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	old.State = "sleeping"
	if _, err := f.repo.CompletePersonalEnvironment(f.ctx, old); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("late receipt committed: %v", err)
	}
	if _, err := f.repo.AbortPersonalEnvironment(f.ctx, old, previous); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("late local abort undid a newer reservation: %v", err)
	}
	next.State = "missing"
	if _, err := f.repo.CompletePersonalEnvironment(f.ctx, next); err != nil {
		t.Fatal(err)
	}
}

func testPersonalEnvironmentQuotaScopeCeiling(t *testing.T, f *personalEnvironmentFixture) {
	view := f.wake(t, "ceiling")
	current, err := f.repo.GetPersonalEnvironment(f.ctx, "ceiling", view.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*contractenv.Lease){func(l *contractenv.Lease) { l.Quota.DiskBytes *= 2 }, func(l *contractenv.Lease) {
		l.ScopeHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}, func(l *contractenv.Lease) { l.OwnerID = "other" }, func(l *contractenv.Lease) { l.Mode = "observe-only" }} {
		lease := current.Lease
		change(&lease)
		if _, err := f.repo.ReservePersonalEnvironment(f.ctx, current, lease, uuid.NewString(), false); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("ceiling changed: %v", err)
		}
	}
}

func testPersonalEnvironmentCanceledReceiptPersisted(t *testing.T, f *personalEnvironmentFixture) {
	callCtx, cancel := context.WithCancel(f.ctx)
	f.cancelInvoke = cancel
	_, err := f.service.ChangePersonalEnvironment(callCtx, f.actor("canceled"), "wake", "personal", "en-US")
	f.cancelInvoke = nil
	cancel()
	if !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("canceled receipt: %v", err)
	}
	view, err := f.service.GetPersonalEnvironment(f.ctx, f.actor("canceled"), "en-US")
	if err != nil || view.State != "unknown" {
		t.Fatalf("cancellation erased dispatch: %+v %v", view, err)
	}
}

func testPersonalEnvironmentCurrentPermissionDenied(t *testing.T, f *personalEnvironmentFixture) {
	before := len(f.calls)
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, domainidentity.Principal{UserID: "denied"}, "wake", "personal", "en-US"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("permission bypass: %v", err)
	}
	f.scopeError = apperrors.ErrAccessDenied
	_, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("node-denied"), "wake", "personal", "en-US")
	f.scopeError = nil
	if !errors.Is(err, apperrors.ErrAccessDenied) || len(f.calls) != before {
		t.Fatalf("node scope bypass: %v", err)
	}
	view, err := f.service.GetPersonalEnvironment(f.ctx, f.actor("node-denied"), "en-US")
	if err != nil || view.State != "unprovisioned" {
		t.Fatalf("denied node bound: %+v %v", view, err)
	}
}

func assertPersonalEnvironmentWakeGeneration(t *testing.T, item, reawakened domain.PersonalEnvironment) {
	t.Helper()
	if reawakened.Backend != item.Backend || reawakened.Provider != item.Provider || reawakened.PolicyRevision != item.PolicyRevision || reawakened.DataRef != item.DataRef || reawakened.ComputeGeneration != item.ComputeGeneration+1 || reawakened.ComputeRef == item.ComputeRef {
		t.Fatalf("wake changed frozen data or lost compute generation: %+v", reawakened)
	}
}
