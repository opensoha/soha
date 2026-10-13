package copilot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	auditrepository "github.com/opensoha/soha/internal/repository/auditlog"
)

type personalVMFixture struct {
	t            *testing.T
	repo         *Repository
	ctx          context.Context
	actor        identity.Principal
	policy       domain.PersonalEnvironmentPolicy
	proof        domain.PersonalVMIdentity
	observeError error
	audit        *guestFailingAudit
	service      *appcopilot.Service
	assistant    domain.PersonalAssistant
	bound        domain.PersonalEnvironmentRecord
	activate     func(identity.Principal) domain.PersonalAssistant
}

// Normal Copilot binding and real PostgreSQL fences; provider observations are stubs.
func TestPersonalVMBindingWithPostgres(t *testing.T) {
	f := newPersonalVMFixture(t)
	t.Run("pending_and_replay", func(t *testing.T) { f.t = t; testPersonalVMPendingAndReplay(t, f) })
	if f.bound.AssistantID == "" {
		t.Fatal("normal pending binding prerequisite failed")
	}
	for _, test := range []struct {
		name string
		run  func(*testing.T, *personalVMFixture)
	}{
		{"foreign_owner_and_private_policy", testPersonalVMForeignOwnerAndPrivatePolicy},
		{"provider_replacement", testPersonalVMProviderReplacement},
		{"unavailable_template_filtered", testPersonalVMUnavailableTemplateFiltered},
		{"exclusive_vm_disk_and_data_ref", testPersonalVMExclusiveVmDiskAndDataRef},
		{"concurrent_owner_assignment", testPersonalVMConcurrentOwnerAssignment},
		{"audit_rollback", testPersonalVMAuditRollback},
		{"revoked_current_access", testPersonalVMRevokedCurrentAccess},
	} {
		t.Run(test.name, func(t *testing.T) { f.t = t; test.run(t, f) })
	}
}

func newPersonalVMFixture(t *testing.T) *personalVMFixture {
	f := &personalVMFixture{t: t}
	f.repo, f.ctx = retainedPersonalGuestRepository(t)
	f.t.Cleanup(func() { appaccess.SetRolePermissionMatrix(nil) })
	f.actor = identity.Principal{UserID: uuid.NewString(), Roles: []string{"chat"}}
	f.policy = domain.PersonalEnvironmentPolicy{TemplateID: "assigned-vm", Name: "Personal VM", Backend: "vm", Provider: "pve",
		PolicyRevision: "sha256:" + strings.Repeat("a", 64), Mode: "restricted-write", LeaseSeconds: 600,
		Quota: contractenv.Quota{DiskBytes: 1 << 30, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16},
		VM:    &domain.PersonalVMPolicy{OwnerID: f.actor.UserID, ComputeRef: uuid.NewString(), Provider: "pve", DataDiskSerial: "testdata01", DataDiskBytes: 8 << 30}}
	f.proof = domain.PersonalVMIdentity{ProviderIdentityHash: "sha256:" + strings.Repeat("b", 64), DataIdentityHash: "sha256:" + strings.Repeat("c", 64), DataDiskSerial: "testdata01", DataDiskBytes: 8 << 30}
	f.audit = &guestFailingAudit{service: appaudit.New(auditrepository.New(f.repo.db), nil)}
	var err error
	f.service, err = appcopilot.New(appcopilot.Dependencies{Sessions: f.repo, Messages: f.repo, DataSources: f.repo, AnalysisProfiles: f.repo, AutomationPolicies: f.repo, RootCauseRuns: f.repo, AgentRuns: f.repo, InspectionTasks: f.repo, InspectionRuns: f.repo, Feed: f.repo, Audits: f.audit,
		Permissions: appaccess.NewPermissionResolver(personalEnvironmentRoleReader{}),
		PersonalEnvironment: &appcopilot.PersonalEnvironmentDependencies{Store: f.repo,
			Policy: func(context.Context, string) (domain.PersonalEnvironmentPolicy, error) { return f.policy, nil },
			Templates: func(context.Context) ([]domain.PersonalEnvironmentPolicy, error) {
				return []domain.PersonalEnvironmentPolicy{f.policy}, nil
			},
			ObserveVM: func(_ context.Context, p identity.Principal, vm domain.PersonalVMPolicy) (domain.PersonalVMIdentity, error) {
				if p.UserID != vm.OwnerID {
					f.t.Fatal("observation lost explicit owner")
				}
				return f.proof, f.observeError
			},
			Transaction: func(ctx context.Context, apply func(context.Context) error) error {
				return dbtx.Within(ctx, f.repo.db, apply)
			},
			Authorize: func(context.Context, identity.Principal, string) error {
				f.t.Fatal("VM reached Docker authorization")
				return nil
			},
			Preflight: func(context.Context, identity.Principal, string, contractenv.Quota) error {
				f.t.Fatal("VM reached Docker quota probe")
				return nil
			},
			Invoke: func(context.Context, identity.Principal, string, contractenv.Request) (contractenv.Result, error) {
				f.t.Fatal("VM reached Docker dispatch")
				return contractenv.Result{}, nil
			},
		}})
	if err != nil {
		f.t.Fatal(err)
	}
	f.activate = func(p identity.Principal) domain.PersonalAssistant {
		f.t.Helper()
		if _, err := f.repo.RequestPersonalAgentAccess(f.ctx, p.UserID); err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.repo.DecidePersonalAgentAccess(f.ctx, p.UserID, "test-admin", "approved", 1); err != nil {
			f.t.Fatal(err)
		}
		assistant, err := f.service.EnsurePersonalAssistant(f.ctx, p, "en-US")
		if err != nil {
			f.t.Fatal(err)
		}
		return assistant
	}
	f.assistant = f.activate(f.actor)
	return f
}

