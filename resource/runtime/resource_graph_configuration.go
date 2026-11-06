package runtime

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// Scope and declared references have separate tokens from ownership and traffic.
const (
	ResourceGraphRelationAppliesIn         = "applies-in"
	ResourceGraphRelationUsesPriorityClass = "uses-priority-class"
	ResourceGraphRelationUsesRuntimeClass  = "uses-runtime-class"
	ResourceGraphRelationWebhookService    = "webhook-service"
)

func graphClusterConfigurationKind(kind string) bool {
	switch kind {
	case "PriorityClass", "RuntimeClass", "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration":
		return true
	}
	return false
}

func (b *graphRules) configurationRelations(o *unstructured.Unstructured, id string) {
	if !graphBuiltinSpec(o) {
		return
	}
	switch o.GetKind() {
	case "ResourceQuota", "LimitRange":
		// Namespace scope is a declaration; selectors/limits do not prove ownership.
		if o.GetNamespace() != "" {
			b.edge(id, b.target("v1", "Namespace", "", o.GetNamespace(), ""), ResourceGraphRelationAppliesIn)
		}
	case "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration":
		for _, target := range graphWebhookServices(o, b.active) {
			b.edge(id, b.target("v1", "Service", target.namespace, target.name, ""), ResourceGraphRelationWebhookService)
		}
	}
	// Lease holderIdentity is opaque. Only metadata ownerReferences add edges.
}

type graphNamespacedName struct{ namespace, name string }

func graphWebhookServices(o *unstructured.Unstructured, active func() bool) []graphNamespacedName {
	if !graphBuiltinSpec(o) || (o.GetKind() != "MutatingWebhookConfiguration" && o.GetKind() != "ValidatingWebhookConfiguration") {
		return nil
	}
	result := []graphNamespacedName{}
	for _, raw := range graphSlice(o.Object, "webhooks") {
		if !active() {
			break
		}
		webhook, _ := raw.(map[string]interface{})
		config := graphMap(webhook, "clientConfig")
		service := graphMap(config, "service")
		namespace, name := graphString(service, "namespace"), graphString(service, "name")
		if graphString(config, "url") == "" && namespace != "" && name != "" {
			result = append(result, graphNamespacedName{namespace, name})
		}
	}
	return result
}
