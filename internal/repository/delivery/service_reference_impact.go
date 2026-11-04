package delivery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	dd "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) CreateDeliveryPlanWithServiceReferenceImpact(ctx context.Context, input dd.DeliveryPlanInput, actor string, snapshot dd.ServiceReferencePlanImpact) (dd.DeliveryPlan, error) {
	var plan dd.DeliveryPlan
	err := dbtx.Within(ctx, r.db, func(txctx context.Context) error {
		var err error
		plan, err = r.CreateDeliveryPlan(txctx, input, actor)
		if err != nil {
			return err
		}
		snapshot.PlanID = plan.ID
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		// Batch retries may encounter a previously created Plan. Never replace its evidence.
		result := dbtx.DB(txctx, r.db).Exec(`INSERT INTO delivery_plan_service_reference_impacts(plan_id,snapshot) VALUES (?,?::jsonb) ON CONFLICT (plan_id) DO NOTHING`, plan.ID, string(raw))
		if result.Error != nil {
			return fmt.Errorf("save Plan reference impact: %w", result.Error)
		}
		saved, err := r.GetDeliveryPlanServiceReferenceSnapshot(txctx, plan.ID)
		if err != nil {
			return err
		}
		if saved.Revision != snapshot.Revision || saved.DatabaseRevision != snapshot.DatabaseRevision {
			return apperrors.ErrConflict
		}
		return nil
	})
	return plan, err
}

func (r *Repository) GetDeliveryPlanServiceReferenceSnapshot(ctx context.Context, planID string) (dd.ServiceReferencePlanImpact, error) {
	var raw []byte
	var snapshot dd.ServiceReferencePlanImpact
	row := dbtx.DB(ctx, r.db).Raw(`SELECT snapshot FROM delivery_plan_service_reference_impacts WHERE plan_id=?`, planID).Row()
	if err := row.Scan(&raw); err != nil {
		return snapshot, normalizeServiceReferenceImpactError(err)
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode Plan reference impact: %w", err)
	}
	if snapshot.Version != 1 || snapshot.PlanID != planID || snapshot.Consumers == nil || snapshot.SourceRevisions == nil {
		return snapshot, apperrors.ErrConflict
	}
	return snapshot, nil
}

func normalizeServiceReferenceImpactError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.ErrNotFound
	}
	return err
}

// SourceServiceReferenceConfigurationRevision hashes authoritative Manifest configuration,
// never reconciliation/status tables. Raw source/package contents stay inside persistence.
func (r *Repository) SourceServiceReferenceConfigurationRevision(ctx context.Context, sourceID string) (string, error) {
	var raw []byte
	err := dbtx.DB(ctx, r.db).Raw(`SELECT COALESCE(jsonb_agg(jsonb_build_object(
  'binding',to_jsonb(b)-'created_at'-'updated_at',
  'package',to_jsonb(p)-'created_at'-'updated_at'-'created_by'-'updated_by',
  'source',to_jsonb(s)-'created_at'-'updated_at'
 ) ORDER BY b.id),'[]'::jsonb) FROM manifest_bindings b
 JOIN manifest_packages p ON p.id=b.package_id
 LEFT JOIN manifest_sources s ON s.package_id=p.id
 WHERE b.application_environment_id=?`, sourceID).Row().Scan(&raw)
	if err != nil {
		return "", fmt.Errorf("read source reference configuration: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
