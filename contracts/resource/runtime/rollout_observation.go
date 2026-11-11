package runtime

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type RolloutObservation struct {
	State     sohaapi.ProgressiveRolloutStatus
	Health    string
	Drifted   bool
	Resources []*unstructured.Unstructured
}

func ObserveRollout(ctx context.Context, client dynamic.Interface, documents []*unstructured.Unstructured, owner, operation string) (RolloutObservation, error) {
	result := RolloutObservation{Health: "unknown"}
	desired, err := ValidateRolloutResources(documents)
	if err != nil {
		return result, err
	}
	root, err := client.Resource(rollouts).Namespace(desired.GetNamespace()).Get(ctx, desired.GetName(), metav1.GetOptions{})
	if err != nil {
		return result, err
	}
	if err := validateRolloutOwner(root, owner, desired.GetName()); err != nil {
		return result, err
	}
	if operation != "" && root.GetAnnotations()[RolloutOperationAnnotation] != operation {
		return result, fmt.Errorf("rollout operation was superseded")
	}
	result.State = rolloutState(root)
	actual := map[string]*unstructured.Unstructured{}
	for _, document := range documents {
		live := root
		if !IsArgoRollout(document) {
			gvk := document.GroupVersionKind()
			live, err = client.Resource(rolloutResourceTypes[gvk.Group+"/"+gvk.Kind]).Namespace(document.GetNamespace()).Get(ctx, document.GetName(), metav1.GetOptions{})
			if err != nil {
				return result, err
			}
			if err := validateRolloutOwner(live, owner, desired.GetName()); err != nil {
				return result, err
			}
		}
		result.Resources = append(result.Resources, live)
		actual[document.GetKind()+"/"+document.GetName()] = live
		result.Drifted = result.Drifted || !RolloutMatchesDesired(document, live)
	}
	state := &result.State
	active, preview := actual["Service/"+state.ActiveService], actual["Service/"+state.PreviewService]
	if active == nil || preview == nil {
		return result, fmt.Errorf("rollout traffic Services are missing")
	}
	state.ActiveRevision = argoString(active, "spec", "selector", rolloutPodHash)
	state.PreviewRevision = argoString(preview, "spec", "selector", rolloutPodHash)
	if state.Strategy == "canary" {
		name := argoString(root, "spec", "strategy", "canary", "trafficRouting", "traefik", "weightedTraefikServiceName")
		weighted := actual["TraefikService/"+name]
		if weighted == nil {
			return result, fmt.Errorf("weighted traffic resource is missing")
		}
		backends, _, _ := unstructured.NestedSlice(weighted.Object, "spec", "weighted", "services")
		for _, raw := range backends {
			backend, _ := raw.(map[string]any)
			weight, ok := backend["weight"].(int64)
			if !ok || weight < 0 || weight > 100 {
				return result, fmt.Errorf("invalid observed traffic weight")
			}
			value := int(weight)
			if backend["name"] == state.ActiveService {
				state.StableWeight = &value
			}
			if backend["name"] == state.PreviewService {
				state.CanaryWeight = &value
			}
		}
		if state.StableWeight == nil || state.CanaryWeight == nil || *state.StableWeight+*state.CanaryWeight != 100 {
			return result, fmt.Errorf("observed traffic weights are incomplete")
		}
	}
	if err := observeRolloutAnalyses(ctx, client, root, state); err != nil {
		return result, err
	}
	result.Health = "progressing"
	current := state.ObservedGeneration != nil && *state.ObservedGeneration == state.Generation
	if result.Drifted || current && (state.Phase == "Degraded" || state.Aborted) {
		result.Health = "degraded"
		return result, nil
	}
	if current && state.Phase == "Healthy" && !state.Paused && state.CurrentRevision != "" && state.StableRevision == state.CurrentRevision && state.ActiveRevision == state.CurrentRevision {
		result.Health = "healthy"
		if state.Strategy == "canary" && (*state.StableWeight != 100 || *state.CanaryWeight != 0) {
			result.Health = "progressing"
		}
	}
	return result, nil
}

