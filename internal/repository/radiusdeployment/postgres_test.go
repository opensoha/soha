package radiusdeployment

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/radiusdeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	pgtest "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in only: an exclusively assigned disposable database, real migrations
// through 0151 already applied by the environment owner. No implicit migrations,
// reset, service, cluster access or production credentials. All synthetic parent
// rows roll back; the concurrency case removes only its unique test UID claim.
func TestRadiusDeploymentPostgres(t *testing.T) {
	text := os.Getenv("SOHA_NETWORK_RUNTIME_TEST_POSTGRES_PORT")
	if text == "" {
		t.Skip("assign disposable migrated PostgreSQL via SOHA_NETWORK_RUNTIME_TEST_POSTGRES_PORT")
	}
	port, e := strconv.Atoi(text)
	if e != nil {
		t.Fatal(e)
	}
	name := os.Getenv("SOHA_NETWORK_RUNTIME_TEST_POSTGRES_DATABASE")
	if name == "" {
		name = "soha"
	}
	store, e := dbstore.New(pgtest.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: name, User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute}), zap.NewNop())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db := store.DB().WithContext(ctx)
	t.Run("same-owner-parent-idempotency", func(t *testing.T) {
		rollbackTest(t, db, func(tx *gorm.DB) error {
			uid := "test-radius-" + uuid.NewString()
			id := uuid.NewString()
			if e := seedParent(tx, "radius", id, uid); e != nil {
				return e
			}
			return tx.Exec(`UPDATE network_radius_pools SET revision=revision+1 WHERE pool_id=?`, id).Error
		})
	})
	for _, other := range []string{"proxy", "outpost"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-radius-conflict-reverse-%t", other, reverse), func(t *testing.T) {
				rollbackTest(t, db, func(tx *gorm.DB) error {
					uid := "test-radius-" + uuid.NewString()
					first, second := other, "radius"
					if reverse {
						first, second = second, first
					}
					if e := seedParent(tx, first, uuid.NewString(), uid); e != nil {
						return e
					}
					e := tx.Transaction(func(attempt *gorm.DB) error { return seedParent(attempt, second, uuid.NewString(), uid) })
					if !errors.Is(conflict(e), apperrors.ErrConflict) {
						return fmt.Errorf("cross-owner trigger did not conflict: %w", e)
					}
					return nil
				})
			})
		}
	}
	t.Run("concurrent-uid-claim", func(t *testing.T) {
		assertConcurrentRadiusUIDClaim(t, db)
	})
	t.Run("bootstrap-idempotent-and-failure-rolls-back", func(t *testing.T) {
		assertRadiusBootstrapRollback(t, ctx, db)
	})
	t.Run("managed-proof-rechecks-current-transaction-time", func(t *testing.T) {
		rollbackTest(t, db, func(tx *gorm.DB) error { return managedProofTransaction(ctx, tx) })
	})
	t.Run("migration-backfill-dual-owner-fails", func(t *testing.T) {
		assertRadiusMigrationDualOwnerFails(t, db)
	})
}
func rollbackTest(t *testing.T, db *gorm.DB, f func(*gorm.DB) error) {
	t.Helper()
	rollback := errors.New("synthetic fixture rollback")
	e := db.Transaction(func(tx *gorm.DB) error {
		if e := f(tx); e != nil {
			return e
		}
		return rollback
	})
	if !errors.Is(e, rollback) {
		t.Fatal(e)
	}
}
func seedParent(tx *gorm.DB, kind, id, uid string) error {
	state := `{"workloadUid":"` + uid + `"}`
	switch kind {
	case "proxy":
		if e := tx.Exec(`INSERT INTO network_proxy_instances(id,name,engine) VALUES(?,?,'mihomo')`, id, id).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO network_proxy_deployments(proxy_id,slot_id,generation,state,owner_epoch,member_set_hash,members) VALUES(?,?,1,?::jsonb,1,'test','[]'::jsonb)`, id, id, state).Error
	case "outpost":
		if e := tx.Exec(`INSERT INTO identity_outposts(id,name) VALUES(?,?)`, id, id).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO identity_outpost_runtime_deployments(outpost_id,slot_id,generation,state) VALUES(?,?,1,?::jsonb)`, id, id, state).Error
	case "radius":
		site := "site-" + id
		binding := "binding-" + id
		if e := tx.Exec(`INSERT INTO network_access_sites(id,name,status) VALUES(?,?,'active')`, site, site).Error; e != nil {
			return e
		}
		if e := tx.Exec(`INSERT INTO network_access_nas_bindings(id,nas_id,runtime_id,site_id,name,status) VALUES(?,?,?,?,?,'active')`, binding, id, domain.PoolPrefix+id, site, id).Error; e != nil {
			return e
		}
		return tx.Exec(`INSERT INTO network_radius_pools(pool_id,site_id,nas_binding_id,logical_runtime_id,slot_id,workload_uid,revision,state) VALUES(?,?,?,?,?,?,1,'{}'::jsonb)`, id, site, binding, domain.PoolPrefix+id, id, uid).Error
	}
	return apperrors.ErrInvalidArgument
}

