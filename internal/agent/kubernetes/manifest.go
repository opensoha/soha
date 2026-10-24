package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

const manifestTaskTimeout = 30 * time.Second

func (c *Client) ExecuteManifestTask(ctx context.Context, payload sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	if len(payload.Documents) == 0 {
		return sohaapi.ManifestExecutionTaskResult{}, fmt.Errorf("manifest task requires rendered documents")
	}
	if c == nil || c.dynamic == nil || c.discovery == nil {
		return sohaapi.ManifestExecutionTaskResult{}, fmt.Errorf("manifest Kubernetes runtime is unavailable")
	}
	if err := validateRootManifestPayload(payload); err != nil {
		return sohaapi.ManifestExecutionTaskResult{}, err
	}
	resources, err := restmapper.GetAPIGroupResources(c.discovery)
	if err != nil {
		return sohaapi.ManifestExecutionTaskResult{}, fmt.Errorf("discover Kubernetes resources: %w", err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(resources)
	if resourceruntime.IsRolloutTask(payload) {
		return resourceruntime.ExecuteRolloutTask(ctx, c.dynamic, payload)
	}
	switch string(payload.Action) {
	case "preflight":
		return c.preflightManifest(ctx, mapper, payload)
	case "apply", "repair", "rollback":
		return c.applyManifest(ctx, mapper, payload)
	case "observe", "adopt":
		return c.observeManifest(ctx, mapper, payload)
	default:
		return sohaapi.ManifestExecutionTaskResult{}, fmt.Errorf("unsupported manifest action %q", payload.Action)
	}
}

type manifestRESTMapper interface {
	RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error)
}

func (c *Client) preflightManifest(ctx context.Context, mapper manifestRESTMapper, payload sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	if isGitOpsManifestTask(payload) {
		return c.executeGitOpsManifest(ctx, mapper, payload)
	}
	diagnostics := make([]sohaapi.ManifestDiagnostic, 0)
	for _, document := range payload.Documents {
		if _, _, err := c.patchManifestDocument(ctx, mapper, payload, document, true); err != nil {
			diagnostics = append(diagnostics, manifestDiagnostic("dry_run", document, err))
		}
	}
	ready := len(diagnostics) == 0
	return sohaapi.ManifestExecutionTaskResult{
		InitializationPlanID: payload.InitializationPlanID, Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation,
		RenderedDigest: payload.RenderedDigest, Stale: false, Diagnostics: diagnostics,
		Inventory: []sohaapi.ManifestResourceInventory{},
		Preflight: &sohaapi.ManifestPreflightResult{InitializationPlanID: payload.InitializationPlanID, Ready: ready, Capability: "available", RenderedDigest: payload.RenderedDigest, ResourceCount: len(payload.Documents), Diagnostics: diagnostics},
	}, nil
}

func (c *Client) applyManifest(ctx context.Context, mapper manifestRESTMapper, payload sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	if isGitOpsManifestTask(payload) {
		return c.executeGitOpsManifest(ctx, mapper, payload)
	}
	diagnostics := make([]sohaapi.ManifestDiagnostic, 0)
	inventory := make([]sohaapi.ManifestResourceInventory, 0, len(payload.Documents))
	for _, document := range payload.Documents {
		live, desired, err := c.patchManifestDocument(ctx, mapper, payload, document, false)
		if err != nil {
			diagnostics = append(diagnostics, manifestDiagnostic("apply", document, err))
			continue
		}
		inventory = append(inventory, manifestInventory(payload, document, desired, live))
	}
	result := sohaapi.ManifestExecutionTaskResult{InitializationPlanID: payload.InitializationPlanID, Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation, RenderedDigest: payload.RenderedDigest, Stale: false, Diagnostics: diagnostics, Inventory: inventory}
	if len(diagnostics) > 0 {
		return result, fmt.Errorf("manifest apply failed for %d resource(s)", len(diagnostics))
	}
	return result, nil
}

type manifestDriftField struct {
	Path          string `json:"path"`
	DesiredValue  any    `json:"desiredValue"`
	ObservedValue any    `json:"observedValue"`
	FieldManager  string `json:"fieldManager,omitempty"`
}

type manifestDriftResource struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Namespace  string               `json:"namespace"`
	Name       string               `json:"name"`
	Fields     []manifestDriftField `json:"fields"`
}

