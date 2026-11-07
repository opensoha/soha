package networkruntime_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	dlprepo "github.com/opensoha/soha/internal/repository/contentdlp"
	runtimerepo "github.com/opensoha/soha/internal/repository/networkruntime"
	"gorm.io/gorm"
)

type holdApprovalReaderFunc func(ctx context.Context, holdID, sourceID, scopeID, subjectID string) (bool, error)

func (f holdApprovalReaderFunc) ContentHoldReleasable(ctx context.Context, holdID, sourceID, scopeID, subjectID string) (bool, error) {
	return f(ctx, holdID, sourceID, scopeID, subjectID)
}

type releaseDLPFixture struct {
	project, hold, first, second string
	updatedAt                    time.Time
}

func (s *runtimeRepositoryScenario) seedReleaseDLPFixture(t *testing.T, tx *gorm.DB, firstStatus string) releaseDLPFixture {
	t.Helper()
	f := releaseDLPFixture{project: "release-project-" + s.suffix, hold: "release-hold-" + s.suffix, first: "release-first-" + s.suffix, second: "release-second-" + s.suffix}
	host := "release-host-" + s.suffix
	execReleaseFixture(t, tx, `INSERT INTO docker_hosts(id,name) VALUES(?,?)`, host, host)
	execReleaseFixture(t, tx, `INSERT INTO docker_projects(id,host_id,name,slug) VALUES(?,?,?,?)`, f.project, host, f.project, f.project)
	if err := tx.Raw(`SELECT updated_at FROM docker_projects WHERE id=?`, f.project).Row().Scan(&f.updatedAt); err != nil {
		t.Fatal(err)
	}
	for id, status := range map[string]string{f.first: firstStatus, f.second: "approved"} {
		execReleaseFixture(t, tx, `INSERT INTO docker_volume_dlp_events(id,project_id,actor_id,action,metadata,created_at)
 VALUES(?,?,?,'transfer',jsonb_build_object('id',?::text,'projectId',?::text,'actorId',?::text,'action','transfer','networkHoldId',?::text),CURRENT_TIMESTAMP)`, id, f.project, s.subjectID, id, f.project, s.subjectID, f.hold)
		execReleaseFixture(t, tx, `INSERT INTO docker_volume_dlp_appeals(event_id,project_id,actor_id,reason_code,status,revision,reviewer_id,review_reason,created_at,updated_at)
 VALUES(?,?,?,'false_positive',?,1,CASE WHEN ?='pending' THEN NULL ELSE 'reviewer' END,'confirmed_false_positive',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, id, f.project, s.subjectID, status, status)
	}
	execReleaseFixture(t, tx, `INSERT INTO network_security_holds(id,source_event_id,source_type,project_id,subject_id,active,revision,created_at)
 VALUES(?,?,'content_dlp',?,?,true,1,CURRENT_TIMESTAMP)`, f.hold, f.first, f.project, s.subjectID)
	return f
}

func execReleaseFixture(t *testing.T, tx *gorm.DB, statement string, args ...any) {
	t.Helper()
	if err := tx.Exec(statement, args...).Error; err != nil {
		t.Fatal(err)
	}
}

func assertReleaseDLPState(t *testing.T, tx *gorm.DB, f releaseDLPFixture, wantStatus string, wantActive bool, wantRevision int) {
	t.Helper()
	var status string
	var active bool
	var revision int
	err := tx.Raw(`SELECT a.status,h.active,h.revision FROM docker_volume_dlp_appeals a CROSS JOIN network_security_holds h WHERE a.event_id=? AND h.id=?`, f.first, f.hold).Row().Scan(&status, &active, &revision)
	if err != nil || status != wantStatus || active != wantActive || revision != wantRevision {
		t.Fatalf("approval=%q hold active=%v revision=%d err=%v, want %q/%v/%d", status, active, revision, err, wantStatus, wantActive, wantRevision)
	}
}

func (s *runtimeRepositoryScenario) checkContentReleaseAtomicity(t *testing.T) {
	t.Helper()
	db := s.store.DB()
	approvals, err := dlprepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback content release fixture")
	readFailure := errors.New("approval read failed")
	err = dbtx.Within(s.ctx, db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, db)
		f := s.seedReleaseDLPFixture(t, tx, "pending")
		session, lease := "old-content-session-"+s.suffix, "old-content-lease-"+s.suffix
		now := time.Now().UTC().Truncate(time.Microsecond)
		until := now.Add(time.Hour)
		execReleaseFixture(t, tx, `INSERT INTO network_runtime_sessions(id,runtime_id,subject_id,device_id,mode,access_profile,status,policy_version,valid_until)
 VALUES(?,?,?,?,'external_direct_ztna','restricted','revoked',?,?)`, session, s.runtimeID, s.subjectID, s.deviceID, s.policyVersion, until)
		execReleaseFixture(t, tx, `INSERT INTO network_runtime_leases(id,session_id,lease_kind,subject_id,device_id,resource_ids,policy_version,status,issued_at,expires_at)
 VALUES(?,?,'resource',?,?,jsonb_build_array(?::text),?,'revoked',?,?)`, lease, session, s.subjectID, s.deviceID, s.protectedID, s.policyVersion, now, until)
		execReleaseFixture(t, tx, `INSERT INTO network_runtime_configurations(runtime_id,configuration_version,policy_version,desired_payload,desired_hash,valid_until)
 VALUES(?,1,?,'{}',?,?)`, s.runtimeID, s.policyVersion, digest(s.runtimeID), until)
		execReleaseFixture(t, tx, `INSERT INTO network_security_hold_targets(hold_id,kind,runtime_id,base_version,state) VALUES(?,'configuration',?,1,'pending')`, f.hold, s.runtimeID)
		review := func(approvalReader runtimerepo.ContentHoldApprovalReader) error {
			return s.repository.WithinContentGovernance(ctx, func(reviewCtx context.Context) error {
				record, err := approvals.Event(reviewCtx, f.project, f.first)
				if err != nil {
					return err
				}
				if _, err := approvals.ResolveAppeal(reviewCtx, record, s.subjectID, dlp.ReviewInput{ExpectedRevision: 1, Resolution: "approved", ReasonCode: "confirmed_false_positive"}, f.updatedAt, now); !errors.Is(err, apperrors.ErrAccessDenied) {
					return fmt.Errorf("self review: got %v", err)
				}
				if _, err := approvals.ResolveAppeal(reviewCtx, record, "reviewer", dlp.ReviewInput{ExpectedRevision: 1, Resolution: "approved", ReasonCode: "confirmed_false_positive"}, f.updatedAt, now); err != nil {
					return err
				}
				return s.repository.ReleaseContentSubject(reviewCtx, f.hold, "reviewer", approvalReader)
			})
		}
		if err := review(holdApprovalReaderFunc(func(context.Context, string, string, string, string) (bool, error) {
			return false, readFailure
		})); !errors.Is(err, readFailure) {
			t.Fatalf("approval reader failure: got %v", err)
		}
		assertReleaseDLPState(t, tx, f, "pending", true, 1)

		failEffect := holdApprovalReaderFunc(func(ctx context.Context, hold, source, project, subject string) (bool, error) {
			allowed, err := approvals.ContentHoldReleasable(ctx, hold, source, project, subject)
			if err != nil || !allowed {
				return allowed, fmt.Errorf("same-transaction review was not visible: %v", err)
			}
			// A real PostgreSQL rejection during the configuration update tests
			// rollback after the preceding hold update has executed.
			err = dbtx.DB(ctx, db).Exec(`ALTER TABLE network_runtime_configurations ADD CONSTRAINT test_content_release_effect
 CHECK (valid_until>'2100-01-01T00:00:00Z'::timestamptz) NOT VALID`).Error
			return allowed, err
		})
		var pgerr *pgconn.PgError
		if err := review(failEffect); !errors.As(err, &pgerr) || pgerr.ConstraintName != "test_content_release_effect" {
			t.Fatalf("release configuration write failure: got %v", err)
		}
		assertReleaseDLPState(t, tx, f, "pending", true, 1)
		assertReleaseConfiguration(t, tx, s.runtimeID, now, false)

		if err := review(approvals); err != nil {
			return err
		}
		assertReleaseDLPState(t, tx, f, "approved", false, 2)
		assertReleaseConfiguration(t, tx, s.runtimeID, time.Now().UTC(), true)
		var leaseStatus string
		if err := tx.Raw(`SELECT status FROM network_runtime_leases WHERE id=?`, lease).Row().Scan(&leaseStatus); err != nil || leaseStatus != "revoked" {
			t.Fatalf("old lease status=%q err=%v", leaseStatus, err)
		}
		repo := runtimerepo.New(tx, appnetworkaccess.VPNGovernancePolicy{})
		_, err := repo.RenewLeases(ctx, s.runtimeID, networkprotocol.LeaseRenewRequest{SessionID: session}, runtime.PolicySnapshot{PolicyVersion: s.policyVersion}, now, time.Minute)
		if !errors.Is(err, apperrors.ErrGone) {
			t.Fatalf("old revoked session became renewable after release: %v", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}

func assertReleaseConfiguration(t *testing.T, tx *gorm.DB, runtimeID string, at time.Time, wantExpired bool) {
	t.Helper()
	var expired bool
	err := tx.Raw(`SELECT valid_until<=? FROM network_runtime_configurations WHERE runtime_id=? AND configuration_version=1`, at, runtimeID).Row().Scan(&expired)
	if err != nil || expired != wantExpired {
		t.Fatalf("configuration expired=%v err=%v, want %v", expired, err, wantExpired)
	}
}

func (s *runtimeRepositoryScenario) checkContentReleaseConcurrentApproval(t *testing.T) {
	t.Helper()
	db := s.store.DB()
	approvals, err := dlprepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	var f releaseDLPFixture
	if err := db.WithContext(s.ctx).Transaction(func(tx *gorm.DB) error {
		f = s.seedReleaseDLPFixture(t, tx, "approved")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	writer := db.WithContext(ctx).Begin()
	if writer.Error != nil {
		t.Fatal(writer.Error)
	}
	defer writer.Rollback()
	var writerPID int
	if err := writer.Raw(`SELECT pg_backend_pid()`).Row().Scan(&writerPID); err != nil {
		t.Fatal(err)
	}
	execReleaseFixture(t, writer, `UPDATE docker_volume_dlp_appeals SET status='rejected' WHERE event_id=?`, f.second)

	waiterPID := make(chan int, 1)
	done := make(chan error, 1)
	reader := holdApprovalReaderFunc(func(ctx context.Context, hold, source, project, subject string) (bool, error) {
		var pid int
		if err := dbtx.DB(ctx, db).Raw(`SELECT pg_backend_pid()`).Row().Scan(&pid); err != nil {
			return false, err
		}
		waiterPID <- pid
		return approvals.ContentHoldReleasable(ctx, hold, source, project, subject)
	})
	go func() { done <- s.repository.ReleaseContentSubject(ctx, f.hold, "reviewer", reader) }()
	var pid int
	select {
	case pid = <-waiterPID:
	case err := <-done:
		t.Fatalf("release finished before reading the owner: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := waitForApprovalRowLock(ctx, db, writerPID, pid); err != nil {
		t.Fatal(err)
	}
	if err := writer.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertReleaseDLPState(t, db.WithContext(ctx), f, "approved", true, 1)
}

func waitForApprovalRowLock(ctx context.Context, db *gorm.DB, writerPID, readerPID int) error {
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var blocked bool
		if err := db.WithContext(ctx).Raw(`SELECT ?=ANY(pg_blocking_pids(?))`, writerPID, readerPID).Row().Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
