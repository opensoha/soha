package manifestruntime

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/kustomize/api/krusty"
	kustomizetypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

const (
	maxManifestDocuments = 50
	maxManifestBytes     = 2 << 20
)

type Renderer struct{}

func NewRenderer() *Renderer { return &Renderer{} }

func (*Renderer) Render(_ context.Context, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, files []domainmanifest.File, revision int) (domainmanifest.RenderResult, error) {
	if len(files) == 0 {
		return domainmanifest.RenderResult{}, fmt.Errorf("%w: manifest files are required", apperrors.ErrInvalidArgument)
	}
	prepared, err := prepareFiles(files, binding.Overlay)
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	var inputs []documentInput
	switch item.Renderer {
	case domainmanifest.RendererRaw:
		inputs = prepared
	case domainmanifest.RendererKustomize:
		inputs, err = renderKustomize(prepared)
	default:
		err = fmt.Errorf("%w: unsupported manifest renderer %q", apperrors.ErrInvalidArgument, item.Renderer)
	}
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	documents, diagnostics, err := decodeDocuments(inputs, binding.Namespace)
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	sort.SliceStable(documents, func(i, j int) bool {
		return documentKey(documents[i]) < documentKey(documents[j])
	})
	for index := range documents {
		documents[index].Index = index
	}
	digest := digestDocuments(documents)
	return domainmanifest.RenderResult{
		PackageID: item.ID, BindingID: binding.ID, Revision: revision, Renderer: item.Renderer,
		RenderedDigest: digest, Documents: documents, Diagnostics: diagnostics,
	}, nil
}

type documentInput struct {
	path    string
	content string
}

func prepareFiles(files []domainmanifest.File, overlay map[string]string) ([]documentInput, error) {
	items := make([]documentInput, 0, len(files))
	total := 0
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		name := path.Clean(strings.TrimSpace(file.Path))
		if name == "." || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("%w: manifest file path escapes package root", apperrors.ErrInvalidArgument)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("%w: duplicate manifest file path %s", apperrors.ErrInvalidArgument, name)
		}
		seen[name] = struct{}{}
		content := applyOverlay(file.Content, overlay)
		total += len(content)
		if total > maxManifestBytes {
			return nil, fmt.Errorf("%w: rendered manifest exceeds %d bytes", apperrors.ErrInvalidArgument, maxManifestBytes)
		}
		items = append(items, documentInput{path: name, content: content})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].path < items[j].path })
	return items, nil
}

func applyOverlay(content string, overlay map[string]string) string {
	keys := make([]string, 0, len(overlay))
	for key := range overlay {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		content = strings.ReplaceAll(content, "${"+key+"}", overlay[key])
	}
	return content
}

func renderKustomize(files []documentInput) ([]documentInput, error) {
	fs := filesys.MakeFsInMemory()
	for _, file := range files {
		if err := fs.WriteFile(path.Join("workspace", file.path), []byte(file.content)); err != nil {
			return nil, fmt.Errorf("write kustomize input %s: %w", file.path, err)
		}
	}
	options := krusty.MakeDefaultOptions()
	options.LoadRestrictions = kustomizetypes.LoadRestrictionsRootOnly
	result, err := krusty.MakeKustomizer(options).Run(fs, "workspace")
	if err != nil {
		return nil, fmt.Errorf("%w: kustomize render failed: %v", apperrors.ErrInvalidArgument, err)
	}
	content, err := result.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("encode kustomize output: %w", err)
	}
	return []documentInput{{path: "kustomization.yaml", content: string(content)}}, nil
}

