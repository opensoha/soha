package runtime

import (
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	RolloutOwnerAnnotation     = "delivery.soha.io/rollout-owner"
	RolloutOperationAnnotation = "delivery.soha.io/rollout-operation"
	rolloutPodHash             = "rollouts-pod-template-hash"
)

var rollouts = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "rollouts"}
var analysisRuns = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "analysisruns"}
var rolloutResourceTypes = map[string]schema.GroupVersionResource{
	"argoproj.io/Rollout":          rollouts,
	"argoproj.io/AnalysisTemplate": {Group: "argoproj.io", Version: "v1alpha1", Resource: "analysistemplates"},
	"/Service":                     {Version: "v1", Resource: "services"},
	"traefik.io/TraefikService":    {Group: "traefik.io", Version: "v1alpha1", Resource: "traefikservices"},
	"traefik.io/IngressRoute":      {Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes"},
}

func IsArgoRollout(object *unstructured.Unstructured) bool {
	return object != nil && object.GetAPIVersion() == "argoproj.io/v1alpha1" && object.GetKind() == "Rollout"
}

// ValidateRolloutResources admits native blue/green and Traefik canaries. Every
// traffic and analysis reference belongs to the same frozen namespace bundle.
func ValidateRolloutResources(documents []*unstructured.Unstructured) (*unstructured.Unstructured, error) {
	objects := map[string]*unstructured.Unstructured{}
	var root *unstructured.Unstructured
	for _, object := range documents {
		if object == nil {
			return nil, fmt.Errorf("rollout document is missing")
		}
		gvk := object.GroupVersionKind()
		gvr, ok := rolloutResourceTypes[gvk.Group+"/"+gvk.Kind]
		key := object.GetKind() + "/" + object.GetName()
		if !ok || gvk.Version != gvr.Version || object.GetName() == "" || object.GetNamespace() == "" || objects[key] != nil {
			return nil, fmt.Errorf("unsupported or repeated rollout document")
		}
		if object.GetUID() != "" || object.GetResourceVersion() != "" || object.GetGenerateName() != "" || object.GetDeletionTimestamp() != nil || len(object.GetOwnerReferences()) != 0 || len(object.GetFinalizers()) != 0 || object.Object["status"] != nil {
			return nil, fmt.Errorf("rollout documents must contain independent desired resources")
		}
		if IsArgoRollout(object) {
			if root != nil {
				return nil, fmt.Errorf("rollout bundle requires exactly one Rollout")
			}
			root = object
		}
		objects[key] = object
	}
	if root == nil || len(objects) > 20 {
		return nil, fmt.Errorf("rollout bundle requires one Rollout and at most 19 dependencies")
	}
	for _, object := range documents {
		if object.GetNamespace() != root.GetNamespace() {
			return nil, fmt.Errorf("rollout dependencies must stay in the target namespace")
		}
		clean := object.DeepCopy()
		clean.SetKind("DesiredResource")
		if err := ValidateDirectManifestOwner(clean); err != nil {
			return nil, err
		}
		for key := range object.GetAnnotations() {
			if strings.HasPrefix(key, "rollout.argoproj.io/") || strings.HasPrefix(key, "rollouts.argoproj.io/") || key == RolloutOwnerAnnotation || key == RolloutOperationAnnotation {
				return nil, fmt.Errorf("rollout controller annotations are executor-owned")
			}
		}
	}
	spec, _, _ := unstructured.NestedMap(root.Object, "spec")
	if err := rolloutFields(spec, "replicas", "revisionHistoryLimit", "selector", "template", "strategy", "progressDeadlineSeconds", "progressDeadlineAbort", "minReadySeconds"); err != nil {
		return nil, err
	}
	selector, _, _ := unstructured.NestedStringMap(root.Object, "spec", "selector", "matchLabels")
	selectorSpec, _, _ := unstructured.NestedMap(root.Object, "spec", "selector")
	if len(selector) == 0 || len(selectorSpec) != 1 || selector[rolloutPodHash] != "" {
		return nil, fmt.Errorf("rollout requires fixed matchLabels without a controller hash")
	}
	labels, _, _ := unstructured.NestedStringMap(root.Object, "spec", "template", "metadata", "labels")
	for key, value := range selector {
		if labels[key] != value {
			return nil, fmt.Errorf("rollout Pod labels do not match its selector")
		}
	}
	for _, field := range []string{"containers", "initContainers"} {
		containers, _, err := unstructured.NestedSlice(root.Object, "spec", "template", "spec", field)
		if err != nil || field == "containers" && len(containers) == 0 {
			return nil, fmt.Errorf("rollout requires container images")
		}
		for _, raw := range containers {
			container, _ := raw.(map[string]any)
			image, _ := container["image"].(string)
			if _, parsed, err := ParseArgoImageOverride(image); err != nil || parsed != image {
				return nil, fmt.Errorf("rollout images require immutable digests")
			}
		}
	}
	used := map[string]bool{"Rollout/" + root.GetName(): true}
	strategy, _, _ := unstructured.NestedMap(root.Object, "spec", "strategy")
	if len(strategy) != 1 {
		return nil, fmt.Errorf("choose exactly one native rollout strategy")
	}
	var serviceNames []string
	if raw, ok := strategy["blueGreen"]; ok {
		blue, _ := raw.(map[string]any)
		if err := rolloutFields(blue, "activeService", "previewService", "autoPromotionEnabled", "prePromotionAnalysis", "postPromotionAnalysis", "scaleDownDelaySeconds", "abortScaleDownDelaySeconds", "previewReplicaCount"); err != nil {
			return nil, err
		}
		if blue["autoPromotionEnabled"] != false {
			return nil, fmt.Errorf("blue/green requires explicit promotion")
		}
		serviceNames = []string{argoString(root, "spec", "strategy", "blueGreen", "activeService"), argoString(root, "spec", "strategy", "blueGreen", "previewService")}
		if err := rolloutAnalysisReference(blue["prePromotionAnalysis"], objects, used); err != nil {
			return nil, err
		}
		if raw := blue["postPromotionAnalysis"]; raw != nil {
			if err := rolloutAnalysisReference(raw, objects, used); err != nil {
				return nil, err
			}
		}
	} else if raw, ok := strategy["canary"]; ok {
		canary, _ := raw.(map[string]any)
		if err := rolloutFields(canary, "stableService", "canaryService", "trafficRouting", "steps", "maxSurge", "maxUnavailable", "scaleDownDelaySeconds", "abortScaleDownDelaySeconds"); err != nil {
			return nil, err
		}
		serviceNames = []string{argoString(root, "spec", "strategy", "canary", "stableService"), argoString(root, "spec", "strategy", "canary", "canaryService")}
		routing, _, _ := unstructured.NestedMap(root.Object, "spec", "strategy", "canary", "trafficRouting")
		traefik, _ := routing["traefik"].(map[string]any)
		weighted, _ := traefik["weightedTraefikServiceName"].(string)
		if len(routing) != 1 || len(traefik) != 1 || weighted == "" {
			return nil, fmt.Errorf("canary requires a frozen Traefik weighted service")
		}
		if err := validateRolloutTraffic(objects, used, serviceNames, weighted); err != nil {
			return nil, err
		}
		steps, _ := canary["steps"].([]any)
		weight, analyses := int64(-1), 0
		if len(steps) == 0 || len(steps) > 30 {
			return nil, fmt.Errorf("canary requires 1 to 30 explicit steps")
		}
		for _, raw := range steps {
			step, _ := raw.(map[string]any)
			if len(step) != 1 {
				return nil, fmt.Errorf("canary step requires one action")
			}
			switch {
			case step["setWeight"] != nil:
				next, ok := step["setWeight"].(int64)
				if !ok || next <= weight || next < 0 || next > 100 {
					return nil, fmt.Errorf("canary weights must increase from 0 to 100")
				}
				weight = next
			case step["pause"] != nil:
				pause, ok := step["pause"].(map[string]any)
				if !ok || rolloutFields(pause, "duration") != nil {
					return nil, fmt.Errorf("invalid canary pause")
				}
				if value := pause["duration"]; value != nil {
					if !validRolloutDuration(value) {
						return nil, fmt.Errorf("pause duration must be positive and at most 24 hours")
					}
				}
			case step["analysis"] != nil:
				if err := rolloutAnalysisReference(step["analysis"], objects, used); err != nil {
					return nil, err
				}
				analyses++
			default:
				return nil, fmt.Errorf("unsupported canary step")
			}
		}
		if weight != 100 || analyses == 0 {
			return nil, fmt.Errorf("canary requires an analysis window and a final 100 percent step")
		}
	} else {
		return nil, fmt.Errorf("unsupported rollout strategy")
	}
	if serviceNames[0] == "" || serviceNames[0] == serviceNames[1] {
		return nil, fmt.Errorf("rollout requires two distinct frozen Services")
	}
	for _, name := range serviceNames {
		service := objects["Service/"+name]
		if service == nil {
			return nil, fmt.Errorf("rollout Service %q is not frozen", name)
		}
		actual, _, _ := unstructured.NestedStringMap(service.Object, "spec", "selector")
		if !reflect.DeepEqual(selector, actual) || argoString(service, "spec", "type") != "" && argoString(service, "spec", "type") != "ClusterIP" || argoString(service, "spec", "externalName") != "" {
			return nil, fmt.Errorf("rollout Services require the fixed Pod selector and ClusterIP routing")
		}
		ports, _, _ := unstructured.NestedSlice(service.Object, "spec", "ports")
		if len(ports) == 0 {
			return nil, fmt.Errorf("rollout Service ports are required")
		}
		for _, raw := range ports {
			port, ok := raw.(map[string]any)
			if !ok || len(port) == 0 {
				return nil, fmt.Errorf("rollout Service port must be an object")
			}
		}
		used["Service/"+name] = true
	}
	for key, object := range objects {
		if !used[key] {
			return nil, fmt.Errorf("unreferenced rollout dependency %s", key)
		}
		if object.GetKind() == "AnalysisTemplate" {
			if err := validateRolloutMetrics(object, serviceNames); err != nil {
				return nil, err
			}
		}
	}
	return root, nil
}

func rolloutFields(object map[string]any, allowed ...string) error {
	for key := range object {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("unsupported rollout field %q", key)
		}
	}
	return nil
}

