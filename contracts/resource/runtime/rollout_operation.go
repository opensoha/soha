package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

var ErrRolloutStopUnconfirmed = errors.New("rollout stop is not confirmed")

// ApplyRollout delegates reconciliation and analysis to Argo Rollouts. Its
// operation annotation resumes an accepted write after a worker restart.
func ApplyRollout(ctx context.Context, client dynamic.Interface, documents []*unstructured.Unstructured, options metav1.PatchOptions, operation string) (*unstructured.Unstructured, error) {
	desired, err := ValidateRolloutResources(documents)
	if err != nil {
		return nil, err
	}
	if options.FieldManager == "" || operation == "" || options.Force != nil && *options.Force {
		return nil, fmt.Errorf("rollout requires a stable non-forced owner and operation")
	}
	observed := map[string]*unstructured.Unstructured{}
	for _, document := range documents {
		gvk := document.GroupVersionKind()
		live, getErr := client.Resource(rolloutResourceTypes[gvk.Group+"/"+gvk.Kind]).Namespace(document.GetNamespace()).Get(ctx, document.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			continue
		}
		if getErr != nil {
			return nil, getErr
		}
		if err := validateRolloutOwner(live, options.FieldManager, desired.GetName()); err != nil {
			return nil, err
		}
		observed[document.GetKind()+"/"+document.GetName()] = live
	}
	root := observed["Rollout/"+desired.GetName()]
	if root != nil && root.GetAnnotations()[RolloutOperationAnnotation] == operation && len(options.DryRun) == 0 {
		if !RolloutMatchesDesired(desired, root) {
			return root, fmt.Errorf("accepted rollout operation no longer matches its frozen spec")
		}
		return waitRollout(ctx, client, documents, options.FieldManager, operation, root)
	}
	if root != nil {
		state := rolloutState(root)
		if state.ObservedGeneration == nil || *state.ObservedGeneration != state.Generation || state.Phase != "Healthy" && !state.Aborted {
			return root, fmt.Errorf("another rollout operation is still active")
		}
		if state.Aborted {
			observation, err := ObserveRollout(ctx, client, rolloutPreviousDocuments(documents, root), options.FieldManager, "")
			if err != nil || !rolloutStopped(observation.State) {
				return root, fmt.Errorf("previous rollout has not restored stable traffic")
			}
		}
	}
	ordered := make([]*unstructured.Unstructured, 0, len(documents))
	for _, document := range documents {
		if !IsArgoRollout(document) {
			ordered = append(ordered, document)
		}
	}
	ordered = append(ordered, desired)
	for _, document := range ordered {
		gvk := document.GroupVersionKind()
		resources := client.Resource(rolloutResourceTypes[gvk.Group+"/"+gvk.Kind]).Namespace(document.GetNamespace())
		live := observed[document.GetKind()+"/"+document.GetName()]
		// The native router owns an atomic weighted backend list after creation.
		// Changing its identities requires a separate target, never force SSA.
		if live != nil && document.GetKind() == "TraefikService" {
			if !RolloutMatchesDesired(document, live) {
				return root, fmt.Errorf("weighted backend identities changed; use a new rollout target")
			}
			continue
		}
		write := document.DeepCopy()
		if live != nil && document.GetKind() == "Service" {
			wanted, _, _ := unstructured.NestedMap(document.Object, "spec", "selector")
			actual, _, _ := unstructured.NestedMap(live.Object, "spec", "selector")
			if !argoMatchesDesired(wanted, actual) {
				return root, fmt.Errorf("rollout Service selector changed; use a new rollout target")
			}
			unstructured.RemoveNestedField(write.Object, "spec", "selector")
		}
		annotations := write.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[RolloutOwnerAnnotation] = options.FieldManager + ":" + desired.GetName()
		if IsArgoRollout(write) {
			annotations[RolloutOperationAnnotation] = operation
		}
		write.SetAnnotations(annotations)
		var applied *unstructured.Unstructured
		if live == nil {
			applied, err = resources.Create(ctx, write, metav1.CreateOptions{FieldManager: options.FieldManager, DryRun: options.DryRun})
		} else {
			applied, err = updateRolloutResource(ctx, resources, write, live, options, desired.GetName())
		}
		if IsArgoRollout(document) && applied != nil {
			root = applied
		}
		if err != nil {
			if IsArgoRollout(document) && len(options.DryRun) == 0 {
				query, stop := context.WithTimeout(context.Background(), 30*time.Second)
				defer stop()
				accepted, readErr := resources.Get(query, document.GetName(), metav1.GetOptions{})
				if readErr != nil {
					return root, errors.Join(err, ErrRolloutStopUnconfirmed, readErr)
				}
				if accepted.GetAnnotations()[RolloutOperationAnnotation] == operation && validateRolloutOwner(accepted, options.FieldManager, desired.GetName()) == nil {
					return accepted, errors.Join(err, StopRollout(query, client, documents, options.FieldManager, operation, string(accepted.GetUID())))
				}
			}
			return root, err
		}
	}
	if len(options.DryRun) != 0 {
		return root, nil
	}
	return waitRollout(ctx, client, documents, options.FieldManager, operation, root)
}

