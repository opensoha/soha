package resourcebackend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	syaml "sigs.k8s.io/yaml"
)

const agentCreateDiscoveryTimeout = 8 * time.Second
const agentCreateMaxCRDs = 10000

type agentResourceCreationClient interface {
	ListCRDs(context.Context) ([]domainresource.CRDView, error)
	DryRunCreateManifest(context.Context, string, string, domainresource.ResolvedCreateManifest) error
	CreateResolvedManifest(context.Context, string, string, domainresource.ResolvedCreateManifest) (domainresource.ResourceYAMLView, error)
}

type agentResourceCreator struct {
	client agentResourceCreationClient
}

type agentCreateDiscoveryFailure struct{ cause error }

func (e agentCreateDiscoveryFailure) Error() string { return "Agent CRD discovery is unavailable" }
func (e agentCreateDiscoveryFailure) Unwrap() []error {
	return []error{apperrors.ErrUnsupportedOperation, e.cause}
}

type agentCreateDiscovery struct {
	authorize appresource.AuthorizeCustomCreateDiscovery
	client    agentResourceCreationClient
	loaded    bool
	items     []domainresource.CRDView
}

func (d *agentCreateDiscovery) resolve(ctx context.Context, gvk schema.GroupVersionKind, namespace string) (schema.GroupVersionResource, bool, error) {
	if gvr, namespaced, err := knownResourceGVRForKind(gvk.Kind); err == nil && gvr.Group == gvk.Group {
		// The Agent's existing discovery-backed dry-run verifies that this version is served.
		gvr.Version = gvk.Version
		return gvr, namespaced, nil
	}
	if d.authorize == nil {
		return schema.GroupVersionResource{}, false, apperrors.ErrAccessDenied
	}
	if err := d.authorize(ctx, gvk.GroupVersion().String(), gvk.Kind, namespace); err != nil {
		return schema.GroupVersionResource{}, false, err
	}
	if !d.loaded {
		if d.client == nil {
			return schema.GroupVersionResource{}, false, fmt.Errorf("%w: Agent CRD discovery is unavailable", apperrors.ErrUnsupportedOperation)
		}
		queryCtx, cancel := context.WithTimeout(ctx, agentCreateDiscoveryTimeout)
		defer cancel()
		items, err := d.client.ListCRDs(queryCtx)
		if err != nil {
			return schema.GroupVersionResource{}, false, agentCreateDiscoveryFailure{cause: err}
		}
		if len(items) > agentCreateMaxCRDs {
			return schema.GroupVersionResource{}, false, fmt.Errorf("%w: Agent CRD discovery exceeds resource budget", apperrors.ErrUnsupportedOperation)
		}
		d.items, d.loaded = items, true
	}
	return resolveAgentCRD(d.items, gvk)
}

func resolveAgentCRD(items []domainresource.CRDView, gvk schema.GroupVersionKind) (schema.GroupVersionResource, bool, error) {
	var found *domainresource.CRDView
	for i := range items {
		item := &items[i]
		if item.Group != gvk.Group || item.Kind != gvk.Kind || !slices.Contains(item.Versions, gvk.Version) {
			continue
		}
		if found != nil || !validAgentCRDMapping(*item) {
			return schema.GroupVersionResource{}, false, fmt.Errorf("%w: Agent CRD discovery returned an ambiguous or invalid resource mapping", apperrors.ErrInvalidArgument)
		}
		found = item
	}
	if found == nil {
		return schema.GroupVersionResource{}, false, fmt.Errorf("%w: Agent has no discovered CRD mapping for %s", apperrors.ErrUnsupportedOperation, gvk.String())
	}
	return schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: found.Plural}, found.Scope == "Namespaced", nil
}

func validAgentCRDMapping(item domainresource.CRDView) bool {
	return item.Group != "" && len(validation.IsDNS1123Subdomain(item.Group)) == 0 &&
		len(validation.IsDNS1035Label(item.Plural)) == 0 && item.Name == item.Plural+"."+item.Group &&
		(item.Scope == "Namespaced" || item.Scope == "Cluster")
}

func (a *agentResourceCreator) ResolveCreateManifests(ctx context.Context, content string, maxDocuments int, authorize appresource.AuthorizeCustomCreateDiscovery) ([]domainresource.ResolvedCreateManifest, error) {
	discovery := agentCreateDiscovery{client: a.client, authorize: authorize}
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
		manifest, err := resolveAgentManifest(ctx, &discovery, len(manifests), object)
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

func resolveAgentManifest(ctx context.Context, discovery *agentCreateDiscovery, index int, object map[string]any) (domainresource.ResolvedCreateManifest, error) {
	item := &unstructured.Unstructured{Object: object}
	gvk := item.GroupVersionKind()
	if strings.TrimSpace(gvk.Kind) == "" || strings.TrimSpace(gvk.Version) == "" {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: document %d requires apiVersion and kind", apperrors.ErrInvalidArgument, index)
	}
	if strings.EqualFold(strings.TrimSpace(gvk.Kind), "List") {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: document %d kind List is not supported; submit each resource as a separate YAML document", apperrors.ErrInvalidArgument, index)
	}
	if strings.TrimSpace(item.GetName()) == "" && strings.TrimSpace(item.GetGenerateName()) == "" {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: document %d metadata.name or metadata.generateName is required", apperrors.ErrInvalidArgument, index)
	}
	rendered, err := syaml.Marshal(object)
	if err != nil {
		return domainresource.ResolvedCreateManifest{}, err
	}
	if len(rendered) > domainresource.ResourceCreateMaxDocumentBytes {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: document %d exceeds %d bytes", apperrors.ErrInvalidArgument, index, domainresource.ResourceCreateMaxDocumentBytes)
	}
	gvr, namespaced, err := discovery.resolve(ctx, gvk, item.GetNamespace())
	if err != nil {
		return domainresource.ResolvedCreateManifest{}, err
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

func (a *agentResourceCreator) DryRunCreateManifest(ctx context.Context, operationID, clusterID string, manifest domainresource.ResolvedCreateManifest) error {
	return a.client.DryRunCreateManifest(ctx, operationID, clusterID, manifest)
}

func (a *agentResourceCreator) CreateResolvedManifest(ctx context.Context, operationID, clusterID string, manifest domainresource.ResolvedCreateManifest) (domainresource.ResourceYAMLView, error) {
	return a.client.CreateResolvedManifest(ctx, operationID, clusterID, manifest)
}

var _ appresource.AgentResourceCreator = (*agentResourceCreator)(nil)
