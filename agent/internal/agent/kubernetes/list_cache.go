package kubernetes

import (
	"context"
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

const cacheEntryCountAnnotation = "cache.soha.internal/data-entries"

// Lists are eventually consistent. Detail and mutation paths continue to read
// live objects. A failed/unsynced/stopped informer must never look like an empty list.
func cachedResourceItems[T any](ctx context.Context, c *Client, kind, namespace string) ([]T, bool) {
	if ctx.Err() != nil || c.resourceEvents == nil || c.resourceEvents.stopped.Load() {
		return nil, false
	}
	for _, spec := range c.resourceEvents.informers {
		if spec.kind != kind {
			continue
		}
		if !spec.informer.HasSynced() || !spec.healthy.Load() {
			return nil, false
		}
		objects := []T{}
		valid := true
		err := cache.ListAllByNamespace(spec.informer.GetIndexer(), namespace, labels.Everything(), func(obj any) {
			object, ok := obj.(runtime.Object)
			if !ok {
				valid = false
				return
			}
			copyObject, ok := any(object.DeepCopyObject()).(*T)
			if !ok {
				valid = false
				return
			}
			objects = append(objects, *copyObject)
		})
		if err != nil || !valid {
			return nil, false
		}
		sort.Slice(objects, func(i, j int) bool {
			left, _ := meta.Accessor(&objects[i])
			right, _ := meta.Accessor(&objects[j])
			return left.GetNamespace()+"/"+left.GetName() < right.GetNamespace()+"/"+right.GetName()
		})
		return objects, true
	}
	return nil, false
}

func configurationCacheKind(kind string) bool { return kind == "ConfigMap" || kind == "Secret" }

// Retain only the summary count, never configuration values or Secret data.
func configurationCacheMetadata(obj any) any {
	switch item := obj.(type) {
	case *corev1.ConfigMap:
		copyItem := item.DeepCopy()
		copyItem.Annotations = map[string]string{cacheEntryCountAnnotation: strconv.Itoa(len(item.Data) + len(item.BinaryData))}
		copyItem.ManagedFields = nil
		copyItem.Data, copyItem.BinaryData = nil, nil
		return copyItem
	case *corev1.Secret:
		copyItem := item.DeepCopy()
		copyItem.Annotations = map[string]string{cacheEntryCountAnnotation: strconv.Itoa(len(item.Data))}
		copyItem.ManagedFields = nil
		copyItem.Data, copyItem.StringData = nil, nil
		return copyItem
	default:
		return obj
	}
}