func rolloutState(root *unstructured.Unstructured) sohaapi.ProgressiveRolloutStatus {
	state := sohaapi.ProgressiveRolloutStatus{Name: root.GetName(), Namespace: root.GetNamespace(), UID: string(root.GetUID()), ResourceVersion: root.GetResourceVersion(), Generation: root.GetGeneration(), OperationID: root.GetAnnotations()[RolloutOperationAnnotation], Phase: argoString(root, "status", "phase"), StableRevision: argoString(root, "status", "stableRS"), CurrentRevision: argoString(root, "status", "currentPodHash"), Metrics: []sohaapi.ProgressiveRolloutMetric{}, PauseReasons: []string{}}
	if generation, err := strconv.ParseInt(argoString(root, "status", "observedGeneration"), 10, 64); err == nil && generation >= 0 {
		state.ObservedGeneration = &generation
	}
	state.Paused, _, _ = unstructured.NestedBool(root.Object, "spec", "paused")
	state.Aborted, _, _ = unstructured.NestedBool(root.Object, "status", "abort")
	pauses, _, _ := unstructured.NestedSlice(root.Object, "status", "pauseConditions")
	for _, raw := range pauses {
		pause, _ := raw.(map[string]any)
		reason, _ := pause["reason"].(string)
		state.PauseReasons = append(state.PauseReasons, reason)
	}
	state.Paused = state.Paused || len(pauses) > 0
	if argoString(root, "spec", "strategy", "blueGreen", "activeService") != "" {
		state.Strategy = "blueGreen"
		state.ActiveService = argoString(root, "spec", "strategy", "blueGreen", "activeService")
		state.PreviewService = argoString(root, "spec", "strategy", "blueGreen", "previewService")
	} else {
		state.Strategy = "canary"
		state.ActiveService = argoString(root, "spec", "strategy", "canary", "stableService")
		state.PreviewService = argoString(root, "spec", "strategy", "canary", "canaryService")
		steps, _, _ := unstructured.NestedSlice(root.Object, "spec", "strategy", "canary", "steps")
		count := len(steps)
		state.TotalSteps = &count
		if step, found, _ := unstructured.NestedInt64(root.Object, "status", "currentStepIndex"); found && step >= 0 {
			index := int(step)
			state.CurrentStep = &index
		}
	}
	return state
}

// RolloutMatchesDesired ignores only the native traffic controller's weights.
// Service hash selectors and spec.paused are absent from the approved template.
func RolloutMatchesDesired(desired, live *unstructured.Unstructured) bool {
	wanted := desired.DeepCopy()
	if wanted.GetKind() == "TraefikService" {
		backends, _, _ := unstructured.NestedSlice(wanted.Object, "spec", "weighted", "services")
		for _, raw := range backends {
			backend, _ := raw.(map[string]any)
			delete(backend, "weight")
		}
		_ = unstructured.SetNestedSlice(wanted.Object, backends, "spec", "weighted", "services")
	}
	return argoMatchesDesired(wanted.Object, live.Object)
}

