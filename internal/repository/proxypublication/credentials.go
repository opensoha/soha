package proxypublication

import (
	"context"
	"encoding/json"
	"errors"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type scopeRow struct {
	IdentityKey string `gorm:"primaryKey"`
	NodeIDs     []byte `gorm:"type:jsonb"`
}

func (scopeRow) TableName() string { return "network_proxy_credential_scopes" }

type renewableCredential struct {
	ID         string
	RenewalKey string
	Record     []byte
	Renewable  bool
}

func (r *Repository) IssueCredential(ctx context.Context, c domain.Credential) (domain.Credential, error) {
	var issued domain.Credential
	denied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := policyLock(tx); err != nil {
			return err
		}
		current, err := credentialCurrent(tx, c)
		if err != nil {
			return err
		}
		c = current
		c.RenewalKey = domain.CredentialIdentity(c)
		rows, err := matchingCredentials(tx, c)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var old domain.Credential
			if json.Unmarshal(row.Record, &old) != nil {
				return apperrors.ErrServiceUnavailable
			}
			c.NodeIDs = domain.IntersectNodes(c.NodeIDs, old.NodeIDs)
		}
		c.NodeIDs, err = narrowCredentialCeiling(tx, c.RenewalKey, c.NodeIDs)
		if err != nil {
			return err
		}
		if err = narrowCredentialRows(tx, rows, c.NodeIDs); err != nil {
			return err
		}
		if len(c.NodeIDs) == 0 {
			denied = true
			return nil
		}
		chosen := chooseRenewable(rows, c.RenewalKey)
		if chosen != nil {
			if json.Unmarshal(chosen.Record, &issued) != nil {
				return apperrors.ErrServiceUnavailable
			}
			issued.NodeIDs = domain.IntersectNodes(issued.NodeIDs, c.NodeIDs)
			issued.ExpiresAt = c.ExpiresAt
			issued.RenewalKey = c.RenewalKey
		} else {
			issued = c
		}
		if err = tx.Where("expires_at <= CURRENT_TIMESTAMP").Delete(&credentialRow{}).Error; err != nil {
			return err
		}
		raw, err := json.Marshal(issued)
		if err != nil {
			return err
		}
		row := credentialRow{ID: issued.ID, InstanceID: issued.InstanceID, BundleID: issued.BundleID, RenewalKey: issued.RenewalKey, ExpiresAt: issued.ExpiresAt, Record: raw}
		if chosen == nil {
			return tx.Create(&row).Error
		}
		return tx.Model(&credentialRow{}).Where("id=?", issued.ID).Updates(map[string]any{"renewal_key": issued.RenewalKey, "expires_at": issued.ExpiresAt, "record": string(raw)}).Error
	})
	if err == nil && denied {
		return issued, apperrors.ErrAccessDenied
	}
	return issued, err
}
func matchingCredentials(tx *gorm.DB, c domain.Credential) ([]renewableCredential, error) {
	var rows []renewableCredential
	err := tx.Table("network_proxy_employee_credentials").Select("*, expires_at>CURRENT_TIMESTAMP AS renewable").Clauses(clause.Locking{Strength: "UPDATE"}).Where("instance_id=? AND bundle_id=? AND record->>'EndpointCredentialID'=? AND record->>'RuntimeID'=? AND record->>'ProfileID'=? AND record->>'ProcessEpoch'=? AND (record->>'Revision')::bigint=? AND (record->>'Epoch')::bigint=?", c.InstanceID, c.BundleID, c.EndpointCredentialID, c.RuntimeID, c.ProfileID, c.ProcessEpoch, c.Revision, c.Epoch).Order("id").Find(&rows).Error
	return rows, err
}
func narrowCredentialCeiling(tx *gorm.DB, key string, requested []string) ([]string, error) {
	var row scopeRow
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("identity_key=?", key).Take(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	nodes := requested
	if err == nil {
		var old []string
		if json.Unmarshal(row.NodeIDs, &old) != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		nodes = domain.IntersectNodes(old, requested)
	}
	raw, _ := json.Marshal(nodes)
	err = tx.Exec("INSERT INTO network_proxy_credential_scopes(identity_key,node_ids) VALUES(?,?::jsonb) ON CONFLICT(identity_key) DO UPDATE SET node_ids=EXCLUDED.node_ids", key, string(raw)).Error
	return nodes, err
}
func narrowCredentialRows(tx *gorm.DB, rows []renewableCredential, allowed []string) error {
	for _, row := range rows {
		var c domain.Credential
		if json.Unmarshal(row.Record, &c) != nil {
			return apperrors.ErrServiceUnavailable
		}
		c.NodeIDs = domain.IntersectNodes(c.NodeIDs, allowed)
		raw, _ := json.Marshal(c)
		if err := tx.Model(&credentialRow{}).Where("id=?", row.ID).Update("record", string(raw)).Error; err != nil {
			return err
		}
	}
	return nil
}
func chooseRenewable(rows []renewableCredential, key string) *renewableCredential {
	var chosen *renewableCredential
	for i := range rows {
		if !rows[i].Renewable {
			continue
		}
		if chosen == nil || rows[i].RenewalKey == key {
			chosen = &rows[i]
		}
	}
	return chosen
}
