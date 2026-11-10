package networkruntime_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	dlprepo "github.com/opensoha/soha/internal/repository/contentdlp"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
	"gorm.io/gorm"
)

func (s *runtimeRepositoryScenario) checkDLPHoldSources(t *testing.T) {
	t.Helper()
	rollback := errors.New("rollback DLP hold sources")
	db := s.store.DB()
	approvals, err := dlprepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	err = dbtx.Within(s.ctx, db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, db)
		now := time.Now().UTC()
		hold, first, second := "dlp-hold-"+s.suffix, "dlp-event-a-"+s.suffix, "dlp-event-b-"+s.suffix
		project, host := "dlp-project-"+s.suffix, "dlp-host-"+s.suffix
		if err := tx.Exec(`INSERT INTO docker_hosts(id,name) VALUES(?,?)`, host, host).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO docker_projects(id,host_id,name,slug) VALUES(?,?,?,?)`, project, host, project, project).Error; err != nil {
			return err
		}
		for _, id := range []string{first, second} {
			if err := tx.Exec(`INSERT INTO docker_volume_dlp_events(id,project_id,actor_id,action,metadata,created_at) VALUES(?,?,?,'transfer',jsonb_build_object('networkHoldId',?::text),?)`, id, project, s.subjectID, hold, now).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO network_security_holds(id,source_event_id,source_type,project_id,subject_id,active,revision,created_at) VALUES(?,?,'content_dlp',?,?,true,1,?)`, hold, first, project, s.subjectID, now).Error; err != nil {
			return err
		}
		approve := func(id, status string) error {
			return tx.Exec(`INSERT INTO docker_volume_dlp_appeals(event_id,project_id,actor_id,reason_code,status,revision,reviewer_id,review_reason,created_at,updated_at) VALUES(?,?,?,'false_positive',?,1,'independent-reviewer','confirmed_false_positive',?,?) ON CONFLICT(event_id) DO UPDATE SET status=EXCLUDED.status`, id, project, s.subjectID, status, now, now).Error
		}
		assertActive := func(want bool, wantRevision int) error {
			if err := s.repository.ReleaseContentSubject(ctx, hold, "independent-reviewer", approvals); err != nil {
				return err
			}
			var got bool
			var revision int
			if err := tx.Raw(`SELECT active,revision FROM network_security_holds WHERE id=?`, hold).Row().Scan(&got, &revision); err != nil {
				return err
			}
			if got != want || revision != wantRevision {
				return fmt.Errorf("hold active=%v revision=%d, want active=%v revision=%d", got, revision, want, wantRevision)
			}
			return nil
		}
		deletion := tx.Transaction(func(attempt *gorm.DB) error { return dockerrepo.New(attempt).DeleteProject(s.ctx, project) })
		if !errors.Is(deletion, apperrors.ErrConflict) {
			return fmt.Errorf("active hold did not block project deletion: %v", deletion)
		}
		if err := approve(first, "approved"); err != nil {
			return err
		}
		if err := assertActive(true, 1); err != nil {
			return err
		} // Later finding has no appeal yet.
		if err := approve(second, "rejected"); err != nil {
			return err
		}
		if err := assertActive(true, 1); err != nil {
			return err
		}
		if err := approve(second, "approved"); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE docker_volume_dlp_events SET metadata=metadata-'networkHoldId' WHERE id=?`, first).Error; err != nil {
			return err
		}
		if err := assertActive(true, 1); err != nil {
			return fmt.Errorf("missing original event linkage: %w", err)
		}
		if err := approvals.AttachHold(ctx, first, hold); err != nil {
			return err
		}
		if err := assertActive(false, 2); err != nil {
			return err
		}
		if err := assertActive(false, 2); err != nil {
			return err
		}
		if err := dockerrepo.New(tx).DeleteProject(s.ctx, project); err != nil {
			return fmt.Errorf("released hold blocked deletion: %w", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}
