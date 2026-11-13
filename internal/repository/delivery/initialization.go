package delivery

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"gorm.io/gorm"
)

// HasTargetWriteHistory is deliberately cross application, binding and environment.
// Failed, queued and uncertain writes retain history; read-only preflight does not.
func (r *Repository) HasTargetWriteHistory(ctx context.Context, cluster, namespace string, keys []string, release, ignorePlan string) (bool, error) {
	return TargetWriteHistory(r.db.WithContext(ctx), cluster, namespace, keys, release, ignorePlan)
}

func TargetWriteHistory(tx *gorm.DB, cluster, namespace string, keys []string, release, ignorePlan string) (bool, error) {
	rows, err := tx.Raw(`SELECT task_kind, payload::text, status, attempt_count FROM execution_tasks WHERE
 (task_kind IN ('manifest_apply','manifest_repair','manifest_rollback','manifest_adopt') AND payload->>'clusterId' = ?)
 OR (task_kind = 'helm_apply' AND payload->'helm'->'snapshot'->>'clusterId' = ?)`, cluster, cluster).Rows()
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind, encoded, status string
		var attempts int
		if err := rows.Scan(&kind, &encoded, &status, &attempts); err != nil {
			return false, err
		}
		previous, match, err := initializationHistoryRecord(kind, encoded, cluster, namespace, release, ignorePlan, status, attempts)
		if err != nil {
			return false, err
		}
		if match {
			return true, nil
		}

		for _, key := range previous {
			if slices.Contains(keys, key) {
				return true, nil
			}
		}
	}
	return false, rows.Err()
}

func initializationHistoryRecord(kind, encoded, cluster, namespace, release, ignorePlan, status string, attempts int) ([]string, bool, error) {
	if kind == "helm_apply" {
		var p struct {
			Helm sohaapi.HelmExecutionTaskPayload `json:"helm"`
		}
		if err := json.Unmarshal([]byte(encoded), &p); err != nil {
			return nil, false, err
		}
		snap := p.Helm.Snapshot
		if ignorePlan != "" && snap.DeliveryPlanID == ignorePlan {
			return nil, !currentInitializationAttempt(status, attempts), nil
		}
		if release != "" && snap.Namespace == namespace && snap.ReleaseName == release {
			return nil, true, nil
		}
		if len(snap.Resources) == 0 && (snap.Namespace == "" || snap.Namespace == namespace) {
			return nil, true, nil
		}
		return domainworkflow.HelmResourceKeys(snap), false, nil
	} else {
		var p domainmanifest.TaskPayload
		if err := json.Unmarshal([]byte(encoded), &p); err != nil {
			return nil, false, err
		}
		if ignorePlan != "" && p.InitializationPlanID == ignorePlan {
			return nil, !currentInitializationAttempt(status, attempts), nil
		}
		if len(p.Documents) == 0 && len(p.GitOpsDocuments) == 0 && (p.Namespace == "" || p.Namespace == namespace) {
			return nil, true, nil
		}
		return domainmanifest.ResourceKeys(cluster, p.Documents, p.GitOpsDocuments), false, nil
	}
}

// A failed/uncertain first write is history even for its own Plan. Never replay
// bootstrap after deletion; only the original first attempt or receipt may continue.
func currentInitializationAttempt(status string, attempts int) bool {
	return attempts <= 1 && slices.Contains([]string{"queued", "dispatching", "running", "completed"}, status)
}
