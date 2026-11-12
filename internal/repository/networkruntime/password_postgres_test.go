package networkruntime

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	access "github.com/opensoha/soha/internal/application/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Run only against an exclusively assigned disposable DB with the repository's
// real migrations (including 0032 and 0150) already applied by its owner. This
// test does not create a service, reset a DB, or run migrations implicitly.
func TestPasswordVerifierPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_NETWORK_RUNTIME_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("assign disposable migrated PostgreSQL via SOHA_NETWORK_RUNTIME_TEST_POSTGRES_PORT")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	name := os.Getenv("SOHA_NETWORK_RUNTIME_TEST_POSTGRES_DATABASE")
	if name == "" {
		name = "soha"
	}
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: name, User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db := store.DB()
	r := New(db, access.VPNGovernancePolicy{})
	suffix := uuid.NewString()
	subject := uuid.NewString()
	site := "password-site-" + suffix
	runtimeID := "radius-" + suffix
	nasID := "nas-" + suffix
	source := "test-source-" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)
	version := now.Add(-time.Minute)
	for query, args := range map[string][]any{
		`INSERT INTO public.users(id,username,email,status) VALUES(?,?,?,'active')`: {subject, "pw-" + suffix, suffix + "@example.test"},
	} {
		if err := db.WithContext(ctx).Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanup := db.WithContext(context.Background())
		cleanup.Exec(`DELETE FROM network_password_stations WHERE subject_id=?`, subject)
		cleanup.Exec(`DELETE FROM network_access_devices WHERE owner_user_id=?`, subject)
		cleanup.Exec(`DELETE FROM network_password_receipts WHERE subject_id=?`, subject)
		cleanup.Exec(`DELETE FROM user_password_credentials WHERE user_id=?`, subject)
		cleanup.Exec(`DELETE FROM users WHERE id=?`, subject)
		cleanup.Exec(`DELETE FROM network_access_sites WHERE id=?`, site)
		for _, key := range []string{passwordBudgetHash("nas", runtimeID+":"+nasID), passwordBudgetHash("source", source), passwordBudgetHash("subject", subject)} {
			cleanup.Exec(`DELETE FROM network_password_budgets WHERE bucket_hash=?`, key)
		}
	})
	if err := db.WithContext(ctx).Exec(`INSERT INTO user_password_credentials(user_id,password_hash,password_updated_at) VALUES(?,'synthetic-test-only',?)`, subject, version).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Exec(`INSERT INTO network_access_sites(id,name,status) VALUES(?,?,'active')`, site, site).Error; err != nil {
		t.Fatal(err)
	}
	proof := func() domain.PasswordReceipt {
		digest := sha256.Sum256([]byte(uuid.NewString()))
		return domain.PasswordReceipt{TokenHash: fmt.Sprintf("%x", digest), RuntimeID: runtimeID, NASID: nasID, SiteID: site, StationID: "02:00:00:00:00:01", SubjectID: subject, PasswordUpdatedAt: version, CreatedAt: now, ExpiresAt: now.Add(30 * time.Second)}
	}
	t.Run("concurrent-subject-budget-and-lock", func(t *testing.T) {
		var admitted atomic.Int32
		var winner domain.PasswordAttempt
		var mu sync.Mutex
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				attempt := domain.PasswordAttempt{ID: uuid.NewString(), RuntimeID: runtimeID, NASID: nasID, Source: source, SubjectID: subject, Now: now}
				ok, err := r.BeginPasswordAttempt(ctx, attempt)
				if err != nil {
					t.Error(err)
				}
				if ok {
					admitted.Add(1)
					mu.Lock()
					winner = attempt
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if admitted.Load() != 1 {
			t.Fatalf("admitted %d concurrent attempts", admitted.Load())
		}
		if err := r.FinishPasswordAttempt(ctx, winner, false, now); err != nil {
			t.Fatal(err)
		}
		for index := 1; index < 5; index++ {
			attempt := domain.PasswordAttempt{ID: uuid.NewString(), RuntimeID: runtimeID, NASID: nasID, Source: source, SubjectID: subject, Now: now.Add(time.Duration(index) * time.Second)}
			ok, err := r.BeginPasswordAttempt(ctx, attempt)
			if err != nil || !ok {
				t.Fatalf("attempt %d: %v %v", index, ok, err)
			}
			if err := r.FinishPasswordAttempt(ctx, attempt, false, attempt.Now); err != nil {
				t.Fatal(err)
			}
		}
		ok, err := r.BeginPasswordAttempt(ctx, domain.PasswordAttempt{ID: uuid.NewString(), RuntimeID: runtimeID, NASID: nasID, Source: source, SubjectID: subject, Now: now.Add(6 * time.Second)})
		if err != nil || ok {
			t.Fatalf("fifth-failure lock: %v %v", ok, err)
		}
	})
	t.Run("receipt-one-cross-request-consumer", func(t *testing.T) {
		p := proof()
		if err := r.SavePasswordReceipt(ctx, p); err != nil {
			t.Fatal(err)
		}
		var successes atomic.Int32
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return consumePasswordReceipt(tx, p, uuid.NewString(), now) })
				if err == nil {
					successes.Add(1)
				} else if !errors.Is(err, apperrors.ErrUnauthorized) {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if successes.Load() != 1 {
			t.Fatalf("proof consumed %d times", successes.Load())
		}
	})
	t.Run("password-change-and-disable-reject-consumption", func(t *testing.T) {
		for _, reason := range []string{"password-change", "disabled"} {
			p := proof()
			if err := r.SavePasswordReceipt(ctx, p); err != nil {
				t.Fatal(err)
			}
			if reason == "password-change" {
				err = db.Exec(`UPDATE user_password_credentials SET password_updated_at=? WHERE user_id=?`, version.Add(time.Second), subject).Error
			} else {
				err = db.Exec(`UPDATE users SET status='disabled' WHERE id=?`, subject).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return consumePasswordReceipt(tx, p, uuid.NewString(), now) })
			if !errors.Is(err, apperrors.ErrUnauthorized) {
				t.Fatalf("changed identity consumption: %v", err)
			}
			if err := db.Exec(`UPDATE user_password_credentials SET password_updated_at=? WHERE user_id=?`, version, subject).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`UPDATE users SET status='active' WHERE id=?`, subject).Error; err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("rejected-authorization-rolls-back-station-and-proof", func(t *testing.T) {
		p := proof()
		if err := r.SavePasswordReceipt(ctx, p); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(p.RuntimeID + "\x00" + p.NASID + "\x00" + p.StationID + "\x00" + p.SubjectID))
		deviceID := fmt.Sprintf("nas-station:%x", digest)
		var policyVersion int
		if err := db.Raw(`SELECT COALESCE(MAX(policy_version),0) FROM network_access_policy_snapshots`).Scan(&policyVersion).Error; err != nil {
			t.Fatal(err)
		}
		_, err := r.SaveNASAuthorization(ctx, domain.NASAuthorization{PasswordProof: &p, RuntimeID: runtimeID, NASID: nasID, RequestID: "rejected-" + suffix, SubjectID: subject, DeviceID: deviceID, StationID: p.StationID, PolicyVersion: policyVersion, CreatedAt: now})
		if !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("expected missing NAS binding rejection, got %v", err)
		}
		var count int64
		if err := db.Table("network_access_devices").Where("id=?", deviceID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("rejected station survived: %d %v", count, err)
		}
		saved, err := r.PasswordReceipt(ctx, p.TokenHash)
		if err != nil || saved.AuthorizationRequestID != "" {
			t.Fatalf("rejected proof consumed: %+v %v", saved, err)
		}
	})
}