func testPersonalVMPendingAndReplay(t *testing.T, f *personalVMFixture) {
	view, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor, "wake", f.policy.TemplateID, "en-US")
	if err != nil || view.State != "pending" || view.DataInitialized || view.ComputeRef != f.policy.VM.ComputeRef || view.ComputeGeneration != 1 || view.DataRef == "" {
		t.Fatalf("VM not frozen pending: %+v %v", view, err)
	}
	f.bound, err = f.repo.GetPersonalEnvironment(f.ctx, f.actor.UserID, f.assistant.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor, "wake", f.policy.TemplateID, "en-US")
	if err != nil || again.DataRef != view.DataRef || again.Version != view.Version || again.ComputeGeneration != 1 {
		t.Fatalf("replay changed binding: %+v %v", again, err)
	}
	if f.bound.AgentClusterID != "" || f.bound.DataCreationAttempted || f.bound.VMIdentity == nil || *f.bound.VMIdentity != f.proof {
		t.Fatal("pending binding inferred data or used Docker")
	}
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor, "sleep", "", "en-US"); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("unimplemented VM sleep dispatched: %v", err)
	}
	if _, err := f.repo.ReservePersonalEnvironment(f.ctx, f.bound, f.bound.Lease, uuid.NewString(), true); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("legacy lifecycle changed VM generation")
	}
}

func testPersonalVMForeignOwnerAndPrivatePolicy(t *testing.T, f *personalVMFixture) {
	foreign := identity.Principal{UserID: uuid.NewString(), Roles: []string{"chat"}}
	f.activate(foreign)
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, foreign, "wake", f.policy.TemplateID, "en-US"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("foreign user claimed VM: %v", err)
	}
	choices, err := f.service.ListPersonalEnvironmentTemplates(f.ctx, foreign)
	if err != nil || len(choices) != 0 {
		t.Fatalf("foreign assignment disclosed: %+v %v", choices, err)
	}
	choices, err = f.service.ListPersonalEnvironmentTemplates(f.ctx, f.actor)
	if err != nil || len(choices) != 1 || choices[0].Backend != "vm" {
		t.Fatalf("owner choice lost: %+v %v", choices, err)
	}
}

func testPersonalVMProviderReplacement(t *testing.T, f *personalVMFixture) {
	original := f.proof
	f.proof.ProviderIdentityHash = "sha256:" + strings.Repeat("d", 64)
	defer func() { f.proof = original }()
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor, "reconcile", "", "en-US"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("replaced provider accepted: %v", err)
	}
	current, err := f.repo.GetPersonalEnvironment(f.ctx, f.actor.UserID, f.assistant.ID)
	if err != nil || current.DataRef != f.bound.DataRef || current.ComputeGeneration != 1 || current.Version != f.bound.Version {
		t.Fatal("replacement rewrote identity")
	}
}

func testPersonalVMUnavailableTemplateFiltered(t *testing.T, f *personalVMFixture) {
	f.observeError = apperrors.ErrConflict
	defer func() { f.observeError = nil }()
	items, err := f.service.ListPersonalEnvironmentTemplates(f.ctx, f.actor)
	if err != nil || len(items) != 0 {
		t.Fatalf("unavailable VM broke template listing: %+v %v", items, err)
	}
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor, "reconcile", "", "en-US"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unavailable VM accepted bound lifecycle action: %v", err)
	}
}

