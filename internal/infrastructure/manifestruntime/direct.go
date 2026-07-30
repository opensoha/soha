package manifestruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

const manifestRuntimeTimeout = 30 * time.Second

type Direct struct{ clusters *k8sinfra.Manager }

func NewDirect(clusters *k8sinfra.Manager) *Direct { return &Direct{clusters: clusters} }

func (d *Direct) Execute(ctx context.Context, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	if d == nil || d.clusters == nil {
		return domainmanifest.TaskResult{}, fmt.Errorf("manifest direct runtime is unavailable")
	}
	bundle, err := d.clusters.Bundle(ctx, payload.ClusterID)
	if err != nil {
		return domainmanifest.TaskResult{}, fmt.Errorf("resolve manifest cluster: %w", err)
	}
	mapper, err := directMapper(bundle)
	if err != nil {
		return domainmanifest.TaskResult{}, err
	}
	switch payload.Action {
	case domainmanifest.TaskActionPreflight:
		return preflightDocuments(ctx, bundle.Dynamic, mapper, payload)
	case domainmanifest.TaskActionApply, domainmanifest.TaskActionRepair, domainmanifest.TaskActionRollback:
		return applyDocuments(ctx, bundle.Dynamic, mapper, payload)
	case domainmanifest.TaskActionObserve, domainmanifest.TaskActionAdopt:
		return observeDocuments(ctx, bundle.Dynamic, mapper, payload)
	default:
		return domainmanifest.TaskResult{}, fmt.Errorf("unsupported manifest action %q", payload.Action)
	}
}

type restMapper interface {
	RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error)
}

func directMapper(bundle *k8sinfra.Bundle) (restMapper, error) {
	resources, err := restmapper.GetAPIGroupResources(bundle.Discovery)
	if err != nil {
		return nil, fmt.Errorf("discover Kubernetes resources: %w", err)
	}
	return restmapper.NewDiscoveryRESTMapper(resources), nil
}

func preflightDocuments(ctx context.Context, client dynamic.Interface, mapper restMapper, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	diagnostics := make([]domainmanifest.Diagnostic, 0)
	for _, document := range payload.Documents {
		_, _, err := patchDocument(ctx, client, mapper, payload, document, true)
		if err != nil {
			diagnostics = append(diagnostics, runtimeDiagnostic("dry_run", document, err))
		}
	}
	ready := len(diagnostics) == 0
	return domainmanifest.TaskResult{
		Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation,
		RenderedDigest: payload.RenderedDigest, Diagnostics: diagnostics, Inventory: []domainmanifest.ResourceInventory{},
		Preflight: &domainmanifest.PreflightResult{Ready: ready, Capability: "available", RenderedDigest: payload.RenderedDigest, ResourceCount: len(payload.Documents), Diagnostics: diagnostics},
	}, nil
}

func applyDocuments(ctx context.Context, client dynamic.Interface, mapper restMapper, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	diagnostics := make([]domainmanifest.Diagnostic, 0)
	inventory := make([]domainmanifest.ResourceInventory, 0, len(payload.Documents))
	for _, document := range payload.Documents {
		applied, desired, err := patchDocument(ctx, client, mapper, payload, document, false)
		if err != nil {
			diagnostics = append(diagnostics, runtimeDiagnostic("apply", document, err))
			continue
		}
		inventory = append(inventory, inventoryItem(payload, document, desired, applied))
	}
	if len(diagnostics) > 0 {
		return domainmanifest.TaskResult{Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation, RenderedDigest: payload.RenderedDigest, Diagnostics: diagnostics, Inventory: inventory}, fmt.Errorf("manifest apply failed for %d resource(s)", len(diagnostics))
	}
	return domainmanifest.TaskResult{Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation, RenderedDigest: payload.RenderedDigest, Diagnostics: diagnostics, Inventory: inventory}, nil
}

