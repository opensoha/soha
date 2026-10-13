package copilot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func testPersonalBindingIdentityFence(t *testing.T, f *personalEnvironmentFixture) {
	view := f.wake(t, "binding-fence")
	current, err := f.repo.GetPersonalEnvironment(f.ctx, "binding-fence", view.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*domain.PersonalEnvironmentRecord){
		func(item *domain.PersonalEnvironmentRecord) { item.DataRef = "personal-data:foreign" },
		func(item *domain.PersonalEnvironmentRecord) { item.ComputeGeneration++ },
		func(item *domain.PersonalEnvironmentRecord) { item.ComputeRef = uuid.NewString() },
		func(item *domain.PersonalEnvironmentRecord) { item.PolicyRevision = "changed" },
		func(item *domain.PersonalEnvironmentRecord) { item.Backend, item.Provider = "vm", "pve" },
	} {
		forged := current
		change(&forged)
		if _, err := f.repo.ReservePersonalEnvironment(f.ctx, forged, current.Lease, uuid.NewString(), false); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("forged binding dispatched: %v", err)
		}
	}
	reserved, err := f.repo.ReservePersonalEnvironment(f.ctx, current, current.Lease, uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	forged := reserved
	forged.ComputeGeneration++
	if _, err := f.repo.CompletePersonalEnvironment(f.ctx, forged); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("foreign generation receipt committed: %v", err)
	}
	if _, err := f.repo.AbortPersonalEnvironment(f.ctx, reserved, current); err != nil {
		t.Fatal(err)
	}
}

func testPersonalBindingUnsupportedBackend(t *testing.T, f *personalEnvironmentFixture) {
	original := f.policy
	defer func() { f.policy = original }()
	f.policy.Backend, f.policy.Provider = "vm", "pve"
	before, probes := len(f.calls), f.preflightCalls
	_, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("unsupported-backend"), "wake", "personal", "en-US")
	if !errors.Is(err, apperrors.ErrServiceUnavailable) || len(f.calls) != before || f.preflightCalls != probes {
		t.Fatalf("unsupported VM reached Docker preflight/dispatch: %v", err)
	}
}

