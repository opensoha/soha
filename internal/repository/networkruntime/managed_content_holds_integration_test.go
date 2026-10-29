package networkruntime_test

import (
	"errors"
	"fmt"
	runtime "github.com/opensoha/soha/internal/repository/networkruntime"
	"gorm.io/gorm"
	"testing"
	"time"
)

// Uses only the existing disposable PostgreSQL fixture. Execution is deferred.
func (s *runtimeRepositoryScenario) checkManagedDLPHoldSources(t *testing.T) {
	t.Helper()
	rollback := errors.New("rollback managed hold sources")
	err := s.store.DB().WithContext(s.ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		dest := "managed-dest-" + s.suffix
		hold := "managed-hold-" + s.suffix
		first := "managed-first-" + s.suffix
		second := "managed-second-" + s.suffix
		if err := tx.Exec(`INSERT INTO content_transfer_destinations(id,owner_id,revision,draft,published_version,updated_at) VALUES(?,?,1,'{}',0,?)`, dest, s.subjectID, now).Error; err != nil {
			return err
		}
		for _, id := range []string{first, second} {
			if err := tx.Exec(`INSERT INTO content_transfer_records(id,actor_id,destination_id,action,state,revision,metadata,private_metadata,created_at,updated_at) VALUES(?,?,?,'upload','blocked',1,jsonb_build_object('networkHoldId',?::text,'appealStatus','pending'),'{}',?,?)`, id, s.subjectID, dest, hold, now, now).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO network_security_holds(id,source_transfer_id,source_type,destination_id,subject_id,active,revision,created_at) VALUES(?,?,'managed_content',?,?,true,1,?)`, hold, first, dest, s.subjectID, now).Error; err != nil {
			return err
		}
		repo := runtime.New(tx)
		active := func(want bool) error {
			var got bool
			if err := tx.Raw(`SELECT active FROM network_security_holds WHERE id=?`, hold).Scan(&got).Error; err != nil {
				return err
			}
			if got != want {
				return fmt.Errorf("managed active=%v want=%v", got, want)
			}
			return nil
		}
		if err := repo.ReleaseContentSubject(s.ctx, hold, "reviewer"); err != nil {
			return err
		}
		if err := active(true); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE content_transfer_records SET metadata=jsonb_set(metadata,'{appealStatus}','"approved"') WHERE id=?`, first).Error; err != nil {
			return err
		}
		if err := repo.ReleaseManagedContentSubject(s.ctx, hold, "reviewer"); err != nil {
			return err
		}
		if err := active(true); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE content_transfer_records SET metadata=jsonb_set(metadata,'{appealStatus}','"rejected"') WHERE id=?`, second).Error; err != nil {
			return err
		}
		if err := repo.ReleaseManagedContentSubject(s.ctx, hold, "reviewer"); err != nil {
			return err
		}
		if err := active(true); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE content_transfer_records SET metadata=jsonb_set(metadata,'{appealStatus}','"approved"') WHERE id=?`, second).Error; err != nil {
			return err
		}
		if err := repo.ReleaseManagedContentSubject(s.ctx, hold, "reviewer"); err != nil {
			return err
		}
		if err := active(false); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}
