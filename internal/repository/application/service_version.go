package application

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func lockServiceUpdate(tx *gorm.DB, item *domainapp.Service, input domainapp.ServiceInput) error {
	var currentVersion int64
	var deploymentTemplate []byte
	err := tx.Raw(`SELECT version, created_at, deployment_template FROM application_services
		WHERE application_id = ? AND id = ? FOR UPDATE`, item.ApplicationID, item.ID).
		Row().Scan(&currentVersion, &item.CreatedAt, &deploymentTemplate)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if input.ExpectedVersion != nil && *input.ExpectedVersion != currentVersion {
		return fmt.Errorf("%w: service was modified; reload before saving", apperrors.ErrConflict)
	}
	if input.DeploymentTemplate != nil && input.ExpectedVersion == nil {
		return fmt.Errorf("%w: deployment template changes require expectedVersion", apperrors.ErrInvalidArgument)
	}
	item.Version = currentVersion + 1
	if input.DeploymentTemplate == nil && len(deploymentTemplate) > 0 {
		if err := json.Unmarshal(deploymentTemplate, &item.DeploymentTemplate); err != nil {
			return fmt.Errorf("decode existing deployment template: %w", err)
		}
	}
	return nil
}
