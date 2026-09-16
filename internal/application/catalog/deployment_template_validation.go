package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.yaml.in/yaml/v3"
)

var (
	deploymentTemplateKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	deploymentServiceKeyPattern  = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	deploymentCommitPattern      = regexp.MustCompile(`^([a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)
	deploymentDigestPattern      = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

func normalizeDeploymentTemplate(input *domaincatalog.DeploymentTemplateSpec) error {
	input.Key, input.Name = strings.TrimSpace(input.Key), strings.TrimSpace(input.Name)
	if !deploymentTemplateKeyPattern.MatchString(input.Key) || len(input.Key) > 128 || input.Name == "" || len(input.Name) > 200 || len(input.Description) > 4096 {
		return fmt.Errorf("%w: invalid template key, name or description", apperrors.ErrInvalidArgument)
	}
	if !slices.Contains([]string{"workload_ready", "job_complete", "configuration_only"}, input.Health.Mode) || input.Health.TimeoutSeconds < 1 || input.Health.TimeoutSeconds > 3600 {
		return fmt.Errorf("%w: invalid template health requirement", apperrors.ErrInvalidArgument)
	}
	if input.Defaults == nil {
		input.Defaults = map[string]any{}
	}
	defaultsSchema := input.ParameterSchema
	defaultsSchema.Required = nil // Required values may be supplied when creating a service.
	if _, err := input.ParameterSchema.Compile(); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	if _, err := defaultsSchema.ResolveParameters(input.Defaults, nil, nil, nil); err != nil {
		return fmt.Errorf("%w: template defaults: %v", apperrors.ErrInvalidArgument, err)
	}
	if err := validateDeploymentTemplateBindings(*input); err != nil {
		return err
	}
	return validateDeploymentTemplateSource(*input)
}

func validateDeploymentTemplateBindings(input domaincatalog.DeploymentTemplateSpec) error {
	if len(input.EnvironmentOverrides) > 64 || len(input.Artifacts) > 32 {
		return fmt.Errorf("%w: too many template overrides or artifacts", apperrors.ErrInvalidArgument)
	}
	for index, key := range input.EnvironmentOverrides {
		if _, exists := input.ParameterSchema.Properties[key]; !exists || slices.Contains(input.EnvironmentOverrides[:index], key) {
			return fmt.Errorf("%w: environment override must name a unique declared parameter", apperrors.ErrInvalidArgument)
		}
	}
	for key, container := range input.Artifacts {
		if !deploymentTemplateKeyPattern.MatchString(key) || container == "" || len(key) > 128 || len(container) > 128 {
			return fmt.Errorf("%w: invalid artifact mapping", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}

func validateDeploymentTemplateSource(input domaincatalog.DeploymentTemplateSpec) error {
	source := input.Source
	if data, err := json.Marshal(source); err != nil || len(data) > 2<<20 {
		return fmt.Errorf("%w: template source exceeds 2 MiB", apperrors.ErrInvalidArgument)
	}
	switch source.Renderer {
	case "raw_yaml":
		if len(source.Files) == 0 || source.Helm != nil || source.Git != nil || source.Kustomize != nil {
			return fmt.Errorf("%w: YAML source requires files only", apperrors.ErrInvalidArgument)
		}
	case "kustomize":
		if err := validateDeploymentTemplateKustomize(input); err != nil {
			return err
		}
	case "helm":
		if source.Helm == nil || source.Git != nil || source.Kustomize != nil || len(source.Files) != 0 {
			return fmt.Errorf("%w: Helm source cannot include manifest or Kustomize fields", apperrors.ErrInvalidArgument)
		}
		if err := validateDeploymentTemplateHelm(*source.Helm); err != nil {
			return err
		}
		var node yaml.Node
		if err := node.Encode(source.Helm.Values); err != nil {
			return err
		}
		return validateTemplateExpressions(&node, input, 0)
	default:
		return fmt.Errorf("%w: unsupported deployment renderer", apperrors.ErrInvalidArgument)
	}
	return validateDeploymentTemplateFiles(input)
}

func validateDeploymentTemplateKustomize(input domaincatalog.DeploymentTemplateSpec) error {
	source := input.Source
	if source.Helm != nil || source.Kustomize == nil || (len(source.Files) == 0) == (source.Git == nil) {
		return fmt.Errorf("%w: Kustomize requires an entry and either files or a fixed Git source", apperrors.ErrInvalidArgument)
	}
	if err := source.Kustomize.Validate(); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	if source.Git != nil && (strings.TrimSpace(source.Git.RepositoryID) == "" || !deploymentCommitPattern.MatchString(source.Git.Commit) || !validTemplatePath(source.Git.Path, true)) {
		return fmt.Errorf("%w: Git source requires repositoryId, an exact commit and a relative path", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateDeploymentTemplateHelm(input domaincatalog.DeploymentTemplateHelm) error {
	repository, err := url.Parse(input.RepositoryURL)
	if err != nil || repository.Host == "" || (repository.Scheme != "https" && repository.Scheme != "oci") || repository.User != nil || repository.RawQuery != "" || repository.Fragment != "" {
		return fmt.Errorf("%w: Helm repository requires HTTPS or OCI without inline credentials", apperrors.ErrInvalidArgument)
	}
	if input.Chart == "" || !validTemplatePath(input.Chart, false) || input.Version == "" || input.Version == "latest" || strings.ContainsAny(input.Version, "*<>=^~ \t\r\n") || len(input.Version) > 128 {
		return fmt.Errorf("%w: Helm chart and an exact version are required", apperrors.ErrInvalidArgument)
	}
	if input.Digest != "" && !deploymentDigestPattern.MatchString(input.Digest) {
		return fmt.Errorf("%w: invalid Helm digest", apperrors.ErrInvalidArgument)
	}
	if input.Values == nil {
		return fmt.Errorf("%w: Helm values must be an object", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validTemplatePath(value string, allowRoot bool) bool {
	clean := path.Clean(value)
	return value != "" && len(value) <= 512 && !path.IsAbs(value) && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.ContainsAny(value, ":\\\x00") && (allowRoot || clean != ".")
}

func validateDeploymentTemplateFiles(input domaincatalog.DeploymentTemplateSpec) error {
	if len(input.Source.Files) > 100 {
		return fmt.Errorf("%w: template exceeds 100 files", apperrors.ErrInvalidArgument)
	}
	seen := make(map[string]bool, len(input.Source.Files))
	for _, file := range input.Source.Files {
		if !validTemplatePath(file.Path, false) || seen[path.Clean(file.Path)] || len(file.Content) > 524288 {
			return fmt.Errorf("%w: invalid, duplicate or oversized template file", apperrors.ErrInvalidArgument)
		}
		seen[path.Clean(file.Path)] = true
		if err := validateDeploymentTemplateFile(file.Path, file.Content, input); err != nil {
			return err
		}
	}
	return nil
}

func validateDeploymentTemplateFile(name, content string, input domaincatalog.DeploymentTemplateSpec) error {
	if ext := strings.ToLower(path.Ext(name)); ext != ".yaml" && ext != ".yml" && path.Base(name) != "Kustomization" {
		if input.Source.Renderer == "raw_yaml" || strings.Contains(content, "${{") {
			return fmt.Errorf("%w: template expressions require YAML files", apperrors.ErrInvalidArgument)
		}
		return nil
	}
	decoder := yaml.NewDecoder(strings.NewReader(content))
	for index := 0; ; index++ {
		var node yaml.Node
		if err := decoder.Decode(&node); err == io.EOF {
			return nil
		} else if err != nil || index >= 50 {
			return fmt.Errorf("%w: invalid YAML template or too many documents", apperrors.ErrInvalidArgument)
		}
		if err := validateTemplateExpressions(&node, input, 0); err != nil {
			return err
		}
	}
}

func validateTemplateExpressions(node *yaml.Node, input domaincatalog.DeploymentTemplateSpec, depth int) error {
	if depth > 64 || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("%w: template aliases or excessive nesting are unsupported", apperrors.ErrInvalidArgument)
	}
	if node.Kind == yaml.ScalarNode && strings.Contains(node.Value, "${{") {
		value := strings.TrimSpace(node.Value)
		if !strings.HasPrefix(value, "${{") || !strings.HasSuffix(value, "}}") || !templateVariableDeclared(strings.TrimSpace(value[3:len(value)-2]), input) {
			return fmt.Errorf("%w: template expression must contain one declared variable", apperrors.ErrInvalidArgument)
		}
	}
	for index, child := range node.Content {
		if node.Kind == yaml.MappingNode && index%2 == 0 && (child.Kind != yaml.ScalarNode || strings.Contains(child.Value, "${{")) {
			return fmt.Errorf("%w: template keys must be literal strings", apperrors.ErrInvalidArgument)
		}
		if err := validateTemplateExpressions(child, input, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func templateVariableDeclared(value string, input domaincatalog.DeploymentTemplateSpec) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	switch parts[0] {
	case "system":
		return len(parts) == 2 && slices.Contains([]string{"applicationId", "serviceKey", "namespace", "previewServiceName"}, parts[1])
	case "artifacts":
		_, ok := input.Artifacts[parts[1]]
		return len(parts) == 2 && ok
	case "parameters":
		schema := input.ParameterSchema
		for _, part := range parts[1:] {
			if schema.Type != "object" || part == "" {
				return false
			}
			child, ok := schema.Properties[part]
			if !ok {
				if schema.MapValues == nil {
					return false
				}
				child = *schema.MapValues
			}
			schema = child
		}
		return true
	}
	return false
}
