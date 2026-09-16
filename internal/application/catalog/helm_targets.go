package catalog

import (
	"fmt"
	"strings"

	"github.com/opensoha/soha-contracts/helmrelease"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validateHelmTargets(inputs []domaincatalog.ReleaseTargetInput, current []domaincatalog.ReleaseTarget) error {
	configured := map[string]bool{}
	for _, target := range current {
		configured[target.ID] = target.Helm != nil
	}
	for _, target := range inputs {
		if target.Helm == nil {
			if configured[target.ID] && target.ExecutorKind == "helm_sdk" {
				return fmt.Errorf("%w: Helm configuration is missing; reload the environment before saving", apperrors.ErrConflict)
			}
			continue
		}
		if target.ExecutorKind != "helm_sdk" || target.TargetKind != "helm_release" || target.WorkloadKind != "HelmRelease" || target.WorkloadName != target.Helm.ReleaseName || strings.TrimSpace(target.ClusterID) == "" || strings.TrimSpace(target.Namespace) == "" {
			return fmt.Errorf("%w: Helm configuration requires a matching Helm release target, cluster and namespace", apperrors.ErrInvalidArgument)
		}
		if err := helmrelease.ValidateConfiguration(*target.Helm); err != nil {
			return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
		}
	}
	return nil
}
