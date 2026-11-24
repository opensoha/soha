package hostedruntime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/distribution/reference"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
)

const (
	managedByLabel   = "app.kubernetes.io/managed-by"
	managedByValue   = "soha-app-hosting"
	revisionIDLabel  = "soha.io/revision-id"
	visibilityLabel  = "networking.knative.dev/visibility"
	clusterLocal     = "cluster-local"
	knativeTimeout   = int64(300)
	responseTimeout  = int64(60)
	idleTimeout      = int64(60)
	concurrencyLimit = int64(10)
)

var (
	// ErrRevisionFailed marks a terminal Knative Configuration failure.
	ErrRevisionFailed = errors.New("hosted runtime: Knative revision failed")

	revisionIDPattern = regexp.MustCompile(`^rev_[0-9a-f]{32}$`)
	knativeServiceGVR = schema.GroupVersionResource{Group: "serving.knative.dev", Version: "v1", Resource: "services"}
)

// Spec describes the immutable Knative workload for one hosted revision.
type Spec struct {
	RevisionID        string
	Image             string
	EnvironmentSecret string
	Limits            *sohaapi.AppHostingRuntimeLimits
}

// Runtime owns Knative Services in one configured namespace.
type Runtime struct {
	client    dynamic.Interface
	namespace string
}

// New creates a namespace-scoped Knative Serving v1 adapter.
func New(client dynamic.Interface, namespace string) (*Runtime, error) {
	if isNilInterface(client) {
		return nil, fmt.Errorf("hosted runtime: Kubernetes dynamic client is required")
	}
	if issues := validation.IsDNS1123Label(namespace); len(issues) != 0 {
		return nil, fmt.Errorf("hosted runtime: invalid namespace")
	}
	return &Runtime{client: client, namespace: namespace}, nil
}

// ServiceName returns the deterministic Knative Service name for a revision.
func (r *Runtime) ServiceName(revisionID string) (string, error) {
	if !revisionIDPattern.MatchString(revisionID) {
		return "", fmt.Errorf("hosted runtime: revision ID must be rev_ followed by 32 lowercase hex characters")
	}
	return "soha-app-" + strings.TrimPrefix(revisionID, "rev_"), nil
}

// Prepare creates a cluster-local Knative Service once per revision.
func (r *Runtime) Prepare(ctx context.Context, spec Spec) error {
	if err := r.available(); err != nil {
		return err
	}
	desired, err := r.desiredService(spec)
	if err != nil {
		return err
	}
	resource := r.client.Resource(knativeServiceGVR).Namespace(r.namespace)
	_, err = resource.Create(ctx, desired, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("hosted runtime: create Knative Service: %w", err)
	}
	existing, err := resource.Get(ctx, desired.GetName(), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("hosted runtime: inspect existing Knative Service: %w", err)
	}
	if !ownedBy(existing, spec.RevisionID) {
		return fmt.Errorf("hosted runtime: Knative Service %s is not owned by this revision", desired.GetName())
	}
	if !sameSpec(existing, desired) {
		return fmt.Errorf("hosted runtime: Knative Service %s already exists with a different specification", desired.GetName())
	}
	return nil
}

// Ready reports whether the current generation has reached a terminal Ready condition.
func (r *Runtime) Ready(ctx context.Context, revisionID string) (bool, error) {
	if err := r.available(); err != nil {
		return false, err
	}
	name, err := r.ServiceName(revisionID)
	if err != nil {
		return false, err
	}
	service, err := r.client.Resource(knativeServiceGVR).Namespace(r.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("hosted runtime: read Knative Service status: %w", err)
	}
	if !ownedBy(service, revisionID) {
		return false, fmt.Errorf("hosted runtime: Knative Service %s is not owned by this revision", name)
	}
	if service.GetDeletionTimestamp() != nil {
		return false, fmt.Errorf("hosted runtime: Knative Service %s is deleting", name)
	}
	generation, found, err := unstructured.NestedInt64(service.Object, "metadata", "generation")
	if err != nil {
		return false, fmt.Errorf("hosted runtime: invalid Knative Service generation: %w", err)
	}
	observed, observedFound, err := unstructured.NestedInt64(service.Object, "status", "observedGeneration")
	if err != nil {
		return false, fmt.Errorf("hosted runtime: invalid Knative Service observed generation: %w", err)
	}
	if !found || !observedFound || observed != generation {
		return false, nil
	}
	conditions, _, err := unstructured.NestedSlice(service.Object, "status", "conditions")
	if err != nil {
		return false, fmt.Errorf("hosted runtime: invalid Knative Service conditions: %w", err)
	}
	return serviceReadyCondition(name, conditions)
}

