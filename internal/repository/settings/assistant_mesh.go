package settings

import (
	"context"

	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) UpdateAssistantMesh(ctx context.Context, actor string, value map[string]any, audit func(context.Context, map[string]any, bool) error) error {
	const key = domainsettings.AssistantMeshSettingKey
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
			return err
		}
		repo := New(tx)
		before, found, err := repo.Get(ctx, key)
		if err != nil {
			return err
		}
		if err := audit(ctx, before, found); err != nil {
			return err
		}
		return repo.Upsert(ctx, key, "ai", value, actor)
	})
}
