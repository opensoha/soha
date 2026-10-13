package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	delivery "github.com/opensoha/soha/internal/domain/delivery"
	identity "github.com/opensoha/soha/internal/domain/identity"
	secret "github.com/opensoha/soha/internal/domain/secret"
	vm "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type sshMemory struct {
	binding  vm.VMAccessBinding
	sessions map[string]vm.VMSSHSession
	commands map[string]delivery.ExecutionTask
}

func (m *sshMemory) GetVMAccessBinding(context.Context, string) (vm.VMAccessBinding, error) {
	return m.binding, nil
}
func (m *sshMemory) SaveVMAccessBinding(_ context.Context, b vm.VMAccessBinding, revision int) (vm.VMAccessBinding, error) {
	if revision != m.binding.Revision {
		return b, apperrors.ErrConflict
	}
	b.Revision++
	m.binding = b
	return b, nil
}
func (m *sshMemory) CreateVMSSHSession(_ context.Context, s vm.VMSSHSession) error {
	if _, exists := m.sessions[s.ID]; exists {
		return apperrors.ErrConflict
	}
	m.sessions[s.ID] = s
	return nil
}
func (m *sshMemory) GetVMSSHSession(_ context.Context, id string) (vm.VMSSHSession, error) {
	s, ok := m.sessions[id]
	if !ok {
		return s, apperrors.ErrNotFound
	}
	return s, nil
}
func (m *sshMemory) TransitionVMSSHSession(_ context.Context, id string, previous []string, status, reason string) error {
	s, ok := m.sessions[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	allowed := false
	for _, state := range previous {
		allowed = allowed || s.Status == state
	}
	if !allowed {
		return apperrors.ErrConflict
	}
	s.Status, s.Reason = status, reason
	m.sessions[id] = s
	return nil
}
func (m *sshMemory) ActivateVMSSHSession(ctx context.Context, s vm.VMSSHSession) error {
	if err := m.TransitionVMSSHSession(ctx, s.ID, []string{"pending"}, "active", ""); err != nil {
		return err
	}
	s.Status = "active"
	m.sessions[s.ID] = s
	return nil
}
func (m *sshMemory) CreateExecutionTask(_ context.Context, t delivery.ExecutionTask) (delivery.ExecutionTask, error) {
	if _, ok := m.commands[t.ID]; ok {
		return t, apperrors.ErrConflict
	}
	m.commands[t.ID] = t
	return t, nil
}
func (m *sshMemory) GetExecutionTask(_ context.Context, id string) (delivery.ExecutionTask, error) {
	t, ok := m.commands[id]
	if !ok {
		return t, apperrors.ErrNotFound
	}
	return t, nil
}

type sshIdentityAdapter struct {
	fakeAdapter
	identity vm.VMIdentity
}

func (a *sshIdentityAdapter) ObserveVMIdentity(context.Context, vm.AdapterConnection, vm.AdapterVM) (vm.VMIdentity, error) {
	return a.identity, nil
}

type sshSecretFake struct {
	denied  bool
	leases  int
	redeems int
	revoked int
	targets []secret.Target
}

func (f *sshSecretFake) PinSSHCredential(_ context.Context, _ identity.Principal, uri string, target secret.Target) (secret.Reference, secret.Kind, error) {
	f.targets = append(f.targets, target)
	if f.denied {
		return secret.Reference{}, "", apperrors.ErrAccessDenied
	}
	return secret.Reference{URI: uri, Alias: "SSH_AUTH", Version: 1}, secret.KindSSHKeypair, nil
}
func (f *sshSecretFake) IssueLease(_ context.Context, _ identity.Principal, _ []secret.Reference, target secret.Target, subject, id, agent string) (*secret.LeaseGrant, error) {
	if subject != "vm_ssh_session" || agent != "agent-one" {
		panic("incorrect SSH lease subject")
	}
	f.targets = append(f.targets, target)
	f.leases++
	return &secret.LeaseGrant{ID: "lease-one", Token: "test-lease-token-long", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (f *sshSecretFake) RedeemVMSSHLease(context.Context, string, string, string, string) (sohaapi.SecretLeaseRedemption, error) {
	f.redeems++
	return sohaapi.SecretLeaseRedemption{}, nil
}
func (f *sshSecretFake) RevokeSubjectLeases(context.Context, string, string) error {
	f.revoked++
	return nil
}

type sshExecutorFake struct {
	capability sohaapi.AgentSSHCapability
	opens      int
	request    sohaapi.AgentSSHSessionRequest
}

func (f *sshExecutorFake) Capability(context.Context, string) (sohaapi.AgentSSHCapability, error) {
	return f.capability, nil
}
func (f *sshExecutorFake) OpenSession(_ context.Context, _ string, input sohaapi.AgentSSHSessionRequest) (SSHChannel, error) {
	f.opens++
	f.request = input
	return nil, apperrors.ErrUnsupportedOperation
}

func sshFixture(t *testing.T) (*Service, *sshMemory, *sshSecretFake, *sshExecutorFake, *sshIdentityAdapter) {
	t.Helper()
	repo := newMemoryRepo()
	connection := repo.addConnection(vm.Connection{Provider: ProviderPVE, Enabled: true, Endpoint: "https://pve.example:8006"})
	repo.vms["vm-one"] = vm.VM{ID: "vm-one", ConnectionID: connection.ID, ExternalID: "101", Name: "test-vm", Status: "running", NodeName: "node-one"}
	adapter := &sshIdentityAdapter{identity: vm.VMIdentity{ProviderVMID: "101", Name: "test-vm", Node: "node-one", Generation: "generation-one", Status: "running", IPAddresses: []string{"10.0.0.30"}}}
	service := newTestService(repo, &captureOperations{}, adapter)
	service.permissions = appaccess.NewPermissionResolver(testRoleReader{matrix: map[string][]string{"admin": {vmConnectPermission, vmAccessPermission, "virtualization.vms.view"}}})
	policy, err := service.capacityPolicy(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G101 -- Synthetic Secret URI for target and version isolation.
	m := &sshMemory{binding: vm.VMAccessBinding{VMID: "vm-one", Revision: 1, ProviderVMID: "101", Generation: "generation-one", SourceID: policy.SourceID, Address: "10.0.0.30", Port: 22, Username: "soha", CredentialRef: "soha://secrets/key/versions/1", CredentialKind: secret.KindSSHKeypair, ExecutionClusterID: "cluster-one", ExecutionAgentID: "agent-one", Status: "ready"}, sessions: map[string]vm.VMSSHSession{}, commands: map[string]delivery.ExecutionTask{}}
	secrets := &sshSecretFake{}
	executor := &sshExecutorFake{capability: sohaapi.AgentSSHCapability{AgentID: "agent-one", ProtocolVersion: 1, Available: true, CommandsAvailable: true}}
	service.executionPrincipals = executionPrincipalFunc(func(context.Context, string, string) (identity.Principal, error) { return testPrincipal(), nil })
	service.ssh = SSHOptions{Access: m, Secrets: secrets, Executor: executor, Commands: m, ControlPlaneEndpoint: func() string { return "https://control.example" }, WithinCommand: func(ctx context.Context, _ string, fn func(context.Context) error) error { return fn(ctx) }, AuthorizeExecutionCluster: func(context.Context, identity.Principal, string) error { return nil }, AuthenticateExecutionAgent: func(_ context.Context, cluster, token string) error {
		if cluster != "cluster-one" || token != "test-agent-token" {
			return apperrors.ErrAccessDenied
		}
		return nil
	}}
	service.ssh.CancelCommand = func(ctx context.Context, id string, _ delivery.ExecutionTaskActionInput) (delivery.ExecutionTask, error) {
		task, _ := m.GetExecutionTask(ctx, id)
		task.Status = "canceled"
		m.commands[id] = task
		return task, nil
	}
	return service, m, secrets, executor, adapter
}

func TestSSHSessionUsesCurrentControlPlaneEndpoint(t *testing.T) {
	service, _, _, executor, _ := sshFixture(t)
	endpoint := "https://control.example"
	service.ssh.ControlPlaneEndpoint = func() string { return endpoint }
	session, err := service.CreateVMSSHSession(t.Context(), testPrincipal(), "vm-one", sohaapi.VirtualMachineSSHSessionInput{BindingRevision: 1, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	endpoint = ""
	if service.sshConfigured(false) {
		t.Fatal("removed control plane endpoint remained available")
	}
	endpoint = "https://updated-control.example"
	if _, err = service.OpenVMSSHStream(t.Context(), testPrincipal(), session.ID); !errors.Is(err, apperrors.ErrUnsupportedOperation) || executor.opens != 1 {
		t.Fatal("configured session did not reach the executor", err)
	}
	if executor.request.ControlPlaneEndpoint != endpoint {
		t.Fatal("SSH session used a stale control plane endpoint")
	}
	service.ssh.ControlPlaneEndpoint = nil
	if service.sshConfigured(false) {
		t.Fatal("missing control plane resolver remained available")
	}
}

func TestSSHSessionRechecksIdentityPurposeOwnerAndOrigin(t *testing.T) {
	service, m, secrets, executor, adapter := sshFixture(t)
	ctx := WithSSHBrowserSession(t.Context(), "browser-one")
	session, err := service.CreateVMSSHSession(ctx, testPrincipal(), "vm-one", sohaapi.VirtualMachineSSHSessionInput{BindingRevision: 1, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if secrets.leases != 1 || m.sessions[session.ID].BrowserSessionID != "browser-one" || m.sessions[session.ID].Purpose != "vm_auth" {
		t.Fatal("wrong session origin or lease")
	}
	public, _ := json.Marshal(session)
	if strings.Contains(string(public), "test-lease-token") || strings.Contains(string(public), "SSH_AUTH") {
		t.Fatal("public session exposed a credential")
	}
	other := testPrincipal()
	other.UserID = "other"
	if _, err = service.GetVMSSHSession(t.Context(), other, session.ID); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("other owner read a session", err)
	}
	stored := m.sessions[session.ID]
	stored.Status = "active"
	m.sessions[session.ID] = stored
	input := sohaapi.VMSSHCredentialRedemptionInput{AgentID: "agent-one", ClusterID: "cluster-one", LeaseID: "lease-one", Token: "test-lease-token-long"}
	service.ssh.CurrentSessionPrincipal = func(context.Context, string, string) (identity.Principal, error) {
		return identity.Principal{}, apperrors.ErrUnauthorized
	}
	if _, err = service.RedeemVMSSHCredentials(t.Context(), session.ID, "test-agent-token", input); !errors.Is(err, apperrors.ErrAccessDenied) || secrets.redeems != 0 {
		t.Fatal("logged-out browser redeemed lease", err)
	}
	service.ssh.CurrentSessionPrincipal = func(context.Context, string, string) (identity.Principal, error) { return testPrincipal(), nil }
	adapter.identity.Generation = "replacement-generation"
	if _, err = service.RedeemVMSSHCredentials(t.Context(), session.ID, "test-agent-token", input); !errors.Is(err, apperrors.ErrAccessDenied) || secrets.redeems != 0 {
		t.Fatal("replacement VM redeemed lease", err)
	}
	adapter.identity.Generation = "generation-one"
	secrets.denied = true
	if service.checkActiveSSHSession(t.Context(), stored) == nil {
		t.Fatal("revoked credential left session active")
	}
	if _, err = service.CloseVMSSHSession(t.Context(), testPrincipal(), session.ID); err != nil || m.sessions[session.ID].Status != "unknown" || executor.opens != 0 {
		t.Fatal("revoked owner cannot close original session", err)
	}
}

func TestSSHCommandFreezesIntentAndCannotBecomeInteractiveShell(t *testing.T) {
	service, m, secrets, _, _ := sshFixture(t)
	input := sohaapi.VirtualMachineSSHCommandInput{BindingRevision: 1, Command: "printf task51-marker", IdempotencyKey: "request-one", TimeoutSeconds: 60}
	created, err := service.CreateVMSSHCommand(t.Context(), testPrincipal(), "vm-one", input)
	if err != nil {
		t.Fatal(err)
	}
	task := m.commands[created.ID]
	raw, _ := json.Marshal(task)
	if strings.Contains(string(raw), input.Command) {
		t.Fatal("command stored as plaintext")
	}
	if len(m.sessions) != 1 || task.TaskKind != vmSSHCommandKind || task.ProviderKind != "vm_ssh.cluster-one" || secrets.leases != 0 {
		t.Fatal("wrong durable command identity")
	}
	recovered, err := service.CreateVMSSHCommand(t.Context(), testPrincipal(), "vm-one", input)
	if err != nil || recovered.ID != created.ID || len(m.commands) != 1 {
		t.Fatal("duplicate intent enqueued another command", err)
	}
	input.Command = "different"
	if _, err = service.CreateVMSSHCommand(t.Context(), testPrincipal(), "vm-one", input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("changed command adopted prior task", err)
	}
	if _, err = service.OpenVMSSHStream(t.Context(), testPrincipal(), created.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("command lease became interactive shell", err)
	}
	testSSHCommandDispatchAndCancel(t, service, m, secrets, task)
}

func testSSHCommandDispatchAndCancel(t *testing.T, service *Service, m *sshMemory, secrets *sshSecretFake, task delivery.ExecutionTask) {
	t.Helper()
	endpoint := "https://control.example"
	service.ssh.ControlPlaneEndpoint = func() string { return endpoint }
	task.Status = "dispatching"
	task.ClaimedByAgentID = "other-agent"
	m.commands[task.ID] = task
	if _, err := service.HydrateSSHExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrAccessDenied) || secrets.leases != 0 {
		t.Fatal("other agent claimed command", err)
	}
	task.ClaimedByAgentID = "agent-one"
	task.AttemptCount = 1
	m.commands[task.ID] = task
	endpoint = "https://updated-control.example"
	hydrated, err := service.HydrateSSHExecutionTask(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	request, ok := hydrated.Payload["sshCommand"].(sohaapi.AgentSSHCommandRequest)
	if !ok {
		t.Fatal("missing typed SSH request")
	}
	if request.Session.ControlPlaneEndpoint != endpoint {
		t.Fatal("queued command used a stale control plane endpoint")
	}
	if request.Command != "printf task51-marker" || request.Session.AgentID != "agent-one" || m.sessions[task.ID].Purpose != "vm_exec" || m.sessions[task.ID].Status != "active" {
		t.Fatal("frozen command was not restored")
	}
	if _, err = service.HydrateSSHExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("command hydrated twice", err)
	}
	result, err := service.CancelVMSSHCommand(t.Context(), testPrincipal(), task.ID)
	if err != nil || !result.Terminal || result.Outcome != "unknown" || m.sessions[task.ID].Status != "unknown" {
		t.Fatal("dispatched cancellation reported success", err)
	}
	if _, err = service.CheckVMSSHCommandAuthorization(t.Context(), task.ID, "test-agent-token", sohaapi.VMSSHCredentialRedemptionInput{ClusterID: "cluster-one", AgentID: "agent-one", LeaseID: "lease-one", Token: "test-lease-token-long"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("canceled command remained authorized", err)
	}
}

func TestSSHCommandPreparationAuditRetainsOwnerAfterCommitFailure(t *testing.T) {
	service, memory, _, _, _ := sshFixture(t)
	audit := &captureAudit{}
	service.audit = audit
	item, err := service.CreateVMSSHCommand(t.Context(), testPrincipal(), "vm-one", sohaapi.VirtualMachineSSHCommandInput{BindingRevision: 1, Command: "printf task51-marker", IdempotencyKey: "commit-failure", TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	task := memory.commands[item.ID]
	task.Status, task.ClaimedByAgentID = "dispatching", "agent-one"
	memory.commands[task.ID] = task
	service.ssh.WithinCommand = func(ctx context.Context, _ string, apply func(context.Context) error) error {
		if err := apply(ctx); err != nil {
			return err
		}
		return apperrors.ErrConflict
	}
	_, err = service.HydrateSSHExecutionTask(t.Context(), task)
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal(err)
	}
	entry := audit.entries[len(audit.entries)-1]
	if entry.ActorID != testPrincipal().UserID || entry.ResourceName != "vm-one" || entry.Metadata["stage"] != "commit" {
		t.Fatal("preparation diagnostic lost its owner", entry)
	}
}

func (m *sshMemory) ListVMSecretUsages(context.Context, string) ([]vm.VMSecretUsage, error) {
	return []vm.VMSecretUsage{{TargetID: "vm-one", VMID: "vm-one", Usage: "login_binding", Version: 1, BindingRevision: 1, Status: "active"}, {TargetID: "hidden", VMID: "hidden", Usage: "login_binding", Version: 1, Status: "active"}, {TargetID: "hidden-operation", Usage: "cloud_init_operation", Version: 1, Status: "queued"}}, nil
}
func TestSSHReferenceLookupFiltersInvisibleResources(t *testing.T) {
	service, _, _, _, _ := sshFixture(t)
	usages, err := service.ListCredentialReferences(t.Context(), testPrincipal(), "key")
	if err != nil || len(usages) != 1 || usages[0].TargetID != "vm-one" {
		t.Fatal("invisible reference disclosed", usages, err)
	}
}

func TestSSHSessionRejectsAnUnknownExecutorProtocolBeforeLease(t *testing.T) {
	service, _, secrets, executor, _ := sshFixture(t)
	executor.capability.ProtocolVersion = 2
	if _, err := service.CreateVMSSHSession(t.Context(), testPrincipal(), "vm-one", sohaapi.VirtualMachineSSHSessionInput{BindingRevision: 1, Cols: 80, Rows: 24}); !errors.Is(err, apperrors.ErrUnsupportedOperation) || secrets.leases != 0 {
		t.Fatal("unknown executor protocol accepted", err)
	}
}
