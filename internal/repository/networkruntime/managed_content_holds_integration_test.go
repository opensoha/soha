package networkruntime_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/platform/dbtx"
	transferrepo "github.com/opensoha/soha/internal/repository/contenttransfer"
)

func (s *runtimeRepositoryScenario) checkManagedDLPHoldSources(t *testing.T) {
	t.Helper()
	rollback := errors.New("rollback managed hold sources")
	db := s.store.DB()
	approvals, err := transferrepo.New(db)
	if err != nil {
		t.Fatal(err)
	}
	err = dbtx.Within(s.ctx, db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, db)
		now := time.Now().UTC()
		dest := "managed-dest-" + s.suffix
		hold := "managed-hold-" + s.suffix
		first := "managed-first-" + s.suffix
		second := "managed-second-" + s.suffix
		if err := tx.Exec(`INSERT INTO content_transfer_destinations(id,owner_id,revision,draft,published_version,updated_at) VALUES(?,?,1,'{}',0,?)`, dest, s.subjectID, now).Error; err != nil {
			return err
		}
		for _, id := range []string{first, second} {
			if err := tx.Exec(`INSERT INTO content_transfer_records(id,actor_id,destination_id,action,state,revision,metadata,private_metadata,created_at,updated_at) VALUES(?,?,?,'upload','blocked',1,jsonb_build_object('id',?::text,'actorId',?::text,'destinationId',?::text,'action','upload','networkHoldId',?::text,'appealStatus','pending'),'{}',?,?)`, id, s.subjectID, dest, id, s.subjectID, dest, hold, now, now).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO network_security_holds(id,source_transfer_id,source_type,destination_id,subject_id,active,revision,created_at) VALUES(?,?,'managed_content',?,?,true,1,?)`, hold, first, dest, s.subjectID, now).Error; err != nil {
			return err
		}
		release := func() error { return s.repository.ReleaseManagedContentSubject(ctx, hold, "reviewer", approvals) }
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
		if err := s.repository.ReleaseContentSubject(ctx, hold, "reviewer", approvals); err != nil {
			return err
		}
		if err := active(true); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE content_transfer_records SET metadata=metadata||'{"appealStatus":"approved","reviewerId":"reviewer"}'::jsonb WHERE id=?`, first).Error; err != nil {
			return err
		}
		if err := release(); err != nil {
			return err
		}
		if err := active(true); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE content_transfer_records SET metadata=jsonb_set(metadata,'{appealStatus}','"rejected"') WHERE id=?`, second).Error; err != nil {
			return err
		}
		if err := release(); err != nil {
			return err
		}
		if err := active(true); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE content_transfer_records SET metadata=jsonb_set(metadata,'{appealStatus}','"approved"') WHERE id=?`, second).Error; err != nil {
			return err
		}
		if err := release(); err != nil {
			return err
		}
		if err := active(true); err != nil {
			return fmt.Errorf("approval without reviewer: %w", err)
		}
		err := s.repository.WithinContentGovernance(ctx, func(review context.Context) error {
			return approvals.WithCurrent(review, []domain.Gate{{DestinationID: dest, PolicyVersion: 0}}, func(locked context.Context) error {
				record, err := approvals.Record(locked, second)
				if err != nil {
					return err
				}
				record.ReviewerID = "reviewer"
				if err := approvals.Update(locked, record, record.Revision, "reviewer", "appeal_reviewed"); err != nil {
					return err
				}
				return s.repository.ReleaseManagedContentSubject(locked, hold, "reviewer", approvals)
			})
		})
		if err != nil {
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
