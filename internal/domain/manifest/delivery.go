package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// DeliveryConfiguration freezes configuration before a build has produced artifacts.
// It carries no rendered plan or approval and never starts a preflight task.
type DeliveryConfiguration struct {
	PackageID           string   `json:"packageId"`
	BindingID           string   `json:"bindingId"`
	Revision            int      `json:"revision"`
	ConfigurationDigest string   `json:"configurationDigest"`
	ResourceKeys        []string `json:"resourceKeys"`
}

// DeliverySnapshot is created by the server and retained through preflight,
// approval, execution and subsequent observation of the same desired state.
type DeliverySnapshot struct {
	GitOpsDocuments          []RenderedDocument     `json:"gitOpsDocuments,omitempty"`
	TemplateInputs           *ServiceTemplateInputs `json:"templateInputs,omitempty"`
	DeliveryPlanID           string                 `json:"deliveryPlanId"`
	TargetID                 string                 `json:"targetId"`
	PackageID                string                 `json:"packageId"`
	ServiceID                string                 `json:"serviceId,omitempty"`
	BindingID                string                 `json:"bindingId"`
	BindingVersion           int64                  `json:"bindingVersion"`
	ApplicationEnvironmentID string                 `json:"applicationEnvironmentId"`
	ClusterID                string                 `json:"clusterId"`
	Namespace                string                 `json:"namespace"`
	Revision                 int                    `json:"revision"`
	RevisionDigest           string                 `json:"revisionDigest"`
	SourceCommit             string                 `json:"sourceCommit,omitempty"`
	PackageUpdatedAt         time.Time              `json:"packageUpdatedAt"`
	RendererVersion          string                 `json:"rendererVersion"`
	InputDigest              string                 `json:"inputDigest"`
	RenderedDigest           string                 `json:"renderedDigest"`
	Documents                []RenderedDocument     `json:"documents"`
	PreflightTaskID          string                 `json:"preflightTaskId"`
	ExpectedGeneration       int64                  `json:"expectedGeneration"`
}

type ServiceTemplateInputs struct {
	ServiceVersion  int64             `json:"serviceVersion"`
	TemplateID      string            `json:"templateId"`
	TemplateVersion int64             `json:"templateVersion"`
	TemplateDigest  string            `json:"templateDigest"`
	Parameters      map[string]any    `json:"parameters"`
	ArtifactImages  map[string]string `json:"artifactImages"`
	ReleaseBundleID string            `json:"releaseBundleId,omitempty"`
}

// DeliveryArtifacts is supplied by the authorized delivery service after checking
// bundle provenance. It is not accepted from a client-rendered snapshot.
type DeliveryArtifacts struct {
	ReleaseBundleID string
	ServiceID       string
	ContainerImages map[string]string
}

func RenderInputDigest(renderer string, binding EnvironmentBinding, files []File) (string, error) {
	files = slices.Clone(files)
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	input := struct {
		Renderer  string
		Namespace string
		Overlay   map[string]string
		Kustomize *KustomizeOptions
		Files     []File
	}{renderer, binding.Namespace, binding.Overlay, binding.Kustomize, files}
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// ResourceKeys ignore API version because versions in the same group address the
// same Kubernetes object. Sorting also makes frozen identity comparison stable.
func ResourceKeys(clusterID string, groups ...[]RenderedDocument) []string {
	keys := make([]string, 0)
	for _, documents := range groups {
		for _, document := range documents {
			group, _, hasGroup := strings.Cut(document.APIVersion, "/")
			if !hasGroup {
				group = ""
			}
			keys = append(keys, strings.Join([]string{clusterID, group, document.Kind, document.Namespace, document.Name}, "/"))
		}
	}
	slices.Sort(keys)
	return keys
}
