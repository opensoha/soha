package networkruntime_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
	runtimerepo "github.com/opensoha/soha/internal/repository/networkruntime"
	"gorm.io/gorm"
)

// Part of the existing isolated PostgreSQL scenario; execution remains deferred.
func (s *runtimeRepositoryScenario) checkDLPHoldSources(t *testing.T) {
	t.Helper()
	rollback := errors.New("rollback DLP hold sources")
	err := s.store.DB().WithContext(s.ctx).Transaction(func(tx *gorm.DB) error {
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
		repo := runtimerepo.New(tx)
		assertActive := func(want bool) error {
			if err := repo.ReleaseContentSubject(s.ctx, hold, "independent-reviewer"); err != nil {
				return err
			}
			var got bool
			if err := tx.Raw(`SELECT active FROM network_security_holds WHERE id=?`, hold).Scan(&got).Error; err != nil {
				return err
			}
			if got != want {
				return fmt.Errorf("hold active=%v, want %v", got, want)
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
		if err := assertActive(true); err != nil {
			return err
		} // Later finding has no appeal yet.
		if err := approve(second, "rejected"); err != nil {
			return err
		}
		if err := assertActive(true); err != nil {
			return err
		}
		if err := approve(second, "approved"); err != nil {
			return err
		}
		if err := assertActive(false); err != nil {
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
