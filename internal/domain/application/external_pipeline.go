package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func ExternalPipelineConfiguration(config map[string]any) (sohaapi.ExternalPipelineConfiguration, error) {
	var source sohaapi.BuildSourceConfig
	encoded, err := json.Marshal(config)
	if err != nil {
		return sohaapi.ExternalPipelineConfiguration{}, apperrors.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil || source.ExternalPipeline == nil {
		return sohaapi.ExternalPipelineConfiguration{}, fmt.Errorf("%w: typed external pipeline configuration is required", apperrors.ErrInvalidArgument)
	}
	value := *source.ExternalPipeline
	if value.Provider != sohaapi.ExternalPipelineGitLab || !pipelineConfigText(value.PipelineTag, 255) || !pipelineConfigText(value.ArtifactJob, 255) || !pipelineConfigText(value.RegistryID, 128) {
		return value, fmt.Errorf("%w: external pipeline requires GitLab, a protected pipeline tag, artifact job and registry", apperrors.ErrInvalidArgument)
	}
	if source.ProviderKind != "" || source.BuilderKind != "" || source.DockerfilePath != "" || source.BuildTemplateID != "" || source.PipelineRef != "" || source.Buildpacks != nil || source.SecretRefs != nil {
		return value, fmt.Errorf("%w: external pipeline cannot override its adapter or use build commands or task secret references", apperrors.ErrInvalidArgument)
	}
	if len(source.RepositoryBindings) != 1 || source.RepositoryBindings[0].Submodules {
		return value, fmt.Errorf("%w: GitLab pipeline requires one explicitly bound source repository; the trusted CI definition owns any further checkout", apperrors.ErrInvalidArgument)
	}
	return value, nil
}

func pipelineConfigText(value string, limit int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= limit && !strings.ContainsFunc(value, unicode.IsControl)
}
