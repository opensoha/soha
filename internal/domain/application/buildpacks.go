package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var pinnedBuildpacksImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[a-f0-9]{64}$`)
var buildpacksProcess = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// BuildpacksConfiguration validates the typed source boundary, including callers
// which do not pass through the HTTP schema validator.
func BuildpacksConfiguration(config map[string]any) (sohaapi.BuildpacksConfiguration, error) {
	var source sohaapi.BuildSourceConfig
	encoded, err := json.Marshal(config)
	if err != nil {
		return sohaapi.BuildpacksConfiguration{}, fmt.Errorf("%w: invalid Buildpacks configuration", apperrors.ErrInvalidArgument)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil || source.Buildpacks == nil {
		return sohaapi.BuildpacksConfiguration{}, fmt.Errorf("%w: typed Buildpacks configuration is required", apperrors.ErrInvalidArgument)
	}
	value := *source.Buildpacks
	if !pinnedBuildpacksImage.MatchString(value.BuilderImage) || !pinnedBuildpacksImage.MatchString(value.RunImage) || !value.Platform.Valid() {
		return value, fmt.Errorf("%w: Buildpacks requires pinned builder and run images and a supported Linux platform", apperrors.ErrInvalidArgument)
	}
	if value.ProcessType != "" && !buildpacksProcess.MatchString(value.ProcessType) {
		return value, fmt.Errorf("%w: invalid Buildpacks process type", apperrors.ErrInvalidArgument)
	}
	if source.ProviderKind != "" || source.BuilderKind != "" || source.DockerfilePath != "" || source.BuildTemplateID != "" || source.PipelineRef != "" || source.ExternalPipeline != nil || len(source.BuildArgs) > 0 {
		return value, fmt.Errorf("%w: Buildpacks cannot override the runner or use Dockerfile, template, pipeline, or Docker build arguments", apperrors.ErrInvalidArgument)
	}
	dir := source.ContextDir
	if strings.ContainsAny(dir, "\\\x00\r\n") || path.IsAbs(dir) || path.Clean(dir) == ".." || strings.HasPrefix(path.Clean(dir), "../") {
		return value, fmt.Errorf("%w: Buildpacks context must remain inside the checked out workspace", apperrors.ErrInvalidArgument)
	}
	return value, nil
}
