package manifestruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.yaml.in/yaml/v3"
)

// RenderDeploymentTemplate replaces complete YAML value nodes. Parameters never
// become YAML syntax, file names, map keys, or executable template expressions.
func (r *Renderer) RenderDeploymentTemplate(ctx context.Context, source domaincatalog.DeploymentTemplateSource, parameters map[string]any, system, artifacts map[string]string) (domaincatalog.DeploymentTemplateSource, error) {
	data, err := json.Marshal(source)
	if err != nil || len(data) > 5<<20 {
		return source, fmt.Errorf("%w: template source exceeds 5 MiB", apperrors.ErrInvalidArgument)
	}
	var result domaincatalog.DeploymentTemplateSource
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	systemValues := stringMapValues(system)
	if serviceKey := system["serviceKey"]; serviceKey != "" {
		name := serviceKey + "-preview"
		if len(name) > 63 {
			digest := sha256.Sum256([]byte(serviceKey))
			name = fmt.Sprintf("%s-%x-preview", strings.TrimRight(serviceKey[:46], "-"), digest[:4])
		}
		systemValues["previewServiceName"] = name
	}
	variables := map[string]any{"parameters": parameters, "system": systemValues, "artifacts": stringMapValues(artifacts)}
	if result.Helm != nil {
		result.Helm.Values, err = renderTemplateValues(ctx, result.Helm.Values, variables)
		return result, err
	}
	if result.Git != nil {
		return result, nil
	} // Fixed Git content is fetched and validated when creating the final plan.
	prepared, err := prepareFiles(result.Files, nil)
	if err != nil {
		return result, err
	}
	result.Files = make([]domainmanifest.File, 0, len(prepared))
	for _, file := range prepared {
		content, err := renderTemplateFile(ctx, file.path, file.content, variables)
		if err != nil {
			return result, err
		}
		result.Files = append(result.Files, domainmanifest.File{Path: file.path, Content: content})
	}
	return result, r.validateTemplateResources(ctx, result, system["namespace"])
}

func (r *Renderer) validateTemplateResources(ctx context.Context, source domaincatalog.DeploymentTemplateSource, namespace string) error {
	result, err := r.Render(ctx, domainmanifest.Package{Renderer: source.Renderer},
		domainmanifest.EnvironmentBinding{Namespace: namespace, Kustomize: source.Kustomize}, source.Files, 0)
	if err != nil {
		return err
	}
	for _, document := range result.Documents {
		if clusterScopedKind(document.Kind) || document.Namespace != namespace {
			return fmt.Errorf("%w: deployment template resource escapes the target namespace", apperrors.ErrAccessDenied)
		}
	}
	return nil
}

func stringMapValues(values map[string]string) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func renderTemplateValues(ctx context.Context, values map[string]any, variables map[string]any) (map[string]any, error) {
	var node yaml.Node
	if err := node.Encode(values); err != nil {
		return nil, err
	}
	if err := replaceTemplateNode(ctx, &node, variables, nil, 0); err != nil {
		return nil, err
	}
	var result map[string]any
	if err := node.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func renderTemplateFile(ctx context.Context, name, content string, variables map[string]any) (string, error) {
	if ext := strings.ToLower(path.Ext(name)); ext != ".yaml" && ext != ".yml" && path.Base(name) != "Kustomization" {
		if strings.Contains(content, "${{") {
			return "", fmt.Errorf("%w: template parameters require a YAML file", apperrors.ErrInvalidArgument)
		}
		return content, nil
	}
	var result bytes.Buffer
	decoder := yaml.NewDecoder(strings.NewReader(content))
	encoder := yaml.NewEncoder(&result)
	encoder.SetIndent(2)
	for index := 0; ; index++ {
		var node yaml.Node
		if err := decoder.Decode(&node); err == io.EOF {
			break
		} else if err != nil {
			return "", fmt.Errorf("%w: invalid YAML template %s", apperrors.ErrInvalidArgument, name)
		}
		if index >= maxManifestDocuments {
			return "", fmt.Errorf("%w: too many template documents", apperrors.ErrInvalidArgument)
		}
		if err := replaceTemplateNode(ctx, &node, variables, nil, 0); err != nil {
			return "", err
		}
		if err := encoder.Encode(&node); err != nil {
			return "", err
		}
		if result.Len() > maxManifestBytes {
			return "", fmt.Errorf("%w: rendered template is too large", apperrors.ErrInvalidArgument)
		}
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return result.String(), nil
}

func replaceTemplateNode(ctx context.Context, node *yaml.Node, variables map[string]any, location []string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 64 || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("%w: YAML templates cannot use aliases or excessive nesting", apperrors.ErrInvalidArgument)
	}
	if node.Kind == yaml.ScalarNode {
		return replaceTemplateScalar(node, variables, location)
	}
	for index, child := range node.Content {
		next := location
		if node.Kind == yaml.MappingNode {
			if index%2 == 0 {
				if child.Kind != yaml.ScalarNode || strings.Contains(child.Value, "${{") {
					return fmt.Errorf("%w: template map keys must be literal strings", apperrors.ErrInvalidArgument)
				}
				continue
			}
			next = append(append([]string(nil), location...), node.Content[index-1].Value)
		}
		if err := replaceTemplateNode(ctx, child, variables, next, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func replaceTemplateScalar(node *yaml.Node, variables map[string]any, location []string) error {
	if !strings.Contains(node.Value, "${{") {
		return nil
	}
	text := strings.TrimSpace(node.Value)
	if !strings.HasPrefix(text, "${{") || !strings.HasSuffix(text, "}}") {
		return fmt.Errorf("%w: template expressions must occupy the entire YAML value", apperrors.ErrInvalidArgument)
	}
	key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "${{"), "}}"))
	if templateIdentityField(location) && !strings.HasPrefix(key, "system.") {
		return fmt.Errorf("%w: resource identity can only use system variables", apperrors.ErrInvalidArgument)
	}
	var value any = variables
	for _, part := range strings.Split(key, ".") {
		object, ok := value.(map[string]any)
		if !ok || part == "" {
			return fmt.Errorf("%w: unknown template variable %q", apperrors.ErrInvalidArgument, key)
		}
		value, ok = object[part]
		if !ok {
			return fmt.Errorf("%w: unknown template variable %q", apperrors.ErrInvalidArgument, key)
		}
	}
	return node.Encode(value)
}

func templateIdentityField(location []string) bool {
	if len(location) == 0 {
		return true
	}
	last := location[len(location)-1]
	if last == "apiVersion" || last == "kind" || last == "metadata" {
		return true
	}
	return len(location) > 1 && location[len(location)-2] == "metadata" && (last == "name" || last == "namespace")
}
