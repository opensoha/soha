package resource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"sigs.k8s.io/yaml"
)

func (r *Runtime) AuthorizeImportManagement(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	_, _, err := r.Workloads.authorize(ctx, principal, clusterID, namespace, "Deployment", domainaccess.ActionUpdate)
	return err
}

// GetImportSource reuses the authorized workload/CR read paths but never exposes their YAML.
func (r *Runtime) GetImportSource(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, crdName string) (domainresource.ResourceSourceSnapshot, error) {
	source, _, err := r.readImportSource(ctx, principal, clusterID, namespace, kind, name, crdName)
	return source, err
}

func (r *Runtime) GetApplicationSourceYAML(ctx context.Context, principal domainidentity.Principal, expected domainresource.ResourceSourceSnapshot) (domainresource.ResourceYAMLView, error) {
	kind := expected.Resource.Kind
	if expected.CRDName != "" {
		if err := r.Workloads.authorizeRuntimePermission(ctx, principal, appaccess.PermPlatformConfigurationSecretDataView); err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		kind = "CustomResource"
	}
	source, item, err := r.readImportSource(ctx, principal, expected.Resource.ClusterID, expected.Resource.Namespace, kind, expected.Resource.Name, expected.CRDName)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if source.Resource.UID != expected.Resource.UID || source.Resource.APIVersion != expected.Resource.APIVersion || source.Resource.Kind != expected.Resource.Kind {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: linked source identity changed; reassociation requires review", apperrors.ErrConflict)
	}
	return item, nil
}

func (r *Runtime) readImportSource(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, crdName string) (domainresource.ResourceSourceSnapshot, domainresource.ResourceYAMLView, error) {
	var item domainresource.ResourceYAMLView
	var err error
	apiVersion, plural := "apps/v1", ""
	switch kind {
	case "Deployment":
		item, err = r.GetDeploymentYAML(ctx, principal, clusterID, namespace, name)
	case "StatefulSet":
		item, err = r.GetStatefulSetYAML(ctx, principal, clusterID, namespace, name)
	case "DaemonSet":
		item, err = r.GetDaemonSetYAML(ctx, principal, clusterID, namespace, name)
	case "Job":
		apiVersion = "batch/v1"
		item, err = r.GetJobYAML(ctx, principal, clusterID, namespace, name)
	case "CronJob":
		apiVersion = "batch/v1"
		item, err = r.GetCronJobYAML(ctx, principal, clusterID, namespace, name)
	case "CustomResource":
		return r.customResources.importSource(ctx, principal, clusterID, namespace, name, crdName)
	default:
		return domainresource.ResourceSourceSnapshot{}, item, fmt.Errorf("%w: unsupported import kind", apperrors.ErrInvalidArgument)
	}
	if err != nil {
		return domainresource.ResourceSourceSnapshot{}, item, err
	}
	source, err := sourceSnapshot(clusterID, namespace, apiVersion, kind, name, crdName, plural, item.Content)
	return source, item, err
}

