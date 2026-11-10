package delivery

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

// existingImportService preserves the logical service, build and template configuration.
// The key-share lock fences deletion between authorization and target association.
func existingImportService(tx *gorm.DB, applicationID, serviceID string) (domaindelivery.KubernetesImportEntityRef, error) {
	var service domaindelivery.KubernetesImportEntityRef
	err := tx.Raw(`SELECT id, service_key, service_name FROM application_services
 WHERE application_id = ? AND id = ? AND enabled = true FOR SHARE`, applicationID, strings.TrimSpace(serviceID)).Row().Scan(&service.ID, &service.Key, &service.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return service, fmt.Errorf("%w: selected logical service is no longer available in this application", apperrors.ErrConflict)
	}
	if err != nil {
		return service, fmt.Errorf("read selected logical service: %w", err)
	}
	return service, nil
}

func lockImportBinding(tx *gorm.DB, bindingID string) error {
	var found string
	if err := tx.Raw(`SELECT id FROM application_environments WHERE id = ? FOR UPDATE`, bindingID).Row().Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: import binding changed; authorize again", apperrors.ErrConflict)
		}
		return fmt.Errorf("lock import binding: %w", err)
	}
	return nil
}

func touchImportBinding(tx *gorm.DB, bindingID string) error {
	// Import and ordinary target edits share the same binding CAS fence.
	return tx.Exec(`UPDATE application_environments SET updated_at = GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond') WHERE id = ?`, bindingID).Error
}
