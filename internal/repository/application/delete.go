package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

func (r *Repository) Delete(ctx context.Context, applicationID string) error {
	applicationID = strings.TrimSpace(applicationID)
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var id string
		if err := tx.Raw(`SELECT id FROM applications WHERE id = ? FOR UPDATE`, applicationID).Row().Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		// Manifest admission locks packages before creating operations or bindings.
		if err := tx.Exec(`SELECT id FROM manifest_packages WHERE application_id = ? ORDER BY id FOR UPDATE`, applicationID).Error; err != nil {
			return err
		}
		if err := requireApplicationIdle(tx, applicationID); err != nil {
			return err
		}
		// Remove owned records in FK order, including archived packages. Shared
		// delivery batches and actual cluster workloads are not owned by this row.
		for _, statement := range []string{
			`DELETE FROM manifest_deployments WHERE package_id IN (SELECT id FROM manifest_packages WHERE application_id = ?)`,
			`DELETE FROM manifest_packages WHERE application_id = ?`,
			`DELETE FROM delivery_plans WHERE application_id = ?`,
			`DELETE FROM workflow_runs WHERE application_id = ? AND scope = 'application'`,
			`DELETE FROM applications WHERE id = ?`,
		} {
			if err := tx.Exec(statement, applicationID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	var constraint *pgconn.PgError
	if errors.As(err, &constraint) && constraint.Code == "23503" {
		return apperrors.NewBusiness(apperrors.ErrConflict, "application_in_use",
			"The application is referenced by other resources; remove their associations before deleting it.",
			"应用仍被其他资源引用，请先解除关联后再删除。")
	}
	if err != nil {
		return fmt.Errorf("delete application: %w", err)
	}
	return nil
}

func requireApplicationIdle(tx *gorm.DB, applicationID string) error {
	var busy bool
	if err := tx.Raw(`SELECT EXISTS (
		SELECT 1 FROM execution_tasks WHERE application_id = ? AND status IN ('queued','dispatching','running','canceling')
		UNION ALL
		SELECT 1 FROM workflow_runs r WHERE r.status NOT IN ('completed','partially_completed','failed','canceled')
		AND (r.application_id = ? OR EXISTS (
			SELECT 1 FROM delivery_batches b WHERE b.root_run_id = r.id
			AND b.snapshot->'targets' @> jsonb_build_array(jsonb_build_object('target', jsonb_build_object('applicationId', ?::text)))
		))
	)`, applicationID, applicationID, applicationID).Row().Scan(&busy); err != nil {
		return err
	}
	if busy {
		return apperrors.NewBusiness(apperrors.ErrConflict, "application_delivery_active",
			"The application has unfinished delivery or execution tasks; finish or cancel them before deleting it.",
			"应用仍有未结束的交付或执行任务，请先完成或取消任务后再删除。")
	}
	if err := tx.Raw(`SELECT EXISTS (
		SELECT 1 FROM manifest_bindings b
		JOIN manifest_packages p ON p.id = b.package_id
		JOIN application_environments e ON e.id = b.application_environment_id
		WHERE p.application_id = ? AND e.application_id <> p.application_id
	)`, applicationID).Row().Scan(&busy); err != nil {
		return err
	}
	if busy {
		return apperrors.NewBusiness(apperrors.ErrConflict, "application_in_use",
			"The application's manifests are bound to another application; remove those bindings before deleting it.",
			"应用的部署清单仍绑定了其他应用的环境，请先解除绑定后再删除。")
	}
	return nil
}