func updateRolloutResource(ctx context.Context, client dynamic.ResourceInterface, desired, initial *unstructured.Unstructured, options metav1.PatchOptions, rootName string) (*unstructured.Unstructured, error) {
	var applied *unstructured.Unstructured
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		live, err := client.Get(ctx, desired.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		if live.GetUID() != initial.GetUID() || validateRolloutOwner(live, options.FieldManager, rootName) != nil {
			return fmt.Errorf("%w: rollout resource identity changed", ErrResourceOwnership)
		}
		if IsArgoRollout(live) && (live.GetAnnotations()[RolloutOperationAnnotation] != initial.GetAnnotations()[RolloutOperationAnnotation] || !reflect.DeepEqual(live.Object["spec"], initial.Object["spec"])) {
			return fmt.Errorf("rollout changed while preparing the approved operation")
		}
		write := live.DeepCopy()
		spec, _, _ := unstructured.NestedMap(desired.Object, "spec")
		if live.GetKind() == "Service" {
			// Service selector is atomic and belongs to Rollouts; cluster-assigned
			// addresses also survive subsequent approved configuration updates.
			// Match API defaults so an unchanged Service does not contend with
			// the controller continuously updating its selector during an abort.
			ports, _ := spec["ports"].([]any)
			for _, raw := range ports {
				port, _ := raw.(map[string]any)
				if port["protocol"] == nil {
					port["protocol"] = "TCP"
				}
				if port["targetPort"] == nil || port["targetPort"] == int64(0) {
					port["targetPort"] = port["port"]
				}
			}
			merged, _, _ := unstructured.NestedMap(live.Object, "spec")
			for key, value := range spec {
				if key != "selector" {
					merged[key] = value
				}
			}
			spec = merged
		}
		write.Object["spec"] = spec
		annotations := write.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		for key, value := range desired.GetAnnotations() {
			annotations[key] = value
		}
		write.SetAnnotations(annotations)
		labels := write.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		for key, value := range desired.GetLabels() {
			labels[key] = value
		}
		if len(labels) > 0 {
			write.SetLabels(labels)
		}
		if len(options.DryRun) == 0 && reflect.DeepEqual(write.Object, live.Object) {
			applied = live
			return nil
		}
		applied, err = client.Update(ctx, write, metav1.UpdateOptions{FieldManager: options.FieldManager, DryRun: options.DryRun})
		return err
	})
	return applied, err
}

