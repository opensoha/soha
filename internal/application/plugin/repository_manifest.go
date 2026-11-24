package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"

	plugincontracts "github.com/opensoha/soha/contracts/plugins"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var repositoryManifestSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	const location = "https://contracts.opensoha.dev/plugins/plugin-manifest.schema.json"
	var document map[string]any
	reader := json.NewDecoder(bytes.NewReader(plugincontracts.ManifestSchema()))
	reader.UseNumber()
	err := reader.Decode(&document)
	if err != nil {
		return nil, err
	}
	properties, ok := document["properties"].(map[string]any)
	if !ok {
		return nil, errors.New("plugin manifest schema properties are missing")
	}
	// Repository distribution does not accept companion packs or their asset metadata.
	properties["companionPack"] = false
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
})

func decodeRepositoryManifest(raw []byte) (domainplugin.PluginManifest, error) {
	var manifest domainplugin.PluginManifest
	schema, err := repositoryManifestSchema()
	if err != nil {
		return manifest, err
	}
	invalid := apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_manifest_invalid", "The repository root plugin manifest is invalid.", "仓库根目录的插件清单无效。")
	reader := json.NewDecoder(bytes.NewReader(raw))
	reader.UseNumber()
	var value any
	if reader.Decode(&value) != nil || reader.Decode(new(any)) != io.EOF ||
		json.Unmarshal(raw, &manifest) != nil {
		return domainplugin.PluginManifest{}, invalid
	}
	if manifest.Type == "companion-pack" || manifest.CompanionPack != nil ||
		(manifest.Runtime != nil && manifest.Runtime.Mode != "" && manifest.Runtime.Mode != "manifest-only" && manifest.Runtime.Mode != "executable-package-v1") {
		return domainplugin.PluginManifest{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_runtime_unsupported", "This runtime is not supported by repository installation.", "仓库安装不支持此运行时。")
	}
	if schema.Validate(value) != nil {
		return domainplugin.PluginManifest{}, invalid
	}
	manifest = withoutManifestTrustClaims(manifest)
	if err := validateManifest(manifest); err != nil {
		return domainplugin.PluginManifest{}, invalid
	}
	return manifest, nil
}
