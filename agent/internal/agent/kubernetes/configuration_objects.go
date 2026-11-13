package kubernetes

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (c *Client) GetConfigMapDetail(ctx context.Context, namespace, name string) (domainresource.ConfigMapDetailView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	return mapConfigMapDetail(*item), nil
}

func (c *Client) UpdateConfigMapData(ctx context.Context, namespace, name string, data, binaryData map[string]string) (domainresource.ConfigMapDetailView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	if item.Immutable != nil && *item.Immutable {
		return domainresource.ConfigMapDetailView{}, apierrors.NewBadRequest("ConfigMap is immutable")
	}
	decoded := make(map[string][]byte, len(binaryData))
	for key, value := range binaryData {
		raw, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return domainresource.ConfigMapDetailView{}, apierrors.NewBadRequest(fmt.Sprintf("invalid binaryData.%s", key))
		}
		decoded[key] = raw
	}
	item.Data, item.BinaryData = data, decoded
	updated, err := c.typed.CoreV1().ConfigMaps(namespace).Update(ctx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	return mapConfigMapDetail(*updated), nil
}

func (c *Client) GetSecretDetail(ctx context.Context, namespace, name string) (domainresource.SecretDetailView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	return mapSecretDetail(*item), nil
}

// Input values are plain text; detail values follow Kubernetes Base64 encoding.
func (c *Client) UpdateSecretData(ctx context.Context, namespace, name string, data map[string]string) (domainresource.SecretDetailView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return domainresource.SecretDetailView{}, err
	}
	if item.Immutable != nil && *item.Immutable {
		return domainresource.SecretDetailView{}, apierrors.NewBadRequest("Secret is immutable")
	}
	item.Data = make(map[string][]byte, len(data))
	item.StringData = nil
	for key, value := range data {
		item.Data[key] = []byte(value)
	}
	updated, err := c.typed.CoreV1().Secrets(namespace).Update(ctx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	return mapSecretDetail(*updated), nil
}

func encodeData(data map[string][]byte) map[string]string {
	out := make(map[string]string, len(data))
	for key, value := range data {
		out[key] = base64.StdEncoding.EncodeToString(value)
	}
	return out
}
func mapConfigMapDetail(item corev1.ConfigMap) domainresource.ConfigMapDetailView {
	return domainresource.ConfigMapDetailView{Name: item.Name, Namespace: item.Namespace, Labels: item.Labels, Annotations: item.Annotations, Data: item.Data, BinaryData: encodeData(item.BinaryData), Immutable: item.Immutable != nil && *item.Immutable, CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}
func mapSecretDetail(item corev1.Secret) domainresource.SecretDetailView {
	return domainresource.SecretDetailView{Name: item.Name, Namespace: item.Namespace, Type: string(item.Type), Labels: item.Labels, Annotations: item.Annotations, Data: encodeData(item.Data), Immutable: item.Immutable != nil && *item.Immutable, CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func (c *Client) ListConfigReferences(ctx context.Context, namespace, name string, configMap bool) ([]domainresource.ConfigReferenceView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	targets := []struct {
		kind string
		gvr  schema.GroupVersionResource
		spec []string
	}{
		{"Pod", schema.GroupVersionResource{Version: "v1", Resource: "pods"}, []string{"spec"}},
		{"ReplicationController", schema.GroupVersionResource{Version: "v1", Resource: "replicationcontrollers"}, []string{"spec", "template", "spec"}},
		{"Deployment", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, []string{"spec", "template", "spec"}},
		{"StatefulSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, []string{"spec", "template", "spec"}},
		{"DaemonSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, []string{"spec", "template", "spec"}},
		{"ReplicaSet", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, []string{"spec", "template", "spec"}},
		{"Job", schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, []string{"spec", "template", "spec"}},
		{"CronJob", schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, []string{"spec", "jobTemplate", "spec", "template", "spec"}},
	}
	refs := make([]domainresource.ConfigReferenceView, 0)
	for _, target := range targets {
		items, err := c.dynamic.Resource(target.gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, item := range items.Items {
			spec, _, err := unstructured.NestedMap(item.Object, target.spec...)
			if err != nil {
				return nil, err
			}
			for _, path := range configReferencePaths(spec, name, configMap) {
				refs = append(refs, domainresource.ConfigReferenceView{Kind: target.kind, Name: item.GetName(), Namespace: item.GetNamespace(), Path: path})
			}
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		return a.Kind+"/"+a.Name+"/"+a.Path < b.Kind+"/"+b.Name+"/"+b.Path
	})
	return refs, nil
}

// Scan only PodSpec reference fields, never configuration data values.
func configReferencePaths(spec map[string]any, name string, configMap bool) []string {
	paths := make([]string, 0)
	var visit func(any, string)
	visit = func(value any, path string) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				next := strings.TrimPrefix(path+"/"+key, "/")
				if configReferenceMatches(key, child, name, configMap) {
					paths = append(paths, next)
				}
				visit(child, next)
			}
		case []any:
			for i, child := range value {
				visit(child, fmt.Sprintf("%s/%d", path, i))
			}
		}
	}
	if !configMap {
		if refs, ok := spec["imagePullSecrets"].([]any); ok {
			for i, ref := range refs {
				if item, ok := ref.(map[string]any); ok && item["name"] == name {
					paths = append(paths, fmt.Sprintf("imagePullSecrets/%d", i))
				}
			}
		}
	}
	visit(spec, "")
	sort.Strings(paths)
	return paths
}
func configReferenceMatches(key string, value any, name string, configMap bool) bool {
	item, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if configMap {
		return (key == "configMap" || key == "configMapRef" || key == "configMapKeyRef") && item["name"] == name
	}
	return ((key == "secretRef" || key == "secretKeyRef") && item["name"] == name) || (key == "secret" && (item["name"] == name || item["secretName"] == name))
}
