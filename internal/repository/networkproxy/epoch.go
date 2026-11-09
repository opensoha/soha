package networkproxy

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) PutConnectionsSnapshot(ctx context.Context, id string, at time.Time, snapshot domain.ConnectionsSnapshot) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := lockedInstance(tx, id)
		if err != nil {
			return err
		}
		if item.ProcessEpoch != snapshot.ProcessEpoch {
			return apperrors.ErrConflict
		}
		var old connectionsRow
		err = tx.Where("instance_id=?", id).Take(&old).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if err == nil && (!at.After(old.ObservedAt) || snapshot.ProcessEpoch != "" && old.ProcessEpoch == snapshot.ProcessEpoch && snapshot.SampleSequence <= old.SampleSequence) {
			return apperrors.ErrConflict
		}
		raw, err := json.Marshal(snapshot.Connections)
		if err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO network_proxy_connections(instance_id,observed_at,connections,process_epoch,sample_sequence,total_active) VALUES(?,?,?::jsonb,?,?,?) ON CONFLICT(instance_id) DO UPDATE SET observed_at=EXCLUDED.observed_at,connections=EXCLUDED.connections,process_epoch=EXCLUDED.process_epoch,sample_sequence=EXCLUDED.sample_sequence,total_active=EXCLUDED.total_active`, id, at, string(raw), snapshot.ProcessEpoch, snapshot.SampleSequence, snapshot.TotalActive).Error
	})
}
func (r *Repository) QueueCloseEpoch(ctx context.Context, id, connectionID, actor, epoch string, now time.Time) (domain.CloseCommand, error) {
	return r.QueueCloseRequest(ctx, id, connectionID, actor, domain.CloseInput{ProcessEpoch: epoch}, now)
}

func (r *Repository) QueueCloseRequest(ctx context.Context, id, connectionID, actor string, input domain.CloseInput, now time.Time) (domain.CloseCommand, error) {
	row := closeRow{InstanceID: id, ConnectionID: connectionID, CreatedBy: actor, ProcessEpoch: input.ProcessEpoch, Status: "pending", CreatedAt: now}
	if input.RequestID != "" {
		input.IssuedAt, input.ExpiresAt = input.IssuedAt.UTC(), input.ExpiresAt.UTC()
		row.ID, row.ExpiresAt, row.CallerInput = input.RequestID, input.ExpiresAt, &input
	} else {
		row.ID, row.ExpiresAt = uuid.NewString(), now.Add(30*time.Second)
	}
	var command domain.CloseCommand
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := lockedInstance(tx, id)
		if err != nil {
			return err
		}
		if row.CallerInput != nil {
			var found bool
			command, found, err = replayClose(tx, row, now)
			if err != nil || found {
				return err
			}
		}
		if row.ProcessEpoch == "" {
			row.ProcessEpoch = item.ProcessEpoch
		}
		if item.ProcessEpoch != row.ProcessEpoch || !row.ExpiresAt.After(now) {
			return apperrors.ErrConflict
		}
		if err := closeConnectionPresent(tx, row, now); err != nil {
			return err
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected != 1 {
			// The instance lock serializes same-target retries. Different targets can race on the global ID.
			return apperrors.ErrConflict
		}
		command = row.receipt(now)
		return nil
	})
	return command, err
}

func closeConnectionPresent(tx *gorm.DB, row closeRow, now time.Time) error {
	var snapshot connectionsRow
	err := tx.Where("instance_id=? AND process_epoch=? AND observed_at>=?", row.InstanceID, row.ProcessEpoch, now.Add(-time.Minute)).Take(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrConflict
	}
	if err != nil {
		return err
	}
	var connections []domain.Connection
	if err := json.Unmarshal(snapshot.Connections, &connections); err != nil {
		return err
	}
	if !slices.ContainsFunc(connections, func(connection domain.Connection) bool { return connection.ID == row.ConnectionID }) {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *Repository) ReplayClose(ctx context.Context, id, connectionID, actor string, input domain.CloseInput, now time.Time) (domain.CloseCommand, bool, error) {
	return replayClose(r.db.WithContext(ctx), closeRow{ID: input.RequestID, InstanceID: id, ConnectionID: connectionID, CreatedBy: actor, ProcessEpoch: input.ProcessEpoch, CallerInput: &input}, now)
}

func replayClose(db *gorm.DB, intent closeRow, now time.Time) (domain.CloseCommand, bool, error) {
	var saved closeRow
	err := db.Where("id=?", intent.ID).Take(&saved).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.CloseCommand{}, false, nil
	}
	if err != nil {
		return domain.CloseCommand{}, false, err
	}
	if saved.CallerInput == nil || saved.InstanceID != intent.InstanceID || saved.ConnectionID != intent.ConnectionID || saved.CreatedBy != intent.CreatedBy || saved.ProcessEpoch != intent.ProcessEpoch ||
		!saved.CallerInput.IssuedAt.Equal(intent.CallerInput.IssuedAt) || !saved.CallerInput.ExpiresAt.Equal(intent.CallerInput.ExpiresAt) {
		return domain.CloseCommand{}, true, apperrors.ErrConflict
	}
	return saved.receipt(now), true, nil
}

func (r *Repository) CompleteCloseEpoch(ctx context.Context, id, commandID, epoch, status, reason string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := lockedInstance(tx, id)
		if err != nil {
			return err
		}
		if item.ProcessEpoch != epoch {
			return apperrors.ErrConflict
		}
		result := tx.Model(&closeRow{}).Where("instance_id=? AND id=? AND process_epoch=? AND status='pending' AND expires_at>?", id, commandID, epoch, now).Updates(map[string]any{"status": status, "reason_code": reason, "completed_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return nil
	})
}