func testPersonalBindingLegacyMigration(t *testing.T, f *personalEnvironmentFixture) {
	view := f.wake(t, "legacy-binding")
	before, err := f.repo.GetPersonalEnvironment(f.ctx, "legacy-binding", view.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	// Only this disposable database is changed. Exercise a real pre-0111 row.
	if err := f.db.Exec(`DROP TRIGGER personal_environment_legacy_binding ON ai_personal_environments;
 DROP FUNCTION sync_legacy_personal_environment_binding()`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`ALTER TABLE ai_personal_environments
 DROP COLUMN backend, DROP COLUMN provider, DROP COLUMN policy_revision,
 DROP COLUMN compute_ref, DROP COLUMN compute_generation, DROP COLUMN data_ref`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`UPDATE ai_personal_environments SET state='unknown',operation_id=?,operation_until=now()+interval '70 seconds' WHERE assistant_id=?`, uuid.NewString(), view.AssistantID).Error; err != nil {
		t.Fatal(err)
	}
	var operation string
	if err := f.db.Raw(`SELECT operation_id FROM ai_personal_environments WHERE assistant_id=?`, view.AssistantID).Scan(&operation).Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "0111_personal_environment_binding.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	after, err := f.repo.GetPersonalEnvironment(f.ctx, "legacy-binding", view.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	assertPersonalBindingMigration(t, before, after, operation)
}

func testPersonalBindingLegacyWriter(t *testing.T, f *personalEnvironmentFixture) {
	source := f.wake(t, "legacy-lease-source")
	seed, err := f.repo.GetPersonalEnvironment(f.ctx, "legacy-lease-source", source.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	owner := "legacy-writer"
	assistant, err := f.service.EnsurePersonalAssistant(f.ctx, f.actor(owner), "en-US")
	if err != nil {
		t.Fatal(err)
	}
	lease := seed.Lease
	lease.ID, lease.RunID, lease.OwnerID = uuid.NewString(), uuid.NewString(), assistant.ID
	scope := sha256.Sum256([]byte("personal-environment:" + assistant.ID + ":" + owner))
	lease.ScopeHash = "sha256:" + hex.EncodeToString(scope[:])
	payload, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	// Exact legacy column set: no binding metadata is supplied by the old writer.
	if err := f.db.Exec(`INSERT INTO ai_personal_environments
 (assistant_id,created_by,template_id,agent_cluster_id,lease_seconds,lease,state,updated_at)
 SELECT id,created_by,?,?,?,?,'unprovisioned',? FROM ai_personal_assistants WHERE id=? AND created_by=?`,
		seed.TemplateID, seed.AgentClusterID, seed.LeaseSeconds, string(payload), time.Now().UTC(), assistant.ID, owner).Error; err != nil {
		t.Fatal(err)
	}
	initial, err := f.repo.GetPersonalEnvironment(f.ctx, owner, assistant.ID)
	if err != nil || initial.Backend != "container" || initial.Provider != "docker" || initial.PolicyRevision != "legacy-docker-v1" || initial.DataRef != "personal-data:"+assistant.ID || initial.ComputeRef != "" || initial.ComputeGeneration != 0 || initial.State != "unprovisioned" || initial.DataInitialized || initial.DataCreationAttempted || !reflect.DeepEqual(initial.Lease, lease) {
		t.Fatalf("legacy insert changed data identity or invented readiness: %+v %v", initial, err)
	}
	testPersonalBindingLegacyReservation(t, f, owner, assistant.ID, initial, payload)
	testPersonalBindingLegacyWake(t, f, owner, assistant.ID, initial)
}

func assertPersonalBindingMigration(t *testing.T, before, after domain.PersonalEnvironmentRecord, operation string) {
	t.Helper()
	if after.Backend != "container" || after.Provider != "docker" || after.PolicyRevision != "legacy-docker-v1" || after.State != "unknown" || after.OperationID != operation || after.Version != before.Version || !reflect.DeepEqual(after.Lease, before.Lease) || after.AgentClusterID != before.AgentClusterID || after.DataRef != before.DataRef || after.ComputeRef != before.Lease.ID || after.ComputeGeneration != 1 || after.DataInitialized != before.DataInitialized {
		t.Fatalf("migration changed the original binding or invented readiness: %+v", after)
	}
}

func testPersonalBindingLegacyReservation(t *testing.T, f *personalEnvironmentFixture, owner, assistantID string, initial domain.PersonalEnvironmentRecord, payload []byte) {
	t.Helper()
	operation := uuid.NewString()
	if err := f.db.Exec(`UPDATE ai_personal_environments SET lease=?,state='pending',version=version+1,
 data_creation_attempted=true,operation_id=?,operation_until=now()+interval '70 seconds',updated_at=now()
 WHERE assistant_id=? AND created_by=? AND version=?`, string(payload), operation, assistantID, owner, initial.Version).Error; err != nil {
		t.Fatal(err)
	}
	reserved, err := f.repo.GetPersonalEnvironment(f.ctx, owner, assistantID)
	if err != nil || reserved.ComputeGeneration != 1 || reserved.ComputeRef != initial.Lease.ID || reserved.DataRef != initial.DataRef || reserved.DataInitialized || reserved.OperationID != operation || !reflect.DeepEqual(reserved.Lease, initial.Lease) {
		t.Fatalf("legacy reservation lost the original lease: %+v %v", reserved, err)
	}
	if _, err := f.repo.ReservePersonalEnvironment(f.ctx, initial, initial.Lease, uuid.NewString(), false); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("new writer accepted a stale legacy reservation: %v", err)
	}
	// A proven pre-dispatch abort still permits first data creation to be retried.
	if err := f.db.Exec(`UPDATE ai_personal_environments SET lease=?,state='unprovisioned',version=version+1,
 data_creation_attempted=false,operation_id='',operation_until=NULL,updated_at=now()
 WHERE assistant_id=? AND created_by=? AND version=? AND operation_id=?`, string(payload), assistantID, owner, reserved.Version, operation).Error; err != nil {
		t.Fatal(err)
	}
	aborted, err := f.repo.GetPersonalEnvironment(f.ctx, owner, assistantID)
	if err != nil || aborted.ComputeGeneration != 0 || aborted.ComputeRef != "" || aborted.DataCreationAttempted || aborted.DataRef != initial.DataRef {
		t.Fatalf("legacy abort consumed first acquisition: %+v %v", aborted, err)
	}
}

func testPersonalBindingLegacyWake(t *testing.T, f *personalEnvironmentFixture, owner, assistantID string, initial domain.PersonalEnvironmentRecord) {
	t.Helper()
	f.wake(t, owner)
	current, err := f.repo.GetPersonalEnvironment(f.ctx, owner, assistantID)
	if err != nil || current.ComputeGeneration != 1 || current.ComputeRef != current.Lease.ID || current.DataRef != initial.DataRef {
		t.Fatalf("new writer cannot continue the legacy binding: %+v %v", current, err)
	}
	next := current.Lease
	next.ID, next.RunID = uuid.NewString(), uuid.NewString()
	payload, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	// A rolled-back writer wakes the same data on a new compute lease.
	if err := f.db.Exec(`UPDATE ai_personal_environments SET lease=?,state='unknown',version=version+1,
 operation_id=?,operation_until=now()+interval '70 seconds',updated_at=now()
 WHERE assistant_id=? AND created_by=? AND version=?`, string(payload), uuid.NewString(), assistantID, owner, current.Version).Error; err != nil {
		t.Fatal(err)
	}
	after, err := f.repo.GetPersonalEnvironment(f.ctx, owner, assistantID)
	if err != nil || after.ComputeGeneration != current.ComputeGeneration+1 || after.ComputeRef != next.ID || after.DataRef != initial.DataRef || after.State != "unknown" || !after.DataInitialized || !reflect.DeepEqual(after.Lease, next) {
		t.Fatalf("legacy wake changed persistent data or hid an unknown outcome: %+v %v", after, err)
	}
	if _, err := f.repo.CompletePersonalEnvironment(f.ctx, current); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("late new-writer receipt crossed a legacy wake: %v", err)
	}
}
