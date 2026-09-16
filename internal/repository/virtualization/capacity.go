package virtualization

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

type pendingCapacity struct {
	Namespace  string
	NodeName   string
	StorageKey string
	CPU        int64
	MemoryMiB  int64 `gorm:"column:memory_mib"`
	DiskGiB    int64 `gorm:"column:disk_gib"`
}

const capacityReleaseCondition = `t.payload->>'providerDispatchStarted' IS DISTINCT FROM 'true' AND (t.status = 'canceled' AND t.attempt_count = 0 OR t.status IN ('failed','canceled','callback_timeout') AND t.result->>'providerEffect' = 'not_started')`
const capacityAccountCondition = `r.task_id IN ? AND t.status IN ('completed','canceled') AND t.result->>'providerEffect' = 'created' AND t.finished_at <= ?`

// SelectCapacity checks the same commitments as admission without changing the
// reservation ledger. The source lock gives its inventory watermark and pending
// reservations a consistent view while concurrent admissions are serialized.
func (r *Repository) SelectCapacity(ctx context.Context, snapshot domain.CapacitySnapshot, demand domain.CapacityDemand) (domain.CapacityNode, domain.CapacityStorage, error) {
	var node domain.CapacityNode
	var storage domain.CapacityStorage
	if !validCapacityDemand(demand) {
		return node, storage, apperrors.ErrInvalidArgument
	}
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "virtualization-capacity/"+snapshot.SourceID).Error; err != nil {
			return err
		}
		var newer bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM virtualization_capacity_observations WHERE source_id=? AND observed_at>?)`, snapshot.SourceID, snapshot.ObservedAt).Scan(&newer).Error; err != nil {
			return err
		}
		if !validCapacitySnapshot(snapshot) || newer {
			return domain.ErrCapacityUnknown
		}
		var pending []pendingCapacity
		if err := tx.Raw(`SELECT r.node_name,r.namespace,r.storage_key,r.cpu,r.memory_mib,r.disk_gib FROM virtualization_capacity_reservations r JOIN virtualization_tasks t ON t.id=r.task_id WHERE r.source_id=? AND r.state='reserved' AND NOT COALESCE((`+capacityReleaseCondition+`),FALSE) AND NOT COALESCE((`+capacityAccountCondition+`),FALSE)`, snapshot.SourceID, snapshot.ObservedOperations, snapshot.ObservedAt).Scan(&pending).Error; err != nil {
			return err
		}
		var err error
		node, storage, err = selectCapacity(snapshot, demand, pending)
		return err
	})
	return node, storage, err
}

func validCapacityDemand(demand domain.CapacityDemand) bool {
	return demand.CPU > 0 && demand.CPU <= 65536 && demand.MemoryMiB > 0 && demand.MemoryMiB <= 1<<40 && demand.DiskGiB > 0 && demand.DiskGiB <= 1<<40
}

func (r *Repository) CreateTaskWithCapacity(ctx context.Context, task domain.Task, snapshot domain.CapacitySnapshot, demand domain.CapacityDemand) (domain.Task, error) {
	return r.admitTaskCapacity(ctx, task, snapshot, demand, false)
}

func (r *Repository) RetryTaskWithCapacity(ctx context.Context, task domain.Task, snapshot domain.CapacitySnapshot, demand domain.CapacityDemand) (domain.Task, error) {
	if task.Status != "queued" || task.Payload["providerDispatchStarted"] == true {
		return domain.Task{}, apperrors.ErrConflict
	}
	return r.admitTaskCapacity(ctx, task, snapshot, demand, true)
}

func (r *Repository) admitTaskCapacity(ctx context.Context, task domain.Task, snapshot domain.CapacitySnapshot, demand domain.CapacityDemand, retry bool) (domain.Task, error) {
	if !validCapacitySnapshot(snapshot) {
		return domain.Task{}, fmt.Errorf("%w: capacity inventory is incomplete or stale", apperrors.ErrConflict)
	}
	if !validCapacityDemand(demand) {
		return domain.Task{}, apperrors.ErrInvalidArgument
	}
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	var created domain.Task
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		// ponytail: serialize admission per physical provider source; split locks by
		// independent pools only if measured admission throughput requires it.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "virtualization-capacity/"+snapshot.SourceID).Error; err != nil {
			return err
		}
		if !validCapacitySnapshot(snapshot) {
			return fmt.Errorf("%w: capacity inventory expired while waiting for admission", apperrors.ErrConflict)
		}
		if err := acceptCapacityObservation(tx, snapshot); err != nil {
			return err
		}
		if err := reconcileCapacityReservations(tx, snapshot); err != nil {
			return err
		}
		if retry {
			var count int64
			if err := tx.Raw(`SELECT count(*) FROM virtualization_capacity_reservations WHERE task_id=? AND source_id=? AND state IN ('reserved','released')`, task.ID, snapshot.SourceID).Scan(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return fmt.Errorf("%w: capacity reservation changed or has already been accounted", apperrors.ErrConflict)
			}
		}
		var pending []pendingCapacity
		if err := tx.Raw(`SELECT node_name, namespace, storage_key, cpu, memory_mib, disk_gib FROM virtualization_capacity_reservations WHERE source_id = ? AND state = 'reserved' AND task_id <> ?`, snapshot.SourceID, task.ID).Scan(&pending).Error; err != nil {
			return err
		}
		node, storage, err := selectCapacity(snapshot, demand, pending)
		if err != nil {
			return err
		}
		task.Payload = capacityTaskPayload(task, snapshot.Namespace, node.Name, storage.Name)
		if retry {
			created, err = (&Repository{db: tx}).updateTask(ctx, task)
		} else {
			created, err = (&Repository{db: tx}).CreateTask(ctx, task)
		}
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO virtualization_capacity_reservations(task_id,source_id,node_name,namespace,storage_key,cpu,memory_mib,disk_gib,observed_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(task_id) DO UPDATE SET node_name=EXCLUDED.node_name,namespace=EXCLUDED.namespace,storage_key=EXCLUDED.storage_key,cpu=EXCLUDED.cpu,memory_mib=EXCLUDED.memory_mib,disk_gib=EXCLUDED.disk_gib,observed_at=EXCLUDED.observed_at,state='reserved',updated_at=now()`, created.ID, snapshot.SourceID, node.Name, snapshot.Namespace, storage.Key, demand.CPU, demand.MemoryMiB, demand.DiskGiB, snapshot.ObservedAt).Error
	})
	return created, err
}