func observeDocuments(ctx context.Context, client dynamic.Interface, mapper restMapper, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	diagnostics := make([]domainmanifest.Diagnostic, 0)
	inventory := make([]domainmanifest.ResourceInventory, 0, len(payload.Documents))
	driftResources := make([]domainmanifest.DriftResource, 0)
	for _, document := range payload.Documents {
		desired, mapping, err := prepareRuntimeDocument(mapper, payload, document)
		if err != nil {
			diagnostics = append(diagnostics, runtimeDiagnostic("observe", document, err))
			continue
		}
		resource := resourceInterface(client, mapping, document.Namespace)
		queryCtx, cancel := context.WithTimeout(ctx, manifestRuntimeTimeout)
		live, getErr := resource.Get(queryCtx, document.Name, metav1.GetOptions{})
		cancel()
		if getErr != nil {
			diagnostics = append(diagnostics, runtimeDiagnostic("observe", document, getErr))
			continue
		}
		inventory = append(inventory, inventoryItem(payload, document, desired, live))
		fields := diffDesiredFields(desired.Object, live.Object, "")
		if len(fields) > 0 {
			driftResources = append(driftResources, domainmanifest.DriftResource{APIVersion: document.APIVersion, Kind: document.Kind, Namespace: document.Namespace, Name: document.Name, Fields: fields})
		}
	}
	now := time.Now().UTC()
	drift := &domainmanifest.DriftReport{Drifted: len(driftResources) > 0, ObservedAt: now, Resources: driftResources, EvidenceRefs: []string{}}
	result := domainmanifest.TaskResult{Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation, RenderedDigest: payload.RenderedDigest, Diagnostics: diagnostics, Inventory: inventory, Drift: drift}
	if len(diagnostics) > 0 {
		return result, fmt.Errorf("manifest observe failed for %d resource(s)", len(diagnostics))
	}
	return result, nil
}

func patchDocument(ctx context.Context, client dynamic.Interface, mapper restMapper, payload domainmanifest.TaskPayload, document domainmanifest.RenderedDocument, dryRun bool) (*unstructured.Unstructured, *unstructured.Unstructured, error) {
	desired, mapping, err := prepareRuntimeDocument(mapper, payload, document)
	if err != nil {
		return nil, nil, err
	}
	body, err := json.Marshal(desired.Object)
	if err != nil {
		return nil, nil, err
	}
	force := payload.ForceConflicts
	options := metav1.PatchOptions{FieldManager: firstString(payload.FieldManager, "opensoha-delivery/v1"), Force: &force}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	queryCtx, cancel := context.WithTimeout(ctx, manifestRuntimeTimeout)
	defer cancel()
	applied, err := resourceInterface(client, mapping, document.Namespace).Patch(queryCtx, document.Name, types.ApplyPatchType, body, options)
	return applied, desired, err
}

func prepareRuntimeDocument(mapper restMapper, payload domainmanifest.TaskPayload, document domainmanifest.RenderedDocument) (*unstructured.Unstructured, *meta.RESTMapping, error) {
	var object map[string]any
	if err := json.Unmarshal([]byte(document.Content), &object); err != nil {
		return nil, nil, fmt.Errorf("decode rendered document: %w", err)
	}
	desired := &unstructured.Unstructured{Object: object}
	gvk := desired.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve %s %s: %w", document.APIVersion, document.Kind, err)
	}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if strings.TrimSpace(document.Namespace) == "" || document.Namespace != payload.Namespace {
			return nil, nil, fmt.Errorf("resource namespace %q does not match binding namespace %q", document.Namespace, payload.Namespace)
		}
	} else if strings.TrimSpace(document.Namespace) != "" {
		return nil, nil, fmt.Errorf("cluster-scoped resource must not declare namespace")
	}
	return desired, mapping, nil
}

func resourceInterface(client dynamic.Interface, mapping *meta.RESTMapping, namespace string) dynamic.ResourceInterface {
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return client.Resource(mapping.Resource).Namespace(namespace)
	}
	return client.Resource(mapping.Resource)
}

func inventoryItem(payload domainmanifest.TaskPayload, document domainmanifest.RenderedDocument, desired, live *unstructured.Unstructured) domainmanifest.ResourceInventory {
	projected := projectDesired(desired.Object, live.Object)
	observedDigest := digestObject(projected)
	return domainmanifest.ResourceInventory{
		DeploymentID: payload.DeploymentID, Generation: payload.Generation, APIVersion: document.APIVersion,
		Kind: document.Kind, Namespace: live.GetNamespace(), Name: live.GetName(), UID: string(live.GetUID()),
		ResourceVersion: live.GetResourceVersion(), DesiredObjectDigest: document.ContentDigest,
		ObservedObjectDigest: observedDigest, Health: resourceHealth(live), LastObservedAt: time.Now().UTC(),
	}
}

