package catalog

import (
	"fmt"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
)

func validateDockerTargets(targets []domaincatalog.ReleaseTargetInput) error {
	for _, target := range targets {
		if target.Docker == nil && target.ExecutorKind != "docker_compose" {
			continue
		}
		config := target.Docker
		if config == nil || target.Helm != nil || target.TargetKind != "host_service" || target.ExecutorKind != "docker_compose" || target.ClusterID != "" || target.Namespace != "" || target.WorkloadKind != "DockerProject" || target.WorkloadName != config.ProjectID || strings.TrimSpace(config.HostID) == "" || strings.TrimSpace(config.ProjectID) == "" || len(config.ImageMappings) == 0 || len(config.ImageMappings) > 100 {
			return fmt.Errorf("%w: Docker target requires host/project identities, image mappings and no Kubernetes fields", apperrors.ErrInvalidArgument)
		}
		for name, container := range config.ImageMappings {
			if strings.TrimSpace(name) == "" || strings.TrimSpace(container) == "" {
				return apperrors.ErrInvalidArgument
			}
		}
	}
	return nil
}