func (r *Repository) retryReservedTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	var updated domain.Task
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var source string
		if err := tx.Raw(`SELECT source_id FROM virtualization_capacity_reservations WHERE task_id=?`, task.ID).Scan(&source).Error; err != nil {
			return err
		}
		if source == "" {
			return fmt.Errorf("%w: retry requires capacity admission", apperrors.ErrConflict)
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "virtualization-capacity/"+source).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Raw(`SELECT count(*) FROM virtualization_capacity_reservations WHERE task_id=? AND state='reserved'`, task.ID).Scan(&count).Error; err != nil {
			return err
		}
		if count != 1 || task.Payload["providerDispatchStarted"] != true {
			return fmt.Errorf("%w: retry requires fresh capacity admission", apperrors.ErrConflict)
		}
		var err error
		updated, err = (&Repository{db: tx}).updateTask(ctx, task)
		return err
	})
	return updated, err
}

func acceptCapacityObservation(tx *gorm.DB, snapshot domain.CapacitySnapshot) error {
	update := tx.Exec(`INSERT INTO virtualization_capacity_observations(source_id,observed_at) VALUES(?,?) ON CONFLICT(source_id) DO UPDATE SET observed_at=EXCLUDED.observed_at WHERE virtualization_capacity_observations.observed_at<=EXCLUDED.observed_at`, snapshot.SourceID, snapshot.ObservedAt)
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("%w: a newer provider capacity observation has already been admitted", apperrors.ErrConflict)
	}
	return nil
}

