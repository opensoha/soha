package resourcebackend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	syaml "sigs.k8s.io/yaml"
)

type agentResourceCreator struct {
	client *agentinfra.Client
}

func (a *agentResourceCreator) ResolveCreateManifests(_ context.Context, content string, maxDocuments int) ([]domainresource.ResolvedCreateManifest, error) {
	decoder := kyaml.NewYAMLOrJSONDecoder(strings.NewReader(content), 4096)
	manifests := make([]domainresource.ResolvedCreateManifest, 0, 1)
	for sourceIndex := 0; ; sourceIndex++ {
		var object map[string]any
		if err := decoder.Decode(&object); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("%w: invalid manifest document %d: %v", apperrors.ErrInvalidArgument, sourceIndex, err)
		}
		if len(object) == 0 {
			continue
		}
		if len(manifests) >= maxDocuments {
			return nil, fmt.Errorf("%w: manifest exceeds %d documents", apperrors.ErrInvalidArgument, maxDocuments)
		}
		manifest, err := resolveKnownAgentManifest(len(manifests), object)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("%w: manifest contains no resources", apperrors.ErrInvalidArgument)
	}
	return manifests, nil
}

func resolveKnownAgentManifest(index int, object map[string]any) (domainresource.ResolvedCreateManifest, error) {
	item := &unstructured.Unstructured{Object: object}
	gvk := item.GroupVersionKind()
	if strings.TrimSpace(gvk.Kind) == "" || strings.TrimSpace(gvk.Version) == "" {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: document %d requires apiVersion and kind", apperrors.ErrInvalidArgument, index)
	}
	if strings.EqualFold(strings.TrimSpace(gvk.Kind), "List") {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: document %d kind List is not supported; submit each resource as a separate YAML document", apperrors.ErrInvalidArgument, index)
	}
	if strings.EqualFold(gvk.Kind, "List") {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: kind List is not accepted; submit individual YAML documents", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(item.GetName()) == "" && strings.TrimSpace(item.GetGenerateName()) == "" {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: document %d metadata.name or metadata.generateName is required", apperrors.ErrInvalidArgument, index)
	}
	gvr, namespaced, err := resourceGVRForKind(gvk.Kind)
	if err != nil {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: agent resource creation requires a published known-kind capability for %s", apperrors.ErrUnsupportedOperation, gvk.Kind)
	}
	if !sameGroupVersion(gvr, gvk.GroupVersion()) {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: resource_kind_mismatch: %s does not match canonical %s", apperrors.ErrInvalidArgument, gvk.String(), gvr.GroupVersion().String())
	}
	rendered, err := syaml.Marshal(object)
	if err != nil {
		return domainresource.ResolvedCreateManifest{}, err
	}
	if len(rendered) > domainresource.ResourceCreateMaxDocumentBytes {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: document %d exceeds %d bytes", apperrors.ErrInvalidArgument, index, domainresource.ResourceCreateMaxDocumentBytes)
	}
	name := item.GetName()
	if name == "" {
		name = item.GetGenerateName()
	}
	hash := sha256.Sum256(rendered)
	return domainresource.ResolvedCreateManifest{
		Index: index,
		Ref:   domainresource.ResourceCreateRef{APIVersion: gvr.GroupVersion().String(), Kind: gvk.Kind, Name: name, Namespace: item.GetNamespace(), Namespaced: namespaced},
		Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
		Content: string(rendered), Object: object, ContentHash: hex.EncodeToString(hash[:]),
	}, nil
}

func sameGroupVersion(gvr schema.GroupVersionResource, version schema.GroupVersion) bool {
	return gvr.Group == version.Group && gvr.Version == version.Version
}

func (a *agentResourceCreator) DryRunCreateManifest(ctx context.Context, operationID, clusterID string, manifest domainresource.ResolvedCreateManifest) error {
	return a.client.DryRunCreateManifest(ctx, operationID, clusterID, manifest)
}

func (a *agentResourceCreator) CreateResolvedManifest(ctx context.Context, operationID, clusterID string, manifest domainresource.ResolvedCreateManifest) (domainresource.ResourceYAMLView, error) {
	return a.client.CreateResolvedManifest(ctx, operationID, clusterID, manifest)
}

var _ appresource.AgentResourceCreator = (*agentResourceCreator)(nil)
