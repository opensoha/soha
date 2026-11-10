package manifest

import (
	"database/sql"
	"errors"
	"fmt"

	dm "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/repository/servicereferencelock"
	"gorm.io/gorm"
)

// Package changes also affect Plans using its bindings. Include every existing
// bound application and any proposed environment before touching package rows.
func lockPackageConfiguration(tx *gorm.DB, packageID, nextApplicationID string, environmentIDs ...string) error {
	return servicereferencelock.Resolve(tx, func() ([]string, error) {
		var apps []string
		err := tx.Raw(`
			SELECT application_id FROM manifest_packages WHERE id = ?
			UNION
			SELECT e.application_id FROM application_environments e
			WHERE e.id IN ? OR e.id IN (
				SELECT application_environment_id FROM manifest_bindings WHERE package_id = ?
			)`, packageID, environmentIDs, packageID).Scan(&apps).Error
		return apps, err
	}, nextApplicationID)
}

func legacyBindingEnvironments(bindings []dm.Binding) []string {
	ids := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		ids = append(ids, binding.ApplicationEnvironmentID)
	}
	return ids
}

// A binding mutation that refreshes the package projection also takes the
// package application lock. The binding's package is rechecked after waiting.
func lockBindingConfiguration(tx *gorm.DB, bindingID, nextEnvironmentID string, writesPackage bool) (string, error) {
	var packageID string
	err := servicereferencelock.Resolve(tx, func() ([]string, error) {
		var currentPackage, currentApplication, packageApplication string
		err := tx.Raw(`
			SELECT b.package_id, e.application_id, p.application_id
			FROM manifest_bindings b
			JOIN application_environments e ON e.id = b.application_environment_id
			JOIN manifest_packages p ON p.id = b.package_id
			WHERE b.id = ?`, bindingID).Row().Scan(&currentPackage, &currentApplication, &packageApplication)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if packageID != "" && packageID != currentPackage {
			return nil, fmt.Errorf("%w: Manifest binding package changed", apperrors.ErrConflict)
		}
		packageID = currentPackage
		apps := []string{currentApplication}
		if writesPackage {
			apps = append(apps, packageApplication)
		}
		if nextEnvironmentID != "" {
			var nextApplication string
			err := tx.Raw(`SELECT application_id FROM application_environments WHERE id = ?`, nextEnvironmentID).Row().Scan(&nextApplication)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			apps = append(apps, nextApplication)
		}
		return apps, nil
	})
	if err == nil && writesPackage {
		err = lockBindingProjectionPackage(tx, packageID)
	}
	return packageID, err
}

// Binding edits refresh the package row. Lock it first, as operation admission
// holds a package share lock before it takes the binding share lock.
func lockBindingProjectionPackage(tx *gorm.DB, packageID string) error {
	return tx.Exec(`SELECT id FROM manifest_packages WHERE id = ? FOR NO KEY UPDATE`, packageID).Error
}
