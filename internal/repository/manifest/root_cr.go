package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	dm "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

type importedManifestTarget struct {
	ID           string
	ConfigRef    string
	WorkloadName string
	Namespace    string
	ExecutorKind string
	Metadata     string
}

// A separate Manifest binding cannot become a second writer for an imported CR.
// Lookup is bounded to this task's explicit names in its namespace, including
// observing roots in other application bindings. No cluster discovery is done.
func (r *Repository) ValidateImportedManifestTargets(ctx context.Context, payload dm.TaskPayload) error {
	if payload.Action == dm.TaskActionObserve || payload.Action == dm.TaskActionSync {
		return nil
	}
	names := make([]string, 0, len(payload.Documents))
	namespaces := []string{payload.Namespace}
	for _, document := range payload.Documents {
		names = append(names, document.Name)
		if document.Namespace != "" {
			namespaces = append(namespaces, document.Namespace)
		}
	}
	if len(names) == 0 {
		return nil
	}
	var targets []importedManifestTarget
	err := dbtx.DB(ctx, r.db).Table("release_targets").Select("id,config_ref,executor_kind,workload_name,namespace,metadata").Where("cluster_id=? AND namespace IN ? AND workload_name IN ? AND workload_kind='CustomResource' AND metadata->>'importSource'='kubernetes'", payload.ClusterID, namespaces, names).Limit(501).Scan(&targets).Error
	if err != nil {
		return err
	}
	if len(targets) > 500 {
		return fmt.Errorf("%w: imported resource ownership is ambiguous", apperrors.ErrConflict)
	}
	for _, target := range targets {
		if err = validateImportedManifestTarget(target, payload); err != nil {
			return err
		}
	}
	return nil
}

func validateImportedManifestTarget(target importedManifestTarget, payload dm.TaskPayload) error {
	var metadata struct {
		Source     string `json:"resourceSource"`
		Ownership  string `json:"ownershipMode"`
		Generation int64  `json:"rootCRGeneration"`
	}
	if json.Unmarshal([]byte(target.Metadata), &metadata) != nil {
		return apperrors.ErrConflict
	}
	var source struct {
		Resource struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			UID        string `json:"uid"`
		}
	}
	if json.Unmarshal([]byte(metadata.Source), &source) != nil || source.Resource.UID == "" || source.Resource.Kind == "" || source.Resource.APIVersion == "" {
		return apperrors.ErrConflict
	}
	sourceGroup, _, _ := strings.Cut(source.Resource.APIVersion, "/")
	for _, document := range payload.Documents {
		group, _, _ := strings.Cut(document.APIVersion, "/")
		namespace := document.Namespace
		if namespace == "" {
			namespace = payload.Namespace
		}
		if group != sourceGroup || document.Kind != source.Resource.Kind || document.Name != target.WorkloadName || namespace != target.Namespace {
			continue
		}
		root := payload.RootResource
		if root == nil || root.TargetID != target.ID || root.UID != source.Resource.UID || root.Generation != metadata.Generation || metadata.Ownership != "managed" || target.ExecutorKind != "manifest_ssa" || target.ConfigRef != payload.BindingID {
			return fmt.Errorf("%w: publish an imported CR through its UID-bound application target", apperrors.ErrConflict)
		}
	}
	return nil
}