func serviceReadyCondition(name string, conditions []any) (bool, error) {
	var readyCondition map[string]any
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		conditionType, _, _ := unstructured.NestedString(condition, "type")
		switch conditionType {
		case "Ready":
			readyCondition = condition
		case "ConfigurationsReady":
			status, _, _ := unstructured.NestedString(condition, "status")
			reason, _, _ := unstructured.NestedString(condition, "reason")
			if status == "False" && reason == "RevisionFailed" {
				return false, ErrRevisionFailed
			}
		}
	}
	status, _, _ := unstructured.NestedString(readyCondition, "status")
	switch status {
	case "True":
		return true, nil
	case "False":
		reason, _, _ := unstructured.NestedString(readyCondition, "reason")
		if reason == "" {
			reason = "Ready=False"
		}
		return false, fmt.Errorf("hosted runtime: Knative Service %s is not ready: %s", name, reason)
	default:
		return false, nil
	}
}

// Delete removes only the Service this revision owns, guarded by its UID.
func (r *Runtime) Delete(ctx context.Context, revisionID string) error {
	if err := r.available(); err != nil {
		return err
	}
	name, err := r.ServiceName(revisionID)
	if err != nil {
		return err
	}
	resource := r.client.Resource(knativeServiceGVR).Namespace(r.namespace)
	service, err := resource.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("hosted runtime: read Knative Service before deletion: %w", err)
	}
	if !ownedBy(service, revisionID) {
		return fmt.Errorf("hosted runtime: Knative Service %s is not owned by this revision", name)
	}
	uid := service.GetUID()
	if uid == "" {
		return fmt.Errorf("hosted runtime: Knative Service %s has no UID", name)
	}
	err = resource.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("hosted runtime: delete Knative Service: %w", err)
	}
	return nil
}

func (r *Runtime) desiredService(spec Spec) (*unstructured.Unstructured, error) {
	name, err := r.ServiceName(spec.RevisionID)
	if err != nil {
		return nil, err
	}
	image, err := pinnedImage(spec.Image)
	if err != nil {
		return nil, err
	}
	secret := spec.EnvironmentSecret
	if secret != "" {
		if issues := validation.IsDNS1123Subdomain(secret); len(issues) != 0 {
			return nil, fmt.Errorf("hosted runtime: invalid environment secret name")
		}
	}
	labels := map[string]any{
		managedByLabel:  managedByValue,
		revisionIDLabel: spec.RevisionID,
	}
	serviceLabels := map[string]any{
		managedByLabel:  managedByValue,
		revisionIDLabel: spec.RevisionID,
		visibilityLabel: clusterLocal,
	}
	cpu, memory, replicas, delay := "500m", "512Mi", "2", "10m"
	if limits := spec.Limits; limits != nil {
		cpu = strconv.Itoa(int(limits.CPUMilli)) + "m"
		memory = strconv.Itoa(int(limits.MemoryMiB)) + "Mi"
		replicas = strconv.Itoa(int(limits.MaxReplicas))
		delay = strconv.Itoa(int(limits.ScaleDownSeconds)) + "s"
	}
	container := hostedContainer(image, secret, cpu, memory)
	service := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      name,
			"namespace": r.namespace,
			"labels":    serviceLabels,
		},
		"spec": map[string]any{
			"traffic": []any{map[string]any{"latestRevision": true, "percent": int64(100)}},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": labels,
					"annotations": map[string]any{
						"autoscaling.knative.dev/min-scale":        "0",
						"autoscaling.knative.dev/max-scale":        replicas,
						"autoscaling.knative.dev/scale-down-delay": delay,
					},
				},
				"spec": map[string]any{
					"automountServiceAccountToken": false,
					"containerConcurrency":         concurrencyLimit,
					"timeoutSeconds":               knativeTimeout,
					"responseStartTimeoutSeconds":  responseTimeout,
					"idleTimeoutSeconds":           idleTimeout,
					"securityContext": map[string]any{
						"runAsNonRoot":   true,
						"runAsUser":      int64(65532),
						"runAsGroup":     int64(65532),
						"fsGroup":        int64(65532),
						"seccompProfile": map[string]any{"type": "RuntimeDefault"},
					},
					"containers": []any{container},
					"volumes": []any{map[string]any{
						"name": "tmp",
						"emptyDir": map[string]any{
							"medium":    "Memory",
							"sizeLimit": "32Mi",
						},
					}},
				},
			},
		},
	}}
	return service, nil
}

