package proxypublication

import (
	"encoding/json"
	"errors"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

// Source CAS and every ceiling update commit under the same policy lock.
func narrowSourceCredentials(tx *gorm.DB, instance, source string) error {
	var bundles []bundleRow
	if err := tx.Where("instance_id=? AND record->'Public'->>'sourceId'=?", instance, source).Find(&bundles).Error; err != nil {
		return err
	}
	for _, row := range bundles {
		var bundle domain.BundleRecord
		if json.Unmarshal(row.Record, &bundle) != nil {
			return apperrors.ErrServiceUnavailable
		}
		allowed, err := allowedSourceNodes(tx, instance, source, bundle.Public.SourceRevision, bundle.SourceNodeIDs)
		if err != nil && !errors.Is(err, apperrors.ErrAccessDenied) {
			return err
		}
		scope := []string{}
		if err == nil {
			narrowed, narrowErr := domain.NarrowBundle(bundle, allowed)
			if narrowErr == nil {
				scope = narrowed.Scope.NodeIDs
			}
		}
		if err = narrowBundleCredentials(tx, instance, bundle.Public.BundleID, scope); err != nil {
			return err
		}
	}
	return nil
}
func narrowBundleCredentials(tx *gorm.DB, instance, bundle string, allowed []string) error {
	var rows []credentialRow
	if err := tx.Where("instance_id=? AND bundle_id=?", instance, bundle).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		var c domain.Credential
		if json.Unmarshal(row.Record, &c) != nil {
			return apperrors.ErrServiceUnavailable
		}
		requested := domain.IntersectNodes(c.NodeIDs, allowed)
		nodes, err := narrowCredentialCeiling(tx, domain.CredentialIdentity(c), requested)
		if err != nil {
			return err
		}
		c.NodeIDs = nodes
		raw, _ := json.Marshal(c)
		if err = tx.Model(&credentialRow{}).Where("id=?", row.ID).Update("record", string(raw)).Error; err != nil {
			return err
		}
	}
	return nil
}