func observeRolloutAnalyses(ctx context.Context, client dynamic.Interface, root *unstructured.Unstructured, state *sohaapi.ProgressiveRolloutStatus) error {
	runs := []*unstructured.Unstructured{}
	seen := map[string]bool{}
	for _, path := range [][]string{{"canary", "currentStepAnalysisRunStatus"}, {"canary", "currentBackgroundAnalysisRunStatus"}, {"blueGreen", "prePromotionAnalysisRunStatus"}, {"blueGreen", "postPromotionAnalysisRunStatus"}} {
		name := argoString(root, "status", path[0], path[1], "name")
		if name == "" {
			continue
		}
		run, err := client.Resource(analysisRuns).Namespace(root.GetNamespace()).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		owner := metav1.GetControllerOf(run)
		if owner == nil || owner.Kind != "Rollout" || owner.APIVersion != root.GetAPIVersion() || owner.Name != root.GetName() || owner.UID != root.GetUID() || run.GetUID() == "" || run.GetLabels()[rolloutPodHash] != state.CurrentRevision {
			return fmt.Errorf("analysis observation does not belong to this Rollout revision")
		}
		runs = append(runs, run)
		seen[run.GetName()] = true
	}
	// Rollouts clears currentStepAnalysisRunStatus when a canary completes.
	// Retained runs keep the controller owner, Pod hash and native revision, so
	// the completed task can preserve this revision's full observation evidence.
	revision := root.GetAnnotations()["rollout.argoproj.io/revision"]
	if revision != "" && state.CurrentRevision != "" {
		options := metav1.ListOptions{LabelSelector: rolloutPodHash + "=" + state.CurrentRevision, Limit: 100}
		for {
			page, err := client.Resource(analysisRuns).Namespace(root.GetNamespace()).List(ctx, options)
			if err != nil {
				return err
			}
			for _, candidate := range page.Items {
				owner := metav1.GetControllerOf(&candidate)
				if seen[candidate.GetName()] || owner == nil || owner.Kind != "Rollout" || owner.APIVersion != root.GetAPIVersion() || owner.Name != root.GetName() || owner.UID != root.GetUID() || candidate.GetUID() == "" || candidate.GetAnnotations()["rollout.argoproj.io/revision"] != revision {
					continue
				}
				runs = append(runs, candidate.DeepCopy())
				seen[candidate.GetName()] = true
			}
			if page.GetContinue() == "" {
				break
			}
			options.Continue = page.GetContinue()
		}
	}
	slices.SortFunc(runs, func(a, b *unstructured.Unstructured) int { return strings.Compare(a.GetName(), b.GetName()) })
	for _, run := range runs {
		metrics, _, _ := unstructured.NestedSlice(run.Object, "spec", "metrics")
		results, _, _ := unstructured.NestedSlice(run.Object, "status", "metricResults")
		for _, raw := range metrics {
			metric, _ := raw.(map[string]any)
			name, _ := metric["name"].(string)
			count, _ := metric["count"].(int64)
			condition, _ := metric["successCondition"].(string)
			item := sohaapi.ProgressiveRolloutMetric{AnalysisRun: run.GetName(), UID: string(run.GetUID()), Name: name, Phase: argoString(run, "status", "phase"), Interval: fmt.Sprint(metric["interval"]), TargetCount: int(count), SuccessCondition: condition}
			for _, raw := range results {
				result, _ := raw.(map[string]any)
				if result["name"] != name {
					continue
				}
				item.Phase, _ = result["phase"].(string)
				for key, target := range map[string]*int{"count": &item.Count, "successful": &item.Successful, "failed": &item.Failed} {
					value, _ := result[key].(int64)
					*target = int(value)
				}
				measurements, _ := result["measurements"].([]any)
				if len(measurements) > 0 {
					last, _ := measurements[len(measurements)-1].(map[string]any)
					item.Value, _ = last["value"].(string)
					first, _ := measurements[0].(map[string]any)
					item.StartedAt, _ = first["startedAt"].(string)
					item.FinishedAt, _ = last["finishedAt"].(string)
				}
			}
			state.Metrics = append(state.Metrics, item)
		}
	}
	return nil
}

func validateRolloutOwner(object *unstructured.Unstructured, owner, rootName string) error {
	if owner == "" || object.GetAnnotations()[RolloutOwnerAnnotation] != owner+":"+rootName || object.GetUID() == "" || object.GetResourceVersion() == "" || object.GetDeletionTimestamp() != nil {
		return fmt.Errorf("%w: rollout resource has another owner or is being deleted", ErrResourceOwnership)
	}
	clean := object.DeepCopy()
	clean.SetKind("OwnedRolloutResource")
	annotations := clean.GetAnnotations()
	delete(annotations, RolloutOwnerAnnotation)
	clean.SetAnnotations(annotations)
	if err := ValidateDirectManifestOwner(clean); err != nil {
		return err
	}
	for _, field := range object.GetManagedFields() {
		if strings.HasPrefix(field.Manager, "opensoha-") && field.Manager != owner {
			return fmt.Errorf("%w: rollout resource has another Soha field owner", ErrResourceOwnership)
		}
	}
	return nil
}