func hostedContainer(image, secret, cpu, memory string) map[string]any {
	container := map[string]any{
		"name":            "app",
		"image":           image,
		"imagePullPolicy": "IfNotPresent",
		"ports": []any{map[string]any{
			"containerPort": int64(8080),
			"protocol":      "TCP",
		}},
		"readinessProbe": map[string]any{
			"httpGet": map[string]any{
				"path":   "/healthz",
				"port":   int64(8080),
				"scheme": "HTTP",
			},
			"initialDelaySeconds": int64(2),
			"periodSeconds":       int64(5),
			"timeoutSeconds":      int64(1),
			"successThreshold":    int64(1),
			"failureThreshold":    int64(3),
		},
		"securityContext": map[string]any{
			"allowPrivilegeEscalation": false,
			"capabilities":             map[string]any{"drop": []any{"ALL"}},
			"readOnlyRootFilesystem":   true,
			"runAsNonRoot":             true,
			"runAsUser":                int64(65532),
			"runAsGroup":               int64(65532),
			"seccompProfile":           map[string]any{"type": "RuntimeDefault"},
		},
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "50m", "memory": "64Mi"},
			"limits":   map[string]any{"cpu": cpu, "memory": memory},
		},
		"terminationMessagePath":   "/dev/termination-log",
		"terminationMessagePolicy": "File",
		"volumeMounts": []any{map[string]any{
			"name":      "tmp",
			"mountPath": "/tmp",
		}},
	}
	if secret != "" {
		container["envFrom"] = []any{map[string]any{"secretRef": map[string]any{"name": secret}}}
	}
	return container
}

func pinnedImage(image string) (string, error) {
	if image == "" || strings.TrimSpace(image) != image {
		return "", fmt.Errorf("hosted runtime: image must be pinned by sha256 digest")
	}
	parsed, err := reference.Parse(image)
	if err != nil {
		return "", fmt.Errorf("hosted runtime: image must be pinned by sha256 digest")
	}
	if _, ok := parsed.(reference.Named); !ok {
		return "", fmt.Errorf("hosted runtime: image must include a repository")
	}
	digested, ok := parsed.(reference.Digested)
	if !ok {
		return "", fmt.Errorf("hosted runtime: image must be pinned by sha256 digest")
	}
	digest := digested.Digest()
	if digest.Validate() != nil || digest.Algorithm().String() != "sha256" || len(digest.Encoded()) != 64 {
		return "", fmt.Errorf("hosted runtime: image must be pinned by sha256 digest")
	}
	return image, nil
}

func (r *Runtime) available() error {
	if r == nil || isNilInterface(r.client) {
		return fmt.Errorf("hosted runtime: runtime is not configured")
	}
	return nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func ownedBy(service *unstructured.Unstructured, revisionID string) bool {
	labels := service.GetLabels()
	return labels[managedByLabel] == managedByValue && labels[revisionIDLabel] == revisionID
}

func sameSpec(existing, desired *unstructured.Unstructured) bool {
	existingSpec, _, err := unstructured.NestedMap(existing.Object, "spec")
	if err != nil {
		return false
	}
	desiredSpec, _, err := unstructured.NestedMap(desired.Object, "spec")
	if err != nil {
		return false
	}
	return reflect.DeepEqual(normalizeServerDefaults(existingSpec), desiredSpec)
}

func normalizeServerDefaults(spec map[string]any) map[string]any {
	normalized := deepCopyMap(spec)
	pod, _, _ := unstructured.NestedMap(normalized, "template", "spec")
	if pod == nil {
		return normalized
	}
	if serviceAccount, _, _ := unstructured.NestedString(pod, "serviceAccountName"); serviceAccount == "" || serviceAccount == "default" {
		delete(pod, "serviceAccountName")
	}
	for key, value := range map[string]string{
		"dnsPolicy":     "ClusterFirst",
		"schedulerName": "default-scheduler",
		"restartPolicy": "Always",
	} {
		if current, _, _ := unstructured.NestedString(pod, key); current == value {
			delete(pod, key)
		}
	}
	delete(pod, "enableServiceLinks")
	_ = unstructured.SetNestedMap(normalized, pod, "template", "spec")
	return normalized
}

func deepCopyMap(value map[string]any) map[string]any {
	copied := make(map[string]any, len(value))
	for key, item := range value {
		switch typed := item.(type) {
		case map[string]any:
			copied[key] = deepCopyMap(typed)
		case []any:
			copied[key] = deepCopySlice(typed)
		default:
			copied[key] = typed
		}
	}
	return copied
}

func deepCopySlice(value []any) []any {
	copied := make([]any, len(value))
	for index, item := range value {
		switch typed := item.(type) {
		case map[string]any:
			copied[index] = deepCopyMap(typed)
		case []any:
			copied[index] = deepCopySlice(typed)
		default:
			copied[index] = typed
		}
	}
	return copied
}