// Only the old Rollout spec is needed to observe stable traffic before replacing
// an aborted template; dependency identities must still match the approved plan.
func rolloutPreviousDocuments(documents []*unstructured.Unstructured, live *unstructured.Unstructured) []*unstructured.Unstructured {
	result := make([]*unstructured.Unstructured, 0, len(documents))
	for _, document := range documents {
		if !IsArgoRollout(document) {
			result = append(result, document)
			continue
		}
		prior := document.DeepCopy()
		prior.Object["spec"], _, _ = unstructured.NestedMap(live.Object, "spec")
		unstructured.RemoveNestedField(prior.Object, "spec", "paused")
		result = append(result, prior)
	}
	return result
}

func waitRollout(ctx context.Context, client dynamic.Interface, documents []*unstructured.Unstructured, owner, operation string, initial *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		observation, err := ObserveRollout(ctx, client, documents, owner, operation)
		var live *unstructured.Unstructured
		for _, object := range observation.Resources {
			if IsArgoRollout(object) {
				live = object
				break
			}
		}
		if live != nil && live.GetUID() != initial.GetUID() {
			return live, fmt.Errorf("rollout identity changed while waiting")
		}
		if err == nil && observation.Health == "healthy" {
			return live, nil
		}
		if err != nil || observation.Health == "degraded" {
			if err == nil {
				err = fmt.Errorf("rollout failed its health or analysis gate")
			}
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			stopErr := StopRollout(stopCtx, client, documents, owner, operation, string(initial.GetUID()))
			cancel()
			return live, errors.Join(err, stopErr)
		}
		select {
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			stopErr := StopRollout(stopCtx, client, documents, owner, operation, string(initial.GetUID()))
			cancel()
			return live, errors.Join(ctx.Err(), stopErr)
		case <-ticker.C:
		}
	}
}

// ControlRollout never skips an analysis or timed pause. A stale browser action
// cannot accidentally promote the next pause because both UID and RV are tested.
func ControlRollout(ctx context.Context, client dynamic.Interface, documents []*unstructured.Unstructured, owner, operation string, input sohaapi.ProgressiveRolloutControlInput) (RolloutObservation, error) {
	observation, err := ObserveRollout(ctx, client, documents, owner, operation)
	if err != nil {
		return observation, err
	}
	state := observation.State
	if input.UID == "" || input.ResourceVersion == "" || input.UID != state.UID || input.ResourceVersion != state.ResourceVersion {
		return observation, apierrors.NewConflict(rollouts.GroupResource(), state.Name, fmt.Errorf("rollout changed; refresh before controlling it"))
	}
	if observation.Drifted {
		return observation, fmt.Errorf("rollout drift must be resolved before control")
	}
	if state.Aborted || state.Phase == "Healthy" {
		return observation, fmt.Errorf("rollout is no longer progressing")
	}
	if state.ObservedGeneration == nil || *state.ObservedGeneration != state.Generation {
		return observation, fmt.Errorf("wait for the rollout controller to observe the current generation")
	}
	var root *unstructured.Unstructured
	for _, object := range observation.Resources {
		if IsArgoRollout(object) {
			root = object
		}
	}
	field, changes := "spec", map[string]any{}
	switch input.Action {
	case "pause":
		changes["paused"] = true
	case "abort":
		field, changes = "status", map[string]any{"abort": true}
	case "promote":
		manual, _, _ := unstructured.NestedBool(root.Object, "spec", "paused")
		if manual {
			changes["paused"] = false
			break
		}
		if len(state.PauseReasons) != 1 {
			return observation, fmt.Errorf("promotion requires a manual pause")
		}
		switch state.PauseReasons[0] {
		case "BlueGreenPause":
			if len(state.Metrics) == 0 {
				return observation, fmt.Errorf("pre-promotion analysis is not observed")
			}
			for _, metric := range state.Metrics {
				if metric.Phase != "Successful" {
					return observation, fmt.Errorf("pre-promotion analysis has not succeeded")
				}
			}
		case "CanaryPauseStep":
			steps, _, _ := unstructured.NestedSlice(root.Object, "spec", "strategy", "canary", "steps")
			if state.CurrentStep == nil || *state.CurrentStep >= len(steps) {
				return observation, fmt.Errorf("canary pause has no current step")
			}
			step, _ := steps[*state.CurrentStep].(map[string]any)
			pause, ok := step["pause"].(map[string]any)
			if !ok || len(pause) != 0 {
				return observation, fmt.Errorf("a timed observation pause cannot be skipped")
			}
		default:
			return observation, fmt.Errorf("analysis pauses cannot be overridden")
		}
		field, changes = "status", map[string]any{"pauseConditions": nil}
	default:
		return observation, fmt.Errorf("unsupported rollout control")
	}
	if _, err := patchRolloutControl(ctx, client, root, field, changes); err != nil {
		return observation, err
	}
	return ObserveRollout(ctx, client, documents, owner, operation)
}

