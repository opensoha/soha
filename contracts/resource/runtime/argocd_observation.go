package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// ArgoObservation retains live child inventory while the operation progresses.
// Cached controller health cannot override missing, unowned or drifted children.
type ArgoObservation struct {
	OperationID string
	Phase       string
	Revision    string
	Health      string
	Drifted     bool
	Resources   []*unstructured.Unstructured
}

func ObserveArgoApplication(ctx context.Context, client dynamic.Interface, desired, object *unstructured.Unstructured, children []*unstructured.Unstructured, owner string) (ArgoObservation, error) {
	observation := ArgoObservation{Health: "unknown"}
	if err := ValidateArgoResources(desired, children); err != nil {
		return observation, err
	}
	if err := validateArgoOwner(object, owner); err != nil {
		return observation, err
	}
	observation.Health = "progressing"
	observation.OperationID = object.GetAnnotations()[ArgoOperationAnnotation]
	observation.Phase = argoString(object, "status", "operationState", "phase")
	observation.Revision = argoString(object, "status", "sync", "revision")
	observation.Drifted = !argoMatchesDesired(desired.Object, object.Object)
	expected := map[string]bool{}
	for _, child := range children {
		expected[argoResourceKey(child)] = true
	}
	reported, _, err := unstructured.NestedSlice(object.Object, "status", "resources")
	if err != nil || len(reported) > 49 {
		return observation, fmt.Errorf("invalid GitOps resource observations")
	}
	seen := map[string]bool{}
	for _, raw := range reported {
		item, ok := raw.(map[string]any)
		if !ok {
			return observation, fmt.Errorf("invalid GitOps resource observation")
		}
		group, _ := item["group"].(string)
		kind, _ := item["kind"].(string)
		name, _ := item["name"].(string)
		key := group + "/" + kind + "/" + name
		if !expected[key] || seen[key] || item["namespace"] != desired.GetNamespace() {
			return observation, fmt.Errorf("GitOps observed a resource outside its frozen inventory")
		}
		seen[key] = true
	}
	queryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	healthy, degraded := len(seen) == len(expected), false
	for _, child := range children {
		gvk := child.GroupVersionKind()
		live, err := client.Resource(argoResourceTypes[gvk.Group+"/"+gvk.Kind]).Namespace(child.GetNamespace()).Get(queryCtx, child.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			healthy = false
			continue
		}
		if err != nil {
			return observation, err
		}
		if live.GetUID() == "" || !argoTracksResource(desired, live) {
			return observation, fmt.Errorf("GitOps child ownership does not match its Application")
		}
		observation.Resources = append(observation.Resources, live)
		health := contractresource.ManifestHealth(live.Object)
		healthy = healthy && health == "healthy"
		degraded = degraded || health == "degraded"
		observation.Drifted = observation.Drifted || !argoMatchesDesired(child.Object, live.Object)
		for _, field := range []string{"containers", "initContainers"} {
			wanted, _, _ := unstructured.NestedSlice(child.Object, "spec", "template", "spec", field)
			actual, _, _ := unstructured.NestedSlice(live.Object, "spec", "template", "spec", field)
			observation.Drifted = observation.Drifted || len(wanted) != len(actual)
		}
	}
	current := observation.OperationID != "" && argoOperationID(object, "status", "operationState", "operation") == observation.OperationID
	if observation.Drifted || degraded || current && (observation.Phase == "Failed" || observation.Phase == "Error") {
		observation.Health = "degraded"
		return observation, nil
	}
	source, _, _ := unstructured.NestedMap(desired.Object, "spec", "source")
	compared, _, _ := unstructured.NestedMap(object.Object, "status", "sync", "comparedTo", "source")
	if healthy && current && observation.Phase == "Succeeded" && argoString(object, "status", "operationState", "finishedAt") != "" && object.Object["operation"] == nil && reflect.DeepEqual(source, compared) && observation.Revision == argoString(desired, "spec", "source", "targetRevision") && argoString(object, "status", "operationState", "syncResult", "revision") == observation.Revision && argoString(object, "status", "sync", "status") == "Synced" && argoString(object, "status", "health", "status") == "Healthy" {
		observation.Health = "healthy"
	}
	return observation, nil
}

func argoResourceKey(object *unstructured.Unstructured) string {
	gvk := object.GroupVersionKind()
	return gvk.Group + "/" + gvk.Kind + "/" + object.GetName()
}

// API defaults may add object fields, but lists must retain their frozen shape.
func argoMatchesDesired(desired, observed any) bool {
	switch value := desired.(type) {
	case map[string]any:
		live, ok := observed.(map[string]any)
		if !ok {
			return false
		}
		for key, child := range value {
			if !argoMatchesDesired(child, live[key]) {
				return false
			}
		}
		return true
	case []any:
		// Kubernetes omits empty optional lists such as container env/args.
		if len(value) == 0 && observed == nil {
			return true
		}
		live, ok := observed.([]any)
		if !ok || len(value) != len(live) {
			return false
		}
		for i, child := range value {
			if !argoMatchesDesired(child, live[i]) {
				return false
			}
		}
		return true
	default:
		wanted, _ := json.Marshal(desired)
		actual, _ := json.Marshal(observed)
		return string(wanted) == string(actual)
	}
}
