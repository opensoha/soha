package proxypublication

import (
	"context"
	"encoding/json"
	"errors"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type ruleRow struct {
	InstanceID string `gorm:"primaryKey"`
	OwnerID    string `gorm:"primaryKey"`
	Revision   int64
	Rules      []byte `gorm:"type:jsonb"`
}

func (ruleRow) TableName() string { return "network_proxy_rule_sets" }
func readRules(tx *gorm.DB, instance, owner string) (api.NetworkProxyRules, error) {
	out := api.NetworkProxyRules{Rules: []api.NetworkProxyRule{}, EffectiveRules: []api.NetworkProxyRule{}, Conflicts: []api.NetworkProxyRuleConflict{}}
	var row ruleRow
	err := tx.Where("instance_id = ? AND owner_id = ?", instance, owner).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if json.Unmarshal(row.Rules, &out.Rules) != nil {
		return out, apperrors.ErrServiceUnavailable
	}
	if owner == "" {
		out.OrgRevision = row.Revision
	} else {
		out.PersonalRevision = row.Revision
	}
	return out, nil
}
func (r *Repository) ReadRules(ctx context.Context, instance, owner string) (api.NetworkProxyRules, error) {
	return readRules(r.db.WithContext(ctx), instance, owner)
}
func (r *Repository) SaveRules(ctx context.Context, instance, owner string, input api.NetworkProxyRules, orgRevision, personalRevision int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := policyLock(tx); err != nil {
			return err
		}
		org, err := readRules(tx, instance, "")
		if err != nil {
			return err
		}
		if org.OrgRevision != orgRevision {
			return apperrors.ErrConflict
		}
		old, err := readRules(tx, instance, owner)
		if err != nil {
			return err
		}
		if owner != "" && old.PersonalRevision != personalRevision {
			return apperrors.ErrConflict
		}
		revision := input.OrgRevision
		if owner != "" {
			revision = input.PersonalRevision
		}
		raw, err := json.Marshal(input.Rules)
		if err != nil {
			return err
		}
		if owner == "" {
			if err := tx.Exec("DELETE FROM network_proxy_employee_credentials WHERE instance_id=?", instance).Error; err != nil {
				return err
			}
		}
		return tx.Exec("INSERT INTO network_proxy_rule_sets(instance_id,owner_id,revision,rules) VALUES(?,?,?,?::jsonb) ON CONFLICT(instance_id,owner_id) DO UPDATE SET revision=EXCLUDED.revision,rules=EXCLUDED.rules", instance, owner, revision, string(raw)).Error
	})
}