func managedProofTransaction(ctx context.Context, tx *gorm.DB) error {
	id := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	site := "site-" + id
	binding := "binding-" + id
	rid := domain.MemberPrefix + id
	if e := tx.Exec(`INSERT INTO network_access_sites(id,name,status) VALUES(?,?,'active')`, site, site).Error; e != nil {
		return e
	}
	member := domain.Member{MemberID: "member-" + id, RuntimeID: rid}
	slot := domain.Slot{ID: "slot-" + id, UID: "uid-" + id, SiteID: site, NASBindingID: binding, NASID: "nas-" + id, NASName: "Managed proof test", AccessMedium: "wired", Members: []domain.Member{member}}
	state := domain.State{Slot: slot, SlotHash: slot.Hash(), ControlRevision: 1, Pool: api.NetworkRADIUSPool{PoolID: id, SiteID: site, NASBindingID: binding, LogicalRuntimeID: domain.PoolPrefix + id, SlotID: slot.ID, ObjectUID: slot.UID, Revision: 1, OwnerEpoch: 1, ConfigurationRevision: 1, DesiredReplicas: 1, Enabled: true}}
	state.Pool.ConfigurationHash = state.ConfigurationHash()
	r := New(tx)
	if _, e := r.Commit(ctx, app.Mutation{State: state, Create: true, CreateApprovedBinding: true, RequestID: "bootstrap", Fingerprint: "proof-test"}); e != nil {
		return e
	}
	scope := runtime.ManagedNASMemberScope{PoolID: id, MemberID: member.MemberID, SlotHash: state.SlotHash}
	if e := ValidateEnrollment(tx, runtime.EnrollmentConsumption{RuntimeID: rid, RuntimeKind: "nas", ManagedNAS: &scope, Capabilities: []string{domain.Capability}}); e != nil {
		return e
	}
	enrollment := uuid.NewString()
	credentialID := uuid.NewString()
	fingerprint := "sha256:" + strings.ReplaceAll(id+id, "-", "")
	if e := tx.Exec(`INSERT INTO network_runtime_enrollments(id,challenge_id,challenge_hash,runtime_id,runtime_kind,device_id,subject_id,expires_at,created_by) VALUES(?,?,?,?,'nas',?,'test',?,'test')`, enrollment, uuid.NewString(), fingerprint, rid, rid, now.Add(time.Hour)).Error; e != nil {
		return e
	}
	if e := tx.Exec(`INSERT INTO network_runtime_credentials(id,enrollment_id,runtime_id,runtime_kind,device_id,subject_id,certificate_fingerprint,public_key_fingerprint,certificate_serial,generation,capabilities,not_before,expires_at) VALUES(?,?,?,'nas',?,'test',?,?,'test',1,'["radius-managed-runtime-v1"]'::jsonb,?,?)`, credentialID, enrollment, rid, rid, fingerprint, fingerprint, now.Add(-time.Hour), now.Add(time.Hour)).Error; e != nil {
		return e
	}
	identity := networkidentity.Identity{ID: rid, Kind: "nas", Scope: networkidentity.ScopeNetworkControl, CertificateFingerprint: fingerprint, Certificate: &x509.Certificate{NotAfter: now.Add(time.Hour)}}
	c, e := r.GrantControl(ctx, identity, member.MemberID, "epoch", state.SlotHash)
	if e != nil {
		return e
	}
	zero := 0
	a := api.NetworkRADIUSRuntimeACK{PoolID: id, MemberID: member.MemberID, RuntimeID: rid, CredentialID: credentialID, ProcessEpoch: "epoch", OwnerEpoch: 1, ControlRevision: 1, ConfigurationRevision: 1, ConfigurationHash: state.Pool.ConfigurationHash, StateCountKnown: true, ActiveStates: &zero, Ready: true, HooksReady: true, ConfigurationApplied: true, AcceptingNewAuthentications: true, ObservedAt: now}
	if e := r.RecordACK(ctx, identity, a, state.SlotHash); e != nil {
		return e
	}
	proof := networkprotocol.ManagedNASProof{PoolID: id, MemberID: member.MemberID, CredentialID: credentialID, ProcessEpoch: "epoch", OwnerEpoch: 1, ControlRevision: 1, ConfigurationRevision: 1, ConfigurationHash: state.Pool.ConfigurationHash}
	auth := runtime.NASAuthorization{RuntimeID: rid, NASID: slot.NASID, ManagedRuntimeProof: &proof, ManagedSlotHash: state.SlotHash, ManagedBindingID: binding, ManagedRuntimeFingerprint: fingerprint, CreatedAt: now.Add(-5 * time.Second)}
	if _, e := ValidateNASAuthorization(tx, auth); e != nil {
		return fmt.Errorf("current managed proof rejected: %w", e)
	}
	records, e := r.Members(ctx, id)
	if e != nil {
		return e
	}
	record := records[0]
	c.ControlLeaseUntil = now.Add(-time.Second)
	record.Control = &c
	if e := saveMember(tx, id, record, false); e != nil {
		return e
	}
	if _, e := ValidateNASAuthorization(tx, auth); !errors.Is(e, apperrors.ErrUnauthorized) {
		return fmt.Errorf("stale lease accepted using application timestamp: %v", e)
	}
	return nil
}

