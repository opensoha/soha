package manifest

import (
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.yaml.in/yaml/v3"
)

const (
	maxManifestFiles       = 100
	maxManifestFileBytes   = 1 << 20
	maxManifestTotalBytes  = 5 << 20
	maxManifestBindings    = 100
	maxManifestOverlayKeys = 100
)

var namespacePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func normalizeFiles(input []domainmanifest.File) ([]domainmanifest.File, error) {
	if len(input) > maxManifestFiles {
		return nil, fmt.Errorf("%w: manifest package cannot contain more than %d files", apperrors.ErrInvalidArgument, maxManifestFiles)
	}
	files := make([]domainmanifest.File, 0, len(input))
	seenPaths := make(map[string]struct{}, len(input))
	totalBytes := 0
	for _, file := range input {
		filePath := strings.TrimSpace(file.Path)
		if filePath == "" || path.IsAbs(filePath) || filePath == "." || strings.HasPrefix(path.Clean(filePath), "../") {
			return nil, fmt.Errorf("%w: invalid manifest file path", apperrors.ErrInvalidArgument)
		}
		filePath = path.Clean(filePath)
		if _, exists := seenPaths[filePath]; exists {
			return nil, fmt.Errorf("%w: duplicate manifest file path %s", apperrors.ErrInvalidArgument, filePath)
		}
		if len(file.Content) > maxManifestFileBytes {
			return nil, fmt.Errorf("%w: manifest file %s exceeds %d bytes", apperrors.ErrInvalidArgument, filePath, maxManifestFileBytes)
		}
		totalBytes += len(file.Content)
		if totalBytes > maxManifestTotalBytes {
			return nil, fmt.Errorf("%w: manifest package exceeds %d bytes", apperrors.ErrInvalidArgument, maxManifestTotalBytes)
		}
		seenPaths[filePath] = struct{}{}
		files = append(files, domainmanifest.File{Path: filePath, Content: file.Content})
	}
	return files, nil
}

func normalizeBindings(input []domainmanifest.Binding) ([]domainmanifest.Binding, error) {
	if len(input) > maxManifestBindings {
		return nil, fmt.Errorf("%w: manifest package cannot contain more than %d bindings", apperrors.ErrInvalidArgument, maxManifestBindings)
	}
	bindings := make([]domainmanifest.Binding, 0, len(input))
	seenIDs := make(map[string]struct{}, len(input))
	seenTargets := make(map[string]struct{}, len(input))
	for index, binding := range input {
		binding.ID = strings.TrimSpace(binding.ID)
		if binding.ID == "" {
			binding.ID = uuid.NewString()
		}
		binding.ApplicationEnvironmentID = strings.TrimSpace(binding.ApplicationEnvironmentID)
		binding.EnvironmentKey = strings.TrimSpace(binding.EnvironmentKey)
		binding.ClusterID = strings.TrimSpace(binding.ClusterID)
		binding.Namespace = strings.TrimSpace(binding.Namespace)
		if binding.ApplicationEnvironmentID == "" || binding.ClusterID == "" || binding.Namespace == "" {
			return nil, fmt.Errorf("%w: binding %d requires applicationEnvironmentId, clusterId and namespace", apperrors.ErrInvalidArgument, index+1)
		}
		if len(binding.Namespace) > 63 || !namespacePattern.MatchString(binding.Namespace) {
			return nil, fmt.Errorf("%w: binding %d has invalid namespace %s", apperrors.ErrInvalidArgument, index+1, binding.Namespace)
		}
		if len(binding.Overlay) > maxManifestOverlayKeys {
			return nil, fmt.Errorf("%w: binding %d overlay exceeds %d entries", apperrors.ErrInvalidArgument, index+1, maxManifestOverlayKeys)
		}
		if _, exists := seenIDs[binding.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate binding id %s", apperrors.ErrInvalidArgument, binding.ID)
		}
		targetKey := binding.ClusterID + "\x00" + binding.Namespace
		if _, exists := seenTargets[targetKey]; exists {
			return nil, fmt.Errorf("%w: duplicate binding target %s/%s", apperrors.ErrInvalidArgument, binding.ClusterID, binding.Namespace)
		}
		seenIDs[binding.ID] = struct{}{}
		seenTargets[targetKey] = struct{}{}
		binding.Status = "not_deployed"
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

func validateRenderableFiles(item domainmanifest.Package) error {
	if len(item.Files) == 0 {
		return fmt.Errorf("%w: at least one manifest file is required", apperrors.ErrInvalidArgument)
	}
	foundKustomization := false
	for _, file := range item.Files {
		entry := item.Renderer == domainmanifest.RendererKustomize && isKustomizationPath(file.Path)
		documents, err := validateYAMLDocuments(file, item.Renderer == domainmanifest.RendererRaw || entry)
		if err != nil {
			return err
		}
		if documents == 0 {
			return fmt.Errorf("%w: manifest file %s contains no YAML documents", apperrors.ErrInvalidArgument, file.Path)
		}
		if entry {
			foundKustomization = true
		}
	}
	if item.Renderer == domainmanifest.RendererKustomize && !foundKustomization {
		return fmt.Errorf("%w: kustomize renderer requires kustomization.yaml", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateYAMLDocuments(file domainmanifest.File, requireKubernetesObject bool) (int, error) {
	decoder := yaml.NewDecoder(strings.NewReader(file.Content))
	documents := 0
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("%w: invalid YAML in %s: %v", apperrors.ErrInvalidArgument, file.Path, err)
		}
		if len(document.Content) == 0 || document.Content[0].Kind == yaml.ScalarNode && document.Content[0].Tag == "!!null" {
			continue
		}
		documents++
		if !requireKubernetesObject {
			continue
		}
		var object map[string]any
		if err := document.Decode(&object); err != nil {
			return 0, fmt.Errorf("%w: YAML document in %s must be an object", apperrors.ErrInvalidArgument, file.Path)
		}
		apiVersion, _ := object["apiVersion"].(string)
		kind, _ := object["kind"].(string)
		if strings.TrimSpace(apiVersion) == "" || strings.TrimSpace(kind) == "" {
			return 0, fmt.Errorf("%w: YAML document in %s requires apiVersion and kind", apperrors.ErrInvalidArgument, file.Path)
		}
		if isKustomizationPath(file.Path) && !strings.EqualFold(kind, "Kustomization") {
			return 0, fmt.Errorf("%w: %s must declare kind Kustomization", apperrors.ErrInvalidArgument, file.Path)
		}
	}
	return documents, nil
}

func isKustomizationPath(filePath string) bool {
	base := strings.ToLower(path.Base(filePath))
	return base == "kustomization.yaml" || base == "kustomization.yml"
}