func sourceSnapshot(clusterID, namespace, apiVersion, kind, name, crdName, plural, content string) (domainresource.ResourceSourceSnapshot, error) {
	invalid := func() (domainresource.ResourceSourceSnapshot, error) {
		return domainresource.ResourceSourceSnapshot{}, fmt.Errorf("%w: source identity is missing or does not match the requested resource", apperrors.ErrConflict)
	}
	if len(content) == 0 || len(content) > 2*1024*1024 {
		return invalid()
	}
	raw, err := yaml.YAMLToJSON([]byte(content))
	if err != nil {
		return invalid()
	}
	var object struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name            string                                  `json:"name"`
			Namespace       string                                  `json:"namespace"`
			UID             string                                  `json:"uid"`
			ResourceVersion string                                  `json:"resourceVersion"`
			Generation      int64                                   `json:"generation"`
			Owners          []domainresource.ResourceOwnerReference `json:"ownerReferences"`
		} `json:"metadata"`
	}
	if json.Unmarshal(raw, &object) != nil || object.Metadata.UID == "" || object.Metadata.Generation < 0 || !validSourceOwners(object.Metadata.Owners) {
		return invalid()
	}
	if object.Metadata.Name != name || object.Metadata.Namespace != namespace || (object.Kind != "" && object.Kind != kind) || (object.APIVersion != "" && object.APIVersion != apiVersion) {
		return invalid()
	}
	if crdName != "" && (object.Kind == "" || object.APIVersion == "" || object.Metadata.Generation <= 0) {
		return invalid()
	}
	digest, err := sourceSpecDigest(raw, apiVersion, kind, crdName != "")
	if err != nil {
		return invalid()
	}
	return domainresource.ResourceSourceSnapshot{
		Replicas: sourceReplicaObservation(raw, kind, object.Metadata.Generation), ExternallyManaged: externalImportManager(raw),
		Resource:   domainresource.ResourceRef{ClusterID: clusterID, APIVersion: apiVersion, Kind: kind, Name: name, Namespace: namespace, ScopeMode: domainresource.ResourceScopeModeNamespace, UID: object.Metadata.UID, ResourceVersion: object.Metadata.ResourceVersion},
		SpecDigest: digest, Generation: object.Metadata.Generation, Owners: object.Metadata.Owners,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), CRDName: crdName, Plural: plural,
	}, nil
}

