package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func validateTargetServiceTx(tx *gorm.DB, bindingID, serviceID string) error {
	if serviceID == "" {
		return nil
	}
	var found string
	err := tx.Raw(`SELECT s.id FROM application_services s
 JOIN application_environments b ON b.application_id=s.application_id
 WHERE b.id=? AND s.id=? AND s.enabled=true FOR SHARE OF s`, bindingID, serviceID).Row().Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: selected service is not available in this application", apperrors.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("validate target service: %w", err)
	}
	return nil
}
