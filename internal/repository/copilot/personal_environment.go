package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

const personalEnvironmentColumns = `assistant_id,created_by,template_id,agent_cluster_id,lease_seconds,lease,state,version,data_initialized,data_creation_attempted,usage,observed_at,updated_at,operation_id,operation_until,active_task_id,backend,provider,policy_revision,compute_ref,compute_generation,data_ref,vm_identity`

func scanPersonalEnvironment(row *sql.Row) (domain.PersonalEnvironmentRecord, error) {
	var item domain.PersonalEnvironmentRecord
	var lease, usage, vmIdentity []byte
	err := row.Scan(&item.AssistantID, &item.CreatedBy, &item.TemplateID, &item.AgentClusterID, &item.LeaseSeconds, &lease, &item.State, &item.Version, &item.DataInitialized, &item.DataCreationAttempted, &usage, &item.ObservedAt, &item.UpdatedAt, &item.OperationID, &item.OperationUntil, &item.ActiveTaskID, &item.Backend, &item.Provider, &item.PolicyRevision, &item.ComputeRef, &item.ComputeGeneration, &item.DataRef, &vmIdentity)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err != nil {
		return item, fmt.Errorf("read personal environment: %w", err)
	}
	if err := json.Unmarshal(lease, &item.Lease); err != nil {
		return item, fmt.Errorf("decode original personal lease: %w", err)
	}
	if len(usage) > 0 {
		if err := json.Unmarshal(usage, &item.Usage); err != nil {
			return item, fmt.Errorf("decode personal resource receipt: %w", err)
		}
	}
	if len(vmIdentity) > 0 {
		if err := json.Unmarshal(vmIdentity, &item.VMIdentity); err != nil {
			return item, fmt.Errorf("decode personal VM identity: %w", err)
		}
	}
	return item, nil
}

func (r *Repository) GetPersonalEnvironment(ctx context.Context, owner, assistantID string) (domain.PersonalEnvironmentRecord, error) {
	return scanPersonalEnvironment(dbtx.DB(ctx, r.db).Raw(`SELECT `+personalEnvironmentColumns+` FROM ai_personal_environments WHERE created_by=? AND assistant_id=?`, owner, assistantID).Row())
}

func (r *Repository) BindPersonalEnvironment(ctx context.Context, candidate domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error) {
	if err := validatePersonalBinding(candidate); err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	if err := (contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "acquire", Lease: candidate.Lease}).Validate(); err != nil {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrInvalidArgument
	}
	lease, err := json.Marshal(candidate.Lease)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	var item domain.PersonalEnvironmentRecord
	err = dbtx.Within(ctx, r.db, func(txCtx context.Context) error {
		if candidate.Backend == "vm" {
			var assistant string
			err := dbtx.DB(txCtx, r.db).Raw(`SELECT a.id FROM ai_personal_assistants a JOIN ai_personal_agent_access x ON x.user_id=a.created_by
 WHERE a.id=? AND a.created_by=? AND x.status='approved' FOR UPDATE OF a,x`, candidate.AssistantID, candidate.CreatedBy).Row().Scan(&assistant)
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrAccessDenied
			}
			if err != nil {
				return fmt.Errorf("lock personal VM authority: %w", err)
			}
		}
		var identity any
		computeRef, generation := "", int64(0)
		state := "unprovisioned"
		if candidate.VMIdentity != nil {
			payload, err := json.Marshal(candidate.VMIdentity)
			if err != nil {
				return err
			}
			identity, state = string(payload), "pending"
			computeRef, generation = candidate.ComputeRef, candidate.ComputeGeneration
		}
		if err := dbtx.DB(txCtx, r.db).Exec(`INSERT INTO ai_personal_environments
 (assistant_id,created_by,template_id,agent_cluster_id,lease_seconds,lease,state,updated_at,backend,provider,policy_revision,data_ref,compute_ref,compute_generation,vm_identity)
 SELECT id,created_by,?,?,?,?,?,?,?,?,?,?,?,?,? FROM ai_personal_assistants WHERE id=? AND created_by=?
 ON CONFLICT DO NOTHING`, candidate.TemplateID, candidate.AgentClusterID, candidate.LeaseSeconds, string(lease), state, candidate.UpdatedAt, candidate.Backend, candidate.Provider, candidate.PolicyRevision, candidate.DataRef, computeRef, generation, identity, candidate.AssistantID, candidate.CreatedBy).Error; err != nil {
			return fmt.Errorf("bind personal environment: %w", err)
		}
		var err error
		item, err = r.GetPersonalEnvironment(txCtx, candidate.CreatedBy, candidate.AssistantID)
		if errors.Is(err, apperrors.ErrNotFound) && candidate.Backend == "vm" {
			return apperrors.ErrConflict
		}
		if err == nil && !samePersonalBinding(item, candidate) {
			return apperrors.ErrConflict
		}
		return err
	})
	return item, err
}

