package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

// ServiceTemplateMigrationRequired is a private admission read, never a source
// of user-visible configuration or an authorization grant for an operation.
func (r *Repository) ServiceTemplateMigrationRequired(ctx context.Context, applicationID, serviceID string) (bool, error) {
	var data []byte
	if err := r.db.WithContext(ctx).Raw(`SELECT deployment_template FROM application_services WHERE id=? AND application_id=?`, serviceID, applicationID).Row().Scan(&data); err != nil {
		return false, err
	}
	var binding *domaincatalog.DeploymentTemplateBinding
	if err := json.Unmarshal(data, &binding); err != nil && len(data) > 0 {
		return false, err
	}
	if binding == nil {
		return false, nil
	}
	if binding.Detached {
		if binding.DetachedTemplate == nil {
			return false, apperrors.ErrConflict
		}
		return migrationPolicyDeclared(binding.DetachedTemplate.MigrationPolicy), nil
	}
	versions, err := r.ListDeploymentTemplateVersions(ctx, binding.TemplateID)
	if err != nil {
		return false, err
	}
	if len(versions) == 0 {
		return false, apperrors.ErrConflict
	}
	for _, v := range versions {
		if migrationPolicyDeclared(v.MigrationPolicy) {
			return true, nil
		}
	}
	return false, nil
}

func migrationPolicyDeclared(policy *sohaapi.TemplateMigrationPolicy) bool {
	return policy != nil && (policy.Sequential || policy.Irreversible)
}

// Called only while holding the existing template head row lock.
func migrationPolicyOnSave(tx *gorm.DB, table, id string, create bool, policy *sohaapi.TemplateMigrationPolicy) (*sohaapi.TemplateMigrationPolicy, error) {
	if !create && policy == nil {
		column := "migration_policy"
		if table == "deployment_templates" {
			column = "spec -> 'migrationPolicy'"
		}
		var data []byte
		if err := tx.Table(table).Select(column).Where("id = ?", id).Row().Scan(&data); err != nil {
			return nil, err
		}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &policy); err != nil {
				return nil, fmt.Errorf("decode migration policy: %w", err)
			}
		}
	}
	return policy, domaincatalog.ValidateMigrationPolicy(policy)
}
