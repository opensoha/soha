package build

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func platformTemplateExecutionCommands(source *domainapp.BuildSource, metadata map[string]any, imageRef string) ([]string, error) {
	parameters := mergeBuildMetadata(maps.Clone(metadataMap(source.Config, "variables")), metadataMap(metadata, "variables"))
	values, err := domaincatalog.BuildVariables(metadataMap(metadata, "buildTemplateVariableSchema"), metadataMap(metadata, "buildTemplateDefaultVariables"), parameters, true)
	if err != nil {
		return nil, templateInputError()
	}
	metadata["variables"] = values
	literals := mergeBuildMetadata(maps.Clone(values), map[string]any{
		"IMAGE_REF": imageRef, "CONTEXT_DIR": firstNonEmptyString(configString(source, "contextDir"), "."),
		"DOCKERFILE_PATH": firstNonEmptyString(configString(source, "dockerfilePath"), "Dockerfile"),
	})
	commands := valueStringSlice(metadata["buildTemplateCommands"])
	if raw, ok := metadata["buildTemplateCommands"].([]any); ok {
		commands = nonEmptyStrings(raw)
	}
	var prepare []string
	if content := metadataString(metadata, "buildTemplateDockerfileTemplate"); content != "" {
		const dockerfile = ".soha-template.Dockerfile"
		literals["DOCKERFILE_PATH"] = dockerfile
		rendered, err := domaincatalog.RenderBuildLiterals(content, literals)
		if err != nil {
			return nil, templateInputError()
		}
		metadata["renderedDockerfile"] = rendered
		// Refuse an existing file or checkout symlink instead of overwriting it.
		prepare = append(prepare, "(set -C; printf '%s\\n' "+shellQuote(rendered)+" > "+shellQuote(dockerfile)+")")
		if len(commands) == 0 {
			copySource := *source
			copySource.Config = mergeBuildMetadata(maps.Clone(source.Config), map[string]any{"dockerfilePath": dockerfile})
			if configString(&copySource, "builderKind") == "" {
				kind := metadataString(metadata, "buildTemplateBuilderKind")
				if kind != "kaniko" && kind != "buildx" {
					kind = "docker"
				}
				copySource.Config["builderKind"] = kind
			}
			commands = containerBuildExecutionCommands(&copySource, imageRef, metadataMap(metadata, "buildArgs"))
		}
	}
	if len(commands) == 0 {
		return nil, templateInputError()
	}
	for _, command := range commands {
		rendered, err := domaincatalog.RenderBuildLiterals(command, literals)
		if err != nil {
			return nil, templateInputError()
		}
		prepare = append(prepare, buildTemplateEnvironment(values)+rendered)
	}
	return prepare, nil
}

func buildTemplateEnvironment(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, key := range keys {
		out.WriteString("export SOHA_BUILD_" + key + "=" + shellQuote(fmt.Sprint(values[key])) + "\n")
	}
	return out.String()
}

func templateInputError() error {
	return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "build_template_parameters_invalid",
		"Check the build template variables and placeholders. Free text must use a quoted SOHA_BUILD variable; credentials require secret leases.",
		"请检查构建模板的必填变量、类型与占位符。自由文本请使用带引号的 SOHA_BUILD 变量，凭据请使用密钥租约。")
}