func (c *Client) observeManifest(ctx context.Context, mapper manifestRESTMapper, payload sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	if isGitOpsManifestTask(payload) {
		return c.executeGitOpsManifest(ctx, mapper, payload)
	}
	diagnostics := make([]sohaapi.ManifestDiagnostic, 0)
	inventory := make([]sohaapi.ManifestResourceInventory, 0, len(payload.Documents))
	driftResources := make([]manifestDriftResource, 0)
	adoptedFiles := make([]sohaapi.ManifestFile, 0)
	for _, document := range payload.Documents {
		desired, mapping, err := prepareManifestDocument(mapper, payload, document)
		if err != nil {
			diagnostics = append(diagnostics, manifestDiagnostic("observe", document, err))
			continue
		}
		queryCtx, cancel := context.WithTimeout(ctx, manifestTaskTimeout)
		live, getErr := manifestResource(c.dynamic, mapping, document.Namespace).Get(queryCtx, document.Name, metav1.GetOptions{})
		if getErr == nil && payload.RootResource != nil && string(live.GetUID()) != payload.RootResource.UID {
			getErr = fmt.Errorf("root CR identity changed")
		}
		if getErr == nil {
			getErr = validateInitialObservation(payload, document, live)
		}
		cancel()
		if getErr != nil {
			diagnostics = append(diagnostics, manifestDiagnostic("observe", document, getErr))
			continue
		}
		item := manifestInventory(payload, document, desired, live)
		item.Health, getErr = resourceruntime.ManifestHealth(ctx, c.dynamic, live)
		if getErr != nil {
			diagnostics = append(diagnostics, manifestDiagnostic("observe", document, getErr))
		}
		if payload.RootResource != nil {
			item.Health = "unknown"
		}
		inventory = append(inventory, item)
		fields := diffManifestFields(desired.Object, live.Object, "")
		if len(fields) > 0 {
			if payload.RootResource != nil {
				fields = []manifestDriftField{{Path: "/spec", DesiredValue: "[restricted]", ObservedValue: "[restricted]"}}
			}
			driftResources = append(driftResources, manifestDriftResource{APIVersion: document.APIVersion, Kind: document.Kind, Namespace: document.Namespace, Name: document.Name, Fields: fields})
		}
		if string(payload.Action) == "adopt" {
			adopted := projectManifestObject(desired.Object, live.Object)
			content, _ := json.Marshal(adopted)
			adoptedFiles = append(adoptedFiles, sohaapi.ManifestFile{Path: document.Path, Content: string(content)})
		}
	}
	result := sohaapi.ManifestExecutionTaskResult{
		InitializationPlanID: payload.InitializationPlanID, Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation,
		RenderedDigest: payload.RenderedDigest, Stale: false, Diagnostics: diagnostics, Inventory: inventory,
		Drift: buildManifestDriftReport(driftResources), AdoptedFiles: adoptedFiles,
	}
	if len(diagnostics) > 0 {
		return result, fmt.Errorf("manifest observe failed for %d resource(s)", len(diagnostics))
	}
	return result, nil
}

func (c *Client) patchManifestDocument(ctx context.Context, mapper manifestRESTMapper, payload sohaapi.ManifestExecutionTaskPayload, document sohaapi.ManifestRenderedDocument, dryRun bool) (*unstructured.Unstructured, *unstructured.Unstructured, error) {
	desired, mapping, err := prepareManifestDocument(mapper, payload, document)
	if err != nil {
		return nil, nil, err
	}
	force := payload.ForceConflicts
	options := metav1.PatchOptions{FieldManager: firstManifestString(payload.FieldManager, "opensoha-delivery/v1"), Force: &force}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	queryCtx, cancel := context.WithTimeout(ctx, manifestTaskTimeout)
	defer cancel()
	var live *unstructured.Unstructured
	if payload.InitializationPlanID != "" {
		live, err = createInitializedManifest(queryCtx, manifestResource(c.dynamic, mapping, document.Namespace), desired, options)
	} else if payload.RootResource != nil {
		live, err = resourceruntime.ApplyRootCR(queryCtx, manifestResource(c.dynamic, mapping, document.Namespace), desired, *payload.RootResource, options)
	} else {
		live, err = resourceruntime.ApplyManifest(queryCtx, manifestResource(c.dynamic, mapping, document.Namespace), desired, options)
	}
	return live, desired, err
}

