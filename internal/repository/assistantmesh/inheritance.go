package assistantmesh

import (
	"context"
	"fmt"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

const inheritableAsset = `kind IN ('skill', 'experience_card') AND jsonb_typeof(payload -> 'published') = 'object'`
const inheritancePageSize = 50

func (r *Repository) ListInheritableAssets(ctx context.Context, ownerID, after string) (domain.InheritancePage, error) {
	page := domain.InheritancePage{Items: []domain.InheritanceAsset{}}
	query := `
		SELECT id, owner_id, COALESCE(NULLIF(payload->>'originalAuthorId', ''), owner_id::text) AS original_author_id,
			kind, payload #>> '{published,input,title}' AS title, (payload #>> '{published,version}')::int AS version
		FROM assistant_mesh_assets
		WHERE owner_id = ? AND ` + inheritableAsset + `
			AND EXISTS (SELECT 1 FROM users WHERE id = owner_id AND status = 'disabled')`
	args := []any{ownerID}
	if after != "" {
		query += " AND id > ?"
		args = append(args, after)
	}
	query += " ORDER BY id LIMIT ?"
	err := dbtx.DB(ctx, r.db).Raw(query, append(args, inheritancePageSize+1)...).Scan(&page.Items).Error
	if err != nil {
		return domain.InheritancePage{}, fmt.Errorf("list inheritable Heya assets: %w", err)
	}
	if len(page.Items) > inheritancePageSize {
		page.Items = page.Items[:inheritancePageSize]
		page.NextAfter = page.Items[inheritancePageSize-1].ID
	}
	return page, nil
}

func (r *Repository) DisposeInheritedAssets(ctx context.Context, input domain.InheritanceInput, record func(context.Context, []string) error) (int, error) {
	var transferred int
	err := dbtx.Within(ctx, r.db, func(tx context.Context) error {
		db := dbtx.DB(tx, r.db)
		if err := lockInheritanceAccounts(db, input.OwnerID, input.HeirID); err != nil {
			return err
		}
		ids, err := disposeInheritedAssets(db, input)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return fmt.Errorf("%w: no inheritable Heya assets remain", apperrors.ErrConflict)
		}
		if err := record(tx, ids); err != nil {
			return err
		}
		transferred = len(ids)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return transferred, nil
}

func lockInheritanceAccounts(db *gorm.DB, owner, heir string) error {
	var accounts []struct{ ID, Status string }
	accountIDs := []string{owner}
	if heir != "" {
		accountIDs = append(accountIDs, heir)
	}
	if err := db.Raw(`
		SELECT id, status FROM users WHERE id IN ? ORDER BY id FOR NO KEY UPDATE
	`, accountIDs).Scan(&accounts).Error; err != nil {
		return fmt.Errorf("lock Heya inheritance accounts: %w", err)
	}
	if len(accounts) != len(accountIDs) {
		return apperrors.ErrNotFound
	}
	for _, account := range accounts {
		if account.ID == owner && account.Status != "disabled" {
			return fmt.Errorf("%w: the source account must be disabled", apperrors.ErrConflict)
		}
		if account.ID == heir && account.Status != "active" {
			return fmt.Errorf("%w: the successor account must be active", apperrors.ErrConflict)
		}
	}
	return nil
}

func disposeInheritedAssets(db *gorm.DB, input domain.InheritanceInput) ([]string, error) {
	// Replace private work with the published baseline and preserve the audience and immutable version.
	query := `
		UPDATE assistant_mesh_assets
		SET owner_id = ?, updated_at = now(),
		    payload = (payload - ARRAY['draft', 'draftDigest', 'review', 'trial', 'findings']) ||
		      jsonb_build_object(
		        'originalAuthorId', COALESCE(NULLIF(payload->>'originalAuthorId', ''), owner_id::text),
		        'draft', payload #> '{published,input}',
		        'draftDigest', payload #> '{published,digest}') ||
		      CASE WHEN payload #>> '{published,audience,visibility}' = 'organization'
		        THEN jsonb_build_object('review', jsonb_build_object('status', 'approved', 'candidate', payload->'published'))
		        ELSE '{}'::jsonb END
	`
	args := []any{input.HeirID}
	if input.HeirID == "" {
		query = `UPDATE assistant_mesh_assets SET payload = payload - ARRAY['published', 'review'], updated_at = now()`
		args = nil
	}
	query += " WHERE owner_id = ? AND " + inheritableAsset
	args = append(args, input.OwnerID)
	if input.AssetIDs != nil {
		query += " AND id IN ?"
		args = append(args, input.AssetIDs)
	}
	var ids []string
	if err := db.Raw(query+" RETURNING id", args...).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("dispose of Heya assets: %w", err)
	}
	if input.AssetIDs != nil && len(ids) != len(input.AssetIDs) {
		return nil, fmt.Errorf("%w: the selected Heya assets have changed", apperrors.ErrConflict)
	}
	return ids, nil
}