func validRolloutDuration(value any) bool {
	var duration time.Duration
	switch value := value.(type) {
	case int64:
		if value > 0 && value <= 86400 {
			return true
		}
	case string:
		duration, _ = time.ParseDuration(value)
	}
	return duration > 0 && duration <= 24*time.Hour
}

func rolloutAnalysisReference(raw any, objects map[string]*unstructured.Unstructured, used map[string]bool) error {
	analysis, _ := raw.(map[string]any)
	if len(analysis) != 1 {
		return fmt.Errorf("rollout analysis requires only frozen namespaced templates")
	}
	templates, _ := analysis["templates"].([]any)
	if len(templates) == 0 {
		return fmt.Errorf("rollout analysis template is required")
	}
	for _, raw := range templates {
		template, _ := raw.(map[string]any)
		name, _ := template["templateName"].(string)
		if len(template) != 1 || objects["AnalysisTemplate/"+name] == nil {
			return fmt.Errorf("analysis must reference a frozen namespaced AnalysisTemplate")
		}
		used["AnalysisTemplate/"+name] = true
	}
	return nil
}

func validateRolloutMetrics(object *unstructured.Unstructured, services []string) error {
	spec, _, _ := unstructured.NestedMap(object.Object, "spec")
	if len(spec) != 1 {
		return fmt.Errorf("AnalysisTemplate supports only frozen web metrics")
	}
	metrics, _ := spec["metrics"].([]any)
	if len(metrics) == 0 || len(metrics) > 10 {
		return fmt.Errorf("analysis requires 1 to 10 metrics")
	}
	names := map[string]bool{}
	for _, raw := range metrics {
		metric, _ := raw.(map[string]any)
		if err := rolloutFields(metric, "name", "interval", "count", "successCondition", "failureCondition", "failureLimit", "inconclusiveLimit", "consecutiveErrorLimit", "provider"); err != nil {
			return err
		}
		name, _ := metric["name"].(string)
		count, _ := metric["count"].(int64)
		success, _ := metric["successCondition"].(string)
		interval, _ := time.ParseDuration(fmt.Sprint(metric["interval"]))
		if name == "" || names[name] || count < 2 || count > 1000 || interval <= 0 || interval > 24*time.Hour/time.Duration(count) || success == "" {
			return fmt.Errorf("each metric requires a unique name, a finite observation window and a success condition")
		}
		names[name] = true
		provider, _ := metric["provider"].(map[string]any)
		web, _ := provider["web"].(map[string]any)
		if len(provider) != 1 || len(web) == 0 || rolloutFields(web, "url", "jsonPath", "timeoutSeconds") != nil {
			return fmt.Errorf("analysis supports read-only web metrics without embedded credentials")
		}
		endpoint, _ := web["url"].(string)
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("metric URL must reference a frozen Service without credentials")
		}
		valid := false
		for _, service := range services {
			prefix := service + "." + object.GetNamespace() + ".svc"
			valid = valid || parsed.Hostname() == prefix || parsed.Hostname() == prefix+".cluster.local"
		}
		if !valid {
			return fmt.Errorf("metric URL must stay within the frozen rollout Services")
		}
	}
	return nil
}