func prepareManifestDocument(mapper manifestRESTMapper, payload sohaapi.ManifestExecutionTaskPayload, document sohaapi.ManifestRenderedDocument) (*unstructured.Unstructured, *meta.RESTMapping, error) {
	var object map[string]any
	if err := json.Unmarshal([]byte(document.Content), &object); err != nil {
		return nil, nil, fmt.Errorf("decode rendered document: %w", err)
	}
	desired := &unstructured.Unstructured{Object: object}
	if err := validateManifestDocumentEnvelope(desired, document); err != nil {
		return nil, nil, err
	}
	gvk := desired.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve %s %s: %w", document.APIVersion, document.Kind, err)
	}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if document.Namespace == "" || document.Namespace != payload.Namespace {
			return nil, nil, fmt.Errorf("resource namespace %q does not match binding namespace %q", document.Namespace, payload.Namespace)
		}
	} else if document.Namespace != "" {
		return nil, nil, fmt.Errorf("cluster-scoped resource must not declare namespace")
	}
	return desired, mapping, nil
}

func validateManifestDocumentEnvelope(desired *unstructured.Unstructured, document sohaapi.ManifestRenderedDocument) error {
	fields := []struct {
		name     string
		declared string
		actual   string
	}{
		{name: "apiVersion", declared: document.APIVersion, actual: desired.GetAPIVersion()},
		{name: "kind", declared: document.Kind, actual: desired.GetKind()},
		{name: "namespace", declared: document.Namespace, actual: desired.GetNamespace()},
		{name: "name", declared: document.Name, actual: desired.GetName()},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.declared) != strings.TrimSpace(field.actual) {
			return fmt.Errorf("rendered document %s does not match content", field.name)
		}
	}
	digest := sha256.Sum256([]byte(document.Content))
	actualDigest := hex.EncodeToString(digest[:])
	if !strings.EqualFold(strings.TrimSpace(document.ContentDigest), actualDigest) {
		return fmt.Errorf("rendered document content digest does not match content")
	}
	return nil
}

func manifestResource(client dynamic.Interface, mapping *meta.RESTMapping, namespace string) dynamic.ResourceInterface {
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return client.Resource(mapping.Resource).Namespace(namespace)
	}
	return client.Resource(mapping.Resource)
}

func manifestInventory(payload sohaapi.ManifestExecutionTaskPayload, document sohaapi.ManifestRenderedDocument, desired, live *unstructured.Unstructured) sohaapi.ManifestResourceInventory {
	observedDigest := digestManifestObject(projectManifestObject(desired.Object, live.Object))
	item := sohaapi.ManifestResourceInventory{
		DeploymentID: payload.DeploymentID, Generation: payload.Generation, APIVersion: document.APIVersion,
		Kind: document.Kind, Namespace: live.GetNamespace(), Name: live.GetName(), UID: string(live.GetUID()),
		ResourceVersion: live.GetResourceVersion(), DesiredObjectDigest: document.ContentDigest,
		ObservedObjectDigest: observedDigest, Health: manifestHealth(live), LastObservedAt: time.Now().UTC(),
		ResourceGeneration: live.GetGeneration(), Finalizers: live.GetFinalizers(),
	}
	if payload.RootResource != nil {
		item.Health = "unknown"
	}
	if generation, found, err := unstructured.NestedInt64(live.Object, "status", "observedGeneration"); err == nil && found && generation >= 0 {
		item.ObservedResourceGeneration = &generation
	}
	if deletingAt := live.GetDeletionTimestamp(); deletingAt != nil {
		item.DeletingAt = &deletingAt.Time
	}
	return item
}

func projectManifestObject(desired, observed map[string]any) map[string]any {
	result := make(map[string]any, len(desired))
	for key, desiredValue := range desired {
		if key == "status" {
			continue
		}
		observedValue := observed[key]
		if key == "metadata" {
			desiredMetadata, _ := desiredValue.(map[string]any)
			observedMetadata, _ := observedValue.(map[string]any)
			result[key] = projectManifestMetadata(desiredMetadata, observedMetadata)
			continue
		}
		result[key] = projectManifestValue(desiredValue, observedValue)
	}
	return result
}

func projectManifestMetadata(desired, observed map[string]any) map[string]any {
	result := make(map[string]any, len(desired))
	for key, value := range desired {
		switch key {
		case "resourceVersion", "uid", "managedFields", "creationTimestamp", "generation":
			continue
		default:
			result[key] = projectManifestValue(value, observed[key])
		}
	}
	return result
}

