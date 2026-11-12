package networkproxy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type commandRow struct {
	ID           string `gorm:"primaryKey"`
	InstanceID   string `gorm:"primaryKey"`
	RequestID    string
	IntentHash   string
	ProcessEpoch string
	Kind         string
	Input        []byte `gorm:"type:jsonb"`
	Result       []byte `gorm:"type:jsonb"`
	Status       string
	ReasonCode   string
	CreatedBy    string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

func (commandRow) TableName() string { return "network_proxy_runtime_commands" }
func (r commandRow) receipt(now time.Time) domain.RuntimeCommandReceipt {
	status := r.Status
	if (status == "pending" || status == "running") && !r.ExpiresAt.After(now) {
		status = "expired"
	}
	var result *domain.CommandDataResult
	_ = json.Unmarshal(r.Result, &result)
	return domain.RuntimeCommandReceipt{CommandID: r.ID, RequestID: r.RequestID, ProcessEpoch: r.ProcessEpoch, Kind: api.NetworkProxyRuntimeCommandKind(r.Kind), Status: api.NetworkProxyRuntimeCommandReceiptStatus(status), ExpiresAt: r.ExpiresAt, ReasonCode: r.ReasonCode, Result: result}
}
func (r *Repository) QueueCommand(ctx context.Context, id, actor string, input domain.RuntimeCommandInput, now time.Time) (domain.RuntimeCommandReceipt, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return domain.RuntimeCommandReceipt{}, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	row := commandRow{ID: input.RequestID, InstanceID: id, RequestID: input.RequestID, ProcessEpoch: input.ProcessEpoch, Kind: string(input.Kind), Input: raw, IntentHash: hash, Status: "pending", CreatedBy: actor, CreatedAt: now, ExpiresAt: input.ExpiresAt}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := lockedInstance(tx, id)
		if err != nil {
			return err
		}
		if item.ProcessEpoch != input.ProcessEpoch {
			return apperrors.ErrConflict
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "instance_id"}, {Name: "request_id"}}, DoNothing: true}).Create(&row)
		if result.Error != nil {
			return result.Error
		}
		var saved commandRow
		if err := tx.Where("instance_id=? AND request_id=?", id, input.RequestID).Take(&saved).Error; err != nil {
			return err
		}
		if saved.IntentHash != hash || saved.CreatedBy != actor {
			return apperrors.ErrConflict
		}
		row = saved
		return nil
	})
	if err != nil {
		return domain.RuntimeCommandReceipt{}, err
	}
	return commandReceipt(row, now), nil
}
func commandReceipt(row commandRow, now time.Time) domain.RuntimeCommandReceipt {
	return row.receipt(now)
}
func (r *Repository) GetCommand(ctx context.Context, id, commandID string, now time.Time) (domain.RuntimeCommandReceipt, error) {
	var row commandRow
	err := r.db.WithContext(ctx).Where("instance_id=? AND id=?", id, commandID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = apperrors.ErrNotFound
	}
	return commandReceipt(row, now), err
}
func (r *Repository) ClaimCommand(ctx context.Context, id, epoch string, now time.Time) (*domain.RuntimeCommand, error) {
	var command *domain.RuntimeCommand
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		instance, err := lockedInstance(tx, id)
		if err != nil {
			return err
		}
		if instance.ProcessEpoch != epoch {
			return apperrors.ErrConflict
		}
		var row commandRow
		err = tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("instance_id=? AND process_epoch=? AND status='pending' AND expires_at>?", id, epoch, now).Order("created_at ASC,id ASC").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var input domain.RuntimeCommandInput
		if err = json.Unmarshal(row.Input, &input); err != nil {
			return err
		}
		if err = tx.Model(&row).Update("status", "running").Error; err != nil {
			return err
		}
		command = &domain.RuntimeCommand{CommandID: row.ID, Input: input}
		return nil
	})
	return command, err
}
func (r *Repository) CompleteCommand(ctx context.Context, id string, result domain.RuntimeCommandResult, now time.Time) error {
	selected := ""
	if result.Result != nil {
		selected = result.Result.SelectedNodeID
	}
	raw, err := json.Marshal(result.Result)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		instance, err := lockedInstance(tx, id)
		if err != nil {
			return err
		}
		if instance.ProcessEpoch != result.ProcessEpoch {
			return apperrors.ErrConflict
		}
		var row commandRow
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("instance_id=? AND id=? AND request_id=? AND process_epoch=? AND kind=?", id, result.CommandID, result.RequestID, result.ProcessEpoch, result.Kind).
			Where("status='running' OR (status=? AND status NOT IN ('cancelled','expired') AND reason_code=? AND result=?::jsonb)", result.Status, result.ReasonCode, string(raw)).
			Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.ErrConflict
		}
		if err != nil {
			return err
		}
		var input domain.RuntimeCommandInput
		if err := json.Unmarshal(row.Input, &input); err != nil {
			return err
		}
		if !result.ExpiresAt.Equal(input.ExpiresAt) || !input.ExpiresAt.After(now) || (result.Kind == "select" && result.Status == "succeeded" && input.NodeID != selected) {
			return apperrors.ErrConflict
		}
		if row.Status != "running" {
			return nil
		}
		updated := tx.Model(&row).Where("status='running'").Updates(map[string]any{"status": result.Status, "reason_code": result.ReasonCode, "result": gorm.Expr("?::jsonb", string(raw))})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return nil
	})
}

func (r *Repository) CancelCommand(ctx context.Context, id, commandID string, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&commandRow{}).Where("instance_id=? AND id=? AND status IN ? AND expires_at>?", id, commandID, []string{"pending", "running"}, now).Updates(map[string]any{"status": gorm.Expr("CASE WHEN status='running' THEN 'unknown' ELSE 'cancelled' END"), "reason_code": gorm.Expr("CASE WHEN status='running' THEN 'cancel_outcome_unknown' ELSE 'caller_cancelled' END")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}
func (r *Repository) ReplayCommand(ctx context.Context, id, actor string, input domain.RuntimeCommandInput, now time.Time) (domain.RuntimeCommandReceipt, bool, error) {
	var row commandRow
	err := r.db.WithContext(ctx).Where("instance_id=? AND request_id=?", id, input.RequestID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.RuntimeCommandReceipt{}, false, nil
	}
	if err != nil {
		return domain.RuntimeCommandReceipt{}, false, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return domain.RuntimeCommandReceipt{}, false, err
	}
	if row.IntentHash != fmt.Sprintf("%x", sha256.Sum256(raw)) || row.CreatedBy != actor {
		return domain.RuntimeCommandReceipt{}, true, apperrors.ErrConflict
	}
	return row.receipt(now), true, nil
}
