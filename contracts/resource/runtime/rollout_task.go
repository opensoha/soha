package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	contractresource "github.com/opensoha/soha-contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type rolloutDriftResource = struct {
	APIVersion string                       `json:"apiVersion"`
	Fields     []sohaapi.ManifestDriftField `json:"fields"`
	Kind       string                       `json:"kind"`
	Name       string                       `json:"name"`
	Namespace  string                       `json:"namespace"`
}

func IsRolloutTask(payload sohaapi.ManifestExecutionTaskPayload) bool {
	if payload.RolloutControl != nil || payload.Action == sohaapi.ManifestTaskActionRolloutControl {
		return true
	}
	for _, document := range payload.Documents {
		if document.APIVersion == "argoproj.io/v1alpha1" && document.Kind == "Rollout" {
			return true
		}
	}
	return false
}

// ExecuteRolloutTask is shared by the control plane and Agent. It verifies the
// frozen wire payload before any Kubernetes write and uses native reconciliation.
func ExecuteRolloutTask(ctx context.Context, client dynamic.Interface, payload sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	result := sohaapi.ManifestExecutionTaskResult{Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation, RenderedDigest: payload.RenderedDigest, Inventory: []sohaapi.ManifestResourceInventory{}, Diagnostics: []sohaapi.ManifestDiagnostic{}}
	if payload.BindingID == "" || payload.FieldManager != "opensoha-manifest/"+payload.BindingID || payload.Namespace == "" || payload.IdempotencyKey == "" || payload.ForceConflicts || len(payload.GitOpsDocuments) > 0 {
		return result, fmt.Errorf("rollout requires a frozen namespace, stable binding owner and operation; force and mixed GitOps are disabled")
	}
	documents := make([]*unstructured.Unstructured, 0, len(payload.Documents))
	for _, document := range payload.Documents {
		desired := &unstructured.Unstructured{}
		if err := desired.UnmarshalJSON([]byte(document.Content)); err != nil {
			return result, fmt.Errorf("decode frozen rollout: %w", err)
		}
		sum := sha256.Sum256([]byte(document.Content))
		if desired.GetAPIVersion() != document.APIVersion || desired.GetKind() != document.Kind || desired.GetNamespace() != document.Namespace || desired.GetName() != document.Name || document.Namespace != payload.Namespace || !strings.EqualFold(document.ContentDigest, hex.EncodeToString(sum[:])) {
			return result, fmt.Errorf("rollout document identity, namespace or digest does not match its frozen content")
		}
		documents = append(documents, desired)
	}
	if _, err := ValidateRolloutResources(documents); err != nil {
		return result, err
	}
	options := metav1.PatchOptions{FieldManager: payload.FieldManager}
	var observation RolloutObservation
	var err error
	switch payload.Action {
	case sohaapi.ManifestTaskActionPreflight:
		options.DryRun = []string{metav1.DryRunAll}
		_, err = ApplyRollout(ctx, client, documents, options, payload.IdempotencyKey)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, sohaapi.ManifestDiagnostic{Stage: "dry_run", Severity: "error", Code: "rollout_preflight", Message: err.Error()})
		}
		result.Preflight = &sohaapi.ManifestPreflightResult{Ready: err == nil, Capability: "available", RenderedDigest: payload.RenderedDigest, ResourceCount: len(documents), Diagnostics: result.Diagnostics}
		return result, nil
	case sohaapi.ManifestTaskActionApply, sohaapi.ManifestTaskActionRepair, sohaapi.ManifestTaskActionRollback:
		_, err = ApplyRollout(ctx, client, documents, options, payload.IdempotencyKey)
	case sohaapi.ManifestTaskActionObserve:
	case sohaapi.ManifestTaskActionRolloutControl:
		if payload.RolloutControl == nil {
			return result, fmt.Errorf("rollout control is required")
		}
		observation, err = ControlRollout(ctx, client, documents, payload.FieldManager, payload.IdempotencyKey, *payload.RolloutControl)
	default:
		return result, fmt.Errorf("unsupported rollout action %q", payload.Action)
	}
	if payload.Action != sohaapi.ManifestTaskActionRolloutControl {
		query, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		var observeErr error
		operation := payload.IdempotencyKey
		if payload.Action == sohaapi.ManifestTaskActionObserve {
			operation = ""
		}
		observation, observeErr = ObserveRollout(query, client, documents, payload.FieldManager, operation)
		err = errors.Join(err, observeErr)
	}
	if err != nil {
		stage := sohaapi.ManifestValidationStageApply
		if payload.Action == sohaapi.ManifestTaskActionObserve {
			stage = sohaapi.ManifestValidationStageObserve
		}
		result.Diagnostics = append(result.Diagnostics, sohaapi.ManifestDiagnostic{Stage: stage, Severity: "error", Code: "rollout_runtime", Message: err.Error()})
	}
	if observation.State.UID == "" {
		return result, err
	}
	result.Rollout = &observation.State
	result.Drift = &sohaapi.ManifestDriftReport{Drifted: observation.Drifted, ObservedAt: time.Now().UTC(), Resources: []rolloutDriftResource{}, EvidenceRefs: []string{}}
	for index, desired := range documents {
		for _, live := range observation.Resources {
			if desired.GroupVersionKind() != live.GroupVersionKind() || desired.GetName() != live.GetName() {
				continue
			}
			document := payload.Documents[index]
			projected := projectRolloutObject(desired.Object, live.Object)
			encoded, _ := json.Marshal(projected)
			sum := sha256.Sum256(encoded)
			item := sohaapi.ManifestResourceInventory{DeploymentID: payload.DeploymentID, Generation: payload.Generation, APIVersion: document.APIVersion, Kind: document.Kind, Namespace: live.GetNamespace(), Name: live.GetName(), UID: string(live.GetUID()), ResourceVersion: live.GetResourceVersion(), ResourceGeneration: live.GetGeneration(), DesiredObjectDigest: document.ContentDigest, ObservedObjectDigest: hex.EncodeToString(sum[:]), Health: contractresource.ManifestHealth(live.Object), LastObservedAt: time.Now().UTC(), Finalizers: live.GetFinalizers()}
			if RolloutMatchesDesired(desired, live) {
				// The immutable policy is matched; live controller weights are
				// separately exposed in rollout rather than treated as spec drift.
				item.ObservedObjectDigest = document.ContentDigest
			} else {
				result.Drift.Resources = append(result.Drift.Resources, rolloutDriftResource{APIVersion: document.APIVersion, Kind: document.Kind, Namespace: document.Namespace, Name: document.Name, Fields: []sohaapi.ManifestDriftField{{Path: "/", DesiredValue: desired.Object, ObservedValue: projected}}})
			}
			if IsArgoRollout(live) {
				item.Health, item.ObservedResourceGeneration = observation.Health, observation.State.ObservedGeneration
			}
			if live.GetKind() == "AnalysisTemplate" || live.GetKind() == "TraefikService" || live.GetKind() == "IngressRoute" {
				// These configuration resources have no Ready condition. Their
				// frozen policy and owning rollout's traffic gate determine readiness.
				item.Health = observation.Health
			}
			if deleting := live.GetDeletionTimestamp(); deleting != nil {
				item.DeletingAt = &deleting.Time
			}
			result.Inventory = append(result.Inventory, item)
			break
		}
	}
	result.EvidenceRefs = []string{"rollout:" + observation.State.Namespace + "/" + observation.State.Name + ":" + observation.State.UID + ":" + observation.State.OperationID}
	return result, err
}

func projectRolloutObject(desired, live any) any {
	switch wanted := desired.(type) {
	case map[string]any:
		actual, _ := live.(map[string]any)
		result := make(map[string]any, len(wanted))
		for key, value := range wanted {
			result[key] = projectRolloutObject(value, actual[key])
		}
		return result
	case []any:
		actual, _ := live.([]any)
		result := make([]any, len(wanted))
		for index, value := range wanted {
			if index < len(actual) {
				result[index] = projectRolloutObject(value, actual[index])
			}
		}
		return result
	default:
		return live
	}
}
