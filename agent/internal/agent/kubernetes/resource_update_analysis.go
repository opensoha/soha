package kubernetes

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const resourceEditFieldManager = "opensoha-resource-edit/v1"

func analyzeResourceUpdate(live, desired *unstructured.Unstructured) domainresource.ResourceUpdateAnalysis {
	analysis := domainresource.ResourceUpdateAnalysis{FieldManager: resourceEditFieldManager, ChangedFields: []string{}, Owners: []domainresource.ManagedFieldOwner{}, Conflicts: []domainresource.FieldConflict{}}
	collectResourceChangedFields(desired.Object, live.Object, "", &analysis.ChangedFields)
	sort.Strings(analysis.ChangedFields)
	for _, entry := range live.GetManagedFields() {
		owner := domainresource.ManagedFieldOwner{Manager: entry.Manager, Operation: string(entry.Operation), APIVersion: entry.APIVersion, Fields: managedFieldPaths(entry.FieldsV1)}
		if len(owner.Fields) > 256 {
			owner.Fields = owner.Fields[:256]
		}
		if entry.Time != nil {
			owner.Time = entry.Time.UTC().Format(time.RFC3339Nano)
		}
		if owner.Manager != "" {
			analysis.Owners = append(analysis.Owners, owner)
		}
	}
	return analysis
}

func collectResourceChangedFields(desired, observed any, path string, fields *[]string) {
	desiredJSON, _ := json.Marshal(desired)
	observedJSON, _ := json.Marshal(observed)
	if string(desiredJSON) == string(observedJSON) {
		return
	}
	desiredMap, desiredOK := desired.(map[string]any)
	observedMap, observedOK := observed.(map[string]any)
	if desiredOK && observedOK {
		keySet := make(map[string]struct{}, len(desiredMap)+len(observedMap))
		for key := range desiredMap {
			keySet[key] = struct{}{}
		}
		for key := range observedMap {
			keySet[key] = struct{}{}
		}
		keys := make([]string, 0, len(keySet))
		for key := range keySet {
			if key == "status" || (path == "/metadata" && isServerMetadataField(key)) {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			collectResourceChangedFields(desiredMap[key], observedMap[key], path+"/"+escapeJSONPointer(key), fields)
		}
		return
	}
	if path != "" {
		*fields = append(*fields, path)
	}
}

func isServerMetadataField(key string) bool {
	switch key {
	case "resourceVersion", "uid", "managedFields", "creationTimestamp", "generation":
		return true
	default:
		return false
	}
}

func managedFieldPaths(fields *metav1.FieldsV1) []string {
	if fields == nil || len(fields.Raw) == 0 {
		return []string{}
	}
	var tree map[string]any
	if err := json.Unmarshal(fields.Raw, &tree); err != nil {
		return []string{}
	}
	paths := []string{}
	collectManagedFieldPaths(tree, "", &paths)
	sort.Strings(paths)
	return paths
}

func collectManagedFieldPaths(tree map[string]any, path string, paths *[]string) {
	if len(tree) == 0 {
		if path != "" {
			*paths = append(*paths, path)
		}
		return
	}
	keys := make([]string, 0, len(tree))
	for key := range tree {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		child, _ := tree[key].(map[string]any)
		next := path
		if strings.HasPrefix(key, "f:") {
			next += "/" + escapeJSONPointer(strings.TrimPrefix(key, "f:"))
		} else if strings.HasPrefix(key, "k:") || strings.HasPrefix(key, "v:") {
			next += "/*"
		} else if key == "." {
			if next != "" {
				*paths = append(*paths, next)
			}
			continue
		}
		collectManagedFieldPaths(child, next, paths)
	}
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func conflictsFromError(message string, analysis domainresource.ResourceUpdateAnalysis) []domainresource.FieldConflict {
	for _, changed := range analysis.ChangedFields {
		for _, owner := range analysis.Owners {
			if owner.Manager == resourceEditFieldManager {
				continue
			}
			for _, owned := range owner.Fields {
				if changed == owned || strings.HasPrefix(changed, owned+"/") || strings.HasPrefix(owned, changed+"/") {
					return []domainresource.FieldConflict{{Field: changed, Manager: owner.Manager, Message: message}}
				}
			}
		}
	}
	return []domainresource.FieldConflict{{Field: "metadata.managedFields", Message: message}}
}