func projectManifestValue(desired, observed any) any {
	switch value := desired.(type) {
	case map[string]any:
		observedMap, _ := observed.(map[string]any)
		result := make(map[string]any, len(value))
		for key, child := range value {
			result[key] = projectManifestValue(child, observedMap[key])
		}
		return result
	case []any:
		observedList, _ := observed.([]any)
		result := make([]any, len(value))
		for index, child := range value {
			var live any
			if index < len(observedList) {
				live = observedList[index]
			}
			result[index] = projectManifestValue(child, live)
		}
		return result
	default:
		return observed
	}
}

func diffManifestFields(desired, observed map[string]any, prefix string) []manifestDriftField {
	fields := make([]manifestDriftField, 0)
	for key, desiredValue := range desired {
		if key == "status" || (prefix == "/metadata" && isVolatileManifestMetadata(key)) {
			continue
		}
		path := prefix + "/" + escapeManifestPointer(key)
		observedValue, exists := observed[key]
		if desiredMap, ok := desiredValue.(map[string]any); ok {
			observedMap, _ := observedValue.(map[string]any)
			fields = append(fields, diffManifestFields(desiredMap, observedMap, path)...)
			continue
		}
		observedValue = projectManifestValue(desiredValue, observedValue)
		if !exists || !manifestJSONEqual(desiredValue, observedValue) {
			fields = append(fields, manifestDriftField{Path: path, DesiredValue: desiredValue, ObservedValue: observedValue, FieldManager: "opensoha-delivery/v1"})
		}
	}
	return fields
}

func buildManifestDriftReport(resources []manifestDriftResource) *sohaapi.ManifestDriftReport {
	value := struct {
		Drifted      bool                    `json:"drifted"`
		ObservedAt   time.Time               `json:"observedAt"`
		Resources    []manifestDriftResource `json:"resources"`
		EvidenceRefs []string                `json:"evidenceRefs"`
	}{Drifted: len(resources) > 0, ObservedAt: time.Now().UTC(), Resources: resources, EvidenceRefs: []string{}}
	encoded, _ := json.Marshal(value)
	result := &sohaapi.ManifestDriftReport{}
	_ = json.Unmarshal(encoded, result)
	return result
}

func manifestDiagnostic(stage string, document sohaapi.ManifestRenderedDocument, err error) sohaapi.ManifestDiagnostic {
	return sohaapi.ManifestDiagnostic{Stage: sohaapi.ManifestValidationStage(stage), Severity: "error", Code: "kubernetes_" + stage + "_failed", Message: publicManifestKubernetesError(err), Path: document.Path, DocumentIndex: document.Index, APIVersion: document.APIVersion, Kind: document.Kind, Namespace: document.Namespace, Name: document.Name, FieldManager: "opensoha-delivery/v1"}
}

func publicManifestKubernetesError(err error) string {
	return "Kubernetes manifest operation failed"
}

func digestManifestObject(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func manifestJSONEqual(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func manifestHealth(item *unstructured.Unstructured) string {
	if item == nil {
		return "unknown"
	}
	return contractresource.ManifestHealth(item.Object)
}

func isVolatileManifestMetadata(key string) bool {
	switch key {
	case "resourceVersion", "uid", "managedFields", "creationTimestamp", "generation":
		return true
	default:
		return false
	}
}

func escapeManifestPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func firstManifestString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func validateRootManifestPayload(payload sohaapi.ManifestExecutionTaskPayload) error {
	if err := resourceruntime.ValidateManifestInitialization(payload); err != nil {
		return err
	}
	if payload.RootResource == nil {
		return nil
	}
	if len(payload.Documents) != 1 || len(payload.GitOpsDocuments) > 0 || payload.RolloutControl != nil || payload.ForceConflicts {
		return fmt.Errorf("invalid root CR task boundary")
	}
	if payload.Action != "apply" && payload.Action != "preflight" && payload.Action != "observe" {
		return fmt.Errorf("unsupported root CR task action")
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(payload.Documents[0].Content), &object); err != nil {
		return err
	}
	return contractresource.ValidateRootCRDocument(object, *payload.RootResource)
}

func validateInitialObservation(payload sohaapi.ManifestExecutionTaskPayload, document sohaapi.ManifestRenderedDocument, live *unstructured.Unstructured) error {
	if payload.InitializationPlanID != "" {
		expected := ""
		for _, original := range payload.Inventory {
			if original.APIVersion == document.APIVersion && original.Kind == document.Kind && original.Namespace == document.Namespace && original.Name == document.Name {
				expected = original.UID
			}
		}
		return resourceruntime.ValidateInitializedUID(live, expected)
	}
	return nil
}