func assertConcurrentRadiusUIDClaim(t *testing.T, db *gorm.DB) {
	t.Helper()
	uid := "test-radius-" + uuid.NewString()
	t.Cleanup(func() { db.Exec(`DELETE FROM runtime_slot_owners WHERE workload_uid=?`, uid) })
	var won atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, kind := range []string{"radius", "proxy"} {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			<-start
			e := db.Transaction(func(tx *gorm.DB) error {
				return tx.Exec(`SELECT reserve_runtime_slot_uid(?,?,?)`, uid, kind, "test-owner").Error
			})
			if e == nil {
				won.Add(1)
			} else if !errors.Is(conflict(e), apperrors.ErrConflict) {
				t.Error(e)
			}
		}(kind)
	}
	close(start)
	wg.Wait()
	if won.Load() != 1 {
		t.Fatalf("concurrent winners=%d", won.Load())
	}
}

func assertRadiusBootstrapRollback(t *testing.T, ctx context.Context, db *gorm.DB) {
	t.Helper()
	rollbackTest(t, db, func(tx *gorm.DB) error {
		id := uuid.NewString()
		site := "site-" + id
		binding := "binding-" + id
		if e := tx.Exec(`INSERT INTO network_access_sites(id,name,status) VALUES(?,?,'active')`, site, site).Error; e != nil {
			return e
		}
		slot := domain.Slot{ExecutorID: "test-radius", ID: "slot-" + id, UID: "uid-" + id, SiteID: site, NASBindingID: binding, NASID: "nas-" + id, NASName: "Test NAS", AccessMedium: "wired"}
		state := domain.State{Slot: slot, SlotHash: slot.Hash(), Pool: api.NetworkRADIUSPool{PoolID: id, SiteID: site, NASBindingID: binding, LogicalRuntimeID: domain.PoolPrefix + id, SlotID: slot.ID, ObjectUID: slot.UID, Revision: 1, OwnerEpoch: 1, ConfigurationRevision: 1, State: "stopped"}}
		m := app.Mutation{State: state, Create: true, CreateApprovedBinding: true, RequestID: "bootstrap", Fingerprint: "fingerprint"}
		r := New(tx)
		first, e := r.Commit(ctx, m)
		if e != nil {
			return e
		}
		m.State.Pool.PoolID = uuid.NewString()
		again, e := r.Commit(ctx, m)
		if e != nil || again.Pool.PoolID != first.Pool.PoolID {
			return fmt.Errorf("bootstrap replay lost identity: %w", e)
		}

		updated := first
		updated.Pool.Revision = 2
		updated.Pool.DesiredReplicas = 1
		updated.Pool.Enabled = true
		command := domain.Command{PoolID: first.Pool.PoolID, Slot: slot, SlotHash: first.SlotHash, Revision: 2, OwnerEpoch: 1, Action: "start", Replicas: 1, Deadline: time.Now().Add(time.Minute)}
		task := delivery.ExecutionTask{ID: uuid.NewString(), TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), TargetKind: "radius_slot", QueueKey: slot.ID, LockKey: "radius-slot/" + slot.ID, Status: "queued", TimeoutSeconds: 60, CallbackToken: uuid.NewString(), Payload: map[string]any{"radiusDeployment": command}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		updated.Pool.OperationID = task.ID
		if _, e := r.Commit(ctx, app.Mutation{State: updated, ExpectedRevision: 1, RequestID: "start", Fingerprint: "start", Task: &task}); e != nil {
			return fmt.Errorf("private Radius execution ownership constraint: %w", e)
		}
		if _, e := r.Commit(ctx, app.Mutation{State: updated, ExpectedRevision: 1, RequestID: "stale", Fingerprint: "stale"}); !errors.Is(e, apperrors.ErrConflict) {
			return fmt.Errorf("stale parent CAS was accepted: %v", e)
		}
		m.RequestID = "new-request"
		m.Fingerprint = "new-input"
		m.State.Pool.NASBindingID = "rollback-" + binding
		m.State.Slot.NASBindingID = m.State.Pool.NASBindingID
		m.State.Slot.NASID = "another-" + id
		m.State.SlotHash = m.State.Slot.Hash()
		// Existing UID owner fails only after an approved binding insertion; the
		// nested mutation must roll back that binding and leave no second pool.
		if _, e := r.Commit(ctx, m); !errors.Is(e, apperrors.ErrConflict) {
			return fmt.Errorf("UID claim expected conflict, got %v", e)
		}
		var count int64
		if e := tx.Raw(`SELECT count(*) FROM network_access_nas_bindings WHERE id=?`, m.State.Pool.NASBindingID).Scan(&count).Error; e != nil {
			return e
		}
		if count != 0 {
			return fmt.Errorf("failed bootstrap leaked binding")
		}
		return nil
	})
}