func decodeDocuments(inputs []documentInput, targetNamespace string) ([]domainmanifest.RenderedDocument, []domainmanifest.Diagnostic, error) {
	documents := make([]domainmanifest.RenderedDocument, 0)
	diagnostics := make([]domainmanifest.Diagnostic, 0)
	for _, input := range inputs {
		reader := kyaml.NewYAMLReader(bufio.NewReader(strings.NewReader(input.content)))
		for {
			raw, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, fmt.Errorf("%w: read %s: %v", apperrors.ErrInvalidArgument, input.path, err)
			}
			if len(bytes.TrimSpace(raw)) == 0 {
				continue
			}
			if len(documents) >= maxManifestDocuments {
				return nil, nil, fmt.Errorf("%w: rendered manifest exceeds %d documents", apperrors.ErrInvalidArgument, maxManifestDocuments)
			}
			document, diagnostic, err := decodeDocument(input.path, raw, targetNamespace)
			if err != nil {
				return nil, nil, err
			}
			documents = append(documents, document)
			if diagnostic != nil {
				diagnostics = append(diagnostics, *diagnostic)
			}
		}
	}
	return documents, diagnostics, nil
}

func decodeDocument(filePath string, raw []byte, targetNamespace string) (domainmanifest.RenderedDocument, *domainmanifest.Diagnostic, error) {
	jsonDocument, err := kyaml.ToJSON(raw)
	if err != nil {
		return domainmanifest.RenderedDocument{}, nil, fmt.Errorf("%w: invalid YAML in %s: %v", apperrors.ErrInvalidArgument, filePath, err)
	}
	var object map[string]any
	if err := json.Unmarshal(jsonDocument, &object); err != nil {
		return domainmanifest.RenderedDocument{}, nil, fmt.Errorf("%w: invalid resource in %s: %v", apperrors.ErrInvalidArgument, filePath, err)
	}
	apiVersion := stringValue(object["apiVersion"])
	kind := stringValue(object["kind"])
	metadata, _ := object["metadata"].(map[string]any)
	name := stringValue(metadata["name"])
	namespace := stringValue(metadata["namespace"])
	if apiVersion == "" || kind == "" || name == "" {
		return domainmanifest.RenderedDocument{}, nil, fmt.Errorf("%w: %s requires apiVersion, kind, and metadata.name", apperrors.ErrInvalidArgument, filePath)
	}
	if strings.EqualFold(kind, "Secret") && (nonEmptyMap(object["data"]) || nonEmptyMap(object["stringData"])) {
		return domainmanifest.RenderedDocument{}, nil, fmt.Errorf("%w: Secret %s/%s contains inline secret data; use a secret reference", apperrors.ErrInvalidArgument, namespace, name)
	}
	var diagnostic *domainmanifest.Diagnostic
	if namespace == "" && !clusterScopedKind(kind) {
		namespace = strings.TrimSpace(targetNamespace)
		metadata["namespace"] = namespace
		object["metadata"] = metadata
		diagnostic = &domainmanifest.Diagnostic{Stage: "scope", Severity: "info", Code: "namespace_defaulted", Message: "metadata.namespace defaulted from the manifest binding", Path: filePath, Kind: kind, Namespace: namespace, Name: name}
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return domainmanifest.RenderedDocument{}, nil, fmt.Errorf("canonicalize manifest document: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return domainmanifest.RenderedDocument{
		Path: filePath, Content: string(canonical), ContentDigest: hex.EncodeToString(sum[:]),
		APIVersion: apiVersion, Kind: kind, Namespace: namespace, Name: name,
	}, diagnostic, nil
}

func digestDocuments(items []domainmanifest.RenderedDocument) string {
	hash := sha256.New()
	for _, item := range items {
		_, _ = io.WriteString(hash, documentKey(item))
		_, _ = io.WriteString(hash, "\x00"+item.ContentDigest+"\n")
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func documentKey(item domainmanifest.RenderedDocument) string {
	return strings.Join([]string{item.APIVersion, item.Kind, item.Namespace, item.Name, item.Path}, "\x00")
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func nonEmptyMap(value any) bool {
	item, ok := value.(map[string]any)
	return ok && len(item) > 0
}

func clusterScopedKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "namespace", "node", "persistentvolume", "storageclass", "clusterrole", "clusterrolebinding", "customresourcedefinition", "mutatingwebhookconfiguration", "validatingwebhookconfiguration", "priorityclass", "runtimeclass", "ingressclass", "gatewayclass":
		return true
	default:
		return false
	}
}
