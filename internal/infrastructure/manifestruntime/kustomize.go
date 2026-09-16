package manifestruntime

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"sigs.k8s.io/kustomize/api/krusty"
	kustomizetypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

func renderKustomize(files []documentInput, config *domainmanifest.KustomizeOptions, namespace string) ([]documentInput, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	entryPath := "."
	if config != nil && config.EntryPath != "" {
		entryPath = config.EntryPath
	}
	fs := filesys.MakeFsInMemory()
	for _, file := range files {
		if err := fs.WriteFile(path.Join("workspace", file.path), []byte(file.content)); err != nil {
			return nil, fmt.Errorf("write kustomize input %s: %w", file.path, err)
		}
	}
	entryCount := 0
	for _, file := range files {
		content := []byte(file.content)
		if isKustomizationFile(file.path) {
			var customization kustomizetypes.Kustomization
			if err := customization.Unmarshal(content); err != nil {
				return nil, fmt.Errorf("%w: invalid Kustomization %s: %v", apperrors.ErrInvalidArgument, file.path, err)
			}
			if err := validateLocalKustomization(fs, file.path, customization); err != nil {
				return nil, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
			}
			if path.Dir(file.path) == entryPath {
				entryCount++
				applyKustomizeOptions(&customization, config, namespace)
				var err error
				content, err = json.Marshal(customization)
				if err != nil {
					return nil, fmt.Errorf("encode Kustomization: %w", err)
				}
			}
		}
		if err := fs.WriteFile(path.Join("workspace", file.path), content); err != nil {
			return nil, fmt.Errorf("write kustomize input %s: %w", file.path, err)
		}
	}
	if entryCount != 1 {
		return nil, fmt.Errorf("%w: kustomize entry %s requires exactly one standard Kustomization file", apperrors.ErrInvalidArgument, entryPath)
	}
	// Keep RootOnly and disabled exec/alpha/Helm plugins. Bases may be sibling
	// directories inside the isolated package; individual file loads stay local.
	result, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(fs, path.Join("workspace", entryPath))
	if err != nil {
		return nil, fmt.Errorf("%w: kustomize render failed: %v", apperrors.ErrInvalidArgument, err)
	}
	content, err := result.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("encode kustomize output: %w", err)
	}
	if len(content) > maxManifestBytes {
		return nil, fmt.Errorf("%w: rendered manifest exceeds %d bytes", apperrors.ErrInvalidArgument, maxManifestBytes)
	}
	return []documentInput{{path: path.Join(entryPath, "kustomization.yaml"), content: string(content)}}, nil
}

func isKustomizationFile(name string) bool {
	switch path.Base(name) {
	case "kustomization.yaml", "kustomization.yml", "Kustomization":
		return true
	default:
		return false
	}
}

func applyKustomizeOptions(customization *kustomizetypes.Kustomization, config *domainmanifest.KustomizeOptions, namespace string) {
	customization.FixKustomization()
	if namespace != "" {
		customization.Namespace = namespace
	}
	if config == nil {
		return
	}
	for _, image := range config.Images {
		next := kustomizetypes.Image{Name: image.Name, NewName: image.NewName, Digest: image.Digest}
		replaced := false
		for index := range customization.Images {
			if customization.Images[index].Name == image.Name {
				customization.Images[index] = next
				replaced = true
			}
		}
		if !replaced {
			customization.Images = append(customization.Images, next)
		}
	}
}

func validateLocalKustomization(fs filesys.FileSystem, file string, config kustomizetypes.Kustomization) error {
	if config.HelmGlobals != nil || len(config.HelmCharts)+len(config.HelmChartInflationGenerator) > 0 {
		return fmt.Errorf("%s: Helm-in-Kustomize is unsupported", file)
	}
	if len(config.Generators)+len(config.Transformers)+len(config.Validators) > 0 {
		return fmt.Errorf("%s: custom generators, transformers and validators are unsupported", file)
	}
	paths := make([]string, 0)
	for _, items := range [][]string{config.Resources, config.Bases, config.Components, config.Configurations, config.Crds} { //nolint:staticcheck // Legacy Kustomize fields must undergo the same path validation.
		paths = append(paths, items...)
	}
	paths = append(paths, config.OpenAPI["path"])
	for _, patch := range append(append([]kustomizetypes.Patch{}, config.Patches...), config.PatchesJson6902...) { //nolint:staticcheck // Validate legacy patch paths before rendering.
		paths = append(paths, patch.Path)
	}
	for _, replacement := range config.Replacements {
		paths = append(paths, replacement.Path)
	}
	for _, patch := range config.PatchesStrategicMerge { //nolint:staticcheck // Validate legacy patch paths before rendering.
		// Strategic merge patches also permit an inline YAML document.
		if !strings.ContainsAny(string(patch), "\n{") {
			paths = append(paths, string(patch))
		}
	}
	for _, generator := range config.ConfigMapGenerator {
		paths = append(paths, kustomizeGeneratorPaths(generator.KvPairSources)...)
	}
	if len(config.SecretGenerator) > 0 {
		return fmt.Errorf("%s: Secret generators contain inline secret data; use a secret reference", file)
	}
	for _, input := range paths {
		if input == "" {
			continue
		}
		resolved := path.Clean(path.Join(path.Dir(file), input))
		if path.IsAbs(input) || strings.ContainsAny(input, ":\\\x00") || resolved == ".." || strings.HasPrefix(resolved, "../") {
			return fmt.Errorf("%s: dependencies must stay within the package; remote paths are unsupported", file)
		}
		if !fs.Exists(path.Join("workspace", resolved)) {
			return fmt.Errorf("%s: dependency %s is not in the package; remote paths are unsupported", file, input)
		}
	}
	return nil
}

func kustomizeGeneratorPaths(sources kustomizetypes.KvPairSources) []string {
	paths := append([]string{sources.EnvSource}, sources.EnvSources...)
	for _, file := range sources.FileSources {
		_, value, mapped := strings.Cut(file, "=")
		if mapped {
			file = value
		}
		paths = append(paths, file)
	}
	return paths
}

func verifyKustomizeImages(documents []domainmanifest.RenderedDocument, config *domainmanifest.KustomizeOptions) error {
	if config == nil || len(config.Images) == 0 {
		return nil
	}
	values := make(map[string]bool)
	for _, document := range documents {
		var object any
		if err := json.Unmarshal([]byte(document.Content), &object); err != nil {
			return err
		}
		collectRenderedStrings(object, values)
	}
	for _, image := range config.Images {
		name := image.NewName
		if name == "" {
			name = image.Name
		}
		if !values[name+"@"+image.Digest] {
			return fmt.Errorf("%w: image mapping %s did not produce the requested digest; check the image name or custom transformer configuration", apperrors.ErrInvalidArgument, image.Name)
		}
	}
	return nil
}

func collectRenderedStrings(value any, values map[string]bool) {
	switch value := value.(type) {
	case string:
		values[value] = true
	case []any:
		for _, child := range value {
			collectRenderedStrings(child, values)
		}
	case map[string]any:
		for _, child := range value {
			collectRenderedStrings(child, values)
		}
	}
}