func validateRolloutTraffic(objects map[string]*unstructured.Unstructured, used map[string]bool, services []string, name string) error {
	if len(services) != 2 {
		return fmt.Errorf("canary requires stable and preview Services")
	}
	stableService, previewService := services[0], services[1]
	weighted := objects["TraefikService/"+name]
	if weighted == nil {
		return fmt.Errorf("weighted TraefikService is not frozen")
	}
	spec, _, _ := unstructured.NestedMap(weighted.Object, "spec")
	config, _ := spec["weighted"].(map[string]any)
	backends, _ := config["services"].([]any)
	if len(spec) != 1 || len(config) != 1 || len(backends) != 2 {
		return fmt.Errorf("canary requires two direct weighted Service backends")
	}
	seen := map[string]bool{}
	for _, raw := range backends {
		backend, _ := raw.(map[string]any)
		if err := rolloutFields(backend, "name", "port", "weight", "kind"); err != nil {
			return err
		}
		name, _ := backend["name"].(string)
		weight, _ := backend["weight"].(int64)
		port, _ := backend["port"].(int64)
		if !slices.Contains(services, name) || seen[name] || port < 1 || port > 65535 || name == stableService && weight != 100 || name == previewService && weight != 0 || backend["kind"] != nil && backend["kind"] != "Service" {
			return fmt.Errorf("weighted backends must start at stable 100 / canary 0")
		}
		seen[name] = true
		service := objects["Service/"+name]
		if service == nil {
			return fmt.Errorf("weighted backend Service is not frozen")
		}
		ports, _, _ := unstructured.NestedSlice(service.Object, "spec", "ports")
		matched := false
		for _, raw := range ports {
			item, _ := raw.(map[string]any)
			matched = matched || item["port"] == port && (item["protocol"] == nil || item["protocol"] == "TCP")
		}
		if !matched {
			return fmt.Errorf("weighted backend port does not match its frozen Service")
		}
	}
	used["TraefikService/"+name] = true
	routeCount := 0
	for key, object := range objects {
		if object.GetKind() != "IngressRoute" {
			continue
		}
		spec, _, _ := unstructured.NestedMap(object.Object, "spec")
		if err := rolloutFields(spec, "entryPoints", "routes"); err != nil {
			return err
		}
		routes, _ := spec["routes"].([]any)
		if len(routes) != 1 {
			return fmt.Errorf("canary requires one explicit HTTP route")
		}
		route, _ := routes[0].(map[string]any)
		if err := rolloutFields(route, "kind", "match", "services"); err != nil {
			return err
		}
		backends, _ := route["services"].([]any)
		if route["kind"] != "Rule" || len(backends) != 1 {
			return fmt.Errorf("canary route must use its frozen weighted service")
		}
		backend, _ := backends[0].(map[string]any)
		if len(backend) != 2 || backend["name"] != name || backend["kind"] != "TraefikService" {
			return fmt.Errorf("canary route must use its frozen weighted service")
		}
		used[key], routeCount = true, routeCount+1
	}
	if routeCount != 1 {
		return fmt.Errorf("canary requires exactly one frozen IngressRoute")
	}
	return nil
}
