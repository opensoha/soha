package networkproxy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"slices"
	"time"
)

// BatchFence must lock and verify the deployment-owned approved member set in
// the same transaction, before instance locks. No secondary publication ledger.
type BatchFence interface {
	LockDesiredBatch(context.Context, *gorm.DB, domain.DesiredBatch) error
	LockSingleDesired(context.Context, *gorm.DB, string) error
}

func (r *Repository) SetBatchFence(fence BatchFence) { r.batchFence = fence }
func (r *Repository) CommitDesiredBatch(ctx context.Context, batch domain.DesiredBatch, now time.Time) error {
	if r.batchFence == nil {
		return apperrors.ErrServiceUnavailable
	}
	members := slices.Clone(batch.Members)
	slices.SortFunc(members, func(a, b domain.DesiredMember) int {
		if a.InstanceID < b.InstanceID {
			return -1
		}
		if a.InstanceID > b.InstanceID {
			return 1
		}
		return 0
	})
	intentHash := batchIntentHash(batch, members)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "network-access-policy-snapshot:default:default").Error; err != nil {
			return err
		}
		saved, err := New(tx).GetDesiredBatchReceipt(ctx, batch.PublicationID)
		if err == nil {
			if saved.IntentHash != intentHash {
				return apperrors.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		if err := r.batchFence.LockDesiredBatch(ctx, tx, batch); err != nil {
			return err
		}
		for i, m := range members {
			if i > 0 && members[i-1].InstanceID == m.InstanceID {
				return apperrors.ErrConflict
			}
			row, err := lockedInstance(tx, m.InstanceID)
			if err != nil {
				return err
			}
			if row.DesiredRevision != m.ExpectedRevision {
				return apperrors.ErrConflict
			}
		}
		for _, m := range members {
			if err := writeConfiguration(ctx, tx, m.InstanceID, m.ExpectedRevision, m.Enabled, m.Encrypted, m.ContentHash, m.Binding, false, now); err != nil {
				return err
			}
			if err := tx.Model(&instanceRow{}).Where("id=?", m.InstanceID).Updates(map[string]any{"bundle_id": m.BundleID, "bundle_revision": m.BundleRevision}).Error; err != nil {
				return err
			}
		}
		receipt := domain.DesiredBatchReceipt{PublicationID: batch.PublicationID, MasterID: batch.MasterID, MemberSetHash: batch.MemberSetHash, OwnerEpoch: batch.OwnerEpoch, IntentHash: intentHash, CommittedAt: now}
		ids := make([]string, 0, len(members))
		for _, m := range members {
			ids = append(ids, m.InstanceID)
		}
		states, err := New(tx).ObserveMemberConfigurations(ctx, ids)
		if err != nil {
			return err
		}
		receipt.Members = states

		raw, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		return tx.Exec("INSERT INTO network_proxy_desired_batch_receipts(publication_id,intent_hash,receipt,created_at) VALUES(?,?,?::jsonb,?)", batch.PublicationID, intentHash, string(raw), now).Error
	})
}
func (r *Repository) ObserveMemberConfigurations(ctx context.Context, ids []string) ([]domain.MemberConfigurationState, error) {
	var rows []instanceRow
	if err := r.db.WithContext(ctx).Where("tenant_id='default' AND workspace_id='default' AND id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.MemberConfigurationState, 0, len(rows))
	for _, row := range rows {
		item := row.domain()
		result = append(result, domain.MemberConfigurationState{ConfigurationValidUntil: configurationLease(row), LastSeenAt: row.LastSeenAt, LastSampleAt: row.LastSampleAt, BundleID: row.BundleID, BundleRevision: row.BundleRevision, AppliedHash: row.ObservedHash, InstanceID: row.ID, DesiredRevision: row.DesiredRevision, AppliedRevision: row.ObservedRevision, ContentHash: row.DesiredHash, DesiredHash: configurationForDigest(row).Digest(), PolicyVersion: item.PolicyVersion, ObservedPolicyVersion: row.ObservedPolicyVersion, ProcessEpoch: row.ProcessEpoch})
	}
	return result, nil
}

func batchIntentHash(batch domain.DesiredBatch, members []domain.DesiredMember) string {
	copyBatch := batch
	copyBatch.Members = slices.Clone(members)
	for i := range copyBatch.Members {
		copyBatch.Members[i].Content = nil
		copyBatch.Members[i].Encrypted = ""
	}
	raw, _ := json.Marshal(copyBatch)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}
func (r *Repository) GetDesiredBatchReceipt(ctx context.Context, id string) (domain.DesiredBatchReceipt, error) {
	var row struct{ Receipt []byte }
	err := r.db.WithContext(ctx).Table("network_proxy_desired_batch_receipts").Where("publication_id=?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = apperrors.ErrNotFound
	}
	if err != nil {
		return domain.DesiredBatchReceipt{}, err
	}
	var receipt domain.DesiredBatchReceipt
	err = json.Unmarshal(row.Receipt, &receipt)
	return receipt, err
}