func projectDesired(desired, observed map[string]any) map[string]any {
	result := make(map[string]any, len(desired))
	for key, desiredValue := range desired {
		if key == "status" {
			continue
		}
		observedValue := observed[key]
		if key == "metadata" {
			desiredMetadata, _ := desiredValue.(map[string]any)
			observedMetadata, _ := observedValue.(map[string]any)
			result[key] = projectMetadata(desiredMetadata, observedMetadata)
			continue
		}
		result[key] = projectValue(desiredValue, observedValue)
	}
	return result
}

func projectMetadata(desired, observed map[string]any) map[string]any {
	result := make(map[string]any, len(desired))
	for key, value := range desired {
		switch key {
		case "resourceVersion", "uid", "managedFields", "creationTimestamp", "generation":
			continue
		default:
			result[key] = projectValue(value, observed[key])
		}
	}
	return result
}

func projectValue(desired, observed any) any {
	switch desiredValue := desired.(type) {
	case map[string]any:
		observedMap, _ := observed.(map[string]any)
		return projectDesired(desiredValue, observedMap)
	case []any:
		observedList, _ := observed.([]any)
		result := make([]any, len(desiredValue))
		for index := range desiredValue {
			if index < len(observedList) {
				result[index] = projectValue(desiredValue[index], observedList[index])
			} else {
				result[index] = nil
			}
		}
		return result
	default:
		return observed
	}
}

func diffDesiredFields(desired, observed map[string]any, prefix string) []domainmanifest.DriftField {
	projected := projectDesired(desired, observed)
	fields := make([]domainmanifest.DriftField, 0)
	collectDiff(desired, projected, prefix, &fields)
	return fields
}

func collectDiff(desired, observed any, path string, fields *[]domainmanifest.DriftField) {
	desiredJSON, _ := json.Marshal(desired)
	observedJSON, _ := json.Marshal(observed)
	if string(desiredJSON) == string(observedJSON) {
		return
	}
	desiredMap, desiredOK := desired.(map[string]any)
	observedMap, observedOK := observed.(map[string]any)
	if desiredOK && observedOK {
		for key, value := range desiredMap {
			if key == "status" || (path == "/metadata" && (key == "resourceVersion" || key == "uid" || key == "managedFields" || key == "creationTimestamp" || key == "generation")) {
				continue
			}
			collectDiff(value, observedMap[key], path+"/"+key, fields)
		}
		return
	}
	*fields = append(*fields, domainmanifest.DriftField{Path: path, DesiredValue: desired, ObservedValue: observed})
}

func digestObject(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func runtimeDiagnostic(stage string, document domainmanifest.RenderedDocument, err error) domainmanifest.Diagnostic {
	code := "runtime_error"
	if apierrors.IsConflict(err) {
		code = "field_conflict"
	} else if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		code = "access_denied"
	} else if apierrors.IsNotFound(err) {
		code = "resource_not_found"
	}
	return domainmanifest.Diagnostic{Stage: stage, Severity: "error", Code: code, Message: err.Error(), Path: document.Path, APIVersion: document.APIVersion, Kind: document.Kind, Namespace: document.Namespace, Name: document.Name}
}

func resourceHealth(item *unstructured.Unstructured) string {
	if item == nil {
		return "unknown"
	}
	conditions, found, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
	if !found {
		if requiresManifestHealthCondition(item.GetKind()) {
			return "progressing"
		}
		return "healthy"
	}
	for _, raw := range conditions {
		condition, _ := raw.(map[string]any)
		status := strings.EqualFold(fmt.Sprint(condition["status"]), "true")
		conditionType := strings.ToLower(fmt.Sprint(condition["type"]))
		if status && (conditionType == "available" || conditionType == "ready" || conditionType == "complete" || conditionType == "established") {
			return "healthy"
		}
		if status && (conditionType == "failed" || conditionType == "degraded") {
			return "degraded"
		}
	}
	return "progressing"
}

func requiresManifestHealthCondition(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deployment", "statefulset", "daemonset", "job", "pod":
		return true
	default:
		return false
	}
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