func assertRadiusMigrationDualOwnerFails(t *testing.T, db *gorm.DB) {
	t.Helper()
	raw, e := os.ReadFile("../../../migrations/postgres/0151_radius_deployment.sql")
	if e != nil {
		t.Fatal(e)
	}
	source := string(raw)
	start := strings.Index(source, "DO $$\nDECLARE row record;")
	end := strings.Index(source, "CREATE TRIGGER radius_parent_slot_owner")
	if start < 0 || end < start {
		t.Fatal("backfill source not found")
	}
	e = db.Transaction(func(tx *gorm.DB) error {
		suffix := uuid.NewString()
		a, b := uuid.NewString(), uuid.NewString()
		if e := seedParent(tx, "proxy", a, "first-"+suffix); e != nil {
			return e
		}
		if e := seedParent(tx, "outpost", b, "second-"+suffix); e != nil {
			return e
		}
		for _, q := range []string{`DROP TRIGGER proxy_parent_slot_owner ON network_proxy_deployments`, `DROP TRIGGER outpost_parent_slot_owner ON identity_outpost_runtime_deployments`} {
			if e := tx.Exec(q).Error; e != nil {
				return e
			}
		}
		encoded := `{"workloadUid":"duplicate-` + suffix + `"}`
		if e := tx.Exec(`UPDATE network_proxy_deployments SET state=?::jsonb WHERE proxy_id=?`, encoded, a).Error; e != nil {
			return e
		}
		if e := tx.Exec(`UPDATE identity_outpost_runtime_deployments SET state=?::jsonb WHERE outpost_id=?`, encoded, b).Error; e != nil {
			return e
		}
		return tx.Exec(source[start:end]).Error
	})
	var pg *pgconn.PgError
	if !errors.As(e, &pg) || pg.Code != "23505" || !strings.Contains(pg.Message, "runtime_slot_owner_conflict") {
		t.Fatalf("duplicate migration backfill=%v", e)
	}
	var count int64
	if e := db.Raw(`SELECT count(*) FROM pg_trigger WHERE tgname IN ('proxy_parent_slot_owner','outpost_parent_slot_owner') AND NOT tgisinternal`).Scan(&count).Error; e != nil || count != 2 {
		t.Fatalf("rollback did not restore parent triggers: %d %v", count, e)
	}
}
