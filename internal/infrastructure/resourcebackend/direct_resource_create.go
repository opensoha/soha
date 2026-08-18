package resourcebackend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/restmapper"
	syaml "sigs.k8s.io/yaml"
)

func (d *Direct) ResolveCreateManifests(ctx context.Context, clusterID, content string, maxDocuments int) ([]domainresource.ResolvedCreateManifest, error) {
	if maxDocuments <= 0 {
		return nil, fmt.Errorf("%w: document limit must be positive", apperrors.ErrInvalidArgument)
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	groupResources, err := restmapper.GetAPIGroupResources(bundle.Discovery)
	if err != nil {
		return nil, fmt.Errorf("resolve Kubernetes discovery: %w", err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)
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
		manifest, err := resolveCreateManifest(mapper, len(manifests), object)
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

func resolveCreateManifest(mapper meta.RESTMapper, index int, object map[string]any) (domainresource.ResolvedCreateManifest, error) {
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
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("%w: resolve document %d %s: %v", apperrors.ErrInvalidArgument, index, gvk.String(), err)
	}
	rendered, err := syaml.Marshal(object)
	if err != nil {
		return domainresource.ResolvedCreateManifest{}, fmt.Errorf("marshal document %d: %w", index, err)
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
		Ref: domainresource.ResourceCreateRef{
			APIVersion: gvk.GroupVersion().String(), Kind: gvk.Kind, Name: name,
			Namespace: item.GetNamespace(), Namespaced: mapping.Scope.Name() == meta.RESTScopeNameNamespace,
		},
		Group: mapping.Resource.Group, Version: mapping.Resource.Version, Resource: mapping.Resource.Resource,
		Content: string(rendered), Object: object, ContentHash: hex.EncodeToString(hash[:]),
	}, nil
}

func (d *Direct) DryRunCreateManifest(ctx context.Context, clusterID string, manifest domainresource.ResolvedCreateManifest, namespace string) error {
	_, err := d.createResolvedManifest(ctx, clusterID, manifest, namespace, []string{metav1.DryRunAll})
	return err
}

func (d *Direct) CreateResolvedManifest(ctx context.Context, clusterID string, manifest domainresource.ResolvedCreateManifest, namespace string) (domainresource.ResourceYAMLView, error) {
	return d.createResolvedManifest(ctx, clusterID, manifest, namespace, nil)
}

func (d *Direct) createResolvedManifest(ctx context.Context, clusterID string, manifest domainresource.ResolvedCreateManifest, namespace string, dryRun []string) (domainresource.ResourceYAMLView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var object map[string]any
	if err := syaml.Unmarshal([]byte(manifest.Content), &object); err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: invalid resolved manifest: %v", apperrors.ErrInvalidArgument, err)
	}
	item := &unstructured.Unstructured{Object: object}
	if manifest.Ref.Namespaced {
		if strings.TrimSpace(namespace) == "" {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: namespace_required: target namespace is required", apperrors.ErrInvalidArgument)
		}
		item.SetNamespace(strings.TrimSpace(namespace))
	} else {
		item.SetNamespace("")
	}
	item.SetResourceVersion("")
	gvr := schema.GroupVersionResource{Group: manifest.Group, Version: manifest.Version, Resource: manifest.Resource}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	created, err := dynamicResource(bundle.Dynamic, gvr, manifest.Ref.Namespaced, item.GetNamespace()).Create(queryCtx, item, metav1.CreateOptions{DryRun: dryRun})
	if err != nil {
		return domainresource.ResourceYAMLView{}, kubernetesCreateError(manifest.Ref.Kind, err)
	}
	unstructured.RemoveNestedField(created.Object, "metadata", "managedFields")
	rendered, err := syaml.Marshal(created.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: created.GetKind(), Name: created.GetName(), Namespace: created.GetNamespace(), Content: string(rendered)}, nil
}

func kubernetesCreateError(kind string, err error) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "resource"
	}
	category := apperrors.ErrInvalidArgument
	code := "resource_validation_failed"
	englishMessage := fmt.Sprintf("Kubernetes could not validate the %s resource", kind)
	chineseMessage := fmt.Sprintf("Kubernetes 无法校验 %s 资源", kind)

	var statusError k8serrors.APIStatus
	if errors.As(err, &statusError) {
		status := statusError.Status()
		if status.Details != nil && strings.TrimSpace(status.Details.Kind) != "" {
			kind = strings.TrimSpace(status.Details.Kind)
		}
		target := kind
		if status.Details != nil && strings.TrimSpace(status.Details.Name) != "" {
			target += "/" + strings.TrimSpace(status.Details.Name)
		}
		if status.Details != nil && len(status.Details.Causes) > 0 {
			cause := status.Details.Causes[0]
			englishReason, chineseReason := kubernetesCauseReason(cause.Type)
			if field := strings.TrimSpace(cause.Field); field != "" {
				englishMessage = fmt.Sprintf("Kubernetes rejected %s: field %s %s", target, field, englishReason)
				chineseMessage = fmt.Sprintf("Kubernetes 拒绝了 %s：字段 %s%s", target, field, chineseReason)
			} else {
				englishMessage = fmt.Sprintf("Kubernetes rejected %s: %s", target, englishReason)
				chineseMessage = fmt.Sprintf("Kubernetes 拒绝了 %s：%s", target, chineseReason)
			}
		}
	}
	if k8serrors.IsAlreadyExists(err) {
		category = apperrors.ErrConflict
		code = "resource_already_exists"
	} else if k8serrors.IsForbidden(err) {
		category = apperrors.ErrAccessDenied
		code = "resource_create_denied"
	}
	return fmt.Errorf("%w: %v", apperrors.NewBusiness(category, code, englishMessage, chineseMessage), err)
}

func kubernetesCauseReason(cause metav1.CauseType) (string, string) {
	switch cause {
	case metav1.CauseTypeFieldValueNotFound:
		return "references a value that was not found", "引用的值不存在"
	case metav1.CauseTypeFieldValueRequired:
		return "is required", "为必填项"
	case metav1.CauseTypeFieldValueDuplicate:
		return "contains a duplicate value", "包含重复值"
	case metav1.CauseTypeFieldValueNotSupported:
		return "uses an unsupported value", "使用了不支持的值"
	case metav1.CauseTypeForbidden:
		return "is not allowed", "不允许使用该值"
	case metav1.CauseTypeTooLong:
		return "is too long", "长度超出限制"
	case metav1.CauseTypeTooMany:
		return "contains too many items", "条目数量超出限制"
	case metav1.CauseTypeTypeInvalid:
		return "has the wrong value type", "值类型不正确"
	default:
		return "has an invalid value", "的值不正确"
	}
}

var _ appresource.DirectResourceCreator = (*Direct)(nil)