func validCapacitySnapshot(snapshot domain.CapacitySnapshot) bool {
	return snapshot.Complete && snapshot.SourceID != "" && !snapshot.ObservedAt.Before(time.Now().Add(-30*time.Second)) && !snapshot.ObservedAt.After(time.Now().Add(5*time.Second))
}

func capacityTaskPayload(task domain.Task, namespace, node, storage string) map[string]any {
	payload := make(map[string]any, len(task.Payload)+3)
	for key, value := range task.Payload {
		payload[key] = value
	}
	params := map[string]any{}
	if existing, ok := payload["providerParams"].(map[string]any); ok {
		for key, value := range existing {
			params[key] = value
		}
	}
	if task.Provider == "kubevirt" {
		params["storageClass"] = storage
	} else {
		params["storage"] = storage
	}
	payload["providerParams"], payload["node"], payload["capacityReserved"] = params, node, true
	if namespace != "" {
		payload["namespace"] = namespace
	}
	return payload
}

func reconcileCapacityReservations(tx *gorm.DB, snapshot domain.CapacitySnapshot) error {
	if err := tx.Exec(`UPDATE virtualization_capacity_reservations r SET state = 'released', updated_at = now() FROM virtualization_tasks t WHERE r.task_id = t.id AND r.source_id = ? AND r.state = 'reserved' AND `+capacityReleaseCondition, snapshot.SourceID).Error; err != nil {
		return err
	}
	if len(snapshot.ObservedOperations) == 0 {
		return nil
	}
	return tx.Exec(`UPDATE virtualization_capacity_reservations r SET state = 'accounted', updated_at = now() FROM virtualization_tasks t WHERE r.task_id = t.id AND r.source_id = ? AND r.state = 'reserved' AND `+capacityAccountCondition, snapshot.SourceID, snapshot.ObservedOperations, snapshot.ObservedAt).Error
}

func selectCapacity(snapshot domain.CapacitySnapshot, demand domain.CapacityDemand, pending []pendingCapacity) (domain.CapacityNode, domain.CapacityStorage, error) {
	if !capacityQuotaFits(snapshot, demand, pending) {
		return domain.CapacityNode{}, domain.CapacityStorage{}, fmt.Errorf("%w: namespace capacity quota is exhausted", apperrors.ErrConflict)
	}
	for _, node := range snapshot.Nodes {
		if demand.Node != "" && demand.Node != node.Name {
			continue
		}
		cpu, memory := node.CPU, node.MemoryMiB
		for _, reservation := range pending {
			if reservation.NodeName == node.Name {
				cpu -= reservation.CPU
				memory -= reservation.MemoryMiB
			}
		}
		if cpu < demand.CPU || memory < demand.MemoryMiB {
			continue
		}
		for _, storage := range snapshot.Storage {
			if storage.Key == "" || strings.TrimSpace(storage.Name) == "" || demand.Storage != "" && demand.Storage != storage.Name || !slices.Contains(storage.Nodes, node.Name) {
				continue
			}
			disk := storage.AvailableGiB
			for _, reservation := range pending {
				if reservation.StorageKey == storage.Key {
					disk -= reservation.DiskGiB
				}
			}
			if disk >= demand.DiskGiB {
				return node, storage, nil
			}
		}
	}
	return domain.CapacityNode{}, domain.CapacityStorage{}, fmt.Errorf("%w: no node and storage pool has sufficient unreserved capacity", apperrors.ErrConflict)
}

func capacityQuotaFits(snapshot domain.CapacitySnapshot, demand domain.CapacityDemand, pending []pendingCapacity) bool {
	cpu, memory, disk := demand.CPU, demand.MemoryMiB, demand.DiskGiB
	for _, item := range pending {
		if item.Namespace == snapshot.Namespace {
			cpu += item.CPU
			memory += item.MemoryMiB
			disk += item.DiskGiB
		}
	}
	return (snapshot.QuotaCPU == nil || cpu <= *snapshot.QuotaCPU) && (snapshot.QuotaMemoryMiB == nil || memory <= *snapshot.QuotaMemoryMiB) && (snapshot.QuotaDiskGiB == nil || disk <= *snapshot.QuotaDiskGiB)
}