func testPersonalVMExclusiveVmDiskAndDataRef(t *testing.T, f *personalVMFixture) {
	foreign := identity.Principal{UserID: uuid.NewString(), Roles: []string{"chat"}}
	a := f.activate(foreign)
	candidate := f.bound
	candidate.CreatedBy, candidate.AssistantID = foreign.UserID, a.ID
	candidate.Lease.OwnerID = a.ID
	for _, change := range []func(*domain.PersonalEnvironmentRecord){
		func(c *domain.PersonalEnvironmentRecord) { c.DataRef = uuid.NewString() },
		func(c *domain.PersonalEnvironmentRecord) {
			c.ComputeRef = uuid.NewString()
			c.DataRef = uuid.NewString()
		},
		func(c *domain.PersonalEnvironmentRecord) {
			c.ComputeRef = uuid.NewString()
			p := *c.VMIdentity
			p.DataIdentityHash = "sha256:" + strings.Repeat("e", 64)
			c.VMIdentity = &p
		},
	} {
		c := candidate
		change(&c)
		if _, err := f.repo.BindPersonalEnvironment(f.ctx, c); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("resource acquired twice: %v", err)
		}
	}
}

func testPersonalVMConcurrentOwnerAssignment(t *testing.T, f *personalVMFixture) {
	candidates := make([]domain.PersonalEnvironmentRecord, 2)
	for i := range candidates {
		p := identity.Principal{UserID: uuid.NewString(), Roles: []string{"chat"}}
		a := f.activate(p)
		c := f.bound
		c.CreatedBy, c.AssistantID, c.Lease.OwnerID = p.UserID, a.ID, a.ID
		c.ComputeRef = "00000000-0000-4000-8000-000000005049"
		c.DataRef = uuid.NewString()
		proof := *f.bound.VMIdentity
		proof.DataIdentityHash = "sha256:" + strings.Repeat("f", 64)
		c.VMIdentity = &proof
		candidates[i] = c
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, c := range candidates {
		wg.Go(func() { _, err := f.repo.BindPersonalEnvironment(f.ctx, c); results <- err })
	}
	wg.Wait()
	close(results)
	passed, conflicts := 0, 0
	for err := range results {
		if err == nil {
			passed++
		} else if errors.Is(err, apperrors.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || conflicts != 1 {
		t.Fatalf("concurrent assignments: success=%d conflict=%d", passed, conflicts)
	}
}

func testPersonalVMAuditRollback(t *testing.T, f *personalVMFixture) {
	p := identity.Principal{UserID: uuid.NewString(), Roles: []string{"chat"}}
	a := f.activate(p)
	oldPolicy, oldProof := f.policy, f.proof
	defer func() { f.policy, f.proof, f.audit.fail = oldPolicy, oldProof, false }()
	v := *f.policy.VM
	v.OwnerID, v.ComputeRef = p.UserID, uuid.NewString()
	f.policy.VM = &v
	f.proof.DataIdentityHash = "sha256:" + strings.Repeat("1", 64)
	f.audit.fail = true
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, p, "wake", f.policy.TemplateID, "en-US"); err == nil {
		t.Fatal("audit failure returned a VM binding")
	}
	if _, err := f.repo.GetPersonalEnvironment(f.ctx, p.UserID, a.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("failed audit retained binding: %v", err)
	}
	var count int
	if err := f.repo.db.WithContext(f.ctx).Raw(`SELECT COUNT(*) FROM audit_logs WHERE resource_name=? AND action='personal_environment.bind_vm'`, a.ID).Row().Scan(&count); err != nil || count != 0 {
		t.Fatalf("audit rollback: %d %v", count, err)
	}
}

func testPersonalVMRevokedCurrentAccess(t *testing.T, f *personalVMFixture) {
	if _, err := f.repo.DecidePersonalAgentAccess(f.ctx, f.actor.UserID, "test-admin", "revoked", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor, "reconcile", "", "en-US"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked owner reconciled: %v", err)
	}
	candidate := f.bound
	candidate.UpdatedAt = time.Now().UTC()
	if _, err := f.repo.BindPersonalEnvironment(f.ctx, candidate); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("repository replay ignored revocation: %v", err)
	}
}
