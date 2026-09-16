package virtualization

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

type workerPoolRow struct {
	ID        uuid.UUID
	Revision  int
	Spec      []byte
	Identity  []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (row workerPoolRow) domain() (domain.WorkerPool, error) {
	pool := domain.WorkerPool{VirtualizationWorkerPool: sohaapi.VirtualizationWorkerPool{ID: row.ID, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}}
	if err := json.Unmarshal(row.Spec, &pool.Spec); err != nil {
		return pool, err
	}
	return pool, json.Unmarshal(row.Identity, &pool.Identity)
}

func (r *Repository) GetWorkerPool(ctx context.Context, id string) (domain.WorkerPool, error) {
	var row workerPoolRow
	result := dbtx.DB(ctx, r.db).Raw(`SELECT id,revision,spec,identity,created_at,updated_at FROM virtualization_worker_pools WHERE id=?`, id).Scan(&row)
	if result.Error != nil {
		return domain.WorkerPool{}, result.Error
	}
	if result.RowsAffected != 1 {
		return domain.WorkerPool{}, apperrors.ErrNotFound
	}
	return row.domain()
}

func (r *Repository) ListWorkerPools(ctx context.Context, connectionID string) ([]domain.WorkerPool, error) {
	var rows []workerPoolRow
	if err := dbtx.DB(ctx, r.db).Raw(`SELECT id,revision,spec,identity,created_at,updated_at FROM virtualization_worker_pools WHERE connection_id=? ORDER BY created_at,id`, connectionID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]domain.WorkerPool, 0, len(rows))
	for _, row := range rows {
		item, err := row.domain()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) SaveWorkerPool(ctx context.Context, pool domain.WorkerPool, expectedRevision int) (domain.WorkerPool, error) {
	if pool.ID == uuid.Nil || expectedRevision < 0 {
		return domain.WorkerPool{}, apperrors.ErrInvalidArgument
	}
	if err := domain.ValidateWorkerPoolSpec(pool.Spec); err != nil {
		return domain.WorkerPool{}, err
	}
	spec, err := json.Marshal(pool.Spec)
	if err != nil {
		return domain.WorkerPool{}, err
	}
	identity, err := json.Marshal(pool.Identity)
	if err != nil {
		return domain.WorkerPool{}, err
	}
	var saved domain.WorkerPool
	err = dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		if err := db.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "worker-pool/"+pool.ID.String()).Error; err != nil {
			return err
		}
		if expectedRevision > 0 {
			current, err := r.GetWorkerPool(ctx, pool.ID.String())
			if err != nil {
				return err
			}
			if current.Revision != expectedRevision || current.Spec.ConnectionID != pool.Spec.ConnectionID || current.Spec.ClusterID != pool.Spec.ClusterID {
				return fmt.Errorf("%w: worker pool changed; cluster and provider targets are immutable", apperrors.ErrConflict)
			}
			count, err := r.workerPoolAllocationCount(ctx, pool.ID.String(), "")
			if err != nil {
				return err
			}
			if count > int64(pool.Spec.MaxNodes) {
				return fmt.Errorf("%w: worker pool limit is below retained allocations", apperrors.ErrConflict)
			}
		}
		result := db.Exec(`INSERT INTO virtualization_worker_pools(id,connection_id,cluster_id,revision,spec,identity) SELECT ?,?,?,1,?::jsonb,?::jsonb WHERE ?=0 ON CONFLICT(id) DO NOTHING`, pool.ID, pool.Spec.ConnectionID, pool.Spec.ClusterID, string(spec), string(identity), expectedRevision)
		if expectedRevision > 0 {
			result = db.Exec(`UPDATE virtualization_worker_pools SET revision=revision+1,spec=?::jsonb,identity=?::jsonb,updated_at=now() WHERE id=? AND revision=?`, string(spec), string(identity), pool.ID, expectedRevision)
		}
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: worker pool revision changed", apperrors.ErrConflict)
		}
		saved, err = r.GetWorkerPool(ctx, pool.ID.String())
		return err
	})
	return saved, err
}

func (r *Repository) DeleteWorkerPool(ctx context.Context, id string, revision int) error {
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		if err := db.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "worker-pool/"+id).Error; err != nil {
			return err
		}
		pool, err := r.GetWorkerPool(ctx, id)
		if err != nil {
			return err
		}
		if revision < 1 || pool.Revision != revision {
			return fmt.Errorf("%w: worker pool revision changed", apperrors.ErrConflict)
		}
		result := db.Exec(`DELETE FROM virtualization_worker_pools WHERE id=? AND revision=? AND NOT EXISTS(SELECT 1 FROM virtualization_tasks WHERE payload->>'workerPoolId'=?)`, id, revision, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: worker pool has operation history; disable it instead", apperrors.ErrConflict)
		}
		return nil
	})
}

// The pool budget and provider capacity reservation commit with the original VM
// task. This is configuration/admission, not another queue or scheduler.
func (r *Repository) WithWorkerPoolAdmission(ctx context.Context, poolID string, revision int, taskID string, apply func(context.Context) error) error {
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		if err := dbtx.DB(ctx, r.db).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "worker-pool/"+poolID).Error; err != nil {
			return err
		}
		pool, err := r.GetWorkerPool(ctx, poolID)
		if err != nil {
			return err
		}
		if !pool.Spec.Enabled || pool.Revision != revision {
			return fmt.Errorf("%w: worker pool is disabled or its revision changed", apperrors.ErrConflict)
		}
		count, err := r.workerPoolAllocationCount(ctx, poolID, taskID)
		if err != nil {
			return err
		}
		if count >= int64(pool.Spec.MaxNodes) {
			return fmt.Errorf("%w: worker pool node budget is exhausted", apperrors.ErrConflict)
		}
		return apply(ctx)
	})
}

func (r *Repository) workerPoolAllocationCount(ctx context.Context, poolID, exceptTaskID string) (int64, error) {
	var count int64
	// Unknown outcomes and orphaned inventory retain their slot. Only a verified
	// deleted VM or a terminal pre-dispatch request can release it.
	err := dbtx.DB(ctx, r.db).Raw(`SELECT count(*) FROM virtualization_tasks t LEFT JOIN virtualization_vms v ON v.id=t.vm_id WHERE t.payload->>'workerPoolId'=? AND t.id::text<>? AND NOT COALESCE(v.status='deleted',FALSE) AND NOT COALESCE((t.status IN ('failed','canceled','callback_timeout') AND t.result->>'providerEffect'='not_started' AND NOT COALESCE((t.payload->>'providerDispatchStarted')::boolean,FALSE)),FALSE)`, poolID, exceptTaskID).Scan(&count).Error
	return count, err
}
