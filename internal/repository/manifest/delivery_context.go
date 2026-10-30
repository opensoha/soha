package manifest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func encodeDeliveryContext(item *domainmanifest.DeliveryContext) (string, error) {
	if item == nil {
		return "", nil
	}
	if err := item.Validate(); err != nil {
		return "", fmt.Errorf("%w: invalid operation publication context", apperrors.ErrInvalidArgument)
	}
	data, err := json.Marshal(item)
	return string(data), err
}

func decodeDeliveryContext(raw sql.NullString) (*domainmanifest.DeliveryContext, error) {
	if !raw.Valid {
		return nil, nil
	}
	var item domainmanifest.DeliveryContext
	decoder := json.NewDecoder(bytes.NewBufferString(raw.String))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&item); err != nil || item.Validate() != nil {
		return nil, fmt.Errorf("%w: operation publication context requires review", apperrors.ErrConflict)
	}
	return &item, nil
}

func sameDeliveryContext(raw sql.NullString, expected *domainmanifest.DeliveryContext) error {
	current, err := decodeDeliveryContext(raw)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, expected) {
		return fmt.Errorf("%w: operation belongs to a different publication context", apperrors.ErrConflict)
	}
	return nil
}

// GetOperationTask retains the immutable task admission identity independently
// from public payloads and survives target deletion or task retries.
func (r *Repository) GetOperationTask(ctx context.Context, taskID string) (domainmanifest.OperationRun, error) {
	var item domainmanifest.OperationRun
	var raw sql.NullString
	err := r.db.WithContext(ctx).Raw(`SELECT id, package_id, COALESCE(binding_id,''), COALESCE(deployment_id,''),
 generation, action, idempotency_key, execution_task_id, created_at, delivery_context::text
 FROM manifest_operation_runs WHERE execution_task_id = ?`, taskID).Row().Scan(
		&item.ID, &item.PackageID, &item.BindingID, &item.DeploymentID, &item.Generation,
		&item.Action, &item.IdempotencyKey, &item.ExecutionTaskID, &item.CreatedAt, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err != nil {
		return item, fmt.Errorf("read Manifest task admission: %w", err)
	}

	item.DeliveryContext, err = decodeDeliveryContext(raw)
	return item, err
}