func externalImportManager(raw []byte) bool {
	var object struct {
		Metadata struct {
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if json.Unmarshal(raw, &object) != nil {
		return true
	}
	manager := strings.ToLower(object.Metadata.Labels["app.kubernetes.io/managed-by"])
	if manager != "" && manager != "soha" {
		return true
	}
	for key := range object.Metadata.Annotations {
		if strings.HasPrefix(key, "meta.helm.sh/") || strings.HasPrefix(key, "argocd.argoproj.io/") {
			return true
		}
	}
	for key := range object.Metadata.Labels {
		if strings.HasSuffix(key, ".toolkit.fluxcd.io/name") || key == "argocd.argoproj.io/instance" {
			return true
		}
	}
	return false
}

func sourceReplicaObservation(raw []byte, kind string, generation int64) *domainresource.SourceReplicaObservation {
	var object struct {
		Spec struct {
			Replicas *int32 `json:"replicas"`
		} `json:"spec"`
		Status struct {
			ObservedGeneration     int64 `json:"observedGeneration"`
			ReadyReplicas          int32 `json:"readyReplicas"`
			UpdatedReplicas        int32 `json:"updatedReplicas"`
			AvailableReplicas      int32 `json:"availableReplicas"`
			DesiredNumberScheduled int32 `json:"desiredNumberScheduled"`
			NumberReady            int32 `json:"numberReady"`
			UpdatedNumberScheduled int32 `json:"updatedNumberScheduled"`
			NumberAvailable        int32 `json:"numberAvailable"`
		} `json:"status"`
	}
	if json.Unmarshal(raw, &object) != nil || object.Status.ObservedGeneration < generation {
		return nil
	}
	if kind == "DaemonSet" {
		return &domainresource.SourceReplicaObservation{Desired: object.Status.DesiredNumberScheduled, Ready: object.Status.NumberReady, Updated: object.Status.UpdatedNumberScheduled, Available: object.Status.NumberAvailable}
	}
	if (kind != "Deployment" && kind != "StatefulSet") || object.Spec.Replicas == nil {
		return nil
	}
	return &domainresource.SourceReplicaObservation{Desired: *object.Spec.Replicas, Ready: object.Status.ReadyReplicas, Updated: object.Status.UpdatedReplicas, Available: object.Status.AvailableReplicas}
}

func validSourceOwners(owners []domainresource.ResourceOwnerReference) bool {
	if len(owners) > 32 {
		return false
	}
	for _, owner := range owners {
		if owner.UID == "" || owner.Name == "" || owner.Kind == "" || owner.APIVersion == "" {
			return false
		}
	}
	return true
}

func sourceSpecDigest(raw []byte, apiVersion, kind string, customResource bool) (string, error) {
	var configuration map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&configuration); err != nil {
		return "", err
	}
	if configuration == nil {
		return "", fmt.Errorf("%w: source must be an object", apperrors.ErrInvalidArgument)
	}
	metadata, _ := configuration["metadata"].(map[string]any)
	if customResource {
		// CR generation fences spec changes without exposing a guessable digest of secret values.
		configuration = map[string]any{"metadata": map[string]any{
			"uid": metadata["uid"], "generation": metadata["generation"], "ownerReferences": metadata["ownerReferences"],
		}}
	} else {
		ownership := map[string]any{}
		for _, field := range []string{"labels", "annotations", "ownerReferences"} {
			if value, ok := metadata[field]; ok {
				ownership[field] = value
			}
		}
		delete(configuration, "status")
		configuration["metadata"] = ownership
	}
	configuration["apiVersion"], configuration["kind"] = apiVersion, kind
	canonical, err := json.Marshal(configuration)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}

func (c *CustomResources) importSource(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, crdName string) (domainresource.ResourceSourceSnapshot, domainresource.ResourceYAMLView, error) {
	if strings.TrimSpace(crdName) == "" || strings.TrimSpace(namespace) == "" {
		return domainresource.ResourceSourceSnapshot{}, domainresource.ResourceYAMLView{}, fmt.Errorf("%w: an explicit CRD and namespace are required", apperrors.ErrInvalidArgument)
	}
	connection, err := c.authorizeCRDDefinitionAccess(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceSourceSnapshot{}, domainresource.ResourceYAMLView{}, err
	}
	definition, err := c.resolveCRDResourceDefinition(ctx, connection, crdName)
	if err != nil {
		return domainresource.ResourceSourceSnapshot{}, domainresource.ResourceYAMLView{}, err
	}
	if !definition.Namespaced {
		return domainresource.ResourceSourceSnapshot{}, domainresource.ResourceYAMLView{}, fmt.Errorf("%w: this import path requires a namespaced resource", apperrors.ErrUnsupportedOperation)
	}
	item, err := c.GetCRDResourceYAML(ctx, principal, clusterID, crdName, namespace, name)
	if err != nil {
		return domainresource.ResourceSourceSnapshot{}, domainresource.ResourceYAMLView{}, err
	}
	source, err := sourceSnapshot(clusterID, namespace, definition.Group+"/"+definition.Version, definition.Kind, name, crdName, definition.Resource, item.Content)
	return source, item, err
}

// AuthorizeRootCRUpdate reuses the workbench's CR scope, saved Agent grants and
// Kubernetes review before a delivery plan may write the root spec.
func (r *Runtime) AuthorizeRootCRUpdate(ctx context.Context, principal domainidentity.Principal, source domainresource.ResourceSourceSnapshot) error {
	if err := r.Workloads.authorizeRuntimePermission(ctx, principal, appaccess.PermPlatformConfigurationSecretDataView); err != nil {
		return err
	}
	c := r.customResources
	connection, err := c.authorizeCRDDefinitionAccess(ctx, principal, source.Resource.ClusterID, domainaccess.ActionView)
	if err != nil {
		return err
	}
	definition, err := c.resolveCRDResourceDefinition(ctx, connection, source.CRDName)
	if err != nil {
		return err
	}
	if !definition.Namespaced || definition.Kind != source.Resource.Kind || definition.Group+"/"+definition.Version != source.Resource.APIVersion {
		return apperrors.ErrConflict
	}
	if _, _, err = c.authorizeCustomResourceAccess(ctx, principal, source.Resource.ClusterID, source.Resource.Namespace, definition.Kind, domainaccess.ActionUpdate); err != nil {
		return err
	}
	return c.requireCustomResourceAction(ctx, connection, definition, source.Resource.Namespace, source.Resource.Name, domainaccess.ActionUpdate)
}
