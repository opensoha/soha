package virtualization

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	delivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	deliveryrepo "github.com/opensoha/soha/internal/repository/delivery"
)

func testVMAccessRepository(t *testing.T, repo *Repository, ctx context.Context) {
	t.Helper()
	connection, err := repo.CreateConnection(ctx, domain.ConnectionInput{Provider: "pve", Name: "task51-access", Endpoint: "https://" + uuid.NewString() + ".example"})
	if err != nil {
		t.Fatal(err)
	}
	vm, err := repo.UpsertVM(ctx, domain.VM{Provider: "pve", ConnectionID: connection.ID, ExternalID: "101", Name: "test-vm", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G101 -- Synthetic reference contains no credential material.
	binding := domain.VMAccessBinding{VMID: vm.ID, Generation: "generation-one", CredentialRef: "soha://secrets/key/versions/1", Status: "ready"}
	var wait sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for range 8 {
		wait.Go(func() {
			_, err := repo.SaveVMAccessBinding(ctx, binding, 0)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				winners++
			} else if !errors.Is(err, apperrors.ErrConflict) {
				t.Errorf("binding CAS failed: %v", err)
			}
		})
	}
	wait.Wait()
	if winners != 1 {
		t.Fatalf("binding CAS winners=%d", winners)
	}
	saved, err := repo.GetVMAccessBinding(ctx, vm.ID)
	if err != nil || saved.Revision != 1 {
		t.Fatal("binding not persisted", err)
	}
	if _, err = repo.SaveVMAccessBinding(ctx, binding, 0); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("stale binding overwritten", err)
	}
	testVMSSHSessionRepository(t, repo, ctx, vm, saved)
}

func testVMSSHSessionRepository(t *testing.T, repo *Repository, ctx context.Context, vm domain.VM, saved domain.VMAccessBinding) {
	t.Helper()
	actor := uuid.NewString()
	// #nosec G101 -- Synthetic encrypted-grant marker for storage round-trip.
	session := domain.VMSSHSession{ID: uuid.NewString(), VMID: vm.ID, BindingRevision: 1, Binding: saved, Principal: domainidentity.Principal{UserID: actor}, Status: "pending", ExpiresAt: time.Now().Add(time.Minute), CreatedAt: time.Now(), LeaseCredential: "soha:v2:encrypted-test-grant"}
	if err := repo.CreateVMSSHSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	usages, usageErr := repo.ListVMSecretUsages(ctx, "key")
	if usageErr != nil || len(usages) != 2 {
		t.Fatal("credential reference lookup failed", usages, usageErr)
	}
	for _, usage := range usages {
		if usage.Version != 1 || usage.VMID != vm.ID {
			t.Fatal("wrong fixed version reference", usage)
		}
	}
	stored, err := repo.GetVMSSHSession(ctx, session.ID)
	if err != nil || stored.LeaseCredential != session.LeaseCredential {
		t.Fatal("encrypted grant storage failed", err)
	}
	if err = repo.TransitionVMSSHSession(ctx, session.ID, []string{"pending"}, "active", ""); err != nil {
		t.Fatal(err)
	}
	if err = repo.TransitionVMSSHSession(ctx, session.ID, []string{"pending"}, "active", ""); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("session consumed twice", err)
	}
	if err = repo.TransitionVMSSHSession(ctx, session.ID, []string{"active"}, "unknown", "connection lost"); err != nil {
		t.Fatal(err)
	}
	stored, err = repo.GetVMSSHSession(ctx, session.ID)
	if err != nil || stored.Status != "unknown" || stored.Reason != "connection lost" {
		t.Fatal("unknown state lost", err)
	}
	testSSHCommandLedger(t, repo, ctx, vm.ID, actor)
}

func testSSHCommandLedger(t *testing.T, repo *Repository, ctx context.Context, vmID, actor string) {
	t.Helper()
	commands := deliveryrepo.New(repo.db)
	now := time.Now().UTC()
	task := delivery.ExecutionTask{ID: uuid.NewString(), TaskKind: "vm_ssh_command", ProviderKind: "vm_ssh.cluster-one", TargetKind: "vm", Status: "queued", CallbackToken: uuid.NewString(), TimeoutSeconds: 60, CreatedAt: now, UpdatedAt: now, Payload: map[string]any{"vmId": vmID, "actorId": actor, "executionAgentId": "agent-one"}, Result: map[string]any{}}
	if _, err := commands.CreateExecutionTask(ctx, task); err != nil {
		t.Fatal("SSH command requires no synthetic Application", err)
	}
	if _, err := commands.ClaimExecutionTask(ctx, []string{task.ProviderKind}, "other-agent", ""); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("other Agent claimed the frozen VM command", err)
	}
	claimed, err := commands.ClaimExecutionTask(ctx, []string{task.ProviderKind}, "agent-one", "")
	if err != nil || claimed.ID != task.ID || claimed.AttemptCount != 1 {
		t.Fatal("original Agent cannot claim command", err)
	}
	staleExpiry := task
	staleExpiry.Status = "failed"
	if state, err := commands.UpdateExecutionTask(ctx, staleExpiry); err != nil || state.Status != "dispatching" || state.ClaimedByAgentID != "agent-one" || state.AttemptCount != 1 {
		t.Fatal("stale queue expiry overwrote a claim", state, err)
	}
	claimed.Status = "canceled"
	if _, err = commands.UpdateExecutionTask(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	late := claimed
	late.Status = "completed"
	late.Result = map[string]any{"late": "success"}
	final, err := commands.UpdateExecutionTask(ctx, late)
	if err != nil || final.Status != "canceled" || final.Result["late"] != nil {
		t.Fatal("late receipt overwrote cancellation", err)
	}
	testSSHQueueExpiryRace(t, commands, ctx, task)
	invalid := task
	invalid.ID = uuid.NewString()
	invalid.Payload = map[string]any{}
	if _, err = commands.CreateExecutionTask(ctx, invalid); err == nil {
		t.Fatal("ownerless SSH task bypassed SQL constraint")
	}
}

func testSSHQueueExpiryRace(t *testing.T, commands *deliveryrepo.Repository, ctx context.Context, original delivery.ExecutionTask) {
	t.Helper()
	for range 8 {
		task := original
		task.ID = uuid.NewString()
		task.CallbackToken = uuid.NewString()
		task.Payload = map[string]any{"vmId": original.Payload["vmId"], "actorId": original.Payload["actorId"], "executionAgentId": "race-agent"}
		if _, err := commands.CreateExecutionTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		expired := task
		expired.Status = "failed"
		var wait sync.WaitGroup
		wait.Go(func() {
			_, err := commands.ClaimExecutionTask(ctx, []string{task.ProviderKind}, "race-agent", "")
			if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
				t.Error(err)
			}
		})
		wait.Go(func() {
			_, err := commands.UpdateExecutionTask(ctx, expired)
			if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
				t.Error(err)
			}
		})
		wait.Wait()
		current, err := commands.GetExecutionTask(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == "failed" {
			if current.AttemptCount != 0 || current.ClaimedByAgentID != "" {
				t.Fatal("claimed task reported not started", current)
			}
		} else if current.Status != "dispatching" || current.AttemptCount != 1 || current.ClaimedByAgentID != "race-agent" {
			t.Fatal("lost claim", current)
		}
		current.Status = "canceled"
		if _, err = commands.UpdateExecutionTask(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
}
