package networkruntime

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	app "github.com/opensoha/soha/internal/application/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func (r *Repository) AuthorizeWorkloadEnrollment(ctx context.Context, runtimeID, deviceID, subjectID, tokenID string, now time.Time) (time.Time, error) {
	binding, err := validateWorkloadEnrollment(r.db.WithContext(ctx), runtimeID, deviceID, subjectID, tokenID, now)
	return binding.ExpiresAt, err
}

func validateWorkloadEnrollment(tx *gorm.DB, runtimeID, deviceID, subjectID, tokenID string, now time.Time) (domain.WorkloadBinding, error) {
	binding, err := accessrepo.New(tx).WorkloadForRuntime(tx.Statement.Context, runtimeID, subjectID)
	if err != nil {
		return binding, err
	}
	if binding.DeviceID != deviceID || tokenID == "" {
		return binding, apperrors.ErrAccessDenied
	}
	var until time.Time
	err = tx.Raw(`SELECT LEAST(COALESCE(t.expires_at,?),?) FROM service_account_tokens t JOIN service_accounts sa ON sa.id=t.service_account_id
 WHERE t.id=? AND sa.id=? AND sa.status='active' AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at>?)
 AND t.permission_keys::jsonb @> '["network_access.enrollments.create"]'::jsonb FOR SHARE OF t,sa`, binding.ExpiresAt, binding.ExpiresAt, tokenID, binding.ServiceAccountID, now).Row().Scan(&until)
	if err != nil {
		return binding, apperrors.ErrAccessDenied
	}
	binding.ExpiresAt = until
	return binding, nil
}

func validateWorkloadCredential(tx *gorm.DB, c credentialRow, now time.Time) (domain.WorkloadBinding, error) {
	if c.RuntimeKind != "endpoint" || !strings.HasPrefix(c.SubjectID, "service_account:") || !credentialHasCapability(c, "workload-identity-v1") {
		return domain.WorkloadBinding{}, apperrors.ErrAccessDenied
	}
	return validateWorkloadEnrollment(tx, c.RuntimeID, c.DeviceID, c.SubjectID, c.SourceTokenID, now)
}

func workloadSubject(tx *gorm.DB, id string) (domain.Subject, error) {
	var subject domain.Subject
	var roles, teams []byte
	err := tx.Raw(`SELECT 'service_account:'||id,status,role_ids,team_ids FROM service_accounts WHERE id=? AND status='active'`, id).Row().Scan(&subject.UserID, &subject.Status, &roles, &teams)
	if err != nil {
		return subject, err
	}
	if err := json.Unmarshal(roles, &subject.Roles); err != nil {
		return subject, err
	}
	err = json.Unmarshal(teams, &subject.Teams)
	return subject, err
}

func (r *Repository) WorkloadContext(ctx context.Context, runtimeID, subjectID string, now time.Time) (domain.WorkloadBinding, domain.Subject, error) {
	tx := r.db.WithContext(ctx)
	credential, err := activeRuntimeCredential(tx, runtimeID, now)
	if err != nil {
		return domain.WorkloadBinding{}, domain.Subject{}, err
	}
	if credential.SubjectID != subjectID {
		return domain.WorkloadBinding{}, domain.Subject{}, apperrors.ErrAccessDenied
	}
	binding, err := validateWorkloadCredential(tx, credential, now)
	if err != nil {
		return binding, domain.Subject{}, err
	}
	subject, err := workloadSubject(tx, binding.ServiceAccountID)
	return binding, subject, err
}

func workloadScopeAllowed(tx *gorm.DB, scope vpnGovernanceScope, now time.Time) (bool, error) {
	credential, err := activeRuntimeCredential(tx, scope.RuntimeID, now)
	if err != nil {
		return false, nil
	}
	binding, err := validateWorkloadCredential(tx, credential, now)
	if err != nil {
		return false, nil
	}
	if credential.SubjectID != scope.SubjectID || binding.DeviceID != scope.DeviceID || binding.SiteID != scope.SiteID || binding.NetworkSpaceID != scope.SpaceID || scope.Mode != domain.ModeExternalDirectZTNA || len(scope.ResourceIDs) == 0 {
		return false, nil
	}
	subject, err := workloadSubject(tx, binding.ServiceAccountID)
	if err != nil {
		return false, err
	}
	repo := accessrepo.New(tx)
	device, err := repo.GetDevice(tx.Statement.Context, binding.DeviceID)
	if err != nil {
		return false, err
	}
	site, err := repo.GetSite(tx.Statement.Context, binding.SiteID)
	if err != nil {
		return false, err
	}
	space, err := repo.GetSpace(tx.Statement.Context, binding.NetworkSpaceID)
	if err != nil {
		return false, err
	}
	snapshot, err := repo.GetPolicySnapshot(tx.Statement.Context)
	if err != nil {
		return false, err
	}
	for _, id := range scope.ResourceIDs {
		if !slices.Contains(binding.ResourceIDs, id) {
			return false, nil
		}
		resource, err := repo.GetResource(tx.Statement.Context, id)
		if err != nil {
			return false, err
		}
		profile := app.EvaluateWorkloadPolicy(app.PreviewInput{SubjectUserID: scope.SubjectID, DeviceID: scope.DeviceID, ResourceID: id, SiteID: scope.SiteID, Mode: scope.Mode}, subject, device, site, space, resource, snapshot)
		if profile == "" || profile != scope.Profile {
			return false, nil
		}
	}
	return true, nil
}
