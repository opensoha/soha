package manifest

import (
	"encoding/json"
	"go.yaml.in/yaml/v3"
	"io"
	"strings"
)

// Configuration keeps the CR YAML read boundary when copied to revisions,
// delivery plans or asynchronous task receipts. Unclassifiable input is private.
func SensitiveFiles(files []File) bool {
	for _, file := range files {
		decoder := yaml.NewDecoder(strings.NewReader(file.Content))
		exhausted := true
		for index := 0; index < 1000; index++ {
			var object map[string]any
			err := decoder.Decode(&object)
			if err == io.EOF {
				exhausted = false
				break
			}
			if err != nil || sensitiveObject(object, 0) {
				return true
			}
		}
		if exhausted {
			return true
		}
	}
	return false
}
func SensitiveDocuments(documents []RenderedDocument) bool {
	files := make([]File, 0, len(documents))
	for _, document := range documents {
		files = append(files, File{Content: document.Content})
	}
	return SensitiveFiles(files)
}
func SensitiveTaskPayload(payload map[string]any) bool {
	if payload["rootResource"] != nil {
		return true
	}
	value, present := payload["documents"]
	if !present {
		return false
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return true
	}
	var documents []RenderedDocument
	if json.Unmarshal(encoded, &documents) != nil {
		return true
	}
	return SensitiveDocuments(documents)
}
func sensitiveObject(object map[string]any, depth int) bool {
	if depth > 8 {
		return true
	}
	apiVersion, _ := object["apiVersion"].(string)
	kind, _ := object["kind"].(string)
	if kind != "" && !builtinManifestKind(apiVersion, kind) {
		return true
	}
	if kind == "List" {
		items, ok := object["items"].([]any)
		if !ok || len(items) > 500 {
			return true
		}
		for _, item := range items {
			value, ok := item.(map[string]any)
			if !ok || sensitiveObject(value, depth+1) {
				return true
			}
		}
	}
	return false
}
func builtinManifestKind(apiVersion, kind string) bool {
	var kinds string
	switch apiVersion {
	case "v1":
		kinds = "|List|ConfigMap|Pod|Service|Endpoints|PersistentVolumeClaim|PersistentVolume|Namespace|ServiceAccount|ReplicationController|ResourceQuota|LimitRange|Node|Event|"
	case "apps/v1":
		kinds = "|Deployment|StatefulSet|DaemonSet|ReplicaSet|ControllerRevision|"
	case "batch/v1":
		kinds = "|Job|CronJob|"
	case "networking.k8s.io/v1":
		kinds = "|Ingress|IngressClass|NetworkPolicy|"
	case "autoscaling/v1", "autoscaling/v2":
		kinds = "|HorizontalPodAutoscaler|"
	case "policy/v1":
		kinds = "|PodDisruptionBudget|"
	case "rbac.authorization.k8s.io/v1":
		kinds = "|Role|ClusterRole|RoleBinding|ClusterRoleBinding|"
	case "storage.k8s.io/v1":
		kinds = "|StorageClass|CSIDriver|CSINode|VolumeAttachment|"
	case "kustomize.config.k8s.io/v1beta1":
		kinds = "|Kustomization|"
	default:
		return false
	}
	return strings.Contains(kinds, "|"+kind+"|")
}

func SensitiveKind(apiVersion, kind string) bool { return !builtinManifestKind(apiVersion, kind) }