func patchRolloutControl(ctx context.Context, client dynamic.Interface, live *unstructured.Unstructured, field string, values map[string]any) (*unstructured.Unstructured, error) {
	body, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": string(live.GetUID()), "resourceVersion": live.GetResourceVersion()}, field: values})
	if err != nil {
		return nil, err
	}
	subresource := []string{}
	if field == "status" {
		subresource = append(subresource, "status")
	}
	return client.Resource(rollouts).Namespace(live.GetNamespace()).Patch(ctx, live.GetName(), types.MergePatchType, body, metav1.PatchOptions{}, subresource...)
}

func StopRollout(ctx context.Context, client dynamic.Interface, documents []*unstructured.Unstructured, owner, operation, uid string) error {
	desired, err := ValidateRolloutResources(documents)
	if err != nil {
		return errors.Join(ErrRolloutStopUnconfirmed, err)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		live, err := client.Resource(rollouts).Namespace(desired.GetNamespace()).Get(ctx, desired.GetName(), metav1.GetOptions{})
		if err != nil {
			return errors.Join(ErrRolloutStopUnconfirmed, err)
		}
		if string(live.GetUID()) != uid || uid == "" || live.GetAnnotations()[RolloutOperationAnnotation] != operation || validateRolloutOwner(live, owner, desired.GetName()) != nil {
			return fmt.Errorf("%w: rollout identity or operation changed", ErrRolloutStopUnconfirmed)
		}
		state := rolloutState(live)
		if state.Phase == "Healthy" && state.ObservedGeneration != nil && *state.ObservedGeneration == state.Generation {
			observation, err := ObserveRollout(ctx, client, documents, owner, operation)
			if err == nil && observation.Health == "healthy" {
				return nil
			}
			return errors.Join(ErrRolloutStopUnconfirmed, err, fmt.Errorf("completed rollout traffic cannot be verified"))
		}
		if !state.Aborted {
			if _, err := patchRolloutControl(ctx, client, live, "status", map[string]any{"abort": true}); err != nil && !apierrors.IsConflict(err) {
				return errors.Join(ErrRolloutStopUnconfirmed, err)
			}
		} else {
			observation, err := ObserveRollout(ctx, client, documents, owner, operation)
			if err == nil && !observation.Drifted && rolloutStopped(observation.State) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.Join(ErrRolloutStopUnconfirmed, ctx.Err())
		case <-ticker.C:
		}
	}
}

func rolloutStopped(state sohaapi.ProgressiveRolloutStatus) bool {
	if !state.Aborted || state.Phase != "Degraded" || state.ObservedGeneration == nil || *state.ObservedGeneration != state.Generation {
		return false
	}
	if state.StableRevision != "" && state.ActiveRevision != state.StableRevision {
		return false
	}
	if state.Strategy == "canary" && (state.StableWeight == nil || state.CanaryWeight == nil || *state.StableWeight != 100 || *state.CanaryWeight != 0) {
		return false
	}
	for _, metric := range state.Metrics {
		if metric.Phase == "Running" || metric.Phase == "Pending" || metric.Phase == "" {
			return false
		}
	}
	return true
}