func validatePersonalBinding(candidate domain.PersonalEnvironmentRecord) error {
	if candidate.PolicyRevision == "" || len(candidate.PolicyRevision) > 128 {
		return apperrors.ErrInvalidArgument
	}
	if candidate.Lease.OwnerID != candidate.AssistantID || candidate.LeaseSeconds < 60 || candidate.LeaseSeconds > 3600 || candidate.TemplateID == "" || len(candidate.TemplateID) > 128 || candidate.UpdatedAt.IsZero() {
		return apperrors.ErrInvalidArgument
	}
	if candidate.Backend == "vm" {
		return validatePersonalVMBinding(candidate)
	}
	if candidate.Backend != "container" || candidate.Provider != "docker" || candidate.DataRef != "personal-data:"+candidate.AssistantID || candidate.VMIdentity != nil {
		return apperrors.ErrInvalidArgument
	}
	if id, err := uuid.Parse(candidate.AgentClusterID); err != nil || id == uuid.Nil {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

// The CAS and durable dispatch window serialize operations across Core nodes.
// Unknown operations keep the window until all bounded Agent cleanup can end.
func (r *Repository) ReservePersonalEnvironment(ctx context.Context, current domain.PersonalEnvironmentRecord, lease contractenv.Lease, operationID string, createData bool) (domain.PersonalEnvironmentRecord, error) {
	if current.Backend != "vm" && (current.Backend != "container" || current.Provider != "docker") || current.Backend == "vm" && (createData || !current.DataInitialized) {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrConflict
	}
	if id, err := uuid.Parse(operationID); err != nil || id == uuid.Nil {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrInvalidArgument
	}
	if err := (contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "acquire", Lease: lease}).Validate(); err != nil {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrInvalidArgument
	}
	payload, err := json.Marshal(lease)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	quota, err := json.Marshal(lease.Quota)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	row := r.db.WithContext(ctx).Raw(`UPDATE ai_personal_environments SET
 compute_generation=compute_generation+CASE WHEN backend='container' AND (? OR lease->>'id'<>?) THEN 1 ELSE 0 END,
 compute_ref=CASE WHEN backend='container' AND (? OR lease->>'id'<>?) THEN ? ELSE compute_ref END,
 lease=?,state=CASE WHEN state IN ('deleting','deletion_unknown') THEN 'deleting' ELSE 'pending' END,version=version+1,data_creation_attempted=data_creation_attempted OR ?,
 operation_id=?,operation_until=now()+interval '70 seconds',updated_at=now()
 WHERE assistant_id=? AND created_by=? AND version=? AND (operation_until IS NULL OR operation_until<=now())
	AND lease->>'ownerId'=? AND lease->>'scopeHash'=? AND lease->>'mode'=? AND lease->'quota'=CAST(? AS jsonb)
 AND (NOT ? OR NOT data_creation_attempted)
 AND backend=? AND provider=? AND policy_revision=? AND data_ref=? AND compute_generation=? AND compute_ref=?
	RETURNING `+personalEnvironmentColumns, createData, lease.ID, createData, lease.ID, lease.ID, string(payload), createData, operationID, current.AssistantID, current.CreatedBy, current.Version, lease.OwnerID, lease.ScopeHash, lease.Mode, string(quota), createData, current.Backend, current.Provider, current.PolicyRevision, current.DataRef, current.ComputeGeneration, current.ComputeRef).Row()
	item, err := scanPersonalEnvironment(row)
	if errors.Is(err, apperrors.ErrNotFound) {
		return item, apperrors.ErrConflict
	}
	return item, err
}

func (r *Repository) CompletePersonalEnvironment(ctx context.Context, item domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error) {
	usage, err := json.Marshal(item.Usage)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	row := r.db.WithContext(ctx).Raw(`UPDATE ai_personal_environments SET
 state=?,version=version+1,data_initialized=data_initialized OR ?,usage=?,observed_at=?,updated_at=now(),
 operation_id=CASE WHEN ? IN ('unknown','deletion_unknown') THEN operation_id ELSE '' END,
 operation_until=CASE WHEN ? IN ('unknown','deletion_unknown') THEN operation_until ELSE NULL END
 ,active_task_id=CASE WHEN ? IN ('sleeping','missing','expired','deleted') OR (backend='vm' AND ?='') THEN '' ELSE active_task_id END
 WHERE assistant_id=? AND created_by=? AND version=? AND operation_id=?
 AND backend=? AND provider=? AND policy_revision=? AND data_ref=? AND compute_generation=? AND compute_ref=?
 RETURNING `+personalEnvironmentColumns, item.State, item.DataInitialized, string(usage), item.ObservedAt, item.State, item.State, item.State, item.ActiveTaskID, item.AssistantID, item.CreatedBy, item.Version, item.OperationID, item.Backend, item.Provider, item.PolicyRevision, item.DataRef, item.ComputeGeneration, item.ComputeRef).Row()
	result, err := scanPersonalEnvironment(row)
	if errors.Is(err, apperrors.ErrNotFound) {
		return result, apperrors.ErrConflict
	}
	return result, err
}

// Only a proven local pre-dispatch failure may restore the preceding state.
// The reservation fence prevents a late abort from undoing a newer operation.
func (r *Repository) AbortPersonalEnvironment(ctx context.Context, reserved, previous domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error) {
	if reserved.Version != previous.Version+1 || reserved.AssistantID != previous.AssistantID || reserved.CreatedBy != previous.CreatedBy || reserved.OperationID == "" {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrInvalidArgument
	}
	lease, err := json.Marshal(previous.Lease)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	usage, err := json.Marshal(previous.Usage)
	if err != nil {
		return domain.PersonalEnvironmentRecord{}, err
	}
	row := r.db.WithContext(ctx).Raw(`UPDATE ai_personal_environments SET
 lease=?,compute_ref=?,compute_generation=?,state=?,version=version+1,data_creation_attempted=?,usage=?,observed_at=?,updated_at=now(),
 operation_id='',operation_until=NULL,active_task_id=?
 WHERE assistant_id=? AND created_by=? AND version=? AND operation_id=? AND state IN ('pending','deleting')
 AND backend=? AND provider=? AND policy_revision=? AND data_ref=? AND compute_generation=? AND compute_ref=?
 RETURNING `+personalEnvironmentColumns, string(lease), previous.ComputeRef, previous.ComputeGeneration, previous.State, previous.DataCreationAttempted, string(usage), previous.ObservedAt, previous.ActiveTaskID, reserved.AssistantID, reserved.CreatedBy, reserved.Version, reserved.OperationID, reserved.Backend, reserved.Provider, reserved.PolicyRevision, reserved.DataRef, reserved.ComputeGeneration, reserved.ComputeRef).Row()
	item, err := scanPersonalEnvironment(row)
	if errors.Is(err, apperrors.ErrNotFound) {
		return item, apperrors.ErrConflict
	}
	return item, err
}

// Explicit owner confirmation and the stopped environment are fenced by one
// update, including the current name. No queued command or new binding is made.
func (r *Repository) ReservePersonalDataDeletion(ctx context.Context, current domain.PersonalEnvironmentRecord, confirmationName, operationID string) (domain.PersonalEnvironmentRecord, error) {
	if id, err := uuid.Parse(operationID); err != nil || id == uuid.Nil || confirmationName == "" {
		return domain.PersonalEnvironmentRecord{}, apperrors.ErrInvalidArgument
	}
	row := r.db.WithContext(ctx).Raw(`UPDATE ai_personal_environments e SET
 state='deleting',version=version+1,operation_id=?,operation_until=now()+interval '70 seconds',updated_at=now()
 FROM ai_personal_assistants a WHERE e.assistant_id=a.id AND a.created_by=e.created_by AND a.name=?
 AND e.assistant_id=? AND e.created_by=? AND e.version=? AND e.data_initialized
 AND e.state IN ('sleeping','deleting','deletion_unknown') AND e.active_task_id=''
 AND (e.operation_until IS NULL OR e.operation_until<=now())
 RETURNING e.`+strings.ReplaceAll(personalEnvironmentColumns, ",", ",e."), operationID, confirmationName, current.AssistantID, current.CreatedBy, current.Version).Row()
	item, err := scanPersonalEnvironment(row)
	if errors.Is(err, apperrors.ErrNotFound) {
		err = apperrors.ErrConflict
	}
	return item, err
}
